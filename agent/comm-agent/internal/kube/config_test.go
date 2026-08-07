package kube

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestTuneRESTConfigSupportsBoundedResourceDiscovery(t *testing.T) {
	cfg := tuneRESTConfig(&rest.Config{})
	if cfg.QPS != 50 || cfg.Burst != 100 {
		t.Fatalf("unexpected Kubernetes client limits: qps=%v burst=%d", cfg.QPS, cfg.Burst)
	}
}
