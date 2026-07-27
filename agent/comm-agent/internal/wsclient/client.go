package wsclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/executor"
	"hypercdr-platform/agent/comm-agent/internal/inventory"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"

	"github.com/gorilla/websocket"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	backupStorageLocationCheckTimeout = 5 * time.Second
	backupStorageLocationRetryCount   = 3
	backupStorageLocationRetryDelay   = time.Second
)

type Client struct {
	cfg            config.Config
	logger         *slog.Logger
	conn           *websocket.Conn
	writeMu        sync.Mutex
	collector      inventory.Collector
	outbox         *eventOutbox
	ledger         *taskLedger
	backupExec     executor.BackupExecutor
	restoreExec    executor.RestoreExecutor
	storageExec    executor.StorageExecutor
	statusReader   kube.ManifestStatusReader
	volumeReader   kube.VolumeProgressReader
	statsReader    kube.BackupObjectStatsReader
	backupReader   kube.VeleroBackupReader
	scheduleReader kube.VeleroScheduleReader
	readiness      kube.RestoreReadinessReader
	applier        kube.ManifestApplier
	deleteWaiter   kube.VeleroBackupDeletionWaiter
	uninstaller    kube.Uninstaller
	agentRuntime   interface {
		kube.AgentRuntimeReader
		kube.AgentUpgrader
		kube.VeleroRuntimeManager
		kube.ComponentLogCollector
	}
	lastInventory     inventory.Snapshot
	backupSamples     map[string][]volumeProgressSample
	backupEvents      map[string]string
	backupContextMu   sync.RWMutex
	backupPlanIDs     map[string]struct{}
	backupTaskIDs     map[string]struct{}
	backupCommandIDs  map[string]struct{}
	activeBackupMu    sync.Mutex
	activeBackupPlans map[string]string
}

type volumeProgressSample struct {
	bytesDone int64
	observed  time.Time
}

func New(cfg config.Config, logger *slog.Logger) *Client {
	return NewWithDependencies(cfg, logger, nil, nil)
}

func NewWithApplier(cfg config.Config, logger *slog.Logger, applier kube.ManifestApplier) *Client {
	return NewWithDependencies(cfg, logger, applier, nil)
}

func NewWithDependencies(cfg config.Config, logger *slog.Logger, applier kube.ManifestApplier, collector inventory.Collector) *Client {
	return NewWithRuntimeDependencies(cfg, logger, applier, collector, nil)
}

func NewWithRuntimeDependencies(cfg config.Config, logger *slog.Logger, applier kube.ManifestApplier, collector inventory.Collector, uninstaller kube.Uninstaller) *Client {
	if collector == nil {
		collector = inventory.NewStaticCollector(cfg)
	}
	statusReader, _ := applier.(kube.ManifestStatusReader)
	volumeReader, _ := applier.(kube.VolumeProgressReader)
	statsReader, _ := applier.(kube.BackupObjectStatsReader)
	backupReader, _ := applier.(kube.VeleroBackupReader)
	scheduleReader, _ := applier.(kube.VeleroScheduleReader)
	readiness, _ := applier.(kube.RestoreReadinessReader)
	deleteWaiter, _ := applier.(kube.VeleroBackupDeletionWaiter)
	outbox, err := newEventOutbox(cfg.StateDir)
	if err != nil && logger != nil {
		logger.Warn("failed to initialize event outbox; terminal events will not survive restart", "state_dir", cfg.StateDir, "error", err)
	}
	ledger, err := newTaskLedger(cfg.StateDir)
	if err != nil && logger != nil {
		logger.Warn("failed to initialize task ledger; velero event filtering will rely on in-memory context", "state_dir", cfg.StateDir, "error", err)
	}
	var agentRuntime interface {
		kube.AgentRuntimeReader
		kube.AgentUpgrader
		kube.VeleroRuntimeManager
		kube.ComponentLogCollector
	}
	if runtime, err := kube.NewKubernetesAgentRuntime(cfg.KubeconfigPath); err == nil {
		agentRuntime = runtime
	}
	return &Client{
		cfg:               cfg,
		logger:            logger,
		collector:         collector,
		outbox:            outbox,
		ledger:            ledger,
		statusReader:      statusReader,
		volumeReader:      volumeReader,
		statsReader:       statsReader,
		backupReader:      backupReader,
		scheduleReader:    scheduleReader,
		readiness:         readiness,
		applier:           applier,
		deleteWaiter:      deleteWaiter,
		uninstaller:       uninstaller,
		agentRuntime:      agentRuntime,
		backupSamples:     map[string][]volumeProgressSample{},
		backupEvents:      map[string]string{},
		backupPlanIDs:     map[string]struct{}{},
		backupTaskIDs:     map[string]struct{}{},
		backupCommandIDs:  map[string]struct{}{},
		activeBackupPlans: map[string]string{},
		backupExec: executor.NewBackupExecutor(executor.Config{
			Mode:           cfg.ExecutorMode,
			AgentNamespace: cfg.Namespace,
			Applier:        applier,
		}),
		restoreExec: executor.NewRestoreExecutor(executor.Config{
			Mode:           cfg.ExecutorMode,
			AgentNamespace: cfg.Namespace,
			Applier:        applier,
		}),
		storageExec: executor.NewStorageExecutor(executor.Config{
			Mode:           cfg.ExecutorMode,
			AgentNamespace: cfg.Namespace,
			Applier:        applier,
		}),
	}
}

func (c *Client) Register() (protocol.RegisterAcceptedPayload, error) {
	if c.cfg.AgentCredential == "" && c.cfg.InstallToken == "" {
		return protocol.RegisterAcceptedPayload{}, errors.New("HCDR_INSTALL_TOKEN or HCDR_AGENT_CREDENTIAL is required for registration")
	}
	if c.cfg.AgentCredential != "" && c.cfg.ClusterID == "" {
		return protocol.RegisterAcceptedPayload{}, errors.New("HCDR_CLUSTER_ID is required when HCDR_AGENT_CREDENTIAL is set")
	}

	dialer := websocket.DefaultDialer
	if c.cfg.PlatformTLSSkipVerify {
		copyDialer := *websocket.DefaultDialer
		copyDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		dialer = &copyDialer
	}
	conn, resp, err := dialer.Dial(c.cfg.PlatformEndpoint, http.Header{})
	if err != nil {
		status := ""
		if resp != nil {
			status = resp.Status
		}
		c.logger.Error("websocket dial failed", "endpoint", c.cfg.PlatformEndpoint, "status", status, "error", err)
		return protocol.RegisterAcceptedPayload{}, err
	}
	c.conn = conn

	register := protocol.NewMessage(protocol.MessageKindRequest, protocol.MessageAgentRegister, c.cfg.ClusterID, c.cfg.AgentID, protocol.RegisterPayload{
		InstallToken:    c.cfg.InstallToken,
		AgentCredential: c.cfg.AgentCredential,
		Cluster: protocol.ClusterSummary{
			Name:        c.cfg.ClusterName,
			KubeVersion: "unknown",
		},
		Agent: protocol.AgentSummary{
			Version:   c.cfg.AgentVersion,
			Namespace: c.cfg.Namespace,
			PodName:   c.cfg.PodName,
		},
		Velero: protocol.VeleroSummary{
			Installed: false,
			Status:    "unknown",
		},
	})

	if err := c.conn.WriteJSON(register); err != nil {
		return protocol.RegisterAcceptedPayload{}, err
	}

	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return protocol.RegisterAcceptedPayload{}, err
	}
	_ = c.conn.SetReadDeadline(time.Time{})

	var meta struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return protocol.RegisterAcceptedPayload{}, err
	}

	switch meta.Type {
	case protocol.MessagePlatformRegisterAccepted:
		var accepted protocol.Message[protocol.RegisterAcceptedPayload]
		if err := json.Unmarshal(data, &accepted); err != nil {
			return protocol.RegisterAcceptedPayload{}, err
		}
		c.cfg.ClusterID = accepted.Payload.ClusterID
		return accepted.Payload, nil
	case protocol.MessagePlatformRegisterRejected:
		var rejected protocol.Message[protocol.RegisterRejectedPayload]
		if err := json.Unmarshal(data, &rejected); err != nil {
			return protocol.RegisterAcceptedPayload{}, err
		}
		return protocol.RegisterAcceptedPayload{}, errors.New(rejected.Payload.Reason + ": " + rejected.Payload.Message)
	default:
		return protocol.RegisterAcceptedPayload{}, errors.New("unexpected platform message type: " + meta.Type)
	}
}

func (c *Client) RunHeartbeat() error {
	if c.conn == nil {
		return errors.New("websocket is not connected")
	}
	defer c.conn.Close()

	interval := c.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	veleroTicker := time.NewTicker(5 * time.Second)
	defer veleroTicker.Stop()
	outboxTicker := time.NewTicker(30 * time.Second)
	defer outboxTicker.Stop()

	if err := c.sendInventory(true); err != nil {
		c.logger.Warn("initial inventory collection failed; heartbeat will continue until inventory recovers", "error", err)
	}
	if err := c.sendHeartbeat(false); err != nil {
		return err
	}
	c.resendPendingEvents()
	go c.recoverLedgerTasks()
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.readMessages()
	}()

	for {
		select {
		case <-ticker.C:
			snapshot, err := c.collector.Collect()
			changed := false
			if err != nil {
				c.logger.Warn("inventory collection failed; heartbeat will continue with last known inventory", "error", err)
			} else {
				changed = snapshot.Hash != c.lastInventory.Hash
				if changed {
					c.lastInventory = snapshot
					if err := c.writeInventory(snapshot.Report); err != nil {
						return err
					}
				}
			}
			if err := c.sendHeartbeat(changed); err != nil {
				return err
			}
		case <-veleroTicker.C:
			if err := c.sendVeleroBackupEvents(context.Background()); err != nil {
				c.logger.Warn("failed to send velero backup events", "error", err)
			}
		case <-outboxTicker.C:
			c.resendPendingEvents()
		case sig := <-stopCh:
			c.logger.Info("stop signal received", "signal", sig.String())
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func (c *Client) readMessages() error {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var meta struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return err
		}
		switch meta.Type {
		case protocol.MessagePlatformTaskDispatch:
			var dispatch protocol.Message[protocol.TaskDispatchPayload]
			if err := json.Unmarshal(data, &dispatch); err != nil {
				return err
			}
			dispatch.Payload.RequestMessageID = dispatch.MessageID
			go c.executeTask(dispatch.Payload)
		case protocol.MessagePlatformInventoryRequest:
			var request protocol.Message[protocol.InventoryRequestPayload]
			if err := json.Unmarshal(data, &request); err != nil {
				return err
			}
			go c.handleInventoryRequest(request)
		case protocol.MessagePlatformLogRequest:
			var request protocol.Message[protocol.LogRequestPayload]
			if err := json.Unmarshal(data, &request); err != nil {
				return err
			}
			go c.handleLogRequest(request)
		case protocol.MessagePlatformTaskCancel:
			var request protocol.Message[protocol.TaskCancelPayload]
			if err := json.Unmarshal(data, &request); err != nil {
				return err
			}
			if err := c.sendMessageError(request.MessageID, protocol.MessagePlatformTaskCancel, "", request.Payload.TaskID, request.Payload.CommandID, "UNSUPPORTED_MESSAGE", "task cancellation is not implemented", false); err != nil {
				c.logger.Warn("failed to send task cancel unsupported response", "task_id", request.Payload.TaskID, "error", err)
			}
		case protocol.MessagePlatformEventAck:
			var ack protocol.Message[protocol.EventAckPayload]
			if err := json.Unmarshal(data, &ack); err != nil {
				return err
			}
			_ = c.outbox.remove(ack.Payload.AckMessageID)
			_ = c.ledger.markTerminalAcked(ack.Payload.TaskID)
			c.logger.Info("platform acknowledged event", "ack_message_id", ack.Payload.AckMessageID, "ack_type", ack.Payload.AckType, "task_id", ack.Payload.TaskID)
		case protocol.MessagePlatformEventError:
			var eventError protocol.Message[protocol.EventErrorPayload]
			if err := json.Unmarshal(data, &eventError); err != nil {
				return err
			}
			if !eventError.Payload.Retryable {
				_ = c.outbox.remove(eventError.Payload.AckMessageID)
			}
			c.logger.Warn("platform rejected event", "ack_message_id", eventError.Payload.AckMessageID, "ack_type", eventError.Payload.AckType, "task_id", eventError.Payload.TaskID, "error_code", eventError.Payload.ErrorCode, "retryable", eventError.Payload.Retryable)
		default:
			c.logger.Info("platform message ignored", "type", meta.Type)
		}
	}
}

