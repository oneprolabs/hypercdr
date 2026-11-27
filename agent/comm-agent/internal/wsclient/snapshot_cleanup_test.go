package wsclient

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type terminalSnapshotCleanerStub struct {
	kube.ManifestApplier
	calls     int
	namespace string
	failFirst bool
}

func (cleaner *terminalSnapshotCleanerStub) CleanupTerminalBackupSnapshots(ctx context.Context, namespace string) error {
	cleaner.calls++
	cleaner.namespace = namespace
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > 10*time.Second {
		return fmt.Errorf("cleanup lacks bounded context")
	}
	if cleaner.failFirst && cleaner.calls == 1 {
		return fmt.Errorf("owned snapshot deletion denied")
	}
	return nil
}

func TestSnapshotCleanupRetriesAndDoesNotWarnForIgnoredSnapshots(t *testing.T) {
	for _, scenario := range []string{"ignored", "retry", "dry-run", "oadp"} {
		t.Run(scenario, func(t *testing.T) {
			var output bytes.Buffer
			cleaner := &terminalSnapshotCleanerStub{failFirst: scenario == "retry"}
			client := &Client{cfg: config.Config{ExecutorMode: "kubernetes", Namespace: "agent"}, applier: cleaner, logger: slog.New(slog.NewTextHandler(&output, nil))}
			if scenario == "dry-run" {
				client.cfg.ExecutorMode = "dry-run"
			}
			if scenario == "oadp" {
				client.cfg.BackupBackend = "oadp"
			}
			client.cleanupTerminalSnapshots(context.Background())
			client.cleanupTerminalSnapshots(context.Background())
			expectedCalls := 2
			if scenario == "dry-run" || scenario == "oadp" {
				expectedCalls = 0
			}
			if cleaner.calls != expectedCalls || (expectedCalls > 0 && cleaner.namespace != "agent") {
				t.Fatalf("unexpected cleanup dispatch: calls=%d namespace=%s", cleaner.calls, cleaner.namespace)
			}
			if (output.Len() > 0) != (scenario == "retry") {
				t.Fatalf("ignored snapshots must not produce warnings: %s", output.String())
			}
		})
	}
}

func TestSnapshotCleanupRunsWithoutWebsocketAndStopsWithProcessContext(t *testing.T) {
	var output bytes.Buffer
	cleaner := &terminalSnapshotCleanerStub{failFirst: true}
	cfg := config.Config{ExecutorMode: "kubernetes", Namespace: "isolated", HeartbeatInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	RunSnapshotCleanup(ctx, cfg, slog.New(slog.NewTextHandler(&output, nil)), cleaner)
	if cleaner.calls < 2 || cleaner.namespace != "isolated" || output.Len() == 0 {
		t.Fatalf("disconnected cleanup did not retry: calls=%d namespace=%s log=%s", cleaner.calls, cleaner.namespace, output.String())
	}
	previous := cleaner.calls
	RunSnapshotCleanup(ctx, cfg, slog.Default(), cleaner)
	if cleaner.calls != previous {
		t.Fatal("cleanup ran after process cancellation")
	}
}
