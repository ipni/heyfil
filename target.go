package main

import (
	"context"
	"regexp"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/ybbus/jsonrpc/v3"
)

var agentShortVersionPattern = regexp.MustCompile(`^(.+)\+.*`)

type (
	Status int
	Target struct {
		hf                *heyFil                  `json:"-"`
		ID                string                   `json:"id,omitempty"`
		Status            Status                   `json:"status,omitempty"`
		AddrInfo          *peer.AddrInfo           `json:"addr_info,omitempty"`
		LastChecked       time.Time                `json:"last_checked,omitempty"`
		ErrMessage        string                   `json:"err,omitempty"`
		Err               error                    `json:"-"`
		Topic             string                   `json:"topic,omitempty"`
		HeadProtocol      protocol.ID              `json:"head_protocol,omitempty"`
		Head              cid.Cid                  `json:"head,omitempty"`
		KnownByIndexer    bool                     `json:"known_by_indexer,omitempty"`
		Protocols         []protocol.ID            `json:"protocols,omitempty"`
		AgentVersion      string                   `json:"agent_version,omitempty"`
		AgentShortVersion string                   `json:"agent_short_version,omitempty"`
		Transports        *TransportsQueryResponse `json:"transports,omitempty"`
		StateMinerPower   *StateMinerPowerResp     `json:"state_miner_power,omitempty"`

		DealCount           int64 `json:"deal_count,omitempty"`
		DealCountWithinDay  int64 `json:"deal_count_within_day,omitempty"`
		DealCountWithinWeek int64 `json:"deal_count_within_week,omitempty"`
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

	defer func() {
		t.LastChecked = time.Now()
		if t.Err != nil {
			t.ErrMessage = t.Err.Error()
		}
	}()
	logger := logger.With("miner", t.ID)

	counts := t.hf.dealStats.getDealCounts(t.ID)
	t.DealCount = counts.count
	t.DealCountWithinDay = counts.countWithinDay
	t.DealCountWithinWeek = counts.countWithinWeek

	// Get state miner power
	t.StateMinerPower, t.Err = t.hf.stateMinerPower(ctx, t.ID)
	if t.Err != nil {
		logger.Warnw("Failed to get state miner power", "err", t.Err)
		// Reset target error and proceed. Because, there are other checks to do and we
		// care less about recording this failure relative to other ones.
		t.Err = nil
	}

	// Get address for miner ID from FileCoin API.
	t.AddrInfo, t.Err = t.hf.stateMinerInfo(ctx, t.ID)
	switch e := t.Err.(type) {
	case nil:
		switch {
		case t.AddrInfo == nil:
			t.Status = StatusNoAddrInfo
			return t
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
			logger = logger.With("peerID", t.AddrInfo.ID)
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

	if t.Err = t.hf.h.Connect(ctx, *t.AddrInfo); t.Err != nil {
		t.Status = StatusUnreachable
		return t
	}
	if t.Protocols, t.Err = t.hf.h.Peerstore().GetProtocols(t.AddrInfo.ID); t.Err != nil {
		t.Status = StatusUnreachable
		return t
	}

	// Get the remote peer agent version, but proceed with other checks if we fail to get it.
	var ok bool
	if anyAgentVersion, err := t.hf.h.Peerstore().Get(t.AddrInfo.ID, "AgentVersion"); err != nil {
		logger.Warnw("Failed to get agent version", "err", err)
	} else if t.AgentVersion, ok = anyAgentVersion.(string); ok {
		t.AgentShortVersion = agentShortVersionPattern.ReplaceAllString(t.AgentVersion, "$1")
	} else if !ok {
		logger.Warnw("Non-string agent version", "agentVersion", anyAgentVersion)
	}

	if t.supportsProtocolID(transportsProtocolID) {
		// Do not populate t.Err with the error that may be returned by queryTransports.
		// Instead, silently proceed to IPNI related head checking, etc.
		tctx, cancel := context.WithTimeout(ctx, t.hf.queryTransportsTimeout)
		defer cancel()
		if t.Transports, err = t.hf.queryTransports(tctx, t.AddrInfo.ID); err != nil {
			logger.Warnw("Failed to query transports", "err", err)
		}
	}

	// Check if the target is an index provider on the expected topic.
	var supportsHeadProtocol bool
	for _, pid := range t.Protocols {
		if supportsHeadProtocol, t.Topic = t.hf.findHeadProtocolMatches(string(pid)); supportsHeadProtocol {
			t.HeadProtocol = pid
			break
		}
	}
	if !supportsHeadProtocol {
		t.Status = StatusUnindexed
		return t
	}
	if t.Topic != t.hf.topic {
		t.Status = StatusTopicMismatch
		return t
	}

	// Check if there is a non-empty head CID and if so announce it.
	switch t.Head, t.Err = t.hf.getHead(ctx, t.AddrInfo, t.HeadProtocol); {
	case t.Err != nil:
		t.Status = StatusGetHeadError
	case cid.Undef.Equals(t.Head):
		t.Status = StatusEmptyHead
	default:
		if t.Err = t.hf.announce(ctx, t.AddrInfo, t.Head); t.Err != nil {
			t.Status = StatusAnnounceError
		} else {
			t.Status = StatusOK
		}
	}
	return t
}

func (t *Target) hasPeerID(pid peer.ID) bool {
	switch {
	case t.AddrInfo != nil && t.AddrInfo.ID == pid:
		return true
	case t.Transports != nil:
		for _, tp := range t.Transports.Protocols {
			for _, ma := range tp.Addresses {
				if addr, err := peer.AddrInfoFromP2pAddr(ma); err != nil {
					continue
				} else if addr.ID == pid {
					return true
				}
			}
		}
	}
	return false
}

func (t *Target) supportsProtocolID(pid protocol.ID) bool {
	for _, id := range t.Protocols {
		if id == pid {
			return true
		}
	}
	return false
}
