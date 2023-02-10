package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/ipfs/go-cid"
	gostream "github.com/libp2p/go-libp2p-gostream"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

type (
	StateMinerInfoResp struct {
		PeerId     string   `json:"PeerId"`
		Multiaddrs []string `json:"Multiaddrs"`
	}
	ChainHead struct {
		Height int64 `json:"Height"`
	}
	StateMarketDealResult struct {
		Key  string
		Deal *StateMarketDeal
		Err  error
	}
	StateMarketDeal struct {
		Proposal struct {
			Provider string `json:"Provider"`
			EndEpoch int64  `json:"EndEpoch"`
		} `json:"Proposal"`
		State struct {
			SectorStartEpoch int64 `json:"SectorStartEpoch"`
			SlashEpoch       int64 `json:"SlashEpoch"`
		} `json:"State"`
	}
)

const (
	methodFilStateMarketParticipants = `Filecoin.StateMarketParticipants`
	methodFilStateMinerInfo          = `Filecoin.StateMinerInfo`
	methodFilChainHead               = `Filecoin.ChainHead`

	// noopURL is a placeholder for mandatory but noop URL required by HTTP client during go-stream
	// connection.
	noopURL = "http://publisher.invalid/head"
)

func (hf *heyFil) stateMarketParticipants(ctx context.Context) ([]string, error) {
	resp, err := hf.c.Call(ctx, methodFilStateMarketParticipants, nil)
	switch {
	case err != nil:
		return nil, err
	case resp.Error != nil:
		return nil, resp.Error
	default:
		smp := make(map[string]any)
		if err = resp.GetObject(&smp); err != nil {
			return nil, err
		}
		mids := make([]string, 0, len(smp))
		for mid := range smp {
			mids = append(mids, mid)
		}
		return mids, nil
	}
}

func (hf *heyFil) stateMinerInfo(ctx context.Context, mid string) (*peer.AddrInfo, error) {
	resp, err := hf.c.Call(ctx, methodFilStateMinerInfo, mid, nil)
	switch {
	case err != nil:
		return nil, err
	case resp.Error != nil:
		return nil, resp.Error
	default:
		var mi StateMinerInfoResp
		if err = resp.GetObject(&mi); err != nil {
			return nil, err
		}
		if len(mi.Multiaddrs) == 0 || len(mi.PeerId) == 0 {
			// Don't bother decoding anything if the resulting addrinfo would not be contactable.
			return nil, nil
		}
		pid, err := peer.Decode(mi.PeerId)
		if err != nil {
			return nil, err
		}
		adds := make([]multiaddr.Multiaddr, 0, len(mi.Multiaddrs))
		for _, s := range mi.Multiaddrs {
			mb, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, err
			}
			addr, err := multiaddr.NewMultiaddrBytes(mb)
			if err != nil {
				return nil, err
			}
			adds = append(adds, addr)
		}
		return &peer.AddrInfo{
			ID:    pid,
			Addrs: adds,
		}, nil
	}
}

func (hf *heyFil) chainHead(ctx context.Context) (*ChainHead, error) {
	resp, err := hf.c.Call(ctx, methodFilChainHead)
	switch {
	case err != nil:
		return nil, err
	case resp.Error != nil:
		return nil, resp.Error
	default:
		var c ChainHead
		if err := resp.GetObject(&c); err != nil {
			return nil, err
		}
		return &c, nil
	}
}

func (hf *heyFil) stateMarketDeals(ctx context.Context) (chan StateMarketDealResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hf.marketDealsAlt, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hf.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("faild to get market deals: %d %s", resp.StatusCode, resp.Status)
	}
	results := make(chan StateMarketDealResult, 1)
	go func() {

		defer func() {
			close(results)
			_ = resp.Body.Close()
		}()

		// Process deal information in streaming fashion for a smaller more predictable memory
		// footprint. Because, it is provided as a giant (currently ~5 GiB) JSON file off S3 usually
		// and we don't know how big it might grow.
		decoder := json.NewDecoder(resp.Body)
		dealIDParser := func() (string, error) {
			token, err := decoder.Token()
			if err != nil {
				return "", err
			}
			switch tt := token.(type) {
			case string:
				return tt, nil
			case json.Delim:
				if tt.String() != `}` {
					return "", fmt.Errorf("expected delimier close object but got: %s", tt)
				}
				return "", nil
			default:
				return "", fmt.Errorf("expected string but got: %T", token)
			}
		}

		dealParser := func() (*StateMarketDeal, error) {
			var deal StateMarketDeal
			err := decoder.Decode(&deal)
			if err != nil {
				return nil, err
			}
			return &deal, nil
		}

		token, err := decoder.Token()
		if err != nil {
			results <- StateMarketDealResult{Err: err}
			return
		}
		if _, ok := token.(json.Delim); !ok {
			results <- StateMarketDealResult{Err: fmt.Errorf("unexpected JSON token: expected delimiter but got: %v", token)}
			return
		}
		for {
			var next StateMarketDealResult
			next.Key, next.Err = dealIDParser()
			if next.Err != nil {
				results <- next
				return
			}
			if next.Key == "" {
				// We should be at the end; assert so.
				if t, err := decoder.Token(); err != io.EOF {
					results <- StateMarketDealResult{Err: fmt.Errorf("expected no more tokens but got: %v", t)}
				}
				return
			}
			next.Deal, next.Err = dealParser()
			if next.Err != nil {
				results <- next
				return
			}
			results <- next
		}
	}()
	return results, nil
}

func (hf *heyFil) supportsHeadProtocol(ctx context.Context, ai *peer.AddrInfo) (bool, string, protocol.ID, error) {
	if err := hf.h.Connect(ctx, *ai); err != nil {
		return false, "", "", err
	}
	pids, err := hf.h.Peerstore().GetProtocols(ai.ID)
	if err != nil {
		return false, "", "", err
	}
	for _, pid := range pids {
		ok, topic := hf.findHeadProtocolMatches(string(pid))
		if ok {
			return true, topic, pid, nil
		}
	}
	return false, "", "", nil
}

func (hf *heyFil) findHeadProtocolMatches(pid string) (bool, string) {
	matches := hf.headProtocolPattern.FindStringSubmatch(pid)
	if len(matches) > 1 {
		return true, matches[1]
	}
	return false, ""
}

func (hf *heyFil) getHead(ctx context.Context, ai *peer.AddrInfo, pid protocol.ID) (cid.Cid, error) {
	client := http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if len(hf.h.Peerstore().Addrs(ai.ID)) == 0 {
					hf.h.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.TempAddrTTL)
				}
				return gostream.Dial(ctx, hf.h, ai.ID, pid)
			},
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, noopURL, nil)
	if err != nil {
		return cid.Undef, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return cid.Undef, err
	}
	defer resp.Body.Close()
	c, err := io.ReadAll(resp.Body)
	switch {
	case err != nil:
		return cid.Undef, err
	case len(c) == 0:
		return cid.Undef, nil
	default:
		return cid.Decode(string(c))
	}
}
