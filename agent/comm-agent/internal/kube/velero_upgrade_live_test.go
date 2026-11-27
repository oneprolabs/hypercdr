package kube

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveManagedVeleroUpgrade(t *testing.T) {
	config := os.Getenv("HYPERCDR_UPGRADE_TEST_KUBECONFIG")
	image := os.Getenv("HYPERCDR_UPGRADE_TEST_IMAGE")
	if config == "" || image == "" {
		t.Skip("explicit test kubeconfig and image required")
	}
	runtime, err := NewKubernetesAgentRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	options := VeleroUpgradeOptions{Namespace: "hypercdr-agent", Image: image, ConcurrentBackups: 4, NodeAgentConcurrency: 4, PrepareQueueLength: 4}
	if os.Getenv("HYPERCDR_UPGRADE_TEST_RESTORE_CACHE") == "1" {
		options.CacheStorageClass = "longhorn"
		options.CacheResidentThresholdMB = 1024
		options.CacheLimitMB = 5120
	}
	if err := runtime.UpgradeVelero(ctx, options); err != nil {
		t.Fatal(err)
	}
}

func TestLiveManagedCommunicationAgentUpgrade(t *testing.T) {
	config := os.Getenv("HYPERCDR_UPGRADE_TEST_KUBECONFIG")
	image := os.Getenv("HYPERCDR_UPGRADE_TEST_COMM_IMAGE")
	if config == "" || image == "" {
		t.Skip("explicit test kubeconfig and communication agent image required")
	}
	runtime, err := NewKubernetesAgentRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	version := os.Getenv("HYPERCDR_UPGRADE_TEST_COMM_VERSION")
	if version == "" {
		version = "sync-settings-dev-20261127"
	}
	if err := runtime.UpgradeAgent(ctx, AgentUpgradeOptions{Namespace: "hypercdr-agent", Image: image, Version: version}); err != nil {
		t.Fatal(err)
	}
}
