package main

import (
	"context"
	"math/big"
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

const (
	filStorageMarket_1_0_1 = "/fil/storage/mk/1.0.1"
	filStorageMarket_1_1_0 = "/fil/storage/mk/1.1.0"
	filStorageMarket_1_1_1 = "/fil/storage/mk/1.1.1"
	filStorageMarket_1_2_0 = "/fil/storage/mk/1.2.0"
)

var (
	attributeWithAddress    = attribute.Bool("with-addr", true)
	attributeKnownByIndexer = attribute.Bool("known-by-indexer", true)
	attributeSpanDay        = attribute.String("span", "1d")
	attributeSpanWeek       = attribute.String("span", "1w")
	attributeWithDeal       = attribute.Bool("with-deal", true)
)

type metrics struct {
	exporter *prometheus.Exporter

	participantsByStatus            asyncint64.Gauge
	participantsByAgentShortVersion asyncint64.Gauge
	participantsByMarketProtocol    asyncint64.Gauge
	participantsByTransportProtocol asyncint64.Gauge
	participantsTotal               asyncint64.Gauge
	participantsCoverage            asyncfloat64.Gauge
	dealsTotal                      asyncint64.Gauge
	dealsCoverage                   asyncfloat64.Gauge
	unindexedDealCount              asyncint64.Gauge
	boostParticipantsTotal          asyncint64.Gauge
	boostQualityAdjustedPowerTotal  asyncint64.Gauge
	boostRawPowerTotal              asyncint64.Gauge
	participantsWithMinPowerTotal   asyncint64.Gauge

	countsMutex               sync.RWMutex
	countsByStatus            map[Status]int64
	countsByAgentShortVersion map[string]int64
	countsByMarketProtocol    map[string]int64
	countsByTransportProtocol map[string]int64

	// totalParticipantCount is the total number of state market participants retrieved from
	// FileCoin API stored as int64.
	totalParticipantCount atomic.Value
	// totalDealCount is the total number of state market deals retrieved from FileCoin API  stored
	// as int64.
	totalDealCount atomic.Value
	// totalDealCountWithinWeek is the total number of state market deals made in the last day.
	totalDealCountWithinDay atomic.Value
	// totalDealCountWithinWeek is the total number of state market deals made in the last week.
	totalDealCountWithinWeek atomic.Value
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
	// totalDealCountWithinDayByParticipantsKnownToIndexer is the total number of deals made by the state
	// market participants in the last day that are listed as a provider by network indexer stored as int64.
	totalDealCountWithinDayByParticipantsKnownToIndexer atomic.Value
	// totalDealCountWithinWeekByParticipantsKnownToIndexer is the total number of deals made by the state
	// market participants in the last week that are listed as a provider by network indexer stored as int64.
	totalDealCountWithinWeekByParticipantsKnownToIndexer atomic.Value
	totalBoostParticipants                               atomic.Value
	totalParticipantsWithMinPower                        atomic.Value
	totalBoostQualityAdjustedPower                       atomic.Value
	totalBoostRawPower                                   atomic.Value
	// unindexedTargets maps the miner ID to target information for participants that are not indexed.
	unindexedTargets map[string]*Target
}

func (m *metrics) start() error {
	m.totalParticipantCount.Store(int64(0))
	m.totalDealCount.Store(int64(0))
	m.totalDealCountWithinDay.Store(int64(0))
	m.totalDealCountWithinWeek.Store(int64(0))
	m.totalAddressableParticipantsWithNonZeroDeals.Store(int64(0))
	m.totalAddressableParticipants.Store(int64(0))
	m.totalParticipantsKnownByIndexer.Store(int64(0))
	m.totalDealCountByParticipantsKnownToIndexer.Store(int64(0))
	m.totalDealCountWithinDayByParticipantsKnownToIndexer.Store(int64(0))
	m.totalDealCountWithinWeekByParticipantsKnownToIndexer.Store(int64(0))
	m.totalBoostParticipants.Store(int64(0))
	m.totalParticipantsWithMinPower.Store(int64(0))
	m.totalBoostQualityAdjustedPower.Store(big.NewInt(0))
	m.totalBoostRawPower.Store(big.NewInt(0))
	m.unindexedTargets = make(map[string]*Target)

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
	if m.participantsByAgentShortVersion, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/miners_by_agent_short_version",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The state markets participants count by shortened agent version of their libp2p host."),
	); err != nil {
		return err
	}
	if m.participantsByMarketProtocol, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/miners_by_market_protocol",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The state markets participants count by markets protocol version."),
	); err != nil {
		return err
	}
	if m.participantsByTransportProtocol, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/miners_by_transport_protocol",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The state markets participants count by transports protocol name."),
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
			"See `known-by-indexer` and `span` attributes."),
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

	if m.unindexedDealCount, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_unindexed_deal_count",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The number of unindexed deals tagged by their miner ID and deal age span."),
	); err != nil {
		return err
	}
	if m.boostParticipantsTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_boost_participants_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total number of participants running Boost."),
	); err != nil {
		return err
	}
	if m.boostQualityAdjustedPowerTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_boost_quality_adjusted_power_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total quality adjusted power across participants that are running Boost."),
	); err != nil {
		return err
	}
	if m.boostRawPowerTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_boost_raw_power_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total raw power across participants that are running Boost."),
	); err != nil {
		return err
	}
	if m.participantsWithMinPowerTotal, err = meter.AsyncInt64().Gauge(
		"ipni/heyfil/state_market_participants_with_min_power_total",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("The total number of participants with min power."),
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
	m.countsMutex.RLock()
	for status, c := range m.countsByStatus {
		m.participantsByStatus.Observe(ctx, c, targetStatusToAttribute(status))
	}
	for agent, c := range m.countsByAgentShortVersion {
		m.participantsByAgentShortVersion.Observe(ctx, c, attribute.String("agentShortVersion", agent))
	}
	for mp, c := range m.countsByMarketProtocol {
		m.participantsByMarketProtocol.Observe(ctx, c, attribute.String("protocol", mp))
	}
	for tp, c := range m.countsByTransportProtocol {
		m.participantsByTransportProtocol.Observe(ctx, c, attribute.String("transportName", tp))
	}
	m.countsMutex.RUnlock()

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

	totalDealCountWithinDay := m.totalDealCountWithinDay.Load().(int64)
	totalDealCountWithinDayByParticipantsKnownToIndexer := m.totalDealCountWithinDayByParticipantsKnownToIndexer.Load().(int64)
	m.dealsTotal.Observe(ctx, totalDealCountWithinDay, attributeSpanDay)
	m.dealsTotal.Observe(ctx, totalDealCountWithinDayByParticipantsKnownToIndexer, attributeKnownByIndexer, attributeSpanDay)

	totalDealCountWithinWeek := m.totalDealCountWithinWeek.Load().(int64)
	totalDealCountWithinWeekByParticipantsKnownToIndexer := m.totalDealCountWithinWeekByParticipantsKnownToIndexer.Load().(int64)
	m.dealsTotal.Observe(ctx, totalDealCountWithinWeek, attributeSpanWeek)
	m.dealsTotal.Observe(ctx, totalDealCountWithinWeekByParticipantsKnownToIndexer, attributeKnownByIndexer, attributeSpanWeek)

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

	for id, t := range m.unindexedTargets {
		idAttr := attribute.String("id", id)
		m.unindexedDealCount.Observe(ctx, t.DealCount, idAttr)
		m.unindexedDealCount.Observe(ctx, t.DealCountWithinDay, idAttr, attributeSpanDay)
		m.unindexedDealCount.Observe(ctx, t.DealCountWithinWeek, idAttr, attributeSpanWeek)
	}

	m.boostParticipantsTotal.Observe(ctx, m.totalBoostParticipants.Load().(int64))
	m.boostQualityAdjustedPowerTotal.Observe(ctx, m.totalBoostQualityAdjustedPower.Load().(*big.Int).Int64())
	m.boostRawPowerTotal.Observe(ctx, m.totalBoostRawPower.Load().(*big.Int).Int64())
	m.participantsWithMinPowerTotal.Observe(ctx, m.totalParticipantsWithMinPower.Load().(int64))

}

