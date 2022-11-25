package main

import (
	"context"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/instrument/asyncfloat64"
	"go.opentelemetry.io/otel/metric/instrument/asyncint64"
	"go.opentelemetry.io/otel/metric/unit"
	"go.opentelemetry.io/otel/sdk/metric"
)

type metrics struct {
	exporter *prometheus.Exporter

	participantsByStatus asyncint64.Gauge
	participantsTotal    asyncint64.Gauge
	participantsCoverage asyncfloat64.Gauge
	dealsTotal           asyncint64.Gauge
	dealsCoverage        asyncfloat64.Gauge

	snapshotLock   sync.RWMutex
	countsByStatus map[Status]int64

	totalParticipantCount atomic.Value
	totalDealCount        atomic.Value

	reachableWithNonZeroDeals      int
	knownByIndexer                 int
	dealCountDiscoverableByIndexer int64
}

func (m *metrics) start() error {
	m.totalParticipantCount.Store(int64(0))
	m.totalDealCount.Store(int64(0))

	var err error
	if m.exporter, err = prometheus.New(prometheus.WithoutUnits()); err != nil {
		return err
	}
	provider := metric.NewMeterProvider(metric.WithReader(m.exporter))
	meter := provider.Meter("ipni/heyfil")

	if m.participantsByStatus, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/miners_by_status",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The state markets participants count by status."),
	); err != nil {
		return err
	}

	if m.participantsTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_participants_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total number of state market participants returned by the FileCoin API."),
	); err != nil {
		return err
	}

	if m.dealsTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_deals_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total number of state market deals discovered from FileCoin API."),
	); err != nil {
		return err
	}

	if m.dealsCoverage, err = meter.AsyncFloat64().Gauge(
		"ipni/heyfil/state_market_deal_coverage_ratio",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The ratio of state market deals covered by the indexer."),
	); err != nil {
		return err
	}

	if m.participantsCoverage, err = meter.AsyncFloat64().Gauge(
		"ipni/heyfil/state_market_participant_coverage_ratio",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The ratio of state market participants covered by the indexer."),
	); err != nil {
		return err
	}

	return meter.RegisterCallback(
		[]instrument.Asynchronous{
			m.participantsByStatus,
			m.participantsTotal,
			m.dealsTotal,
			m.dealsCoverage,
			m.participantsCoverage,
		},
		func(ctx context.Context) {
			m.participantsTotal.Observe(ctx, m.totalParticipantCount.Load().(int64))
			m.dealsTotal.Observe(ctx, m.totalDealCount.Load().(int64))
			m.reportFromSnapshot(ctx)
		},
	)
}

func (m *metrics) reportFromSnapshot(ctx context.Context) {
	m.snapshotLock.RLock()
	defer m.snapshotLock.RUnlock()

	for status, c := range m.countsByStatus {
		m.participantsByStatus.Observe(ctx, c, targetStatusToAttribute(status))
	}

	var dc float64
	totalDealCount := m.totalDealCount.Load().(int64)
	if totalDealCount > 0 {
		dc = float64(m.dealCountDiscoverableByIndexer) / float64(totalDealCount)
	}
	m.dealsCoverage.Observe(ctx, dc)

	var pc float64
	if m.reachableWithNonZeroDeals > 0 {
		pc = float64(m.knownByIndexer) / float64(m.reachableWithNonZeroDeals)
	}
	m.participantsCoverage.Observe(ctx, pc)
}

func (m *metrics) snapshot(targets map[string]*Target) {
	m.snapshotLock.Lock()
	defer m.snapshotLock.Unlock()

	m.countsByStatus = make(map[Status]int64)
	m.reachableWithNonZeroDeals = 0
	m.knownByIndexer = 0
	m.dealCountDiscoverableByIndexer = 0
	for _, t := range targets {
		m.countsByStatus[t.Status] = m.countsByStatus[t.Status] + 1
		if t.AddrInfo != nil {
			if t.DealCount > 0 {
				if t.KnownByIndexer {
					m.knownByIndexer++
					m.dealCountDiscoverableByIndexer += t.DealCount
				}
				m.reachableWithNonZeroDeals++
			}
		}
	}
}

func (m *metrics) notifyParticipantCount(c int64) {
	m.totalParticipantCount.Store(c)
}

func (m *metrics) notifyDealCount(c int64) {
	m.totalDealCount.Store(c)
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
