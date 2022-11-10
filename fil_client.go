package main

import (
	"context"
	"encoding/base64"
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
		PeerId     string
		Multiaddrs []string
	}
)

const (
	methodFilStateMarketParticipants = "Filecoin.StateMarketParticipants"
	methodFilStateMinerInfo          = "Filecoin.StateMinerInfo"

	// noopURL is a placeholder for mandatory but noop URL reqired by HTTP client during go-stream
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

func (hf *heyFil) supportsHeadProtocol(ctx context.Context, ai *peer.AddrInfo) (bool, string, protocol.ID, error) {
	if err := hf.h.Connect(ctx, *ai); err != nil {
		return false, "", "", err
	}
	pids, err := hf.h.Peerstore().GetProtocols(ai.ID)
	if err != nil {
		return false, "", "", err
	}
	for _, pid := range pids {
		ok, topic := hf.findHeadProtocolMatches(pid)
		if ok {
			return true, topic, protocol.ID(pid), nil
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
