package main

import (
	"context"
	"sync"
)

type dealStats struct {
	hf                     *heyFil
	refreshLock            sync.RWMutex
	dealCountByParticipant map[string]int64
	latestRefreshErr       error
}

func (ds *dealStats) start(ctx context.Context) {
	go func() {
		for {
			ds.latestRefreshErr = ds.refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ds.hf.dealStatsRefreshInterval.C:
				ds.latestRefreshErr = ds.refresh(ctx)
			}
		}
	}()
}

func (ds *dealStats) refresh(ctx context.Context) error {
	ch, err := ds.hf.chainHead(ctx)
	if err != nil {
		return err
	}
	epoch := ch.Height

	deals, err := ds.hf.stateMarketDeals(ctx)
	if err != nil {
		return err
	}

	dealCountByParticipant := make(map[string]int64)
	var totalDealCount int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case dr, ok := <-deals:
			if !ok {
				ds.refreshLock.Lock()
				ds.dealCountByParticipant = dealCountByParticipant
				ds.refreshLock.Unlock()
				ds.hf.metrics.notifyDealCount(totalDealCount)
				logger.Infow("fetched state market deals", "count", totalDealCount)
				return nil
			}
			switch {
			case dr.Err != nil:
				return dr.Err
			case dr.Deal.State.SectorStartEpoch == -1:
			case dr.Deal.State.SlashEpoch != -1:
			case dr.Deal.Proposal.EndEpoch < epoch:
			default:
				totalDealCount++
				dealProvider := dr.Deal.Proposal.Provider
				dealCountByParticipant[dealProvider] = dealCountByParticipant[dealProvider] + 1
			}
		}
	}
}

func (ds *dealStats) getDealCount(id string) int64 {
	ds.refreshLock.RLock()
	defer ds.refreshLock.RUnlock()
	return ds.dealCountByParticipant[id]
}
