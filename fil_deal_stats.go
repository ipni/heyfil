package main

import (
	"context"
	"sync"
)

const (
	// filEpochSeconds is the length of one FIL epoch in seconds.
	// See: https://docs.filecoin.io/reference/general/glossary/#epoch
	filEpochSeconds = 30
	// filEpochDay is the number of FIL epochs in one day.
	filEpochDay = 24 * 60 * 60 / filEpochSeconds
	// filEpochWeek is the number of FIL epochs in one week.
	filEpochWeek = 7 * filEpochDay
)

type (
	dealStats struct {
		hf                     *heyFil
		refreshLock            sync.RWMutex
		dealCountByParticipant map[string]dealCounts
		latestRefreshErr       error
	}
	dealCounts struct {
		count           int64
		bytes           int64
		countWithinDay  int64
		bytesWithinDay  int64
		countWithinWeek int64
		bytesWithinWeek int64
	}
)

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
	currentEpoch := ch.Height
	logger.Infow("fetched fil chain height", "height", currentEpoch)

	var deals chan StateMarketDealResult
	if ds.hf.marketDealsFilToolsEnabled {
		deals, err = ds.hf.stateMarketDealsViaFilTools(ctx)
	} else {
		deals, err = ds.hf.stateMarketDealsViaS3Snapshot(ctx)
	}
	if err != nil {
		return err
	}

	dealCountByParticipant := make(map[string]dealCounts)
	var totalDealCount, totalDealCountWithinWeek, totalDealCountWithinDay int64
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
				ds.hf.metrics.notifyDealCountWithinDay(totalDealCountWithinDay)
				ds.hf.metrics.notifyDealCountWithinWeek(totalDealCountWithinWeek)
				logger.Infow("fetched state market deals", "count", totalDealCount, "countWithinDay", totalDealCountWithinDay, "countWithinWeek", totalDealCountWithinWeek)
				return nil
			}
			switch {
			case dr.Err != nil:
				return dr.Err
			case dr.Deal.State.SectorStartEpoch == -1:
			case dr.Deal.State.SlashEpoch != -1:
			case dr.Deal.Proposal.EndEpoch < currentEpoch:
			default:
				totalDealCount++
				provider := dr.Deal.Proposal.Provider
				providerDealCount := dealCountByParticipant[provider]
				providerDealCount.count++
				providerDealCount.bytes += dr.Deal.Proposal.PieceSize
				elapsedSinceStartEpoch := currentEpoch - dr.Deal.Proposal.StartEpoch
				if elapsedSinceStartEpoch < filEpochDay {
					totalDealCountWithinDay++
					providerDealCount.countWithinDay++
					providerDealCount.bytesWithinDay += dr.Deal.Proposal.PieceSize
				}
				if elapsedSinceStartEpoch < filEpochWeek {
					totalDealCountWithinWeek++
					providerDealCount.countWithinWeek++
					providerDealCount.bytesWithinWeek += dr.Deal.Proposal.PieceSize
				}
				dealCountByParticipant[provider] = providerDealCount
			}
		}
	}
}

func (ds *dealStats) getDealCounts(id string) dealCounts {
	ds.refreshLock.RLock()
	defer ds.refreshLock.RUnlock()
	return ds.dealCountByParticipant[id]
}
