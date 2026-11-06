package wsclient

import (
	"context"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"time"
)

func (c *Client) executeCacheSettingsTask(task protocol.TaskDispatchPayload) {
	command := task.CacheSettings
	if command == nil {
		_ = c.sendTaskFailed(task, "CACHE_SETTINGS_REQUIRED", "Cache settings are missing")
		return
	}
	runtime, ok := c.agentRuntime.(interface {
		ConfigureCache(context.Context, string, bool, int, bool) error
	})
	if !ok {
		_ = c.sendTaskFailed(task, "CACHE_RUNTIME_UNAVAILABLE", "Cache management is unavailable")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := runtime.ConfigureCache(ctx, command.Namespace, command.Enabled, command.MaxGiB, command.Clean); err != nil {
		_ = c.sendTaskFailed(task, "CACHE_SETTINGS_FAILED", err.Error())
		return
	}
	_ = c.sendTaskCompleted(task, map[string]any{"kind": "CacheSettings", "enabled": command.Enabled, "maxGiB": command.MaxGiB, "clean": command.Clean}, "Cache settings applied for new tasks")
}