func (c *Client) handleLogRequest(request protocol.Message[protocol.LogRequestPayload]) {
	report := protocol.LogReportPayload{RequestID: request.Payload.RequestID, Component: request.Payload.Component, Entries: []protocol.LogEntry{}}
	if c.agentRuntime == nil {
		report.ErrorCode = "LOG_COLLECTION_UNAVAILABLE"
		report.Message = "Kubernetes log collector is unavailable"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		entries, truncated, err := c.agentRuntime.CollectComponentLogs(ctx, c.cfg.Namespace, request.Payload.Component, request.Payload.Since, request.Payload.TailLines)
		if err != nil {
			report.ErrorCode = "LOG_COLLECTION_FAILED"
			report.Message = err.Error()
		} else {
			report.Truncated = truncated
			for _, entry := range entries {
				report.Entries = append(report.Entries, protocol.LogEntry{Timestamp: entry.Timestamp, Level: inferLogLevel(entry.Message), Component: request.Payload.Component, Pod: entry.Pod, Node: entry.Node, Message: entry.Message})
			}
		}
	}
	message := protocol.NewMessage(protocol.MessageKindResponse, protocol.MessageAgentLogReport, c.cfg.ClusterID, c.cfg.AgentID, report)
	if err := c.writeJSON(message); err != nil {
		c.logger.Warn("failed to send component log report", "request_id", request.Payload.RequestID, "component", request.Payload.Component, "error", err)
	}
}

func inferLogLevel(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, `"level":"error"`) || strings.Contains(lower, " error ") {
		return "error"
	}
	if strings.Contains(lower, `"level":"warn"`) || strings.Contains(lower, " warning ") {
		return "warning"
	}
	if strings.Contains(lower, `"level":"debug"`) {
		return "debug"
	}
	return "info"
}

func (c *Client) handleInventoryRequest(request protocol.Message[protocol.InventoryRequestPayload]) {
	snapshot, err := c.collector.Collect()
	if err != nil {
		if writeErr := c.sendMessageError(request.MessageID, protocol.MessagePlatformInventoryRequest, request.Payload.RequestID, "", "", "INVENTORY_COLLECT_FAILED", err.Error(), true); writeErr != nil {
			c.logger.Warn("failed to send inventory request error response", "request_id", request.Payload.RequestID, "error", writeErr)
		}
		return
	}
	report := snapshot.Report
	report.AckRequired = false
	report.AckMessageID = request.MessageID
	report.AckType = protocol.MessagePlatformInventoryRequest
	report.RequestID = request.Payload.RequestID
	report.Scope = request.Payload.Scope
	if report.Scope == "" {
		report.Scope = "summary"
	}
	report.Reason = request.Payload.Reason
	report.Namespace = request.Payload.Namespace
	report.Velero.RecentBackups = c.filterInventoryBackups(report.Velero.RecentBackups)
	c.lastInventory = snapshot
	message := protocol.NewMessage(protocol.MessageKindResponse, protocol.MessageAgentInventoryReport, c.cfg.ClusterID, c.cfg.AgentID, report)
	if err := c.writeJSON(message); err != nil {
		c.logger.Warn("failed to send inventory request response", "request_id", request.Payload.RequestID, "error", err)
		return
	}
	c.logger.Info("inventory request responded",
		"request_id", request.Payload.RequestID,
		"scope", report.Scope,
		"namespace", report.Namespace,
		"inventory_hash", report.InventoryHash,
	)
}

func (c *Client) sendMessageError(ackMessageID string, ackType string, requestID string, taskID string, commandID string, code string, text string, retryable bool) error {
	message := protocol.NewMessage(protocol.MessageKindResponse, protocol.MessageAgentMessageError, c.cfg.ClusterID, c.cfg.AgentID, protocol.MessageErrorPayload{
		AckMessageID: ackMessageID,
		AckType:      ackType,
		RequestID:    requestID,
		TaskID:       taskID,
		CommandID:    commandID,
		ErrorCode:    code,
		Message:      text,
		Retryable:    retryable,
	})
	return c.writeJSON(message)
}

func (c *Client) resendPendingEvents() {
	for _, raw := range c.outbox.list() {
		if err := c.writeRawJSON(raw); err != nil {
			c.logger.Warn("failed to resend pending event", "error", err)
			return
		}
	}
}

func (c *Client) recoverLedgerTasks() {
	if c.statusReader == nil {
		return
	}
	for _, record := range c.ledger.recoverableRecords() {
		if c.outbox.hasTask(record.TaskID) {
			continue
		}
		var task protocol.TaskDispatchPayload
		if err := json.Unmarshal(record.Task, &task); err != nil {
			c.logger.Warn("failed to decode ledger task", "task_id", record.TaskID, "error", err)
			continue
		}
		object := kube.AppliedObject{
			APIVersion: record.Object.APIVersion,
			Kind:       record.Object.Kind,
			Namespace:  record.Object.Namespace,
			Name:       record.Object.Name,
		}
		c.logger.Info("recovering ledger task observation", "task_id", task.TaskID, "type", task.Type, "kind", object.Kind, "name", object.Name)
		switch task.Type {
		case "backup":
			c.markBackupContext(task)
			go c.pollVeleroStatus(task, object, manifestPayload(object.Kind, object.Name, object.Namespace), backupStatusResult)
		case "restore", "drill", "takeover":
			go c.pollRestoreStatus(task, object, manifestPayload(object.Kind, object.Name, object.Namespace))
		default:
			c.logger.Info("ledger task type is not recoverable by status polling", "task_id", task.TaskID, "type", task.Type)
		}
	}
}

func (c *Client) executeTask(task protocol.TaskDispatchPayload) {
	c.logger.Info("task received", "task_id", task.TaskID, "type", task.Type, "command_id", task.CommandID)
	switch task.Type {
	case "backup":
		c.executeBackupTask(task)
	case "backup-cancel":
		c.executeBackupCancelTask(task)
	case "restore", "drill", "takeover":
		c.executeRestoreTask(task)
	case "storage-sync":
		c.executeStorageSyncTask(task)
	case "retention-cleanup":
		c.executeRetentionCleanupTask(task)
	case "protection-cleanup":
		c.executeProtectionCleanupTask(task)
	case "agent-upgrade":
		c.executeAgentUpgradeTask(task)
	case "velero-upgrade":
		c.executeVeleroUpgradeTask(task)
	case "unregister":
		c.executeUnregisterTask(task)
	default:
		_ = c.sendTaskFailed(task, "TASK_UNSUPPORTED", "unsupported task type: "+task.Type)
	}
}

