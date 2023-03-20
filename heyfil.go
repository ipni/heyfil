package main

import (
	"context"
	"net/http"
	"time"

	"github.com/ipfs/go-log/v2"
	"github.com/ybbus/jsonrpc/v3"
)

var logger = log.Logger("heyfil")

type (
	heyFil struct {
		*options
		c       jsonrpc.RPCClient
		targets map[string]*Target

		toCheck chan *Target
		checked chan *Target

		metrics   metrics
		dealStats *dealStats
		server    http.Server
	}
)

func newHeyFil(o ...Option) (*heyFil, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}
	hf := &heyFil{
		options: opts,
		c:       jsonrpc.NewClient(opts.api),
		targets: make(map[string]*Target),
		toCheck: make(chan *Target, 100),
		checked: make(chan *Target, 100),
	}
	hf.dealStats = &dealStats{hf: hf}
	return hf, nil
}

func (hf *heyFil) Start(ctx context.Context) error {
	if err := hf.metrics.start(); err != nil {
		return err
	}
	if err := hf.startServer(); err != nil {
		return err
	}
	hf.dealStats.start(ctx)

	// start checkers.
	for i := 0; i < hf.maxConcurrentParticipantCheck; i++ {
		go hf.checker(ctx)
	}

	// Start check dispatcher.
	go func() {
		dispatch := func(ctx context.Context, t time.Time) {
			logger := logger.With("t", t)
			mids, err := hf.stateListMiners(ctx)
			if err != nil {
				logger.Errorw("failed to get state market participants", "err", err)
				return
			}
			hf.metrics.notifyParticipantCount(int64(len(mids)))
			logger.Infow("fetched state market participants", "count", len(mids))
			for _, mid := range mids {
				select {
				case <-ctx.Done():
					return
				case hf.toCheck <- hf.newTarget(mid):
				}
			}
		}
		dispatch(ctx, time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-hf.participantsCheckInterval.C:
				dispatch(ctx, t)
			}
		}
	}()

	// Start checked result handler
	go func() {
		snapshot := func(ctx context.Context, t time.Time) {
			logger := logger.With("t", t)
			hf.metrics.snapshot(hf.targets)
			logger.Debugw("reported check results", "miner-count", len(hf.targets))
		}
		snapshot(ctx, time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-hf.checked:
				hf.targets[r.ID] = r
			case t := <-hf.snapshotInterval.C:
				snapshot(ctx, t)
			}
		}
	}()
	logger.Info("heyfil started")
	return nil
}

func (hf *heyFil) newTarget(mid string) *Target {
	return &Target{ID: mid, hf: hf}
}

func (hf *heyFil) checker(ctx context.Context) {
	for target := range hf.toCheck {
		logger := logger.With("id", target.ID)
		logger.Debug("checking target")
		select {
		case <-ctx.Done():
			return
		case hf.checked <- target.check(ctx):
			logger.Debug("stored check result")
		}
	}
}
func (hf *heyFil) Shutdown(ctx context.Context) error {
	merr := hf.metrics.shutdown(ctx)
	serr := hf.shutdownServer(ctx)
	if merr != nil {
		return merr
	}
	return serr
}
