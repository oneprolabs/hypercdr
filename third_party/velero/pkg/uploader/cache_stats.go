package uploader

import (
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const CacheStatsAnnotation = "hypercdr.io/cache-stats-v1"

type CacheStats struct {
	Version        int   `json:"version"`
	IndexEnabled   bool  `json:"indexEnabled"`
	DataEnabled    bool  `json:"dataEnabled"`
	IndexHitBytes  int64 `json:"indexHitBytes"`
	IndexReadBytes int64 `json:"indexReadBytes"`
	DataHitBytes   int64 `json:"dataHitBytes"`
	DataReadBytes  int64 `json:"dataReadBytes"`
}

func SetCacheStats(object metav1.Object, stats *CacheStats) {
	if stats == nil {
		return
	}
	encoded, err := json.Marshal(stats)
	if err != nil {
		return
	}
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[CacheStatsAnnotation] = string(encoded)
	object.SetAnnotations(annotations)
}