func (c *Client) executeAgentUpgradeTask(task protocol.TaskDispatchPayload) {
	if task.AgentUpgrade == nil {
		_ = c.sendTaskFailed(task, "AGENT_UPGRADE_COMMAND_INVALID", "agent upgrade command is required")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	if c.agentRuntime == nil {
		_ = c.sendTaskFailed(task, "AGENT_UPGRADER_NOT_CONFIGURED", "agent upgrader is not configured")
		return
	}
	command := task.AgentUpgrade
	namespace := command.Namespace
	if namespace == "" {
		namespace = c.cfg.Namespace
	}
	if namespace == "" {
		namespace = "hypercdr-agent"
	}
	deploymentName := command.DeploymentName
	if deploymentName == "" {
		deploymentName = "hypercdr-comm-agent"
	}
	containerName := command.ContainerName
	if containerName == "" {
		containerName = "comm-agent"
	}
	if err := c.sendTaskProgress(task, map[string]any{
		"kind":       "AgentUpgrade",
		"name":       deploymentName,
		"namespace":  namespace,
		"image":      command.Image,
		"targetHash": command.ExpectedDigest,
	}, 20, "agent upgrade accepted; updating deployment"); err != nil {
		c.logger.Error("failed to send agent upgrade progress", "task_id", task.TaskID, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.agentRuntime.UpgradeAgent(ctx, kube.AgentUpgradeOptions{
		Namespace:         namespace,
		DeploymentName:    deploymentName,
		ContainerName:     containerName,
		Image:             command.Image,
		Version:           command.Version,
		RolloutAnnotation: command.RolloutAnnotation,
	}); err != nil {
		c.logger.Error("agent upgrade failed", "task_id", task.TaskID, "error", err)
		_ = c.sendTaskFailedWithDetails(task, "AGENT_UPGRADE_FAILED", "agent deployment update failed", map[string]any{
			"error":      err.Error(),
			"namespace":  namespace,
			"deployment": deploymentName,
			"image":      command.Image,
		})
		return
	}
	_ = c.sendTaskProgress(task, map[string]any{
		"kind":      "AgentUpgrade",
		"name":      deploymentName,
		"namespace": namespace,
		"image":     command.Image,
	}, 80, "agent deployment updated; waiting for reconnect")
	_ = c.sendTaskCompleted(task, map[string]any{
		"kind":           "AgentUpgrade",
		"name":           deploymentName,
		"namespace":      namespace,
		"image":          command.Image,
		"expectedDigest": command.ExpectedDigest,
	}, "agent deployment upgrade submitted")
}

func (c *Client) executeVeleroUpgradeTask(task protocol.TaskDispatchPayload) {
	if task.VeleroUpgrade == nil {
		_ = c.sendTaskFailed(task, "VELERO_UPGRADE_COMMAND_INVALID", "velero upgrade command is required")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	if c.agentRuntime == nil {
		_ = c.sendTaskFailed(task, "VELERO_UPGRADER_NOT_CONFIGURED", "velero upgrader is not configured")
		return
	}
	command := task.VeleroUpgrade
	_ = c.sendTaskProgress(task, map[string]any{"kind": "VeleroUpgrade", "image": command.Image}, 15, "velero upgrade accepted; updating server and node agents")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.agentRuntime.UpgradeVelero(ctx, kube.VeleroUpgradeOptions{
		Namespace: command.Namespace, Image: command.Image, DeploymentName: command.DeploymentName, DaemonSetName: command.DaemonSetName,
		AWSPluginImage: command.AWSPluginImage, AzurePluginImage: command.AzurePluginImage, GCPPluginImage: command.GCPPluginImage,
	}); err != nil {
		_ = c.sendTaskFailedWithDetails(task, "VELERO_UPGRADE_FAILED", "velero rollout failed", map[string]any{"error": err.Error(), "image": command.Image})
		return
	}
	_ = c.sendTaskProgress(task, map[string]any{"kind": "VeleroUpgrade", "image": command.Image}, 85, "velero server and all scheduled node agents are ready; verifying image digest")
	_ = c.sendTaskCompleted(task, map[string]any{"kind": "VeleroUpgrade", "image": command.Image, "expectedDigest": command.ExpectedDigest}, "velero rollout completed; waiting for heartbeat verification")
}

func (c *Client) executeUnregisterTask(task protocol.TaskDispatchPayload) {
	if task.Unregister == nil {
		_ = c.sendTaskFailed(task, "UNREGISTER_COMMAND_INVALID", "unregister command is required")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	if err := c.sendTaskProgress(task, map[string]any{
		"kind":      "Unregister",
		"name":      c.cfg.ClusterID,
		"namespace": task.Unregister.Namespace,
	}, 40, "agent unregister acknowledged; cluster-side cleanup is starting"); err != nil {
		c.logger.Error("failed to send unregister progress", "task_id", task.TaskID, "error", err)
		return
	}
	if c.uninstaller == nil {
		_ = c.sendTaskFailed(task, "UNINSTALLER_NOT_CONFIGURED", "cluster-side uninstaller is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := c.uninstaller.Uninstall(ctx, kube.UninstallOptions{
		Namespace:       task.Unregister.Namespace,
		DeleteVelero:    task.Unregister.DeleteVelero,
		DeleteNamespace: task.Unregister.DeleteNamespace,
	}); err != nil {
		c.logger.Error("cluster-side unregister cleanup failed", "task_id", task.TaskID, "error", err)
		_ = c.sendTaskFailedWithDetails(task, "UNREGISTER_CLEANUP_FAILED", "cluster-side cleanup failed", map[string]any{
			"error":     err.Error(),
			"namespace": task.Unregister.Namespace,
		})
		return
	}
	if err := c.sendTaskCompleted(task, map[string]any{
		"kind":            "Unregister",
		"name":            c.cfg.ClusterID,
		"namespace":       task.Unregister.Namespace,
		"deleteVelero":    task.Unregister.DeleteVelero,
		"deleteNamespace": task.Unregister.DeleteNamespace,
	}, "cluster-side cleanup request completed"); err != nil {
		c.logger.Error("failed to send unregister completed", "task_id", task.TaskID, "error", err)
		return
	}
	c.logger.Info("cluster-side unregister cleanup completed", "namespace", task.Unregister.Namespace)
}

func (c *Client) executeBackupTask(task protocol.TaskDispatchPayload) {
	if task.Backup == nil {
		_ = c.sendTaskFailed(task, "BACKUP_COMMAND_INVALID", "backup command is required")
		return
	}
	releasePlan, ok := c.acquireBackupPlan(task)
	if !ok {
		return
	}
	defer releasePlan()
	if err := c.requireBackupStorageLocation(context.Background(), task.Backup.StorageRepo); err != nil {
		_ = c.sendTaskFailedWithDetails(task, "BSL_NOT_READY", err.Error(), map[string]any{
			"storageRepo": task.Backup.StorageRepo,
		})
		return
	}
	manifest, err := c.backupExec.BuildBackupManifest(task)
	if err != nil {
		_ = c.sendTaskFailed(task, "BACKUP_MANIFEST_INVALID", err.Error())
		return
	}
	c.markBackupContext(task)
	ensureSourceClusterLabel(manifest.Metadata.Labels, c.cfg.ClusterID)
	if err := c.ledger.recordTask(task.TaskID, task.CommandID, backupTaskPlanID(task), task, taskLedgerObject{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Namespace:  manifest.Metadata.Namespace,
		Name:       manifest.Metadata.Name,
	}); err != nil {
		_ = c.sendTaskFailed(task, "LOCAL_LEDGER_WRITE_FAILED", "failed to persist task before accepting: "+err.Error())
		return
	}

	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	if existing, ok, err := c.findConflictingActiveBackup(context.Background(), task, manifest); err != nil {
		_ = c.sendTaskFailedWithDetails(task, "BACKUP_CONFLICT_CHECK_FAILED", err.Error(), map[string]any{
			"namespace": task.Backup.SourceNamespace,
			"planId":    task.Backup.PlanID,
		})
		return
	} else if ok {
		_ = c.sendVeleroBackupEvent(existing)
		_ = c.sendTaskFailedWithDetails(task, "BACKUP_ALREADY_RUNNING", "A backup is already running for this protection plan.", map[string]any{
			"backupName": existing.Name,
			"phase":      existing.Phase,
			"startedAt":  existing.StartedAt,
			"planId":     task.Backup.PlanID,
		})
		return
	}
	if err := c.submitBackupWithConfirmation(context.Background(), manifest); err != nil {
		_ = c.sendTaskFailed(task, "BACKUP_SUBMIT_FAILED", err.Error())
		return
	}
	if c.statusReader != nil {
		c.pollVeleroStatus(task, kube.AppliedObject{
			APIVersion: manifest.APIVersion,
			Kind:       manifest.Kind,
			Namespace:  manifest.Metadata.Namespace,
			Name:       manifest.Metadata.Name,
		}, backupVeleroPayload(manifest, true), backupStatusResult)
		return
	}
	steps := []struct {
		progress int
		message  string
	}{
		{15, "backup command accepted"},
		{45, "Backup operation prepared."},
		{75, "backup data transfer running"},
	}
	for _, step := range steps {
		time.Sleep(500 * time.Millisecond)
		if err := c.sendTaskProgress(task, backupVeleroPayload(manifest, step.progress >= 45), step.progress, step.message); err != nil {
			c.logger.Error("failed to send task progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	time.Sleep(500 * time.Millisecond)
	if err := c.sendTaskCompleted(task, backupVeleroPayload(manifest, true), "backup dry-run completed"); err != nil {
		c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
		return
	}
}

func (c *Client) executeBackupCancelTask(task protocol.TaskDispatchPayload) {
	if task.BackupCancel == nil {
		_ = c.sendTaskFailed(task, "BACKUP_CANCEL_COMMAND_INVALID", "backup cancel command is required")
		return
	}
	if c.applier == nil {
		_ = c.sendTaskFailed(task, "BACKUP_CANCEL_APPLIER_UNAVAILABLE", "kubernetes manifest applier is not configured")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send backup cancel accepted", "task_id", task.TaskID, "error", err)
		return
	}
	backupName := strings.TrimSpace(task.BackupCancel.VeleroBackupName)
	if backupName == "" {
		found, ok, err := c.findActiveBackupNameForCancel(context.Background(), task.BackupCancel.PlanID)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "BACKUP_CANCEL_LOOKUP_FAILED", err.Error(), map[string]any{
				"planId":       task.BackupCancel.PlanID,
				"targetTaskId": task.BackupCancel.TargetTaskID,
			})
			return
		}
		if ok {
			backupName = found
		}
	}
	if backupName == "" {
		if err := c.sendTaskCompleted(task, map[string]any{
			"kind":          "BackupCancel",
			"targetTaskId":  task.BackupCancel.TargetTaskID,
			"planId":        task.BackupCancel.PlanID,
			"backupMissing": true,
			"deleted":       false,
		}, "sync force stop completed; no active Velero backup was found"); err != nil {
			c.logger.Error("failed to send backup cancel completed", "task_id", task.TaskID, "error", err)
		}
		return
	}
	namespace := c.cfg.Namespace
	manifest, err := velero.BuildDeleteBackupRequestManifest(namespace, backupName)
	if err != nil {
		_ = c.sendTaskFailed(task, "BACKUP_CANCEL_MANIFEST_INVALID", err.Error())
		return
	}
	raw, err := kube.ManifestFromStruct(manifest)
	if err != nil {
		_ = c.sendTaskFailed(task, "BACKUP_CANCEL_MANIFEST_INVALID", err.Error())
		return
	}
	if err := c.sendTaskProgress(task, map[string]any{
		"kind":         "DeleteBackupRequest",
		"name":         manifest.Metadata.Name,
		"namespace":    manifest.Metadata.Namespace,
		"backupName":   backupName,
		"targetTaskId": task.BackupCancel.TargetTaskID,
	}, 30, "submitting Velero backup delete request"); err != nil {
		c.logger.Error("failed to send backup cancel progress", "task_id", task.TaskID, "error", err)
		return
	}
	if _, err := c.applier.ApplyManifest(context.Background(), raw); err != nil {
		_ = c.sendTaskFailedWithDetails(task, "BACKUP_CANCEL_SUBMIT_FAILED", err.Error(), map[string]any{
			"backupName":   backupName,
			"targetTaskId": task.BackupCancel.TargetTaskID,
		})
		return
	}
	if c.deleteWaiter != nil {
		if err := c.sendTaskProgress(task, map[string]any{
			"kind":         "DeleteBackupRequest",
			"name":         manifest.Metadata.Name,
			"namespace":    manifest.Metadata.Namespace,
			"backupName":   backupName,
			"targetTaskId": task.BackupCancel.TargetTaskID,
		}, 70, "waiting for Velero backup deletion"); err != nil {
			c.logger.Error("failed to send backup cancel progress", "task_id", task.TaskID, "error", err)
			return
		}
		if err := c.deleteWaiter.WaitForVeleroBackupDeleted(context.Background(), namespace, backupName, 10*time.Minute); err != nil {
			_ = c.sendTaskFailedWithDetails(task, "BACKUP_CANCEL_DELETE_FAILED", err.Error(), map[string]any{
				"backupName":   backupName,
				"targetTaskId": task.BackupCancel.TargetTaskID,
			})
			return
		}
	}
	c.releaseActiveBackupPlanForCancel(task.BackupCancel.PlanID, task.BackupCancel.TargetTaskID)
	if err := c.sendTaskCompleted(task, map[string]any{
		"kind":          "BackupCancel",
		"targetTaskId":  task.BackupCancel.TargetTaskID,
		"planId":        task.BackupCancel.PlanID,
		"backupName":    backupName,
		"deleteRequest": manifest.Metadata.Name,
		"deleted":       true,
	}, "sync force stop completed"); err != nil {
		c.logger.Error("failed to send backup cancel completed", "task_id", task.TaskID, "error", err)
	}
}

func (c *Client) releaseActiveBackupPlanForCancel(planID string, targetTaskID string) {
	planID = strings.TrimSpace(planID)
	if planID == "" || targetTaskID == "" {
		return
	}
	c.activeBackupMu.Lock()
	defer c.activeBackupMu.Unlock()
	if c.activeBackupPlans[planID] == targetTaskID {
		delete(c.activeBackupPlans, planID)
	}
}

func (c *Client) acquireBackupPlan(task protocol.TaskDispatchPayload) (func(), bool) {
	planID := backupTaskPlanID(task)
	if planID == "" {
		return func() {}, true
	}
	c.activeBackupMu.Lock()
	if runningTaskID := c.activeBackupPlans[planID]; runningTaskID != "" && runningTaskID != task.TaskID {
		c.activeBackupMu.Unlock()
		_ = c.sendTaskFailedWithDetails(task, "BACKUP_ALREADY_RUNNING", "A backup is already running for this protection plan.", map[string]any{
			"planId":        planID,
			"runningTaskId": runningTaskID,
		})
		return nil, false
	}
	c.activeBackupPlans[planID] = task.TaskID
	c.activeBackupMu.Unlock()
	return func() {
		c.activeBackupMu.Lock()
		if c.activeBackupPlans[planID] == task.TaskID {
			delete(c.activeBackupPlans, planID)
		}
		c.activeBackupMu.Unlock()
	}, true
}

func (c *Client) submitBackupWithConfirmation(ctx context.Context, manifest velero.BackupManifest) error {
	object := kube.AppliedObject{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Namespace:  manifest.Metadata.Namespace,
		Name:       manifest.Metadata.Name,
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.backupExec.SubmitBackup(ctx, manifest); err == nil {
			return nil
		} else {
			lastErr = err
			if c.backupManifestExists(ctx, object) {
				c.logger.Warn("backup submit returned error but Backup CR exists; continuing status polling", "backup", object.Name, "error", err)
				return nil
			}
			if !isUncertainKubernetesSubmitError(err) && attempt == 0 {
				return err
			}
		}
		for i := 0; i < 4; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i+1) * time.Second):
			}
			if c.backupManifestExists(ctx, object) {
				c.logger.Warn("Backup CR appeared after submit error; continuing status polling", "backup", object.Name, "error", lastErr)
				return nil
			}
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("backup submit failed")
}

func (c *Client) backupManifestExists(ctx context.Context, object kube.AppliedObject) bool {
	if c.statusReader == nil {
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.statusReader.GetManifestStatus(checkCtx, object)
	return err == nil
}

func isUncertainKubernetesSubmitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	needles := []string{
		"etcdserver: request timed out",
		"request timed out",
		"resource quota evaluation timed out",
		"context deadline exceeded",
		"connect: connection refused",
		"http2: server sent goaway",
		"unexpected eof",
		"timeout",
		"temporarily unavailable",
		"connection reset",
	}
	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func (c *Client) executeRestoreTask(task protocol.TaskDispatchPayload) {
	if task.Restore == nil {
		_ = c.sendTaskFailed(task, "RESTORE_COMMAND_INVALID", "restore command is required")
		return
	}
	if err := c.requireBackupStorageLocation(context.Background(), task.Restore.StorageRepo); err != nil {
		_ = c.sendTaskFailedWithDetails(task, "BSL_NOT_READY", err.Error(), map[string]any{
			"storageRepo": task.Restore.StorageRepo,
		})
		return
	}
	manifest, err := c.restoreExec.BuildRestoreManifest(task)
	if err != nil {
		_ = c.sendTaskFailed(task, "RESTORE_MANIFEST_INVALID", err.Error())
		return
	}
	if err := c.ledger.recordTask(task.TaskID, task.CommandID, "", task, taskLedgerObject{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Namespace:  manifest.Metadata.Namespace,
		Name:       manifest.Metadata.Name,
	}); err != nil {
		_ = c.sendTaskFailed(task, "LOCAL_LEDGER_WRITE_FAILED", "failed to persist task before accepting: "+err.Error())
		return
	}

	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	if err := c.restoreExec.SubmitRestore(context.Background(), manifest); err != nil {
		_ = c.sendTaskFailed(task, "RESTORE_SUBMIT_FAILED", err.Error())
		return
	}
	if c.statusReader != nil {
		payload := restoreVeleroPayload(manifest, true)
		payload["storageLocation"] = task.Restore.StorageRepo
		c.pollRestoreStatus(task, kube.AppliedObject{
			APIVersion: manifest.APIVersion,
			Kind:       manifest.Kind,
			Namespace:  manifest.Metadata.Namespace,
			Name:       manifest.Metadata.Name,
		}, payload)
		return
	}
	steps := []struct {
		progress int
		message  string
	}{
		{15, task.Type + " command accepted"},
		{45, "Restore operation prepared."},
		{75, task.Type + " resources running"},
	}
	for _, step := range steps {
		time.Sleep(500 * time.Millisecond)
		if err := c.sendTaskProgress(task, restoreVeleroPayload(manifest, step.progress >= 45), step.progress, step.message); err != nil {
			c.logger.Error("failed to send task progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	time.Sleep(500 * time.Millisecond)
	if err := c.sendTaskCompleted(task, restoreVeleroPayload(manifest, true), task.Type+" dry-run completed"); err != nil {
		c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
		return
	}
}

func (c *Client) executeStorageSyncTask(task protocol.TaskDispatchPayload) {
	if task.StorageSync == nil {
		_ = c.sendTaskFailed(task, "STORAGE_SYNC_COMMAND_INVALID", "storage sync command is required")
		return
	}
	manifests, err := c.storageExec.BuildStorageManifests(task)
	if err != nil {
		_ = c.sendTaskFailed(task, "BSL_MANIFEST_INVALID", err.Error())
		return
	}

	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	if err := c.storageExec.SubmitStorage(context.Background(), manifests); err != nil {
		_ = c.sendTaskFailed(task, "BSL_SUBMIT_FAILED", err.Error())
		return
	}
	if c.statusReader != nil {
		bsl := manifests.BackupStorageLocation
		c.pollVeleroStatus(task, kube.AppliedObject{
			APIVersion: bsl.APIVersion,
			Kind:       bsl.Kind,
			Namespace:  bsl.Metadata.Namespace,
			Name:       bsl.Metadata.Name,
		}, storageVeleroPayload(bsl, true), storageStatusResult)
		return
	}
	steps := []struct {
		progress int
		message  string
	}{
		{25, "storage repository sync accepted"},
		{60, "velero backup storage location manifest generated"},
	}
	for _, step := range steps {
		time.Sleep(500 * time.Millisecond)
		if err := c.sendTaskProgress(task, storageVeleroPayload(manifests.BackupStorageLocation, step.progress >= 60), step.progress, step.message); err != nil {
			c.logger.Error("failed to send task progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	time.Sleep(500 * time.Millisecond)
	if err := c.sendTaskCompleted(task, storageVeleroPayload(manifests.BackupStorageLocation, true), "storage repository sync dry-run completed"); err != nil {
		c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
		return
	}
}

func (c *Client) executeRetentionCleanupTask(task protocol.TaskDispatchPayload) {
	if task.RetentionCleanup == nil {
		_ = c.sendTaskFailed(task, "RETENTION_CLEANUP_COMMAND_INVALID", "retention cleanup command is required")
		return
	}
	if c.applier == nil {
		_ = c.sendTaskFailed(task, "RETENTION_APPLIER_UNAVAILABLE", "kubernetes manifest applier is not configured")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	deleted := []string{}
	for _, point := range task.RetentionCleanup.RestorePoints {
		namespace := point.Namespace
		if namespace == "" {
			namespace = c.cfg.Namespace
		}
		manifest, err := velero.BuildDeleteBackupRequestManifest(namespace, point.VeleroBackupName)
		if err != nil {
			_ = c.sendTaskFailed(task, "RETENTION_DELETE_MANIFEST_INVALID", err.Error())
			return
		}
		raw, err := kube.ManifestFromStruct(manifest)
		if err != nil {
			_ = c.sendTaskFailed(task, "RETENTION_DELETE_MANIFEST_INVALID", err.Error())
			return
		}
		if _, err := c.applier.ApplyManifest(context.Background(), raw); err != nil {
			_ = c.sendTaskFailedWithDetails(task, "RETENTION_DELETE_SUBMIT_FAILED", err.Error(), map[string]any{
				"restorePointId": point.ID,
				"backupName":     point.VeleroBackupName,
			})
			return
		}
		if c.deleteWaiter != nil {
			if err := c.deleteWaiter.WaitForVeleroBackupDeleted(context.Background(), namespace, point.VeleroBackupName, 10*time.Minute); err != nil {
				_ = c.sendTaskFailedWithDetails(task, "RETENTION_DELETE_FAILED", err.Error(), map[string]any{
					"restorePointId": point.ID,
					"backupName":     point.VeleroBackupName,
				})
				return
			}
		}
		deleted = append(deleted, point.ID)
		if err := c.sendTaskProgress(task, map[string]any{
			"kind":      "DeleteBackupRequest",
			"name":      manifest.Metadata.Name,
			"namespace": manifest.Metadata.Namespace,
			"deleted":   deleted,
		}, retentionCleanupProgress(len(deleted), len(task.RetentionCleanup.RestorePoints)), "retention cleanup deleting expired restore points"); err != nil {
			c.logger.Error("failed to send retention cleanup progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	if err := c.sendTaskCompleted(task, map[string]any{
		"kind":    "RetentionCleanup",
		"planId":  task.RetentionCleanup.PlanID,
		"deleted": deleted,
	}, "retention cleanup completed"); err != nil {
		c.logger.Error("failed to send retention cleanup completed", "task_id", task.TaskID, "error", err)
	}
}

func (c *Client) executeProtectionCleanupTask(task protocol.TaskDispatchPayload) {
	if task.ProtectionCleanup == nil {
		_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_COMMAND_INVALID", "protection cleanup command is required")
		return
	}
	if c.applier == nil {
		_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_APPLIER_UNAVAILABLE", "kubernetes manifest applier is not configured")
		return
	}
	deleter, ok := c.applier.(kube.ObjectDeleter)
	if !ok {
		_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_DELETER_UNAVAILABLE", "kubernetes object deleter is not configured")
		return
	}
	cleaner, _ := c.applier.(kube.VeleroProtectionCleaner)
	if err := c.sendTaskAccepted(task); err != nil {
		c.logger.Error("failed to send task accepted", "task_id", task.TaskID, "error", err)
		return
	}
	namespace := task.ProtectionCleanup.Namespace
	if namespace == "" {
		namespace = c.cfg.Namespace
	}
	mode := strings.TrimSpace(task.ProtectionCleanup.CleanupMode)
	if mode == "" {
		mode = "source"
	}
	deleted := []string{}
	total := len(task.ProtectionCleanup.RestorePoints)
	backupNames := make([]string, 0, total)
	for _, point := range task.ProtectionCleanup.RestorePoints {
		if point.VeleroBackupName != "" {
			backupNames = append(backupNames, point.VeleroBackupName)
		}
		if mode == "target" {
			continue
		}
		pointNamespace := point.Namespace
		if pointNamespace == "" {
			pointNamespace = namespace
		}
		manifest, err := velero.BuildDeleteBackupRequestManifest(pointNamespace, point.VeleroBackupName)
		if err != nil {
			_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_DELETE_MANIFEST_INVALID", err.Error())
			return
		}
		raw, err := kube.ManifestFromStruct(manifest)
		if err != nil {
			_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_DELETE_MANIFEST_INVALID", err.Error())
			return
		}
		if _, err := c.applier.ApplyManifest(context.Background(), raw); err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_DELETE_SUBMIT_FAILED", err.Error(), map[string]any{
				"restorePointId": point.ID,
				"backupName":     point.VeleroBackupName,
			})
			return
		}
		if c.deleteWaiter != nil {
			if err := c.deleteWaiter.WaitForVeleroBackupDeleted(context.Background(), pointNamespace, point.VeleroBackupName, 10*time.Minute); err != nil {
				_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_BACKUP_DELETE_FAILED", err.Error(), map[string]any{
					"restorePointId": point.ID,
					"backupName":     point.VeleroBackupName,
				})
				return
			}
		}
		deleted = append(deleted, point.ID)
		progress := 100
		if total > 0 {
			progress = 20 + int(float64(len(deleted))/float64(total)*80)
		}
		if err := c.sendTaskProgress(task, map[string]any{
			"kind":      "DeleteBackupRequest",
			"planId":    task.ProtectionCleanup.PlanID,
			"name":      manifest.Metadata.Name,
			"namespace": manifest.Metadata.Namespace,
			"deleted":   deleted,
		}, progress, "protection cleanup deleting restore point data"); err != nil {
			c.logger.Error("failed to send protection cleanup progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	if mode == "target" {
		if cleaner == nil {
			_ = c.sendTaskFailed(task, "PROTECTION_CLEANUP_CLEANER_UNAVAILABLE", "velero protection cleaner is not configured")
			return
		}
		crDeleted, err := cleaner.DeleteVeleroBackupArtifacts(context.Background(), namespace, backupNames)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_TARGET_CR_DELETE_FAILED", err.Error(), map[string]any{
				"backupNames": backupNames,
			})
			return
		}
		prefixCRDeleted, err := cleaner.DeleteVeleroBackupArtifactsByNamePrefix(context.Background(), namespace, task.ProtectionCleanup.BackupNamePrefix)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_TARGET_PREFIX_CR_DELETE_FAILED", err.Error(), map[string]any{
				"backupNamePrefix": task.ProtectionCleanup.BackupNamePrefix,
			})
			return
		}
		repoDeleted, err := cleaner.DeleteBackupRepositories(context.Background(), namespace, task.ProtectionCleanup.StorageRepo, task.ProtectionCleanup.SourceNamespaces)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_TARGET_REPOSITORY_DELETE_FAILED", err.Error(), map[string]any{
				"storageRepo":      task.ProtectionCleanup.StorageRepo,
				"sourceNamespaces": task.ProtectionCleanup.SourceNamespaces,
			})
			return
		}
		if err := c.sendTaskCompleted(task, map[string]any{
			"kind":                "ProtectionCleanup",
			"mode":                mode,
			"planId":              task.ProtectionCleanup.PlanID,
			"deleted":             deleted,
			"deletedVeleroCRs":    crDeleted,
			"deletedPrefixCRs":    prefixCRDeleted,
			"deletedRepositories": repoDeleted,
		}, "target protection resources cleaned"); err != nil {
			c.logger.Error("failed to send protection cleanup completed", "task_id", task.TaskID, "error", err)
		}
		return
	}
	if task.ProtectionCleanup.ScheduleName != "" {
		if err := deleter.DeleteObject(context.Background(), kube.AppliedObject{
			APIVersion: "velero.io/v1",
			Kind:       "Schedule",
			Namespace:  namespace,
			Name:       task.ProtectionCleanup.ScheduleName,
		}); err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_SCHEDULE_DELETE_FAILED", err.Error(), map[string]any{
				"scheduleName": task.ProtectionCleanup.ScheduleName,
				"namespace":    namespace,
			})
			return
		}
		if err := c.sendTaskProgress(task, map[string]any{
			"kind":         "ProtectionCleanup",
			"planId":       task.ProtectionCleanup.PlanID,
			"scheduleName": task.ProtectionCleanup.ScheduleName,
			"namespace":    namespace,
			"deleted":      deleted,
		}, 100, "velero schedule deleted"); err != nil {
			c.logger.Error("failed to send protection cleanup progress", "task_id", task.TaskID, "error", err)
			return
		}
	}
	deletedRepositories := []string{}
	deletedKopiaRepositories := []string{}
	deletedPrefixCRs := map[string][]string{}
	deletedBackupObjects := []string{}
	deletedRestoreObjects := []string{}
	if cleaner != nil && task.ProtectionCleanup.StorageRepo != "" && len(task.ProtectionCleanup.SourceNamespaces) > 0 {
		var err error
		deletedPrefixCRs, err = cleaner.DeleteVeleroBackupArtifactsByNamePrefix(context.Background(), namespace, task.ProtectionCleanup.BackupNamePrefix)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_PREFIX_CR_DELETE_FAILED", err.Error(), map[string]any{
				"backupNamePrefix": task.ProtectionCleanup.BackupNamePrefix,
			})
			return
		}
		deletedRepositories, err = cleaner.DeleteBackupRepositories(context.Background(), namespace, task.ProtectionCleanup.StorageRepo, task.ProtectionCleanup.SourceNamespaces)
		if err != nil {
			_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_REPOSITORY_DELETE_FAILED", err.Error(), map[string]any{
				"storageRepo":      task.ProtectionCleanup.StorageRepo,
				"sourceNamespaces": task.ProtectionCleanup.SourceNamespaces,
			})
			return
		}
		if task.ProtectionCleanup.CleanupObjectStorage {
			deletedBackupObjects, err = cleaner.DeleteBackupObjectsByNamePrefix(context.Background(), namespace, task.ProtectionCleanup.StorageRepo, task.ProtectionCleanup.BackupNamePrefix)
			if err != nil {
				_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_BACKUP_OBJECT_DELETE_FAILED", err.Error(), map[string]any{
					"storageRepo":      task.ProtectionCleanup.StorageRepo,
					"backupNamePrefix": task.ProtectionCleanup.BackupNamePrefix,
					"sourceNamespaces": task.ProtectionCleanup.SourceNamespaces,
				})
				return
			}
			deletedRestoreObjects, err = cleaner.DeleteRestoreObjects(context.Background(), namespace, task.ProtectionCleanup.StorageRepo, task.ProtectionCleanup.RestoreNames)
			if err != nil {
				_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_RESTORE_OBJECT_DELETE_FAILED", err.Error(), map[string]any{
					"storageRepo":  task.ProtectionCleanup.StorageRepo,
					"restoreNames": task.ProtectionCleanup.RestoreNames,
				})
				return
			}
			deletedKopiaRepositories, err = cleaner.DeleteKopiaRepositories(context.Background(), namespace, task.ProtectionCleanup.StorageRepo, task.ProtectionCleanup.SourceNamespaces)
			if err != nil {
				_ = c.sendTaskFailedWithDetails(task, "PROTECTION_CLEANUP_KOPIA_DELETE_FAILED", err.Error(), map[string]any{
					"storageRepo":      task.ProtectionCleanup.StorageRepo,
					"sourceNamespaces": task.ProtectionCleanup.SourceNamespaces,
				})
				return
			}
		}
	}
	if err := c.sendTaskCompleted(task, map[string]any{
		"kind":                     "ProtectionCleanup",
		"mode":                     mode,
		"planId":                   task.ProtectionCleanup.PlanID,
		"scheduleName":             task.ProtectionCleanup.ScheduleName,
		"deleted":                  deleted,
		"deletedPrefixCRs":         deletedPrefixCRs,
		"deletedBackupObjects":     deletedBackupObjects,
		"deletedRestoreObjects":    deletedRestoreObjects,
		"deletedRepositories":      deletedRepositories,
		"deletedKopiaRepositories": deletedKopiaRepositories,
	}, "protection resources cleaned"); err != nil {
		c.logger.Error("failed to send protection cleanup completed", "task_id", task.TaskID, "error", err)
	}
}

