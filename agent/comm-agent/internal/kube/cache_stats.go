package kube

import "encoding/json"

type CacheStats struct {
	Version        int   `json:"version"`
	IndexEnabled   bool  `json:"indexEnabled"`
	DataEnabled    bool  `json:"dataEnabled"`
	IndexHitBytes  int64 `json:"indexHitBytes"`
	IndexReadBytes int64 `json:"indexReadBytes"`
	DataHitBytes   int64 `json:"dataHitBytes"`
	DataReadBytes  int64 `json:"dataReadBytes"`
}

func parseCacheStats(raw string) *CacheStats {
	if raw == "" {
		return nil
	}
	var stats CacheStats
	if json.Unmarshal([]byte(raw), &stats) != nil || stats.Version != 1 || stats.IndexHitBytes < 0 || stats.IndexReadBytes < stats.IndexHitBytes || stats.DataHitBytes < 0 || stats.DataReadBytes < stats.DataHitBytes {
		return nil
	}
	return &stats
}
