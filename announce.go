package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/fxamacker/cbor/v2"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type announcement struct {
	Cid   cid.Cid
	Addrs []multiaddr.Multiaddr
}

func (a announcement) MarshalCBOR() ([]byte, error) {
	msg := make([]any, 0, 3)
	msg = append(msg, cbor.Tag{
		Number:  42,
		Content: append([]byte{0}, a.Cid.Bytes()...),
	})
	baddrs := make([][]byte, 0, len(a.Addrs))
	for _, addr := range a.Addrs {
		baddrs = append(baddrs, addr.Bytes())
	}
	msg = append(msg, baddrs)
	msg = append(msg, []byte{0})
	return cbor.Marshal(msg)
}

func (hf *heyFil) announce(ctx context.Context, ai *peer.AddrInfo, head cid.Cid) error {
	maddrs, err := peer.AddrInfoToP2pAddrs(ai)
	if err != nil {
		return err
	}
	anncb, err := cbor.Marshal(announcement{
		Cid:   head,
		Addrs: maddrs,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, hf.httpAnnounceEndpoint, bytes.NewBuffer(anncb))
	if err != nil {
		return err
	}
	resp, err := hf.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respb, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode > 299 {
		logger.Debugw("unsuccessful HTTP reponse while announcing", "status", resp.Close, "body", string(respb))
		return fmt.Errorf("unsuccessful announce: %d", resp.StatusCode)
	}
	logger.Info("successfully announced")
	return nil
}