func retentionCleanupProgress(done int, total int) int {
	if total <= 0 {
		return 100
	}
	progress := int(float64(done) / float64(total) * 100)
	if progress < 1 {
		return 1
	}
	if progress > 99 {
		return 99
	}
	return progress
}

type veleroStatusDecision func(status kube.ManifestStatus, elapsed time.Duration) (terminal bool, success bool, progress int, message string, code string)

func (c *Client) pollVeleroStatus(task protocol.TaskDispatchPayload, object kube.AppliedObject, basePayload map[string]any, decide veleroStatusDecision) {
	interval := veleroPollInterval(object)
	deadline := task.Deadline
	if deadline.IsZero() {
		deadline = time.Now().UTC().Add(30 * time.Minute)
	}
	started := time.Now().UTC()
	progress := 0
	samples := make([]volumeProgressSample, 0, 12)
	for {
		status, err := c.statusReader.GetManifestStatus(context.Background(), object)
		if err != nil {
			_ = c.sendTaskFailed(task, object.Kind+"_STATUS_READ_FAILED", err.Error())
			return
		}
		terminal, success, nextProgress, message, code := decide(status, time.Since(started))
		if !usesVolumeProgress(object) && nextProgress > progress {
			progress = nextProgress
		}
		if usesVolumeProgress(object) {
			if itemProgress := manifestItemProgress(status); itemProgress > progress {
				progress = itemProgress
			}
		}
		payload := cloneVeleroPayload(basePayload)
		payload["status"] = map[string]any{
			"phase":    status.Phase,
			"message":  status.Message,
			"reason":   status.Reason,
			"errors":   status.Errors,
			"warnings": status.Warnings,
			"raw":      status.Raw,
		}
		if status.ItemsTotal > 0 {
			payload["itemProgress"] = map[string]any{
				"itemsDone":  status.ItemsDone,
				"itemsTotal": status.ItemsTotal,
				"percent":    manifestItemProgressPercent(status),
			}
		}
		volumeReady := !usesVolumeProgress(object)
		if volumePayload, volumeProgress, volumeMessage, ready := c.buildVolumeProgressPayload(context.Background(), object, &samples); volumePayload != nil {
			payload["volumeProgress"] = volumePayload
			if failedCount := int64FromAny(volumePayload["failedCount"]); object.Kind == "Restore" && failedCount > 0 {
				failureMessage := "Volume data restore validation failed."
				if items, ok := volumePayload["items"].([]map[string]any); ok {
					for _, item := range items {
						if detail := strings.TrimSpace(fmt.Sprint(item["message"])); detail != "" {
							failureMessage = detail
							break
						}
					}
				}
				_ = c.sendTaskFailedWithDetails(task, "RESTORE_VOLUME_DEPENDENCY_MISSING", failureMessage, map[string]any{"velero": payload})
				return
			}
			volumeReady = ready
			if ready && volumeProgress > progress {
				progress = volumeProgress
			}
			if ready && volumeMessage != "" && status.Phase == "InProgress" {
				message = volumeMessage
			}
		}
		if terminal {
			if success {
				c.attachBackupSizeStats(context.Background(), object, payload)
				if err := c.sendTaskCompleted(task, payload, message); err != nil {
					c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
				}
				return
			}
			if status.Message != "" {
				message = status.Message
			}
			if code == "" {
				code = object.Kind + "_FAILED"
			}
			_ = c.sendTaskFailedWithDetails(task, code, message, map[string]any{"velero": payload})
			return
		}
		if time.Now().UTC().After(deadline) {
			_ = c.sendTaskFailed(task, object.Kind+"_STATUS_TIMEOUT", "timed out waiting for Velero "+object.Kind+" to complete")
			return
		}
		if usesVolumeProgress(object) && !volumeReady && status.ItemsTotal <= 0 {
			time.Sleep(interval)
			continue
		}
		if err := c.sendTaskProgress(task, payload, progress, message); err != nil {
			c.logger.Error("failed to send task progress", "task_id", task.TaskID, "error", err)
			return
		}
		time.Sleep(interval)
	}
}

