package main

import (
	"net/http"
	"path"
	"regexp"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
)

type (
	Option  func(*options) error
	options struct {
		api                           string
		h                             host.Host
		topic                         string
		headProtocolPattern           *regexp.Regexp
		maxConcurrentParticipantCheck int
		participantsCheckInterval     *time.Ticker
		dealStatsRefreshInterval      *time.Ticker
		snapshotInterval              *time.Ticker
		metricsListenAddr             string
		apiListenAddr                 string
		httpClient                    *http.Client
		marketDealsS3Snapshot         string
		marketDealsFilTools           string
		marketDealsFilToolsEnabled    bool
		httpIndexerEndpoint           string
		storePath                     string
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		api:                           `https://api.node.glif.io`,
		marketDealsS3Snapshot:         `https://marketdeals.s3.amazonaws.com/StateMarketDeals.json.zst`,
		marketDealsFilTools:           `https://filecoin.tools/api/deals/list`,
		httpIndexerEndpoint:           `https://cid.contact`,
		httpClient:                    http.DefaultClient,
		maxConcurrentParticipantCheck: 10,
		topic:                         `/indexer/ingest/mainnet`,
		participantsCheckInterval:     time.NewTicker(1 * time.Hour),
		snapshotInterval:              time.NewTicker(10 * time.Second),
		dealStatsRefreshInterval:      time.NewTicker(1 * time.Hour),
		metricsListenAddr:             "0.0.0.0:8080",
		apiListenAddr:                 "0.0.0.0:8081",
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.h == nil {
		var err error
		opts.h, err = libp2p.New()
		if err != nil {
			return nil, err
		}
	}
	opts.headProtocolPattern = regexp.MustCompile("/legs/head/?(/.+)/0.0.1")
	return opts, nil
}

// WithFileCoinAPI sets the FileCoin API endpoint.
// Defaults to https://api.node.glif.io/rpc/v0/
func WithFileCoinAPI(url string) Option {
	return func(o *options) error {
		o.api = url
		return nil
	}
}

// WithHost specifies the libp2p host.
// If unset, a new host with random identity is instantiated.
func WithHost(h host.Host) Option {
	return func(o *options) error {
		o.h = h
		return nil
	}
}

// WithIndexerTopic sets the indexer ingest topic to look for.
// Defaults to "/indexer/ingest/mainnet"
func WithIndexerTopic(t string) Option {
	return func(o *options) error {
		o.topic = t
		return nil
	}
}

// WithMaxConcurrentChecks sets the maximum number of state miner participants checked concurrently.
// Defaults to 10.
func WithMaxConcurrentChecks(m int) Option {
	return func(o *options) error {
		o.maxConcurrentParticipantCheck = m
		return nil
	}
}

// WithParticipantCheckInterval sets the interval at which state market participants are checked.
// Defaults to 1 hours if unset.
func WithParticipantCheckInterval(t *time.Ticker) Option {
	return func(o *options) error {
		o.participantsCheckInterval = t
		return nil
	}
}

// WithSnapshotInterval sets the interval at which check results are summarized the result of which
// will be visible via metrics.
// Defaults to 10 seconds if unset.
func WithSnapshotInterval(t *time.Ticker) Option {
	return func(o *options) error {
		o.snapshotInterval = t
		return nil
	}
}

// WithMetricsListenAddr sets the listen address of the HTTP server on which metrics are reported.
// Defaults to "0.0.0.0:8080
func WithMetricsListenAddr(addr string) Option {
	return func(o *options) error {
		o.metricsListenAddr = addr
		return nil
	}
}

// WithApiListenAddr sets the listen address of the HTTP server on which serves the HeyFil API.
// Defaults to "0.0.0.0:8081
func WithApiListenAddr(addr string) Option {
	return func(o *options) error {
		o.metricsListenAddr = addr
		return nil
	}
}

// WithHttpIndexerEndpoint sets the HTTP endpoint of an IPNI node.
// Defaults to https://cid.contact
func WithHttpIndexerEndpoint(url string) Option {
	return func(o *options) error {
		o.httpIndexerEndpoint = url
		return nil
	}
}

// WithStorePath sets the directory to use for storing the SP data.
// The stored information is then used to reload the state on service restart if it is present.
// Defaults to in-memory storage only, which means on each service restart
// previous SP state is re-populated from scratch.
func WithStorePath(p string) Option {
	return func(o *options) error {
		// Check if path is empty before cleaning it since cleaning an empty path results in ".",
		// and empty path instead should disable local storage.
		if p != "" {
			o.storePath = path.Clean(p)
		}
		return nil
	}
}
