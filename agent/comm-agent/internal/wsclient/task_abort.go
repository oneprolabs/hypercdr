package wsclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type abortableTaskRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func isAbortableTask(taskType string) bool {
	switch taskType {
	case "backup", "restore", "drill", "takeover", "protection-cleanup", "retention-cleanup", "storage-sync", "schedule-sync":
		return true
	}
	return false
}

func (c *Client) executeTask(task protocol.TaskDispatchPayload) {
	c.runAbortableTask(task, func() { c.executeTaskBody(task) })
}

func (c *Client) runAbortableTask(task protocol.TaskDispatchPayload, execute func()) {
	if !isAbortableTask(task.Type) {
		execute()
		return
	}
	c.taskRunMu.Lock()
	if c.taskRuns == nil {
		c.taskRuns = map[string]*abortableTaskRun{}
	}
	if c.taskRuns[task.TaskID] != nil || c.ledger.abortRequested(task.TaskID) {
		c.taskRunMu.Unlock()
		return
	}
	deadline := task.Deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(30 * time.Minute)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	run := &abortableTaskRun{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.taskRuns[task.TaskID] = run
	c.taskRunMu.Unlock()
	defer func() {
		cancel()
		c.taskRunMu.Lock()
		delete(c.taskRuns, task.TaskID)
		close(run.done)
		c.taskRunMu.Unlock()
	}()
	if task.Type == "protection-cleanup" || task.Type == "retention-cleanup" {
		if err := c.ledger.recordTask(task.TaskID, task.CommandID, "", task, taskLedgerObject{Kind: "Task", Name: task.TaskID}); err != nil {
			_ = c.sendTaskFailed(task, "LOCAL_LEDGER_WRITE_FAILED", err.Error())
			return
		}
		go c.reportCleanupActivity(task, ctx)
	}
	execute()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_ = c.sendTaskFailed(task, "TASK_DEADLINE_EXCEEDED", "The task exceeded its execution deadline. Check cluster and storage health before retrying.")
	}
}

func (c *Client) reportCleanupActivity(task protocol.TaskDispatchPayload, ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.sendTaskProgress(task, map[string]any{"kind": "Cleanup", "heartbeat": true}, 0, "Cleanup is still in progress; waiting for the current operation.")
		}
	}
}

func (c *Client) taskContext(task protocol.TaskDispatchPayload) context.Context {
	c.taskRunMu.Lock()
	defer c.taskRunMu.Unlock()
	if run := c.taskRuns[task.TaskID]; run != nil {
		return run.ctx
	}
	if c.ledger.abortRequested(task.TaskID) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return context.Background()
}

func (c *Client) waitTaskDelay(task protocol.TaskDispatchPayload, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-c.taskContext(task).Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *taskLedger) abortRequested(taskID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Records[taskID].AbortRequested
}

func (l *taskLedger) requestAbort(task protocol.TaskDispatchPayload) error {
	if l == nil {
		return errors.New("persistent task ledger is unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureMaps()
	record := l.Records[task.TaskID]
	record.TaskID = task.TaskID
	record.CommandID = task.CommandID
	record.Type = task.Type
	record.AbortRequested = true
	record.UpdatedAt = nowRFC3339()
	l.Records[task.TaskID] = record
	return l.flushLocked()
}

func (c *Client) executeTaskAbort(task protocol.TaskDispatchPayload) {
	if _, running := c.taskAborts.LoadOrStore(task.TaskID, true); running {
		return
	}
	defer c.taskAborts.Delete(task.TaskID)
	target := task.TaskAbort
	if target == nil || strings.TrimSpace(target.TaskID) == "" || !isAbortableTask(target.Type) {
		_ = c.sendTaskFailed(task, "TASK_ABORT_INVALID", "An abortable target task is required.")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	if err := c.ledger.requestAbort(*target); err != nil {
		_ = c.sendTaskFailed(task, "TASK_ABORT_LEDGER_FAILED", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c.taskRunMu.Lock()
	run := c.taskRuns[target.TaskID]
	if run != nil {
		run.cancel()
	}
	c.taskRunMu.Unlock()
	if run != nil {
		select {
		case <-run.done:
		case <-ctx.Done():
			_ = c.sendTaskFailed(task, "TASK_ABORT_STOP_TIMEOUT", "The agent has not confirmed that task execution stopped. Retry force abort.")
			return
		}
	}
	if err := c.stopTaskObjects(ctx, *target); err != nil {
		_ = c.sendTaskFailed(task, "TASK_ABORT_STOP_FAILED", "Could not confirm that background operations stopped: "+err.Error())
		return
	}
	_ = c.sendTaskCompleted(task, map[string]any{"targetTaskId": target.TaskID, "aborted": true}, "Task execution stopped. Completed changes have not been rolled back.")
}

func (c *Client) stopTaskObjects(ctx context.Context, task protocol.TaskDispatchPayload) error {
	if task.Type == "storage-sync" || task.Type == "schedule-sync" {
		return nil
	}
	if task.ProtectionCleanup != nil || task.RetentionCleanup != nil {
		stopper, ok := c.applier.(interface {
			StopBackupDeletionRequests(context.Context, string, []string) error
		})
		if !ok {
			return errors.New("backup deletion cancellation is unavailable")
		}
		namespace := c.cfg.Namespace
		backupNames := []string{}
		if command := task.ProtectionCleanup; command != nil {
			if command.Namespace != "" {
				namespace = command.Namespace
			}
			for _, point := range command.RestorePoints {
				backupNames = append(backupNames, point.VeleroBackupName)
			}
		}
		if command := task.RetentionCleanup; command != nil {
			for _, point := range command.RestorePoints {
				backupNames = append(backupNames, point.VeleroBackupName)
			}
		}
		return stopper.StopBackupDeletionRequests(ctx, namespace, backupNames)
	}
	deleter, ok := c.applier.(kube.ObjectDeleter)
	if !ok {
		return errors.New("Kubernetes object deletion is unavailable")
	}
	if command := task.Backup; command != nil {
		manifest, err := c.backupExec.BuildBackupManifest(task)
		if err != nil {
			return err
		}
		canceler, ok := c.applier.(kube.BackupVolumeCanceler)
		if !ok {
			return errors.New("backup volume cancellation is unavailable")
		}
		if err := canceler.CancelBackupVolumeOperations(ctx, manifest.Metadata.Namespace, manifest.Metadata.Name); err != nil {
			return err
		}
		return deleter.DeleteObject(ctx, kube.AppliedObject{APIVersion: manifest.APIVersion, Kind: manifest.Kind, Namespace: manifest.Metadata.Namespace, Name: manifest.Metadata.Name})
	}
	if task.Restore != nil {
		manifest, err := c.restoreExec.BuildRestoreManifest(task)
		if err != nil {
			return err
		}
		canceler, ok := c.applier.(kube.RestoreVolumeCanceler)
		if !ok {
			return errors.New("restore volume cancellation is unavailable")
		}
		if err := canceler.CancelRestoreVolumeOperations(ctx, manifest.Metadata.Namespace, manifest.Metadata.Name); err != nil {
			return err
		}
		return deleter.DeleteObject(ctx, kube.AppliedObject{APIVersion: manifest.APIVersion, Kind: manifest.Kind, Namespace: manifest.Metadata.Namespace, Name: manifest.Metadata.Name})
	}
	return fmt.Errorf("unsupported abort task type %q", task.Type)
}