func (c *Client) pollRestoreStatus(task protocol.TaskDispatchPayload, object kube.AppliedObject, basePayload map[string]any) {
	c.pollVeleroStatusWithSuccess(task, object, basePayload, restoreStatusResult, func(payload map[string]any, message string) {
		if c.readiness == nil || task.Restore == nil {
			if err := c.sendTaskCompleted(task, payload, message); err != nil {
				c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
			}
			return
		}
		c.pollRestoredNamespaceReady(task, payload, restoreTargetNamespace(task), message)
	})
}

func (c *Client) pollVeleroStatusWithSuccess(task protocol.TaskDispatchPayload, object kube.AppliedObject, basePayload map[string]any, decide veleroStatusDecision, onSuccess func(map[string]any, string)) {
	interval := veleroPollInterval(object)
	deadline := task.Deadline
	if deadline.IsZero() {
		deadline = time.Now().UTC().Add(30 * time.Minute)
	}
	started := time.Now().UTC()
	progress := 0
	samples := make([]volumeProgressSample, 0, 12)
	for {
		status, err := c.statusReader.GetManifestStatus(context.Background(), object)
		if err != nil {
			_ = c.sendTaskFailed(task, object.Kind+"_STATUS_READ_FAILED", err.Error())
			return
		}
		terminal, success, nextProgress, message, code := decide(status, time.Since(started))
		if !usesVolumeProgress(object) && nextProgress > progress {
			progress = nextProgress
		}
		payload := cloneVeleroPayload(basePayload)
		payload["status"] = map[string]any{
			"phase":    status.Phase,
			"message":  status.Message,
			"reason":   status.Reason,
			"errors":   status.Errors,
			"warnings": status.Warnings,
			"raw":      status.Raw,
		}
		volumeReady := !usesVolumeProgress(object)
		if volumePayload, volumeProgress, volumeMessage, ready := c.buildVolumeProgressPayload(context.Background(), object, &samples); volumePayload != nil {
			payload["volumeProgress"] = volumePayload
			if failedCount := int64FromAny(volumePayload["failedCount"]); object.Kind == "Restore" && failedCount > 0 {
				failureMessage := "Volume data restoration did not start because a required dependency is unavailable."
				if items, ok := volumePayload["items"].([]map[string]any); ok {
					for _, item := range items {
						if detail := strings.TrimSpace(fmt.Sprint(item["message"])); detail != "" {
							failureMessage = detail
							break
						}
					}
				}
				_ = c.sendTaskFailedWithDetails(task, "RESTORE_VOLUME_DEPENDENCY_MISSING", failureMessage, map[string]any{"velero": payload})
				return
			}
			volumeReady = ready
			if ready && volumeProgress > progress {
				progress = volumeProgress
			}
			if ready && volumeMessage != "" && status.Phase == "InProgress" {
				message = volumeMessage
			}
		}
		if terminal {
			if success {
				c.attachBackupSizeStats(context.Background(), object, payload)
				onSuccess(payload, message)
				return
			}
			message, payload = c.enrichVeleroFailure(context.Background(), object, payload, status, message)
			if code == "" {
				code = object.Kind + "_FAILED"
			}
			_ = c.sendTaskFailedWithDetails(task, code, message, map[string]any{"velero": payload})
			return
		}
		if time.Now().UTC().After(deadline) {
			_ = c.sendTaskFailed(task, object.Kind+"_STATUS_TIMEOUT", "timed out waiting for Velero "+object.Kind+" to complete")
			return
		}
		if usesVolumeProgress(object) && !volumeReady {
			time.Sleep(interval)
			continue
		}
		if err := c.sendTaskProgress(task, payload, progress, message); err != nil {
			c.logger.Error("failed to send task progress", "task_id", task.TaskID, "error", err)
			return
		}
		time.Sleep(interval)
	}
}

