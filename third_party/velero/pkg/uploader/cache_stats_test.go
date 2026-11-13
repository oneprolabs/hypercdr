package uploader

import (
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestCacheStatsAnnotationPreservesOtherAnnotations(t *testing.T) {
	object := &metav1.ObjectMeta{Annotations: map[string]string{"existing": "value"}}
	SetCacheStats(object, nil)
	if len(object.Annotations) != 1 {
		t.Fatal(object.Annotations)
	}
	stats := &CacheStats{Version: 1, IndexEnabled: true, IndexHitBytes: 50, IndexReadBytes: 100}
	SetCacheStats(object, stats)
	var decoded CacheStats
	if err := json.Unmarshal([]byte(object.Annotations[CacheStatsAnnotation]), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != *stats || object.Annotations["existing"] != "value" {
		t.Fatal(object.Annotations)
	}
}
