package main

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/ybbus/jsonrpc/v3"
)

type (
	Status int
	Target struct {
		hf             *heyFil
		ID             string
		Status         Status
		AddrInfo       peer.AddrInfo
		LastChecked    time.Time
		Err            error
		Topic          string
		HeadProtocol   protocol.ID
		Head           cid.Cid
		KnownByIndexer bool

		DealCount           int64
		DealCountWithinDay  int64
		DealCountWithinWeek int64
	}
)

const (
	StatusUnknown Status = iota
	StatusOK
	StatusAPICallFailed
	StatusInternalError
	StatusUnknownRPCError
	StatusNotMiner
	StatusUnreachable
	StatusUnaddressable
	StatusUnindexed
	StatusTopicMismatch
	StatusEmptyHead
	StatusGetHeadError
	StatusAnnounceError
	StatusUnidentifiable
	StatusNoAddrInfo
)

func (t *Target) check(ctx context.Context) *Target {
	defer func() { t.LastChecked = time.Now() }()
	logger := logger.With("miner", t.ID)

	counts := t.hf.dealStats.getDealCounts(t.ID)
	t.DealCount = counts.count
	t.DealCountWithinDay = counts.countWithinDay
	t.DealCountWithinWeek = counts.countWithinWeek

	// Get address for miner ID from FileCoin API.
	t.AddrInfo, t.Err = t.hf.stateMinerInfo(ctx, t.ID)
	switch e := t.Err.(type) {
	case nil:
		switch {
		case t.AddrInfo.ID == "" && len(t.AddrInfo.Addrs) == 0:
			t.Status = StatusNoAddrInfo
			return t
		case t.AddrInfo.ID == "":
			t.Status = StatusUnidentifiable
			return t
		case len(t.AddrInfo.Addrs) == 0:
			t.Status = StatusUnaddressable
			return t
		default:
			logger.Debugw("Discovered Addrs for miner", "addrs", t.AddrInfo)
		}
	case *jsonrpc.HTTPError:
		logger.Debugw("HTTP error while getting state miner info", "status", e.Code, "err", t.Err)
		t.Status = StatusAPICallFailed
		return t
	case *jsonrpc.RPCError:
		logger.Debugw("RPC API error while getting state miner info", "code", e.Code, "err", t.Err)
		switch e.Code {
		case 1:
			t.Status = StatusNotMiner
		default:
			logger.Warn("RPC API error code missing miner status mapping", "code", e.Code, "err", t.Err)
			t.Status = StatusUnknownRPCError
		}
		return t
	default:
		logger.Debugw("failed to get state miner info", "err", t.Err)
		t.Status = StatusInternalError
		return t
	}

	// Check if the target is known by the indexer now that it has a non-nil addrinfo.
	var err error
	t.KnownByIndexer, err = t.hf.isKnownByIndexer(ctx, t.AddrInfo.ID)
	if err != nil {
		logger.Errorw("failed to check if target is known by indexer", "err", err)
	}

	// Check if the target is an index provider on the expected topic.
	var ok bool
	ok, t.Topic, t.HeadProtocol, t.Err = t.hf.supportsHeadProtocol(ctx, &t.AddrInfo)
	switch {
	case t.Err != nil:
		t.Status = StatusUnreachable
		return t
	case !ok:
		t.Status = StatusUnindexed
		return t
	default:
		if t.Topic != t.hf.topic {
			t.Status = StatusTopicMismatch
			return t
		}
	}

	// Check if there is a non-empty head CID and if so announce it.
	t.Head, t.Err = t.hf.getHead(ctx, &t.AddrInfo, t.HeadProtocol)
	switch {
	case t.Err != nil:
		t.Status = StatusGetHeadError
	case cid.Undef.Equals(t.Head):
		t.Status = StatusEmptyHead
	default:
		if t.Err = t.hf.announce(ctx, &t.AddrInfo, t.Head); t.Err != nil {
			t.Status = StatusAnnounceError
		} else {
			t.Status = StatusOK
		}
	}
	return t
}
