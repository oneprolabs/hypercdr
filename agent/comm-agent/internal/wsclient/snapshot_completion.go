package wsclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func (c *Client) agentCapabilities() []string {
	capabilities := []string{"plan-capacity-v1", "force-protection-cleanup-v1", "task-abort-v1"}
	_, completion := c.applier.(interface {
		ReadSnapshotBackupCompletion(context.Context, string, string) (map[string]any, error)
	})
	_, preflight := c.applier.(interface {
		PrepareSnapshotBackup(context.Context, []string) (map[string]string, error)
	})
	if completion && preflight && c.ledger != nil && c.outbox != nil && strings.EqualFold(c.cfg.ExecutorMode, "kubernetes") && !strings.EqualFold(c.cfg.BackupBackend, "oadp") {
		capabilities = append(capabilities, "snapshot-backup-evidence-v1")
	}
	return capabilities
}

func (c *Client) verifiedSnapshotCompletion(task protocol.TaskDispatchPayload, payload map[string]any) (map[string]any, error) {
	reader, ok := c.applier.(interface {
		ReadSnapshotBackupCompletion(context.Context, string, string) (map[string]any, error)
	})
	if !ok {
		return nil, fmt.Errorf("verified snapshot completion reader is unavailable")
	}
	name, _ := payload["name"].(string)
	namespace, _ := payload["namespace"].(string)
	if name == "" || namespace == "" {
		return nil, fmt.Errorf("snapshot completion identity is missing")
	}
	ctx, cancel := context.WithTimeout(c.taskContext(task), 30*time.Second)
	defer cancel()
	verified, err := reader.ReadSnapshotBackupCompletion(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	merged := cloneVeleroPayload(payload)
	for key, value := range verified {
		merged[key] = value
	}
	if err := protocol.ValidateSnapshotBackupResult(task, merged); err != nil {
		return nil, err
	}
	return merged, nil
}
