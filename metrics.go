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

var (
	attributeWithAddress    = attribute.Bool("with-addr", true)
	attributeKnownByIndexer = attribute.Bool("known-by-indexer", true)
	attributeWithDeal       = attribute.Bool("with-deal", true)
)

type metrics struct {
	exporter *prometheus.Exporter

	participantsByStatus asyncint64.Gauge
	participantsTotal    asyncint64.Gauge
	participantsCoverage asyncfloat64.Gauge
	dealsTotal           asyncint64.Gauge
	dealsCoverage        asyncfloat64.Gauge

	countsByStatusLock sync.RWMutex
	countsByStatus     map[Status]int64

	// totalParticipantCount is the total number of state market participants retrieved from
	// FileCoin API stored as int64.
	totalParticipantCount atomic.Value
	// totalDealCount is the total number of state market deals retrieved from FileCoin API  stored
	// as int64.
	totalDealCount atomic.Value
	// totalAddressableParticipantsWithNonZeroDeals is the total number of state market participants
	// which: 1) had non-nil peer.AddrInfo on FileCoin API, and 2) have made at least one deal
	// stored as int64.
	totalAddressableParticipantsWithNonZeroDeals atomic.Value
	// totalAddressableParticipants s the total number of state market participants which had
	// non-nil peer.AddrInfo on FileCoin API.
	totalAddressableParticipants atomic.Value
	// totalParticipantsKnownByIndexer is the total number of state market participants that are
	// listed as a provider by network indexer stored as int64.
	totalParticipantsKnownByIndexer atomic.Value
	// totalDealCountByParticipantsKnownToIndexer is the total number of deals made by the state
	// market participants that are listed as a provider by network indexer stored as int64.
	totalDealCountByParticipantsKnownToIndexer atomic.Value
}

func (m *metrics) start() error {
	m.totalParticipantCount.Store(int64(0))
	m.totalDealCount.Store(int64(0))
	m.totalAddressableParticipantsWithNonZeroDeals.Store(int64(0))
	m.totalAddressableParticipants.Store(int64(0))
	m.totalParticipantsKnownByIndexer.Store(int64(0))
	m.totalDealCountByParticipantsKnownToIndexer.Store(int64(0))

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
		instrument.WithDescription("The total number of state market participants returned by the FileCoin API. "+
			"This gauge also offers the total numbers for participants with address, the ones known by the indexer, and "+
			"participants with address plus at least one deal. See `with-address`, `known-by-indexer`, and `with-deal` "+
			"attributes."),
	); err != nil {
		return err
	}

	if m.dealsTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_deals_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total number of state market deals discovered from FileCoin API. "+
			"This gauge also offers the total number of deals made by participants that are known to the indexer. "+
			"See `known-by-indexer`attribute."),
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
			m.participantsCoverage,
			m.dealsTotal,
			m.dealsCoverage,
		},
		m.reportAsyncMetrics,
	)
}

func (m *metrics) reportAsyncMetrics(ctx context.Context) {
	m.countsByStatusLock.RLock()
	for status, c := range m.countsByStatus {
		m.participantsByStatus.Observe(ctx, c, targetStatusToAttribute(status))
	}
	m.countsByStatusLock.RUnlock()

	totalParticipants := m.totalParticipantCount.Load().(int64)
	totalParticipantsKnownByIndexer := m.totalParticipantsKnownByIndexer.Load().(int64)
	totalAddressableParticipants := m.totalAddressableParticipants.Load().(int64)
	totalAddressableParticipantsWithDeal := m.totalAddressableParticipantsWithNonZeroDeals.Load().(int64)
	m.participantsTotal.Observe(ctx, totalParticipants)
	m.participantsTotal.Observe(ctx, totalParticipantsKnownByIndexer, attributeKnownByIndexer)
	m.participantsTotal.Observe(ctx, totalAddressableParticipants, attributeWithAddress)
	m.participantsTotal.Observe(ctx, totalAddressableParticipantsWithDeal, attributeWithAddress, attributeWithDeal)

	totalDealCount := m.totalDealCount.Load().(int64)
	totalDealCountByParticipantsKnownToIndexer := m.totalDealCountByParticipantsKnownToIndexer.Load().(int64)
	m.dealsTotal.Observe(ctx, totalDealCount)
	m.dealsTotal.Observe(ctx, totalDealCountByParticipantsKnownToIndexer, attributeKnownByIndexer)

	var dc float64
	if totalDealCount > 0 {
		dc = float64(totalDealCountByParticipantsKnownToIndexer) / float64(totalDealCount)
	}
	m.dealsCoverage.Observe(ctx, dc)

	var pc float64
	if totalAddressableParticipantsWithDeal > 0 {
		pc = float64(totalParticipantsKnownByIndexer) / float64(totalAddressableParticipantsWithDeal)
	}
	m.participantsCoverage.Observe(ctx, pc)
}

func (m *metrics) snapshot(targets map[string]*Target) {
	var totalAddressableParticipants int64
	var totalAddressableParticipantsWithNonZeroDeals int64
	var totalParticipantsKnownByIndexer int64
	var totalDealCountByParticipantsKnownToIndexer int64
	countsByStatus := make(map[Status]int64)

	for _, t := range targets {
		countsByStatus[t.Status] = countsByStatus[t.Status] + 1
		if t.AddrInfo.ID != "" && len(t.AddrInfo.Addrs) > 0 {
			if t.DealCount > 0 {
				if t.KnownByIndexer {
					totalParticipantsKnownByIndexer++
					totalDealCountByParticipantsKnownToIndexer += t.DealCount
				}
				totalAddressableParticipantsWithNonZeroDeals++
			}
			totalAddressableParticipants++
		}
	}

	m.countsByStatusLock.Lock()
	m.countsByStatus = countsByStatus
	defer m.countsByStatusLock.Unlock()

	m.totalAddressableParticipants.Store(totalAddressableParticipants)
	m.totalAddressableParticipantsWithNonZeroDeals.Store(totalAddressableParticipantsWithNonZeroDeals)
	m.totalParticipantsKnownByIndexer.Store(totalParticipantsKnownByIndexer)
	m.totalDealCountByParticipantsKnownToIndexer.Store(totalDealCountByParticipantsKnownToIndexer)
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
	case StatusUnidentifiable:
		value = "unidentifiable"
	case StatusNoAddrInfo:
		value = "no-addrinfo"
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
