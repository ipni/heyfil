package main

import (
	"context"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/instrument/asyncint64"
	"go.opentelemetry.io/otel/metric/unit"
	"go.opentelemetry.io/otel/sdk/metric"
)

type metrics struct {
	exporter                *prometheus.Exporter
	minersByStatus          asyncint64.Gauge
	stateMarketParticipants asyncint64.Gauge

	countsByStatusLock sync.RWMutex
	countsByStatus     map[Status]int64
	totalTargetCount   atomic.Value
}

func (m *metrics) start() error {
	var err error
	if m.exporter, err = prometheus.New(); err != nil {
		return err
	}
	provider := metric.NewMeterProvider(metric.WithReader(m.exporter))
	meter := provider.Meter("ipni/heyfil")

	if m.minersByStatus, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/miners_by_status",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The state markets participants count by status."),
	); err != nil {
		return err
	}

	if m.stateMarketParticipants, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_participants",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The number of state market participants returned by the FileCoin API."),
	); err != nil {
		return err
	}

	return meter.RegisterCallback(
		[]instrument.Asynchronous{m.minersByStatus, m.stateMarketParticipants},
		func(ctx context.Context) {
			m.stateMarketParticipants.Observe(ctx, m.totalTargetCount.Load().(int64))
			m.reportCountsByStatus(ctx)
		},
	)
}

func (m *metrics) reportCountsByStatus(ctx context.Context) {
	m.countsByStatusLock.RLock()
	defer m.countsByStatusLock.RUnlock()
	for status, c := range m.countsByStatus {
		m.minersByStatus.Observe(ctx, c, targetStatusToAttribute(status))
	}
}

func (m *metrics) snapshotCountByStatus(targets map[string]*Target) {
	m.countsByStatusLock.Lock()
	defer m.countsByStatusLock.Unlock()
	m.countsByStatus = make(map[Status]int64)
	for _, res := range targets {
		c := m.countsByStatus[res.Status]
		c++
		m.countsByStatus[res.Status] = c
	}
}

func (m *metrics) notifyTargetCount(c int) {
	m.totalTargetCount.Store(int64(c))
}

func (m *metrics) shutdown(ctx context.Context) error {
	var err error
	if m.exporter != nil {
		err = m.exporter.Shutdown(ctx)
	}
	return err
}

// targetStatusToAttribute returns the status attribute corresponding to the given minerStatus.
func targetStatusToAttribute(s Status) attribute.KeyValue {
	const key = "status"
	var value string
	switch s {
	case StatusUnknown:
		value = "unknown"
	case StatusOK:
		value = "ok"
	case StatusAPICallFailed:
		value = "err-api-call"
	case StatusInternalError:
		value = "err-internal"
	case StatusUnknownRPCError:
		value = "err-rpc-unknown"
	case StatusNotMiner:
		value = "not-miner"
	case StatusUnreachable:
		value = "unreachable"
	case StatusUnaddressable:
		value = "unaddressable"
	case StatusUnindexed:
		value = "unindexed"
	case StatusTopicMismatch:
		value = "topic-mismatch"
	case StatusEmptyHead:
		value = "empty-head"
	case StatusGetHeadError:
		value = "err-get-head"
	case StatusAnnounceError:
		value = "err-announce"
	default:
		logger.Warnw("unrecognised miner status", "status", s)
		value = "unknown"
	}
	return attribute.String(key, value)
}
