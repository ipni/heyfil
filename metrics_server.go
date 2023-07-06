package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (hf *heyFil) startMetricsServer() error {
	mux := http.NewServeMux()
	mux.Handle(`/metrics`, promhttp.Handler())
	hf.metricsServer = http.Server{
		Addr:    hf.metricsListenAddr,
		Handler: mux,
	}
	go func() {
		err := hf.metricsServer.ListenAndServe()
		switch {
		case errors.Is(err, http.ErrServerClosed):
			logger.Info("server stopped")
		default:
			logger.Infow("server failed", "")
		}
	}()
	return nil
}

func (hf *heyFil) shutdownMetricsServer(ctx context.Context) error {
	return hf.metricsServer.Shutdown(ctx)
}
