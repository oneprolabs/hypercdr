package wsclient

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
)

func RunSnapshotCleanup(ctx context.Context, cfg config.Config, logger *slog.Logger, applier kube.ManifestApplier) {
	client := &Client{cfg: cfg, logger: logger, applier: applier}
	interval := cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		client.cleanupTerminalSnapshots(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Client) cleanupTerminalSnapshots(ctx context.Context) {
	if !strings.EqualFold(c.cfg.ExecutorMode, "kubernetes") || strings.EqualFold(c.cfg.BackupBackend, "oadp") {
		return
	}
	cleaner, ok := c.applier.(interface {
		CleanupTerminalBackupSnapshots(context.Context, string) error
	})
	if !ok {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cleaner.CleanupTerminalBackupSnapshots(cleanupCtx, c.cfg.Namespace); err != nil && ctx.Err() == nil {
		c.logger.Warn("temporary snapshot cleanup will retry", "error", err)
	}
}
