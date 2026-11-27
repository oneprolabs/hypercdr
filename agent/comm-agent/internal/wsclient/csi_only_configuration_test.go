package wsclient

import (
	"io"
	"log/slog"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestLegacyConfigurationCannotEnableApplicationCoordination(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := kube.NewDynamicManifestApplierWithClient(client)
	cfg := config.Config{
		ExecutorMode: "kubernetes", BackupBackend: "velero",
		Namespace: "hcdr-consistency-velero", StateDir: t.TempDir(),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	configured := NewWithRuntimeDependencies(cfg, logger, applier, nil, nil)
	if configured == nil {
		t.Fatal("Agent construction failed")
	}
	if len(client.Actions()) != 0 {
		t.Fatal("Agent construction touched cluster resources")
	}
}
