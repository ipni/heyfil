package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/ipfs/go-cid"
	"github.com/klauspost/compress/zstd"
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
	filecoinToolsDealListPage struct {
		Deals []struct {
			DealID   int
			DealInfo StateMarketDeal
		}
	}
)

const (
	methodFilStateListMiners = `Filecoin.StateListMiners`
	methodFilStateMinerInfo  = `Filecoin.StateMinerInfo`
	methodFilChainHead       = `Filecoin.ChainHead`

	// noopURL is a placeholder for mandatory but noop URL required by HTTP client during go-stream
	// connection.
	noopURL = "http://publisher.invalid/head"
)

func (hf *heyFil) stateListMiners(ctx context.Context) ([]string, error) {
	resp, err := hf.c.Call(ctx, methodFilStateListMiners, nil)
	switch {
	case err != nil:
		return nil, err
	case resp.Error != nil:
		return nil, resp.Error
	default:
		var mids []string
		if err = resp.GetObject(&mids); err != nil {
			return nil, err
		}
		return mids, nil
	}
}

func (hf *heyFil) stateMinerInfo(ctx context.Context, mid string) (peer.AddrInfo, error) {
	var ai peer.AddrInfo
	resp, err := hf.c.Call(ctx, methodFilStateMinerInfo, mid, nil)
	switch {
	case err != nil:
		return ai, err
	case resp.Error != nil:
		return ai, resp.Error
	default:
		var mi StateMinerInfoResp
		if err = resp.GetObject(&mi); err != nil {
			return ai, err
		}
		if len(mi.PeerId) != 0 {
			var err error
			if ai.ID, err = peer.Decode(mi.PeerId); err != nil {
				return ai, err
			}
		}
		if len(mi.Multiaddrs) != 0 {
			ai.Addrs = make([]multiaddr.Multiaddr, 0, len(mi.Multiaddrs))
			for _, s := range mi.Multiaddrs {
				mb, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return ai, err
				}
				addr, err := multiaddr.NewMultiaddrBytes(mb)
				if err != nil {
					return ai, err
				}
				ai.Addrs = append(ai.Addrs, addr)
			}
		}
		return ai, nil
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

func (hf *heyFil) stateMarketDealsViaFilTools(ctx context.Context) (chan StateMarketDealResult, error) {

	baseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, hf.marketDealsFilTools, nil)
	if err != nil {
		return nil, err
	}
	results := make(chan StateMarketDealResult, 1)
	go func() {
		defer close(results)
		page := 1
		for {
			req := baseReq.Clone(ctx)
			q := req.URL.Query()
			// The endpoint https://filecoin.tools/api/deals/list has a hard upper limit of 20_000 for page number.
			// This is unfortunately true regardless of per_page parameter value. This means if per_page is small
			// enough then a client cannot get deals that would fall beyond page 20K. Further, the results returned do
			// not indicate the total number of deals available which makes it impossible to know if any deals were
			// missed out due to the hard limit to the page number.
			// To work around this unfortunate API design, use a large enough per_page value to have reasonable
			// confidence that we will get all the deals.
			q.Add("per_page", "3000")
			q.Add("page", strconv.Itoa(page))
			req.URL.RawQuery = q.Encode()

			resp, err := hf.httpClient.Do(req)
			if err != nil {
				results <- StateMarketDealResult{Err: err}
				return
			}
			if resp.StatusCode != http.StatusOK {
				results <- StateMarketDealResult{
					Err: fmt.Errorf("received unsuccessful response from FileCoin Tools while lsiting deals: %d", resp.StatusCode),
				}
				_ = resp.Body.Close()
				return
			}
			var p filecoinToolsDealListPage
			if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
				results <- StateMarketDealResult{Err: err}
				_ = resp.Body.Close()
				return
			}
			_ = resp.Body.Close()

			switch {
			case len(p.Deals) == 0:
				// No more deals left; we are done.
				return
			default:
				for _, deal := range p.Deals {
					results <- StateMarketDealResult{
						Key:  strconv.Itoa(deal.DealID),
						Deal: &deal.DealInfo,
					}
				}
				page++
			}
		}
	}()
	return results, nil
}

func (hf *heyFil) stateMarketDealsViaS3Snapshot(ctx context.Context) (chan StateMarketDealResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hf.marketDealsS3Snapshot, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hf.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("faild to get market deals: %d %s", resp.StatusCode, resp.Status)
	}

	zstr, err := zstd.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate ZST reader: %w", err)
	}

	results := make(chan StateMarketDealResult, 1)
	go func() {

		defer func() {
			close(results)
			zstr.Close()
			_ = resp.Body.Close()
		}()

		// Process deal information in streaming fashion for a smaller more predictable memory
		// footprint. Because, it is provided as a giant (currently ~5 GiB) JSON file off S3 usually
		// and we don't know how big it might grow.
		decoder := json.NewDecoder(zstr)
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