func (m *metrics) snapshot(targets map[string]*Target) {
	var totalAddressableParticipants,
		totalAddressableParticipantsWithNonZeroDeals,
		totalParticipantsKnownByIndexer,
		totalDealCountByParticipantsKnownToIndexer,
		totalDealCountWithinDayByParticipantsKnownToIndexer,
		totalDealCountWithinWeekByParticipantsKnownToIndexer,
		totalBoostNodes,
		totalWithMinPower int64
	totalBoostQualityAdjustedPower, totalBoostRawPower := big.NewInt(0), big.NewInt(0)
	countsByStatus := make(map[Status]int64)
	countsByAgentShortVersion := make(map[string]int64)
	countsByMarketProtocol := make(map[string]int64)
	countsByTransportProtocol := make(map[string]int64)

	for _, t := range targets {
		countsByStatus[t.Status]++
		if t.AddrInfo != nil && t.AddrInfo.ID != "" && len(t.AddrInfo.Addrs) > 0 {
			if t.DealCount > 0 {
				if t.KnownByIndexer {
					totalParticipantsKnownByIndexer++
					totalDealCountByParticipantsKnownToIndexer += t.DealCount
					totalDealCountWithinDayByParticipantsKnownToIndexer += t.DealCountWithinDay
					totalDealCountWithinWeekByParticipantsKnownToIndexer += t.DealCountWithinWeek
				}
				totalAddressableParticipantsWithNonZeroDeals++
			}
			totalAddressableParticipants++
		}
		if t.Status == StatusUnindexed {
			m.unindexedTargets[t.ID] = t
		}
		if t.StateMinerPower != nil && t.StateMinerPower.HasMinPower {
			totalWithMinPower++
			if t.supportsProtocolID(filStorageMarket_1_2_0) {
				totalBoostNodes++
				if qap, ok := big.NewInt(0).SetString(t.StateMinerPower.MinerPower.QualityAdjPower, 10); ok {
					totalBoostQualityAdjustedPower = big.NewInt(0).Add(totalBoostQualityAdjustedPower, qap)
				}
				if rbp, ok := big.NewInt(0).SetString(t.StateMinerPower.MinerPower.RawBytePower, 10); ok {
					totalBoostRawPower = big.NewInt(0).Add(totalBoostRawPower, rbp)
				}
			}
		}
		if t.AgentShortVersion != "" {
			countsByAgentShortVersion[t.AgentShortVersion]++
		}
		if t.supportsProtocolID(filStorageMarket_1_0_1) {
			countsByMarketProtocol[filStorageMarket_1_0_1]++
		}
		if t.supportsProtocolID(filStorageMarket_1_1_0) {
			countsByMarketProtocol[filStorageMarket_1_1_0]++
		}
		if t.supportsProtocolID(filStorageMarket_1_1_1) {
			countsByMarketProtocol[filStorageMarket_1_1_1]++
		}
		if t.supportsProtocolID(filStorageMarket_1_2_0) {
			countsByMarketProtocol[filStorageMarket_1_2_0]++
		}
		if t.Transports != nil {
			for _, protocol := range t.Transports.Protocols {
				countsByTransportProtocol[protocol.Name]++
			}
		}
	}

	m.countsMutex.Lock()
	m.countsByStatus = countsByStatus
	m.countsByAgentShortVersion = countsByAgentShortVersion
	m.countsByMarketProtocol = countsByMarketProtocol
	m.countsByTransportProtocol = countsByTransportProtocol
	defer m.countsMutex.Unlock()

	m.totalAddressableParticipants.Store(totalAddressableParticipants)
	m.totalAddressableParticipantsWithNonZeroDeals.Store(totalAddressableParticipantsWithNonZeroDeals)
	m.totalParticipantsKnownByIndexer.Store(totalParticipantsKnownByIndexer)
	m.totalDealCountByParticipantsKnownToIndexer.Store(totalDealCountByParticipantsKnownToIndexer)
	m.totalDealCountWithinDayByParticipantsKnownToIndexer.Store(totalDealCountWithinDayByParticipantsKnownToIndexer)
	m.totalDealCountWithinWeekByParticipantsKnownToIndexer.Store(totalDealCountWithinWeekByParticipantsKnownToIndexer)
	m.totalBoostParticipants.Store(totalBoostNodes)
	m.totalParticipantsWithMinPower.Store(totalWithMinPower)
	m.totalBoostQualityAdjustedPower.Store(totalBoostQualityAdjustedPower)
	m.totalBoostRawPower.Store(totalBoostRawPower)
}

func (m *metrics) notifyParticipantCount(c int64) {
	m.totalParticipantCount.Store(c)
}

func (m *metrics) notifyDealCount(c int64) {
	m.totalDealCount.Store(c)
}

func (m *metrics) notifyDealCountWithinWeek(c int64) {
	m.totalDealCountWithinWeek.Store(c)
}

func (m *metrics) notifyDealCountWithinDay(c int64) {
	m.totalDealCountWithinDay.Store(c)
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
