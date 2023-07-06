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
