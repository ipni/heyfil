package main

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func TestName(t *testing.T) {

	fil, err := newHeyFil()
	if err != nil {
		t.Fail()
	}
	pid, err := peer.Decode("12D3KooWE8yt84RVwW3sFcd6WMjbUdWrZer2YtT4dmtj3dHdahSZ")
	if err != nil {
		t.Fail()
	}

	a, err := multiaddr.NewMultiaddr("/ip4/85.11.148.122/tcp/24001")
	if err != nil {
		t.Fail()
	}
	// c, err := cid.Decode("baguqeeralc4rk4iyoqolyimdjew5mv6pidypmvahvr4ifaag3htoqmpnmjeq")
	// if err != nil {
	// 	t.Fail()
	// }

	// err = fil.announce(c, &peer.AddrInfo{
	// 	ID:    pid,
	// 	Addrs: []multiaddr.Multiaddr{a},
	// })
	// if err != nil {
	// 	t.Fail()
	// }

	head, err := fil.getHead(context.Background(), &peer.AddrInfo{
		ID:    pid,
		Addrs: []multiaddr.Multiaddr{a},
	}, "/legs/head/indexer/ingest/mainnet/0.0.1")

	if err != nil {
		t.Fail()
	}
	t.Log(head.String())

}
