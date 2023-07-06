package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
)

func (hf *heyFil) startApiServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc(`/sp`, hf.handleSPRoot)
	mux.HandleFunc(`/sp/`, hf.handleSPSubtree)
	mux.HandleFunc(`/`, handleDefault)
	hf.apiServer = http.Server{
		Addr:    hf.apiListenAddr,
		Handler: mux,
	}
	go func() {
		err := hf.apiServer.ListenAndServe()
		switch {
		case errors.Is(err, http.ErrServerClosed):
			logger.Info("server stopped")
		default:
			logger.Infow("server failed", "")
		}
	}()
	return nil
}

func (hf *heyFil) handleSPRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodGet, http.MethodOptions))
	case http.MethodGet:
		pid := r.URL.Query().Get("peerid")
		filterByPeerID := pid != ""
		var pidFilter peer.ID
		if filterByPeerID {
			var err error
			pidFilter, err = peer.Decode(pid)
			if err != nil {
				http.Error(w, `The "peerid" query parameter is not a valid peer ID`, http.StatusBadRequest)
				return
			}
		}
		hf.targetsMutex.RLock()
		spIDs := make([]string, 0, len(hf.targets))
		for id, target := range hf.targets {
			if filterByPeerID && target.AddrInfo != nil && target.AddrInfo.ID != pidFilter {
				continue
			}
			spIDs = append(spIDs, id)
		}
		sort.Strings(spIDs)
		hf.targetsMutex.RUnlock()
		if err := json.NewEncoder(w).Encode(spIDs); err != nil {
			logger.Errorw("Failed to encode SP IDs", "err", err)
		}
	default:
		hf.respondWithNotAllowed(w, http.MethodGet, http.MethodOptions)
	}
}

func (hf *heyFil) handleSPSubtree(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodGet, http.MethodOptions))
	case http.MethodGet:
		pathSuffix := strings.TrimPrefix(r.URL.Path, "/sp/")
		switch segments := strings.SplitN(pathSuffix, "/", 3); len(segments) {
		case 1:
			id := segments[0]
			if id == "" {
				// Path does not contain SP ID.
				http.Error(w, "SP ID must be specified as URL parameter", http.StatusBadRequest)
			} else {
				hf.handleGetSP(w, id)
			}
		default:
			// Path has multiple segments and therefore 404
			http.NotFound(w, r)
		}
	default:
		hf.respondWithNotAllowed(w, http.MethodGet, http.MethodOptions)
	}
}

func (hf *heyFil) handleGetSP(w http.ResponseWriter, id string) {
	hf.targetsMutex.RLock()
	target, ok := hf.targets[id]
	hf.targetsMutex.RUnlock()
	if !ok {
		http.Error(w, "SP not found", http.StatusNotFound)
	} else if err := json.NewEncoder(w).Encode(target); err != nil {
		logger.Errorw("Failed to encode SP info", "id", id, "err", err)
	}
}

func handleDefault(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set(httpHeaderAllow(http.MethodOptions))
	default:
		http.NotFound(w, r)
	}
}

func httpHeaderAllow(methods ...string) (string, string) {
	return "Allow", strings.Join(methods, ",")
}

func (hf *heyFil) respondWithNotAllowed(w http.ResponseWriter, allowedMethods ...string) {
	w.Header().Set(httpHeaderAllow(allowedMethods...))
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func (hf *heyFil) shutdownApiServer(ctx context.Context) error {
	return hf.apiServer.Shutdown(ctx)
}
