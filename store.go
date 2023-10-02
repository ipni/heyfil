package main

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (hf *heyFil) store(t *Target) error {
	if hf.storePath == "" || t == nil || t.ID == "" {
		return nil
	}
	dest, err := os.OpenFile(path.Join(hf.storePath, t.ID+".json"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		logger.Errorw("Failed to open store file for target", "id", t.ID, "err", err)
		return err
	}
	defer dest.Close()
	return json.NewEncoder(dest).Encode(t)
}
func (hf *heyFil) loadTargets() error {
	if hf.storePath == "" {
		return nil
	}
	var totalLoaded int
	hf.targetsMutex.Lock()
	defer func() {
		hf.targetsMutex.Unlock()
		logger.Infow("finished loading SP information from store", "total", totalLoaded)
	}()
	return filepath.Walk(hf.storePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Errorw("failed to access path", "path", path, "err", err)
			return nil
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			source, err := os.Open(path)
			if err != nil {
				logger.Errorw("failed to open target file at path", "path", path, "err", err)
				return nil
			}

			var target Target
			if err := json.NewDecoder(source).Decode(&target); err != nil {
				logger.Errorw("failed to decode target file at path", "path", path, "err", err)
				return nil
			}
			hf.targets[target.ID] = &target
			totalLoaded++
		}
		return nil
	})
}

type recentPieces map[string]string

func (hf *heyFil) storeRecentPieces(rp recentPieces) error {
	if hf.storePath == "" || rp == nil {
		return nil
	}
	dest, err := os.OpenFile(path.Join(hf.storePath, "recent.pieces"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		logger.Errorw("Failed to open store file for recent pieces", "err", err)
		return err
	}
	defer dest.Close()
	if err := json.NewEncoder(dest).Encode(rp); err != nil {
		return err
	}
	hf.recentPiecesMutex.Lock()
	hf.recentPieces = rp
	hf.recentPiecesMutex.Unlock()
	return nil
}

func (hf *heyFil) loadRecentPieces() error {
	if hf.storePath == "" {
		return nil
	}

	hf.recentPiecesMutex.Lock()
	defer func() {
		hf.recentPiecesMutex.Unlock()
		logger.Infow("finished loading recent pieces")
	}()

	source, err := os.Open(path.Join(hf.storePath, "recent.pieces"))
	if err != nil {
		logger.Errorw("failed to open recent pieces", "err", err)
		return nil
	}

	var target recentPieces
	if err := json.NewDecoder(source).Decode(&target); err != nil {
		logger.Errorw("failed to decode recent pieces", "err", err)
		return nil
	}
	hf.recentPieces = target
	return nil
}
