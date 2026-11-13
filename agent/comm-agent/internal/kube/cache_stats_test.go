package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestCacheStatsAreReadFromVolumeAnnotations(t *testing.T) {
	item := unstructured.Unstructured{}
	item.SetName("pvb-test")
	item.SetAnnotations(map[string]string{"hypercdr.io/cache-stats-v1": `{"version":1,"indexEnabled":true,"dataEnabled":false,"indexHitBytes":70,"indexReadBytes":100,"dataHitBytes":0,"dataReadBytes":0}`})
	item.SetCreationTimestamp(metav1.Now())
	progress := volumeProgressFromItem("PodVolumeBackup", item)
	if progress.CacheStats == nil || progress.CacheStats.IndexHitBytes != 70 || !progress.CacheStats.IndexEnabled {
		t.Fatalf("%+v", progress)
	}
	for _, raw := range []string{"", `{`, `{"version":2}`, `{"version":1,"dataHitBytes":10,"dataReadBytes":2}`, `{"version":1,"indexHitBytes":-1}`} {
		if parseCacheStats(raw) != nil {
			t.Fatalf("accepted invalid statistics %q", raw)
		}
	}
}
