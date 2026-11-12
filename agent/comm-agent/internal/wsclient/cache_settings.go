package wsclient

import (
	"context"
	"fmt"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"time"
)

func (c *Client) executeCacheSettingsTask(task protocol.TaskDispatchPayload) {
	if _, running := c.cacheSettingsRunning.LoadOrStore(task.TaskID, true); running {
		return
	}
	defer c.cacheSettingsRunning.Delete(task.TaskID)
	c.cacheConfigurationMu.Lock()
	defer c.cacheConfigurationMu.Unlock()
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
	if command.Transition != nil {
		if err := c.ledger.recordTask(task.TaskID, task.CommandID, "", task, taskLedgerObject{Kind: "CacheSettings", Namespace: command.Namespace, Name: task.TaskID}); err != nil {
			_ = c.sendTaskFailed(task, "CACHE_SETTINGS_PERSIST_FAILED", err.Error())
			return
		}
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var applyErr error
	if command.Transition != nil {
		transitionRuntime, supported := c.agentRuntime.(cacheTransitionRuntime)
		if !supported {
			_ = c.sendTaskFailed(task, "CACHE_ISOLATION_UNSUPPORTED", "Upgrade the cluster agent before changing cache directories")
			return
		}
		state, err := c.ledger.cacheTransition(task.TaskID)
		if err != nil {
			_ = c.sendTaskFailed(task, "CACHE_SETTINGS_PERSIST_FAILED", err.Error())
			return
		}
		safe, err := resumeCacheTransition(ctx, transitionRuntime, *command, state, func(next cacheTransitionState) error { return c.ledger.saveCacheTransition(task.TaskID, next) })
		if !safe {
			_ = c.sendTaskProgress(task, map[string]any{"kind": "CacheSettings", "recoveryRequired": true}, 0, "Cache configuration recovery is required: "+err.Error())
			return
		}
		applyErr = err
	} else {
		applyErr = runtime.ConfigureCache(ctx, command.Namespace, command.RootPath, command.IndexPath, command.DrillPath, command.IndexEnabled, command.IndexMaxGiB, command.DrillEnabled, command.DrillMaxGiB, command.Clean)
	}
	if applyErr != nil {
		_ = c.sendTaskFailed(task, "CACHE_SETTINGS_FAILED", applyErr.Error())
		return
	}
	_ = c.sendTaskCompleted(task, map[string]any{"kind": "CacheSettings", "rootPath": command.RootPath, "indexPath": command.IndexPath, "drillPath": command.DrillPath, "indexEnabled": command.IndexEnabled, "indexMaxGiB": command.IndexMaxGiB, "drillEnabled": command.DrillEnabled, "drillMaxGiB": command.DrillMaxGiB, "clean": command.Clean}, "Cache settings applied for new tasks")
}

func (c *Client) executeCacheCleanupTask(task protocol.TaskDispatchPayload) {
	if _, running := c.cacheCleanupRunning.LoadOrStore(task.TaskID, true); running {
		return
	}
	defer c.cacheCleanupRunning.Delete(task.TaskID)
	command := task.CacheSettings
	if command == nil || len(command.CleanupPaths) == 0 {
		_ = c.sendTaskFailed(task, "CACHE_CLEANUP_REQUIRED", "cache cleanup paths are missing")
		return
	}
	cleaner, ok := c.agentRuntime.(interface {
		ScheduleCacheCleanup(context.Context, string, []string) error
	})
	if !ok {
		_ = c.sendTaskFailed(task, "CACHE_RUNTIME_UNAVAILABLE", "Cache management is unavailable")
		return
	}
	if err := c.ledger.recordTask(task.TaskID, task.CommandID, "", task, taskLedgerObject{Kind: "CacheCleanup", Namespace: command.Namespace, Name: task.TaskID}); err != nil {
		_ = c.sendTaskFailed(task, "CACHE_CLEANUP_PERSIST_FAILED", err.Error())
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour+time.Minute)
	defer cancel()
	var cleanupErr error
	lastMessage := ""
	progressCallback := func(completed, total, failedAttempts int) {
		message := fmt.Sprintf("Cleaning old cache: %d of %d node directories completed", completed, total)
		if failedAttempts > 0 {
			message += fmt.Sprintf("; retrying after %d failed attempts", failedAttempts)
		}
		if message == lastMessage {
			return
		}
		lastMessage = message
		progress := 0
		if total > 0 {
			progress = completed * 100 / total
		}
		_ = c.sendTaskProgress(task, map[string]any{"kind": "CacheCleanup", "completed": completed, "total": total, "failedAttempts": failedAttempts}, progress, message)
	}
	if command.Transition != nil {
		stageRuntime, ok := c.agentRuntime.(interface {
			RunCacheDirectoryStageWithProgress(context.Context, string, string, string, []string, []string, string, func(int, int, int)) error
		})
		if !ok {
			_ = c.sendTaskFailed(task, "CACHE_ISOLATION_UNSUPPORTED", "Cluster agent does not support isolated cache cleanup")
			return
		}
		transition := command.Transition
		cleanupErr = stageRuntime.RunCacheDirectoryStageWithProgress(ctx, command.Namespace, "delete-isolated", transition.IsolationID, transition.Paths, transition.Nodes, task.TaskID, progressCallback)
	} else if progressCleaner, ok := c.agentRuntime.(interface {
		ScheduleCacheCleanupForTask(context.Context, string, string, []string, func(int, int, int)) error
	}); ok {
		cleanupErr = progressCleaner.ScheduleCacheCleanupForTask(ctx, command.Namespace, task.TaskID, command.CleanupPaths, progressCallback)
	} else {
		cleanupErr = cleaner.ScheduleCacheCleanup(ctx, command.Namespace, command.CleanupPaths)
	}
	if cleanupErr != nil {
		_ = c.sendTaskFailed(task, "CACHE_CLEANUP_FAILED", cleanupErr.Error())
		return
	}
	_ = c.sendTaskCompleted(task, map[string]any{"kind": "CacheCleanup", "cleanupPaths": command.CleanupPaths}, "Cache cleanup completed")
}
