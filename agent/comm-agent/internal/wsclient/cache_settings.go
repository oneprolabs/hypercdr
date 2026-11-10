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
		ConfigureCache(context.Context, string, string, string, string, bool, int, bool, int, bool) error
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
	if err := runtime.ConfigureCache(ctx, command.Namespace, command.RootPath, command.IndexPath, command.DrillPath, command.IndexEnabled, command.IndexMaxGiB, command.DrillEnabled, command.DrillMaxGiB, command.Clean); err != nil {
		_ = c.sendTaskFailed(task, "CACHE_SETTINGS_FAILED", err.Error())
		return
	}
	_ = c.sendTaskCompleted(task, map[string]any{"kind": "CacheSettings", "rootPath": command.RootPath, "indexPath": command.IndexPath, "drillPath": command.DrillPath, "indexEnabled": command.IndexEnabled, "indexMaxGiB": command.IndexMaxGiB, "drillEnabled": command.DrillEnabled, "drillMaxGiB": command.DrillMaxGiB, "clean": command.Clean}, "Cache settings applied for new tasks")
}
