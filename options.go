package main

import (
	"net/http"
	"regexp"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
)

type (
	Option  func(*options) error
	options struct {
		api                  string
		h                    host.Host
		topic                string
		headProtocolPattern  *regexp.Regexp
		maxConcCheck         int
		checkInterval        *time.Ticker
		snapshotInterval     *time.Ticker
		serverListenAddr     string
		httpClient           *http.Client
		httpAnnounceEndpoint string
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		api:                  `https://api.node.glif.io/rpc/v0/`,
		httpAnnounceEndpoint: `https://cid.contact/ingest/announce`,
		httpClient:           http.DefaultClient,
		maxConcCheck:         10,
		topic:                `/indexer/ingest/mainnet`,
		checkInterval:        time.NewTicker(1 * time.Hour),
		snapshotInterval:     time.NewTicker(10 * time.Second),
		serverListenAddr:     "0.0.0.0:8080",
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
		o.maxConcCheck = m
		return nil
	}
}

// WithCheckInterval sets the interval at which state market participants are checked.
// Defaults to 1 hours if unset.
func WithCheckInterval(t *time.Ticker) Option {
	return func(o *options) error {
		o.checkInterval = t
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

// WithListenAddr sets the listen address of the HTTP server on which metrics are reported.
// Defaults to "0.0.0.0:8080
func WithListenAddr(addr string) Option {
	return func(o *options) error {
		o.serverListenAddr = addr
		return nil
	}
}

// WithHttpAnnounceEndpoint sets the HTTP endpoint that accepts IPNI announcement requests.
// Defaults to https://cid.contact/ingest/announce
func WithHttpAnnounceEndpoint(url string) Option {
	return func(o *options) error {
		o.httpAnnounceEndpoint = url
		return nil
	}
}