func (c *Client) pollRestoredNamespaceReady(task protocol.TaskDispatchPayload, basePayload map[string]any, namespace string, restoreMessage string) {
	deadline := task.Deadline
	if deadline.IsZero() {
		deadline = time.Now().UTC().Add(30 * time.Minute)
	}
	startPayload := cloneVeleroPayload(basePayload)
	startPayload["readinessStage"] = "started"
	if err := c.sendTaskProgress(task, startPayload, 95, "Resource and volume data restoration completed; restored application readiness validation started."); err != nil {
		c.logger.Error("failed to send restore readiness start event", "task_id", task.TaskID, "error", err)
		return
	}
	for {
		readiness, err := c.readiness.GetNamespaceReadiness(context.Background(), namespace)
		payload := cloneVeleroPayload(basePayload)
		if err == nil {
			payload["readiness"] = readiness
			if readiness.FailureCode != "" {
				_ = c.sendTaskFailedWithDetails(task, readiness.FailureCode, readiness.FailureMessage, map[string]any{"velero": payload})
				return
			}
			if readiness.Ready {
				payload["readinessStage"] = "succeeded"
				if err := c.sendTaskProgress(task, payload, 99, "Restored application readiness validation completed successfully."); err != nil {
					c.logger.Error("failed to send restore readiness success event", "task_id", task.TaskID, "error", err)
					return
				}
				if err := c.sendTaskCompleted(task, payload, restoreMessage+"; restored application is ready"); err != nil {
					c.logger.Error("failed to send task completed", "task_id", task.TaskID, "error", err)
				}
				return
			}
			if time.Now().UTC().After(deadline) {
				_ = c.sendTaskFailedWithDetails(task, "RESTORE_READINESS_TIMEOUT", "timed out waiting for restored application readiness", map[string]any{"velero": payload})
				return
			}
			if err := c.sendTaskProgress(task, payload, 95, restoreReadinessProgressMessage(readiness)); err != nil {
				c.logger.Error("failed to send restore readiness progress", "task_id", task.TaskID, "error", err)
				return
			}
		} else {
			if time.Now().UTC().After(deadline) {
				_ = c.sendTaskFailedWithDetails(task, "RESTORE_READINESS_READ_FAILED", err.Error(), map[string]any{"velero": payload})
				return
			}
			if err := c.sendTaskProgress(task, payload, 90, "waiting for restored namespace to be created"); err != nil {
				c.logger.Error("failed to send restore readiness progress", "task_id", task.TaskID, "error", err)
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func restoreReadinessProgressMessage(readiness kube.NamespaceReadiness) string {
	message := fmt.Sprintf(
		"Restored application readiness validation is in progress: namespace %s is %s; pods %d/%d ready; workloads %d/%d ready",
		readiness.Namespace,
		strings.ToLower(readiness.NamespacePhase),
		readiness.ReadyPodCount,
		readiness.PodCount,
		readiness.ReadyWorkloads,
		readiness.WorkloadCount,
	)
	if strings.TrimSpace(readiness.Message) != "" {
		message += "; " + strings.TrimSuffix(strings.TrimSpace(readiness.Message), ".")
	}
	return message + "."
}

func (c *Client) enrichVeleroFailure(ctx context.Context, object kube.AppliedObject, payload map[string]any, status kube.ManifestStatus, fallback string) (string, map[string]any) {
	message := fallback
	if status.Message != "" {
		message = status.Message
	}
	if object.Kind != "Restore" {
		return message, payload
	}
	summary := c.restoreResultSummary(ctx, object, payload)
	if summary.Key != "" || len(summary.Errors) > 0 || len(summary.Warnings) > 0 {
		payload["restoreResults"] = map[string]any{
			"key":          summary.Key,
			"errorCount":   summary.ErrorCount,
			"warningCount": summary.WarningCount,
			"errors":       summary.Errors,
			"warnings":     summary.Warnings,
		}
	}
	if len(summary.Errors) > 0 {
		return summarizeRestoreFailure(summary.Errors[0], summary.ErrorCount), payload
	}
	if status.Errors > 0 {
		return fmt.Sprintf("velero restore failed with %d error(s); restore result details are not available yet", status.Errors), payload
	}
	return message, payload
}

func (c *Client) restoreResultSummary(ctx context.Context, object kube.AppliedObject, payload map[string]any) kube.RestoreResultSummary {
	if c.statsReader == nil {
		return kube.RestoreResultSummary{}
	}
	storageLocation := backupStorageLocationFromPayload(payload)
	if storageLocation == "" {
		return kube.RestoreResultSummary{}
	}
	summary, err := c.statsReader.GetRestoreResultSummary(ctx, object.Namespace, storageLocation, object.Name)
	if err != nil {
		return kube.RestoreResultSummary{
			Key:    restoreResultsObjectKey(object.Name),
			Errors: []string{"failed to read Velero restore results: " + err.Error()},
		}
	}
	return summary
}

func summarizeRestoreFailure(firstError string, total int) string {
	firstError = strings.TrimSpace(firstError)
	if firstError == "" {
		return "velero restore failed"
	}
	if total > 1 {
		return fmt.Sprintf("%s; %d restore errors total", firstError, total)
	}
	return firstError
}

func restoreResultsObjectKey(restoreName string) string {
	parts := []string{"restores", restoreName, "restore-" + restoreName + "-results.gz"}
	return strings.Join(parts, "/")
}

func restoreTargetNamespace(task protocol.TaskDispatchPayload) string {
	if task.Restore == nil {
		return ""
	}
	if task.Restore.TargetNamespace != "" {
		return task.Restore.TargetNamespace
	}
	return task.Restore.SourceNamespace
}

func (c *Client) requireBackupStorageLocation(ctx context.Context, name string) error {
	return c.requireBackupStorageLocationWithRetry(ctx, name, backupStorageLocationRetryCount, backupStorageLocationRetryDelay)
}

func (c *Client) requireBackupStorageLocationWithRetry(ctx context.Context, name string, retryCount int, baseDelay time.Duration) error {
	if name == "" {
		return errors.New("backup storage location is required before starting this task")
	}
	if c.statusReader == nil {
		return nil
	}
	object := kube.AppliedObject{
		APIVersion: "velero.io/v1",
		Kind:       "BackupStorageLocation",
		Namespace:  c.cfg.Namespace,
		Name:       name,
	}
	var status kube.ManifestStatus
	var err error
	for attempt := 0; attempt <= retryCount; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, backupStorageLocationCheckTimeout)
		status, err = c.statusReader.GetManifestStatus(checkCtx, object)
		cancel()
		if err == nil {
			break
		}
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("BackupStorageLocation %s is not configured in namespace %s", name, c.cfg.Namespace)
		}
		if !isTransientKubernetesReadError(err) {
			return fmt.Errorf("BackupStorageLocation %s could not be read from namespace %s: %w", name, c.cfg.Namespace, err)
		}
		if attempt == retryCount {
			return fmt.Errorf("BackupStorageLocation %s could not be verified because the Kubernetes API is temporarily unavailable after %d retries: %w", name, retryCount, err)
		}
		if c.logger != nil {
			c.logger.Warn("temporary Kubernetes API error while checking backup storage location; retrying",
				"storage_location", name,
				"attempt", attempt+1,
				"retry_in", baseDelay*time.Duration(attempt+1),
				"error", err,
			)
		}
		if baseDelay > 0 {
			timer := time.NewTimer(baseDelay * time.Duration(attempt+1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if status.Phase != "Available" {
		if status.Message != "" {
			return errors.New("BackupStorageLocation " + name + " is " + status.Phase + ": " + status.Message)
		}
		if status.Phase == "" {
			return errors.New("BackupStorageLocation " + name + " has no Available status yet")
		}
		return errors.New("BackupStorageLocation " + name + " is " + status.Phase)
	}
	return nil
}

func isTransientKubernetesReadError(err error) bool {
	return err != nil && (apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || isUncertainKubernetesSubmitError(err))
}

func (c *Client) waitForBackupStorageLocation(ctx context.Context, name string, timeout time.Duration) error {
	if timeout <= 0 || c.statusReader == nil {
		return c.requireBackupStorageLocation(ctx, name)
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = c.requireBackupStorageLocation(ctx, name)
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func backupStatusResult(status kube.ManifestStatus, elapsed time.Duration) (bool, bool, int, string, string) {
	switch status.Phase {
	case "Completed":
		return true, true, 100, "Backup completed successfully.", ""
	case "FailedValidation", "Failed", "PartiallyFailed":
		return true, false, 100, "velero backup failed", "BACKUP_FAILED"
	case "InProgress":
		return false, false, 70, "Application resource backup is in progress.", ""
	case "Finalizing", "FinalizingPartiallyFailed":
		return false, false, 90, "Backup finalization is in progress.", ""
	case "New":
		return false, false, 30, "Backup operation queued.", ""
	default:
		return false, false, progressFromElapsed(elapsed), "Waiting for the backup operation to start.", ""
	}
}

func restoreStatusResult(status kube.ManifestStatus, elapsed time.Duration) (bool, bool, int, string, string) {
	switch status.Phase {
	case "Completed":
		return true, true, 90, "Resource restoration completed successfully.", ""
	case "FailedValidation", "Failed", "PartiallyFailed":
		return true, false, 100, "velero restore failed", "RESTORE_FAILED"
	case "InProgress":
		return false, false, 70, "Application resource restoration is in progress.", ""
	case "Finalizing", "FinalizingPartiallyFailed":
		return false, false, 85, "Resource restoration finalization is in progress.", ""
	case "New":
		return false, false, 30, "Restore operation queued.", ""
	default:
		return false, false, progressFromElapsed(elapsed), "Waiting for the restore operation to start.", ""
	}
}

func storageStatusResult(status kube.ManifestStatus, elapsed time.Duration) (bool, bool, int, string, string) {
	switch status.Phase {
	case "Available":
		return true, true, 100, "backup storage location available", ""
	case "Unavailable":
		return true, false, 100, "backup storage location unavailable", "BSL_UNAVAILABLE"
	default:
		return false, false, progressFromElapsed(elapsed), "waiting for backup storage location validation", ""
	}
}

func usesVolumeProgress(object kube.AppliedObject) bool {
	return object.Kind == "Backup" || object.Kind == "Restore"
}

func veleroPollInterval(object kube.AppliedObject) time.Duration {
	if usesVolumeProgress(object) {
		return 2 * time.Second
	}
	return 5 * time.Second
}

func (c *Client) buildVolumeProgressPayload(ctx context.Context, object kube.AppliedObject, samples *[]volumeProgressSample) (map[string]any, int, string, bool) {
	if c.volumeReader == nil {
		return nil, 0, "", false
	}
	var progress kube.VolumeProgress
	var err error
	operation := ""
	switch object.Kind {
	case "Backup":
		operation = "backup"
		progress, err = c.volumeReader.GetBackupVolumeProgress(ctx, object.Namespace, object.Name)
	case "Restore":
		operation = "restore"
		progress, err = c.volumeReader.GetRestoreVolumeProgress(ctx, object.Namespace, object.Name)
	default:
		return nil, 0, "", false
	}
	if err != nil || len(progress.Items) == 0 {
		return nil, 0, "", false
	}

	now := time.Now().UTC()
	*samples = append(*samples, volumeProgressSample{bytesDone: progress.BytesDone, observed: now})
	cutoff := now.Add(-60 * time.Second)
	kept := (*samples)[:0]
	for _, sample := range *samples {
		if sample.observed.After(cutoff) || len(*samples) <= 2 {
			kept = append(kept, sample)
		}
	}
	*samples = kept

	speedBytesPerSecond := estimateBytesPerSecond(*samples)
	etaSeconds := int64(0)
	if progress.KnownTotal && progress.TotalBytes > progress.BytesDone && speedBytesPerSecond > 0 {
		etaSeconds = int64(float64(progress.TotalBytes-progress.BytesDone) / speedBytesPerSecond)
	}
	percent := 0
	if progress.AllTotalsKnown && progress.TotalBytes > 0 {
		percent = int((progress.BytesDone * 100) / progress.TotalBytes)
		if percent > 99 && progress.BytesDone < progress.TotalBytes {
			percent = 99
		}
	}
	taskProgress := percent
	if taskProgress > 99 && progress.BytesDone < progress.TotalBytes {
		taskProgress = 99
	}

	items := make([]map[string]any, 0, len(progress.Items))
	for _, item := range progress.Items {
		items = append(items, map[string]any{
			"kind":       item.Kind,
			"name":       item.Name,
			"phase":      item.Phase,
			"bytesDone":  item.BytesDone,
			"totalBytes": item.TotalBytes,
			"knownTotal": item.KnownTotal,
			"message":    item.Message,
		})
	}
	payload := map[string]any{
		"operation":           operation,
		"bytesDone":           progress.BytesDone,
		"totalBytes":          progress.TotalBytes,
		"knownTotal":          progress.KnownTotal,
		"allTotalsKnown":      progress.AllTotalsKnown,
		"knownTotalCount":     progress.KnownTotalCount,
		"unknownTotalCount":   progress.UnknownTotalCount,
		"percent":             percent,
		"speedBytesPerSecond": int64(speedBytesPerSecond),
		"etaSeconds":          etaSeconds,
		"itemCount":           len(progress.Items),
		"runningCount":        progress.RunningCount,
		"completedCount":      progress.Completed,
		"failedCount":         progress.FailedCount,
		"items":               items,
	}
	ready := len(progress.Items) > 0
	return payload, taskProgress, volumeProgressMessage(operation, progress, speedBytesPerSecond, etaSeconds, percent), ready
}

func (c *Client) attachBackupSizeStats(ctx context.Context, object kube.AppliedObject, payload map[string]any) {
	if object.Kind != "Backup" || payload == nil {
		return
	}
	storageLocation := backupStorageLocationFromPayload(payload)
	volumeProgress := wsMapFromAny(payload["volumeProgress"])
	volumeBytes, volumeAccuracy, _ := finalVolumeBytesFromProgress(volumeProgress)

	var objectStats kube.BackupObjectStats
	var objectStatsErr string
	var volumeInfoStatsErr string
	if c.statsReader != nil && storageLocation != "" {
		stats, err := c.statsReader.GetBackupObjectStats(ctx, object.Namespace, storageLocation, object.Name)
		if err != nil {
			objectStatsErr = err.Error()
		} else {
			objectStats = stats
		}
		infoStats, err := c.statsReader.GetBackupVolumeInfoStats(ctx, object.Namespace, storageLocation, object.Name)
		if err != nil {
			volumeInfoStatsErr = err.Error()
		} else {
			if infoStats.Accurate {
				volumeBytes = infoStats.VolumeBytes
				volumeAccuracy = "accurate"
			}
		}
	}

	metadataBytes := objectStats.MetadataPackageBytes
	uploadedMetadataBytes := metadataBytes
	uploadedVolumeBytes := volumeBytes
	totalBytes := metadataBytes + volumeBytes
	uploadedBytes := uploadedMetadataBytes + uploadedVolumeBytes
	sizeStatus := "complete"
	if objectStatsErr != "" || volumeAccuracy != "accurate" {
		sizeStatus = "partial"
	}
	sizeWarnings := make([]map[string]any, 0, 2)
	if objectStatsErr != "" {
		sizeWarnings = append(sizeWarnings, map[string]any{
			"scope":   "restorePointSize.metadataBytes",
			"code":    "METADATA_SIZE_SCAN_FAILED",
			"message": objectStatsErr,
		})
	}
	if volumeInfoStatsErr != "" {
		sizeWarnings = append(sizeWarnings, map[string]any{
			"scope":   "restorePointSize.volumeBytes",
			"code":    "VOLUME_INFO_READ_FAILED",
			"message": volumeInfoStatsErr,
		})
	}
	if volumeAccuracy != "accurate" {
		sizeWarnings = append(sizeWarnings, map[string]any{
			"scope":   "restorePointSize.volumeBytes",
			"code":    "VOLUME_SIZE_NOT_ACCURATE",
			"message": "volume size was not available from Velero backup volume info",
		})
	}

	restorePointSize := map[string]any{
		"totalBytes":            totalBytes,
		"metadataBytes":         metadataBytes,
		"volumeBytes":           volumeBytes,
		"uploadedBytes":         uploadedBytes,
		"uploadedMetadataBytes": uploadedMetadataBytes,
		"uploadedVolumeBytes":   uploadedVolumeBytes,
	}
	_ = objectStats
	payload["sizeStatus"] = sizeStatus
	if len(sizeWarnings) > 0 {
		payload["sizeWarnings"] = sizeWarnings
	}
	payload["restorePointSize"] = restorePointSize
	payload["sizeBytes"] = totalBytes
	delete(payload, "volumeProgress")

	c.attachPlanObjectStorageStats(ctx, object, storageLocation, payload)
}

func (c *Client) attachPlanObjectStorageStats(ctx context.Context, object kube.AppliedObject, storageLocation string, payload map[string]any) {
	if c.statsReader == nil || object.Kind != "Backup" || storageLocation == "" || payload == nil {
		return
	}
	planID := backupPlanIDFromPayload(payload)
	backupNamePrefix := backupNamePrefixForPlan(planID)
	if backupNamePrefix == "" {
		return
	}
	namespaces := backupNamespacesFromPayload(payload)
	stats, err := c.statsReader.GetPlanObjectStorageStats(ctx, object.Namespace, storageLocation, backupNamePrefix, namespaces)
	if err != nil {
		payload["sizeStatus"] = "partial"
		payload["sizeWarnings"] = appendWarning(payload["sizeWarnings"], map[string]any{
			"scope":   "planStorageSize",
			"code":    "PLAN_STORAGE_SIZE_SCAN_FAILED",
			"message": err.Error(),
		})
		return
	}
	payload["planStorageSize"] = map[string]any{
		"totalBytes":    stats.TotalBytes,
		"metadataBytes": stats.MetadataBytes,
		"kopiaBytes":    stats.KopiaBytes,
	}
}

func appendWarning(existing any, warning map[string]any) []map[string]any {
	warnings := []map[string]any{}
	for _, item := range sliceFromAny(existing) {
		if mapped := wsMapFromAny(item); len(mapped) > 0 {
			warnings = append(warnings, mapped)
		}
	}
	warnings = append(warnings, warning)
	return warnings
}

func sliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func backupStorageLocationFromPayload(payload map[string]any) string {
	if value, _ := payload["storageLocation"].(string); strings.TrimSpace(value) != "" {
		return value
	}
	manifest := mapFromAnyViaJSON(payload["manifest"])
	spec := wsMapFromAny(manifest["spec"])
	if value, _ := spec["storageLocation"].(string); strings.TrimSpace(value) != "" {
		return value
	}
	return ""
}

func backupPlanIDFromPayload(payload map[string]any) string {
	if value := stringFromAny(payload["planId"]); value != "" {
		return value
	}
	for _, labels := range []map[string]string{
		stringMapFromAny(payload["labels"]),
		stringMapFromAny(wsMapFromAny(payload["metadata"])["labels"]),
		stringMapFromAny(wsMapFromAny(wsMapFromAny(payload["manifest"])["metadata"])["labels"]),
	} {
		if value := strings.TrimSpace(labels["hypercdr.io/plan-id"]); value != "" {
			return value
		}
	}
	manifest := mapFromAnyViaJSON(payload["manifest"])
	metadata := mapFromAnyViaJSON(manifest["metadata"])
	labels := stringMapFromAny(metadata["labels"])
	if value := strings.TrimSpace(labels["hypercdr.io/plan-id"]); value != "" {
		return value
	}
	return ""
}

func backupNamePrefixForPlan(planID string) string {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return ""
	}
	if strings.HasPrefix(planID, "hcdr-") {
		return strings.ReplaceAll(planID, "-", "")
	}
	return "hcdr-" + strings.ReplaceAll(planID, "-", "")
}

func backupNamespacesFromPayload(payload map[string]any) []string {
	values := []string{}
	values = append(values, stringSliceFromAny(payload["includedNamespaces"])...)
	values = append(values, stringSliceFromAny(payload["sourceNamespaces"])...)
	values = append(values, stringSliceFromAny(wsMapFromAny(payload["spec"])["includedNamespaces"])...)
	manifest := mapFromAnyViaJSON(payload["manifest"])
	spec := mapFromAnyViaJSON(manifest["spec"])
	values = append(values, stringSliceFromAny(spec["includedNamespaces"])...)
	if value := stringFromAny(payload["sourceNamespace"]); value != "" {
		values = append(values, value)
	}
	for _, labels := range []map[string]string{
		stringMapFromAny(payload["labels"]),
		stringMapFromAny(wsMapFromAny(payload["metadata"])["labels"]),
		stringMapFromAny(mapFromAnyViaJSON(mapFromAnyViaJSON(payload["manifest"])["metadata"])["labels"]),
	} {
		if value := strings.TrimSpace(labels["hypercdr.io/source-namespace"]); value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func volumeSizeAccuracy(volumeProgress map[string]any) string {
	if len(volumeProgress) == 0 {
		return "unknown"
	}
	if allKnown, _ := volumeProgress["allTotalsKnown"].(bool); allKnown {
		return "accurate"
	}
	if known, _ := volumeProgress["knownTotal"].(bool); known {
		return "partial"
	}
	return "estimated"
}

func finalVolumeBytesFromProgress(volumeProgress map[string]any) (int64, string, string) {
	if len(volumeProgress) == 0 {
		return 0, "unknown", "unavailable"
	}
	if allKnown, _ := volumeProgress["allTotalsKnown"].(bool); allKnown {
		if totalBytes := int64FromAny(volumeProgress["totalBytes"]); totalBytes > 0 {
			return totalBytes, "accurate", "veleroVolumeProgress"
		}
	}
	return 0, volumeSizeAccuracy(volumeProgress), "veleroVolumeProgress"
}

func wsMapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}

func mapFromAnyViaJSON(value any) map[string]any {
	if mapped := wsMapFromAny(value); len(mapped) > 0 {
		return mapped
	}
	if value == nil {
		return map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var mapped map[string]any
	if err := json.Unmarshal(data, &mapped); err != nil {
		return map[string]any{}
	}
	return mapped
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func stringMapFromAny(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, raw := range typed {
			if value := stringFromAny(raw); value != "" {
				out[key] = value
			}
		}
		return out
	default:
		return map[string]string{}
	}
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, raw := range typed {
			if value := stringFromAny(raw); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func estimateBytesPerSecond(samples []volumeProgressSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	first := samples[0]
	last := samples[len(samples)-1]
	seconds := last.observed.Sub(first.observed).Seconds()
	if seconds <= 0 || last.bytesDone <= first.bytesDone {
		return 0
	}
	return float64(last.bytesDone-first.bytesDone) / seconds
}

func volumeProgressMessage(operation string, progress kube.VolumeProgress, speedBytesPerSecond float64, etaSeconds int64, percent int) string {
	verb := "Processing"
	if operation == "backup" {
		verb = "Backing up"
	} else if operation == "restore" {
		verb = "Restoring"
	}
	if progress.KnownTotal && progress.TotalBytes > 0 {
		message := verb + " volume data " + formatBytes(progress.BytesDone) + " / " + formatBytes(progress.TotalBytes)
		if percent > 0 {
			message += fmt.Sprintf(" (%d%%)", percent)
		}
		if speedBytesPerSecond > 0 {
			message += " at " + formatBytes(int64(speedBytesPerSecond)) + "/s"
		}
		if etaSeconds > 0 {
			message += ", about " + formatDuration(etaSeconds) + " remaining"
		}
		return message
	}
	if progress.BytesDone <= 0 {
		return "Volume data transfer is being prepared."
	}
	message := verb + " volume data " + formatBytes(progress.BytesDone)
	if speedBytesPerSecond > 0 {
		message += " at " + formatBytes(int64(speedBytesPerSecond)) + "/s"
	}
	return message
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	for _, suffix := range units {
		amount = amount / unit
		if amount < unit {
			return fmt.Sprintf("%.1f %s", amount, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", amount/unit)
}

func formatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}
	hours := minutes / 60
	return fmt.Sprintf("%dh %dm", hours, minutes%60)
}

func progressFromElapsed(elapsed time.Duration) int {
	progress := 30 + int(elapsed/(15*time.Second))*10
	if progress > 85 {
		return 85
	}
	return progress
}

func cloneVeleroPayload(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+1)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (c *Client) sendVeleroBackupEvents(ctx context.Context) error {
	if c.backupReader == nil {
		return nil
	}
	if err := c.refreshScheduleBackupContext(ctx); err != nil {
		c.logger.Warn("failed to refresh local velero schedule context", "error", err)
	}
	backups, err := c.backupReader.ListVeleroBackups(ctx, c.cfg.Namespace, 100)
	if err != nil {
		return err
	}
	for _, backup := range backups {
		if !isHyperCDRBackup(backup.Labels) {
			continue
		}
		if !c.shouldReportVeleroBackup(backup) {
			continue
		}
		if err := c.sendVeleroBackupEvent(backup); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) refreshScheduleBackupContext(ctx context.Context) error {
	if c.scheduleReader == nil {
		return nil
	}
	schedules, err := c.scheduleReader.ListVeleroSchedules(ctx, c.cfg.Namespace, 100)
	if err != nil {
		return err
	}
	c.backupContextMu.Lock()
	defer c.backupContextMu.Unlock()
	for _, schedule := range schedules {
		if !isHyperCDRBackup(schedule.Labels) {
			continue
		}
		if !c.labelsBelongToSourceCluster(schedule.Labels) {
			continue
		}
		if planID := strings.TrimSpace(schedule.Labels["hypercdr.io/plan-id"]); planID != "" {
			c.backupPlanIDs[planID] = struct{}{}
			_ = c.ledger.remember("", "", planID)
		}
		if taskID := strings.TrimSpace(schedule.Labels["hypercdr.io/task-id"]); taskID != "" {
			c.backupTaskIDs[taskID] = struct{}{}
			_ = c.ledger.remember(taskID, "", "")
		}
		if commandID := strings.TrimSpace(schedule.Labels["hypercdr.io/command-id"]); commandID != "" {
			c.backupCommandIDs[commandID] = struct{}{}
			_ = c.ledger.remember("", commandID, "")
		}
	}
	return nil
}

func (c *Client) markBackupContext(task protocol.TaskDispatchPayload) {
	c.backupContextMu.Lock()
	defer c.backupContextMu.Unlock()
	if task.TaskID != "" {
		c.backupTaskIDs[task.TaskID] = struct{}{}
	}
	if task.CommandID != "" {
		c.backupCommandIDs[task.CommandID] = struct{}{}
	}
	if task.Backup != nil && strings.TrimSpace(task.Backup.PlanID) != "" {
		c.backupPlanIDs[strings.TrimSpace(task.Backup.PlanID)] = struct{}{}
	}
	_ = c.ledger.remember(task.TaskID, task.CommandID, backupTaskPlanID(task))
}

func (c *Client) shouldReportVeleroBackup(backup kube.VeleroBackupSummary) bool {
	labels := backup.Labels
	if !c.labelsBelongToSourceCluster(labels) {
		return false
	}

	taskID := strings.TrimSpace(labels["hypercdr.io/task-id"])
	commandID := strings.TrimSpace(labels["hypercdr.io/command-id"])
	planID := strings.TrimSpace(labels["hypercdr.io/plan-id"])
	c.backupContextMu.RLock()
	defer c.backupContextMu.RUnlock()
	if taskID != "" {
		if _, ok := c.backupTaskIDs[taskID]; ok {
			return true
		}
		if c.ledger.hasTask(taskID) {
			return true
		}
	}
	if commandID != "" {
		if _, ok := c.backupCommandIDs[commandID]; ok {
			return true
		}
		if c.ledger.hasCommand(commandID) {
			return true
		}
	}
	if planID != "" {
		if _, ok := c.backupPlanIDs[planID]; ok {
			return true
		}
		if c.ledger.hasPlan(planID) {
			return true
		}
	}
	return false
}

func (c *Client) labelsBelongToSourceCluster(labels map[string]string) bool {
	sourceClusterID := strings.TrimSpace(labels["hypercdr.io/source-cluster-id"])
	return sourceClusterID != "" && sourceClusterID == c.cfg.ClusterID
}

func backupTaskPlanID(task protocol.TaskDispatchPayload) string {
	if task.Backup != nil {
		return strings.TrimSpace(task.Backup.PlanID)
	}
	return ""
}

func ensureSourceClusterLabel(labels map[string]string, clusterID string) {
	if labels == nil || strings.TrimSpace(clusterID) == "" {
		return
	}
	labels["hypercdr.io/source-cluster-id"] = clusterID
}

func (c *Client) sendVeleroBackupEvent(backup kube.VeleroBackupSummary) error {
	samples := c.backupSamples[backup.Name]
	volumePayload, volumeProgress, volumeMessage, _ := c.buildVolumeProgressPayload(context.Background(), kube.AppliedObject{
		APIVersion: "velero.io/v1",
		Kind:       "Backup",
		Namespace:  backup.Namespace,
		Name:       backup.Name,
	}, &samples)
	c.backupSamples[backup.Name] = samples

	progress := backupProgressPercent(backup)
	if volumeProgress > progress {
		progress = volumeProgress
	}
	if isTerminalBackupPhase(backup.Phase) {
		progress = 100
	}
	message := backupEventMessage(backup)
	if volumeMessage != "" && backup.Phase == "InProgress" {
		message = volumeMessage
	}
	velero := map[string]any{
		"kind":               "Backup",
		"name":               backup.Name,
		"namespace":          backup.Namespace,
		"phase":              backup.Phase,
		"resourceVersion":    backup.ResourceVersion,
		"storageLocation":    backup.StorageLocation,
		"includedNamespaces": backup.IncludedNamespaces,
		"labels":             backup.Labels,
		"createdAt":          backup.CreatedAt,
		"startedAt":          backup.StartedAt,
		"completedAt":        backup.CompletedAt,
		"itemsTotal":         backup.ItemsTotal,
		"itemsDone":          backup.ItemsDone,
		"errors":             backup.Errors,
		"warnings":           backup.Warnings,
	}
	if volumePayload != nil {
		velero["volumeProgress"] = volumePayload
	}
	if backup.Phase == "Completed" {
		c.attachBackupSizeStats(context.Background(), kube.AppliedObject{
			APIVersion: "velero.io/v1",
			Kind:       "Backup",
			Namespace:  backup.Namespace,
			Name:       backup.Name,
		}, velero)
	}
	signature := fmt.Sprintf("%s|%s|%d", backup.Phase, backup.ResourceVersion, progress)
	if volumePayload != nil {
		signature = fmt.Sprintf("%s|%v|%v|%v", signature, volumePayload["bytesDone"], volumePayload["totalBytes"], volumePayload["etaSeconds"])
	}
	if c.backupEvents[backup.Name] == signature {
		return nil
	}
	eventType := backupEventType(backup.Phase)
	event := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentVeleroEvent, c.cfg.ClusterID, c.cfg.AgentID, protocol.VeleroEventPayload{
		AckRequired:        eventType == "backup_completed" || eventType == "backup_failed",
		EventType:          backupEventType(backup.Phase),
		BackupName:         backup.Name,
		Namespace:          backup.Namespace,
		PlanID:             backup.Labels["hypercdr.io/plan-id"],
		TaskID:             backup.Labels["hypercdr.io/task-id"],
		CommandID:          backup.Labels["hypercdr.io/command-id"],
		SourceClusterID:    backup.Labels["hypercdr.io/source-cluster-id"],
		SourceNamespace:    backup.Labels["hypercdr.io/source-namespace"],
		ScheduleName:       backup.Labels["velero.io/schedule-name"],
		Phase:              backup.Phase,
		Progress:           progress,
		Message:            message,
		ResourceVersion:    backup.ResourceVersion,
		StorageLocation:    backup.StorageLocation,
		IncludedNamespaces: backup.IncludedNamespaces,
		StartedAt:          backup.StartedAt,
		CompletedAt:        backup.CompletedAt,
		Labels:             backup.Labels,
		Velero:             velero,
	})
	if event.Payload.AckRequired {
		if err := c.sendReliableEvent(event); err != nil {
			return err
		}
	} else if err := c.writeJSON(event); err != nil {
		return err
	}
	c.backupEvents[backup.Name] = signature
	return nil
}

func (c *Client) findConflictingActiveBackup(ctx context.Context, task protocol.TaskDispatchPayload, manifest velero.BackupManifest) (kube.VeleroBackupSummary, bool, error) {
	if c.backupReader == nil || task.Backup == nil {
		return kube.VeleroBackupSummary{}, false, nil
	}
	backups, err := c.backupReader.ListVeleroBackups(ctx, c.cfg.Namespace, 100)
	if err != nil {
		return kube.VeleroBackupSummary{}, false, err
	}
	planID := strings.TrimSpace(task.Backup.PlanID)
	if planID == "" {
		return kube.VeleroBackupSummary{}, false, nil
	}
	for _, backup := range backups {
		if backup.Name == manifest.Metadata.Name || !isActiveBackupPhase(backup.Phase) {
			continue
		}
		if backup.Labels["hypercdr.io/plan-id"] == planID {
			return backup, true, nil
		}
	}
	return kube.VeleroBackupSummary{}, false, nil
}

func (c *Client) findActiveBackupNameForCancel(ctx context.Context, planID string) (string, bool, error) {
	if c.backupReader == nil {
		return "", false, nil
	}
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return "", false, nil
	}
	backups, err := c.backupReader.ListVeleroBackups(ctx, c.cfg.Namespace, 100)
	if err != nil {
		return "", false, err
	}
	var latest kube.VeleroBackupSummary
	for _, backup := range backups {
		if backup.Labels["hypercdr.io/plan-id"] != planID || !isActiveBackupPhase(backup.Phase) {
			continue
		}
		if latest.Name == "" || backup.CreatedAt.After(latest.CreatedAt) {
			latest = backup
		}
	}
	if latest.Name == "" {
		return "", false, nil
	}
	return latest.Name, true, nil
}

func isHyperCDRBackup(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	return labels["hypercdr.io/managed-by"] == "hypercdr" || labels["hypercdr.io/plan-id"] != ""
}

func isActiveBackupPhase(phase string) bool {
	switch phase {
	case "", "New", "InProgress", "WaitingForPluginOperations", "WaitingForPluginOperationsPartiallyFailed", "Finalizing":
		return true
	default:
		return false
	}
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func manifestItemProgress(status kube.ManifestStatus) int {
	percent := manifestItemProgressPercent(status)
	if percent > 99 && !isTerminalBackupPhase(status.Phase) && !isTerminalRestorePhase(status.Phase) {
		return 99
	}
	return percent
}

func manifestItemProgressPercent(status kube.ManifestStatus) int {
	if status.ItemsTotal <= 0 || status.ItemsDone <= 0 {
		return 0
	}
	percent := int((status.ItemsDone * 100) / status.ItemsTotal)
	if percent > 100 {
		return 100
	}
	return percent
}

func backupProgressPercent(backup kube.VeleroBackupSummary) int {
	if backup.ItemsTotal > 0 && backup.ItemsDone > 0 {
		percent := int((backup.ItemsDone * 100) / backup.ItemsTotal)
		if percent > 99 && !isTerminalBackupPhase(backup.Phase) {
			return 99
		}
		return percent
	}
	switch backup.Phase {
	case "New", "":
		return 5
	case "InProgress":
		return 25
	default:
		if isTerminalBackupPhase(backup.Phase) {
			return 100
		}
		return 10
	}
}

func backupEventType(phase string) string {
	switch phase {
	case "Completed":
		return "backup_completed"
	case "Failed", "FailedValidation", "PartiallyFailed", "Canceled":
		return "backup_failed"
	case "InProgress":
		return "backup_progress"
	default:
		return "backup_started"
	}
}

func backupEventMessage(backup kube.VeleroBackupSummary) string {
	if backup.ItemsTotal > 0 {
		return fmt.Sprintf("Velero backup %s: %d / %d items", backup.Phase, backup.ItemsDone, backup.ItemsTotal)
	}
	if backup.Phase == "" {
		return "Velero backup observed"
	}
	return "Velero backup " + backup.Phase
}

func isTerminalBackupPhase(phase string) bool {
	switch phase {
	case "Completed", "PartiallyFailed", "Failed", "FailedValidation", "Canceled":
		return true
	default:
		return false
	}
}

func isTerminalRestorePhase(phase string) bool {
	switch phase {
	case "Completed", "PartiallyFailed", "Failed", "FailedValidation", "Canceled":
		return true
	default:
		return false
	}
}

func (c *Client) sendTaskAccepted(task protocol.TaskDispatchPayload) error {
	if err := c.ledger.remember(task.TaskID, task.CommandID, backupTaskPlanID(task)); err != nil {
		return err
	}
	message := protocol.NewMessage(protocol.MessageKindResponse, protocol.MessageAgentTaskAccepted, c.cfg.ClusterID, c.cfg.AgentID, protocol.TaskAcceptedPayload{
		AckMessageID: task.RequestMessageID,
		AckType:      protocol.MessagePlatformTaskDispatch,
		TaskID:       task.TaskID,
		CommandID:    task.CommandID,
		AcceptedAt:   time.Now().UTC(),
	})
	return c.writeJSON(message)
}

func (c *Client) sendTaskProgress(task protocol.TaskDispatchPayload, veleroPayload map[string]any, progress int, text string) error {
	metrics := taskProgressMetrics(veleroPayload)
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentTaskProgress, c.cfg.ClusterID, c.cfg.AgentID, protocol.TaskProgressPayload{
		AckRequired:         false,
		TaskID:              task.TaskID,
		CommandID:           task.CommandID,
		Status:              "running",
		Progress:            progress,
		TotalBytes:          metrics.totalBytes,
		SyncedBytes:         metrics.syncedBytes,
		SpeedBytesPerSecond: metrics.speedBytesPerSecond,
		Percent:             metrics.percent,
		EtaSeconds:          metrics.etaSeconds,
		Message:             text,
		Velero:              veleroPayload,
	})
	return c.writeJSON(message)
}

type taskProgressMetricSet struct {
	totalBytes          int64
	syncedBytes         int64
	speedBytesPerSecond float64
	percent             float64
	etaSeconds          int64
}

func taskProgressMetrics(veleroPayload map[string]any) taskProgressMetricSet {
	volumeProgress := wsMapFromAny(veleroPayload["volumeProgress"])
	if len(volumeProgress) == 0 {
		return taskProgressMetricSet{}
	}
	knownTotal, _ := volumeProgress["knownTotal"].(bool)
	allTotalsKnown, _ := volumeProgress["allTotalsKnown"].(bool)
	totalBytes := int64FromAny(volumeProgress["totalBytes"])
	syncedBytes := int64FromAny(volumeProgress["bytesDone"])
	if !knownTotal || !allTotalsKnown || totalBytes <= 0 {
		totalBytes = 0
	}
	percent := float64(0)
	if knownTotal && allTotalsKnown && totalBytes > 0 {
		percent = float64(syncedBytes) * 100 / float64(totalBytes)
		if percent > 100 {
			percent = 100
		}
	}
	return taskProgressMetricSet{
		totalBytes:          totalBytes,
		syncedBytes:         syncedBytes,
		speedBytesPerSecond: float64(int64FromAny(volumeProgress["speedBytesPerSecond"])),
		percent:             percent,
		etaSeconds:          int64FromAny(volumeProgress["etaSeconds"]),
	}
}

func (c *Client) sendTaskCompleted(task protocol.TaskDispatchPayload, veleroPayload map[string]any, text string) error {
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentTaskCompleted, c.cfg.ClusterID, c.cfg.AgentID, protocol.TaskCompletedPayload{
		AckRequired: true,
		TaskID:      task.TaskID,
		CommandID:   task.CommandID,
		Status:      "succeeded",
		Operation:   task.Type,
		Progress:    100,
		Message:     text,
		Velero:      veleroPayload,
	})
	return c.sendReliableEvent(message)
}

func (c *Client) sendTaskFailed(task protocol.TaskDispatchPayload, code string, text string) error {
	return c.sendTaskFailedWithDetails(task, code, text, nil)
}

func (c *Client) sendTaskFailedWithDetails(task protocol.TaskDispatchPayload, code string, text string, details map[string]any) error {
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentTaskFailed, c.cfg.ClusterID, c.cfg.AgentID, protocol.TaskFailedPayload{
		AckRequired: true,
		TaskID:      task.TaskID,
		CommandID:   task.CommandID,
		ErrorCode:   code,
		Message:     text,
		Details:     details,
	})
	return c.sendReliableEvent(message)
}

func backupVeleroPayload(manifest velero.BackupManifest, includeManifest bool) map[string]any {
	payload := manifestPayload(manifest.Kind, manifest.Metadata.Name, manifest.Metadata.Namespace)
	if includeManifest {
		payload["manifest"] = manifest
	}
	return payload
}

func restoreVeleroPayload(manifest velero.RestoreManifest, includeManifest bool) map[string]any {
	payload := manifestPayload(manifest.Kind, manifest.Metadata.Name, manifest.Metadata.Namespace)
	if includeManifest {
		payload["manifest"] = manifest
	}
	return payload
}

func storageVeleroPayload(manifest velero.BackupStorageLocationManifest, includeManifest bool) map[string]any {
	payload := manifestPayload(manifest.Kind, manifest.Metadata.Name, manifest.Metadata.Namespace)
	if includeManifest {
		payload["manifest"] = manifest
	}
	return payload
}

func manifestPayload(kind string, name string, namespace string) map[string]any {
	payload := map[string]any{
		"kind":      kind,
		"name":      name,
		"namespace": namespace,
	}
	return payload
}

func (c *Client) sendInventory(full bool) error {
	snapshot, err := c.collector.Collect()
	if err != nil {
		return err
	}
	snapshot.Report.Full = full
	c.lastInventory = snapshot
	return c.writeInventory(snapshot.Report)
}

func (c *Client) writeInventory(report protocol.InventoryReportPayload) error {
	report.Velero.RecentBackups = c.filterInventoryBackups(report.Velero.RecentBackups)
	report.AckRequired = false
	if report.Scope == "" {
		report.Scope = "summary"
	}
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentInventoryReport, c.cfg.ClusterID, c.cfg.AgentID, report)
	if err := c.writeJSON(message); err != nil {
		return err
	}
	c.logger.Info("inventory sent",
		"cluster_id", c.cfg.ClusterID,
		"full", report.Full,
		"node_count", report.Cluster.NodeCount,
		"application_count", len(report.Apps),
		"inventory_hash", report.InventoryHash,
	)
	return nil
}

func (c *Client) filterInventoryBackups(backups []map[string]any) []map[string]any {
	if len(backups) == 0 {
		return backups
	}
	filtered := make([]map[string]any, 0, len(backups))
	for _, backup := range backups {
		labels := stringMapFromAny(backup["labels"])
		sourceClusterID := strings.TrimSpace(labels["hypercdr.io/source-cluster-id"])
		if sourceClusterID != "" && sourceClusterID != c.cfg.ClusterID {
			continue
		}
		filtered = append(filtered, backup)
	}
	return filtered
}

func (c *Client) sendHeartbeat(_ bool) error {
	snapshot := c.lastInventory
	veleroStatus := snapshot.Report.Velero.Status
	if veleroStatus == "" {
		veleroStatus = "unknown"
	}
	agentImage := c.cfg.AgentImage
	agentImageID := ""
	agentImageDigest := ""
	veleroRuntime := kube.VeleroRuntimeStatus{}
	if c.agentRuntime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		image, imageID, digest, err := c.agentRuntime.PodImageStatus(ctx, c.cfg.Namespace, c.cfg.PodName, "comm-agent")
		cancel()
		if err != nil {
			c.logger.Warn("failed to read agent pod image status", "error", err)
		} else {
			if image != "" {
				agentImage = image
			}
			agentImageID = imageID
			agentImageDigest = digest
		}
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		veleroRuntime, err = c.agentRuntime.VeleroRuntimeStatus(ctx, c.cfg.Namespace)
		cancel()
		if err != nil {
			c.logger.Warn("failed to read velero runtime status", "error", err)
		}
	}
	payload := protocol.HeartbeatPayload{
		AckRequired:                false,
		Status:                     "healthy",
		AgentVersion:               c.cfg.AgentVersion,
		AgentImage:                 agentImage,
		AgentImageID:               agentImageID,
		AgentImageDigest:           agentImageDigest,
		VeleroStatus:               veleroStatus,
		VeleroVersion:              veleroRuntime.Version,
		VeleroImage:                veleroRuntime.Image,
		VeleroImageDigest:          veleroRuntime.ImageDigest,
		VeleroServerReady:          veleroRuntime.ServerReady,
		VeleroNodeAgentDesired:     veleroRuntime.NodeAgentDesired,
		VeleroNodeAgentReady:       veleroRuntime.NodeAgentReady,
		VeleroNodeAgentImageDigest: veleroRuntime.NodeAgentImageDigest,
		ActiveTasks:                0,
		LastInventoryAt:            snapshot.Report.CollectedAt.Format(time.RFC3339),
	}
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentHeartbeat, c.cfg.ClusterID, c.cfg.AgentID, payload)
	if err := c.writeJSON(message); err != nil {
		return err
	}
	c.logger.Info("heartbeat sent",
		"cluster_id", c.cfg.ClusterID,
		"status", payload.Status,
		"last_inventory_at", payload.LastInventoryAt,
	)
	return nil
}

func (c *Client) writeJSON(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(value)
}

func (c *Client) writeRawJSON(raw []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, raw)
}

func (c *Client) sendReliableEvent(value any) error {
	if err := c.outbox.add(value); err != nil {
		return err
	}
	return c.writeJSON(value)
}
