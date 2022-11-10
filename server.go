package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (hf *heyFil) startServer() error {
	mux := http.NewServeMux()
	mux.Handle(`/metrics`, promhttp.Handler())
	hf.server = http.Server{
		Addr:    hf.serverListenAddr,
		Handler: mux,
	}
	go func() {
		err := hf.server.ListenAndServe()
		switch {
		case errors.Is(err, http.ErrServerClosed):
			logger.Info("server stopped")
		default:
			logger.Infow("server failed", "")
		}
	}()
	return nil
}

func (hf *heyFil) shutdownServer(ctx context.Context) error {
	return hf.server.Shutdown(ctx)
}
