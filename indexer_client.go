package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/libp2p/go-libp2p/core/peer"
)

func (hf *heyFil) isKnownByIndexer(ctx context.Context, pid peer.ID) (bool, error) {
	url := hf.httpIndexerEndpoint + `/providers/` + pid.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	response, err := hf.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return false, err
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unsuccessful response %d : %s", response.StatusCode, string(body))
	}
	return true, nil
}
