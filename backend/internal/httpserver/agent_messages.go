package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"strings"
	"time"
)

func (r *Router) readAgentMessages(conn *websocket.Conn, clusterID string) {
	for {
		var meta struct {
			Type      string `json:"type"`
			ClusterID string `json:"clusterId"`
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			r.logger.Info("agent websocket closed", "cluster_id", clusterID, "error", err)
			return
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			r.logger.Warn("failed to decode agent message metadata", "cluster_id", clusterID, "error", err)
			return
		}

		switch meta.Type {
		case protocol.MessageAgentHeartbeat:
			var heartbeat protocol.Message[protocol.HeartbeatPayload]
			if err := json.Unmarshal(data, &heartbeat); err != nil {
				r.logger.Warn("failed to decode heartbeat", "cluster_id", clusterID, "error", err)
				return
			}
			updated, ok, err := r.store.UpdateHeartbeat(store.HeartbeatInput{
				ClusterID:                  clusterID,
				Status:                     heartbeat.Payload.Status,
				AgentVersion:               heartbeat.Payload.AgentVersion,
				AgentImage:                 heartbeat.Payload.AgentImage,
				AgentImageID:               heartbeat.Payload.AgentImageID,
				AgentImageDigest:           heartbeat.Payload.AgentImageDigest,
				VeleroStatus:               heartbeat.Payload.VeleroStatus,
				VeleroVersion:              heartbeat.Payload.VeleroVersion,
				VeleroImage:                heartbeat.Payload.VeleroImage,
				VeleroImageDigest:          heartbeat.Payload.VeleroImageDigest,
				VeleroServerReady:          heartbeat.Payload.VeleroServerReady,
				VeleroNodeAgentDesired:     heartbeat.Payload.VeleroNodeAgentDesired,
				VeleroNodeAgentReady:       heartbeat.Payload.VeleroNodeAgentReady,
				VeleroNodeAgentImageDigest: heartbeat.Payload.VeleroNodeAgentImageDigest,
				ActiveTasks:                heartbeat.Payload.ActiveTasks,
			})
			if err != nil {
				r.logger.Error("failed to update heartbeat", "cluster_id", clusterID, "error", err)
				return
			}
			if !ok {
				r.logger.Warn("heartbeat for unknown cluster", "cluster_id", clusterID)
				return
			}
			r.completeAgentUpgradeAfterHeartbeat(updated)
			r.completeVeleroUpgradeAfterHeartbeat(updated)
			r.logger.Info("agent heartbeat",
				"cluster_id", updated.ID,
				"status", updated.Status,
				"last_inventory_at", heartbeat.Payload.LastInventoryAt,
			)
		case protocol.MessageAgentInventoryReport:
			var inventory protocol.Message[protocol.InventoryReportPayload]
			if err := json.Unmarshal(data, &inventory); err != nil {
				r.logger.Warn("failed to decode inventory report", "cluster_id", clusterID, "error", err)
				return
			}
			apps := make([]store.Application, 0, len(inventory.Payload.Apps))
			for _, app := range inventory.Payload.Apps {
				workloadCount := app.Resources.Deployments + app.Resources.StatefulSets + app.Resources.DaemonSets + app.Resources.Jobs + app.Resources.CronJobs
				resourceSummary := map[string]any{
					"deployments":     app.Resources.Deployments,
					"statefulsets":    app.Resources.StatefulSets,
					"daemonsets":      app.Resources.DaemonSets,
					"jobs":            app.Resources.Jobs,
					"cronjobs":        app.Resources.CronJobs,
					"services":        app.Resources.Services,
					"ingresses":       app.Resources.Ingresses,
					"networkPolicies": app.Resources.NetworkPolicies,
					"configmaps":      app.Resources.ConfigMaps,
					"secrets":         app.Resources.Secrets,
					"serviceAccounts": app.Resources.ServiceAccounts,
					"pvcs":            app.Resources.PVCs,
					"pvCapacityBytes": app.Resources.PVCapacityBytes,
					"ageSeconds":      app.AgeSeconds,
				}
				if len(app.Resources.Categories) > 0 {
					resourceSummary["categories"] = app.Resources.Categories
				}
				if app.Resources.DRSupport != nil {
					resourceSummary["drSupport"] = app.Resources.DRSupport
				}
				apps = append(apps, store.Application{
					Namespace:       app.Namespace,
					Name:            app.Namespace,
					Status:          app.Status,
					Labels:          app.Labels,
					WorkloadCount:   workloadCount,
					ServiceCount:    app.Resources.Services,
					IngressCount:    app.Resources.Ingresses,
					ConfigMapCount:  app.Resources.ConfigMaps,
					SecretCount:     app.Resources.Secrets,
					PVCCount:        app.Resources.PVCs,
					PVCapacityBytes: app.Resources.PVCapacityBytes,
					ResourceSummary: resourceSummary,
					LastCollectedAt: inventory.Payload.CollectedAt,
				})
			}
			updated, ok, err := r.store.ApplyInventory(store.InventoryInput{
				ClusterID:            clusterID,
				KubeVersion:          inventory.Payload.Cluster.KubeVersion,
				VeleroStatus:         inventory.Payload.Velero.Status,
				NodeCount:            inventory.Payload.Cluster.NodeCount,
				NamespaceCount:       inventory.Payload.Cluster.NamespaceCount,
				Nodes:                mapInventoryNodes(inventory.Payload.Nodes),
				StorageClasses:       mapInventoryStorageClasses(inventory.Payload.StorageClasses),
				APIResources:         mapInventoryAPIResources(inventory.Payload.APIResources),
				NamespaceAPIs:        mapInventoryNamespaceAPIs(inventory.Payload.NamespaceAPIs),
				Capabilities:         mapInventoryCapabilities(inventory.Payload.Capabilities),
				CapabilityScan:       inventory.Payload.Scope == "capabilities",
				CapabilityNamespace:  inventory.Payload.Namespace,
				CapabilitiesComplete: inventory.Payload.CapabilitiesComplete,
				Apps:                 apps,
				CollectedAt:          inventory.Payload.CollectedAt,
				Hash:                 inventory.Payload.InventoryHash,
			})
			if err != nil {
				r.logger.Error("failed to apply inventory", "cluster_id", clusterID, "error", err)
				return
			}
			if !ok {
				r.logger.Warn("inventory for unknown cluster", "cluster_id", clusterID)
				return
			}
			if r.editionMetering != nil && inventory.Payload.Full && inventory.Payload.Scope == "" {
				nodes := make([]EditionMeteredNode, 0, len(inventory.Payload.Nodes))
				for _, node := range inventory.Payload.Nodes {
					billable := !(node.Role == "control-plane" && node.Unschedulable)
					nodes = append(nodes, EditionMeteredNode{Name: node.Name, Billable: billable})
				}
				if meterErr := r.editionMetering(context.Background(), EditionMeteringEvent{Operation: "cluster.inventory", TenantID: updated.TenantID, ClusterID: updated.ID, Nodes: nodes}); meterErr != nil {
					r.logger.Error("failed to record edition metering inventory", "cluster_id", updated.ID, "error", meterErr)
				}
			}
			r.logger.Info("agent inventory applied",
				"cluster_id", updated.ID,
				"node_count", updated.NodeCount,
				"application_count", updated.ApplicationCount,
			)
			r.ingestVeleroBackupsFromInventory(clusterID, inventory.Payload.Velero.RecentBackups)
			r.completeInventoryRequest(clusterID, inventory.Payload)
		case protocol.MessageAgentMessageError:
			var messageError protocol.Message[protocol.MessageErrorPayload]
			if err := json.Unmarshal(data, &messageError); err != nil {
				r.logger.Warn("failed to decode agent message error", "cluster_id", clusterID, "error", err)
				return
			}
			r.logger.Warn("agent rejected platform message",
				"cluster_id", clusterID,
				"ack_message_id", messageError.Payload.AckMessageID,
				"ack_type", messageError.Payload.AckType,
				"request_id", messageError.Payload.RequestID,
				"task_id", messageError.Payload.TaskID,
				"error_code", messageError.Payload.ErrorCode,
				"message", messageError.Payload.Message,
				"retryable", messageError.Payload.Retryable,
			)
			if messageError.Payload.AckType == protocol.MessagePlatformInventoryRequest {
				r.failInventoryRequest(clusterID, messageError.Payload)
			}
		case protocol.MessageAgentLogReport:
			var report protocol.Message[protocol.LogReportPayload]
			if err := json.Unmarshal(data, &report); err != nil {
				r.logger.Warn("failed to decode agent log report", "cluster_id", clusterID, "error", err)
				continue
			}
			clusters, _ := r.store.ListClusters()
			tenantID := ""
			for _, cluster := range clusters {
				if cluster.ID == clusterID {
					tenantID = cluster.TenantID
					break
				}
			}
			if tenantID == "" {
				r.logger.Warn("log report for unknown cluster", "cluster_id", clusterID)
				report.Payload.ErrorCode = "CLUSTER_NOT_FOUND"
				report.Payload.Message = "The cluster associated with the log report no longer exists."
			} else if report.Payload.ErrorCode != "" {
				_, _ = r.store.CreateDiagnosticLog(store.DiagnosticLogInput{TenantID: tenantID, Scope: "tenant", Level: "error", Component: report.Payload.Component, Operation: "collect_logs", Message: report.Payload.Message, ClusterID: clusterID, RequestID: report.Payload.RequestID, ErrorCode: report.Payload.ErrorCode})
			} else {
				for _, entry := range report.Payload.Entries {
					message := sanitizeDiagnosticMessage(entry.Message)
					fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{clusterID, entry.Component, entry.Pod, entry.Node, entry.Timestamp.UTC().Format(time.RFC3339Nano), message}, "\x00"))))
					_, err := r.store.CreateDiagnosticLog(store.DiagnosticLogInput{TenantID: tenantID, Scope: "tenant", Level: entry.Level, Component: entry.Component, Operation: "container_log", Message: message, ClusterID: clusterID, RequestID: report.Payload.RequestID, EventAt: entry.Timestamp, Fingerprint: fingerprint, Details: map[string]any{"pod": entry.Pod, "node": entry.Node, "sourceTimestamp": entry.Timestamp}})
					if err != nil {
						r.logger.Error("persist agent log entry failed", "cluster_id", clusterID, "request_id", report.Payload.RequestID, "error", err)
						report.Payload.ErrorCode = "LOG_PERSIST_FAILED"
						report.Payload.Message = "The logs were collected but could not be saved by the platform."
						break
					}
				}
			}
			// Notify the waiting HTTP request only after every entry has been persisted.
			// A completed response therefore guarantees that an immediate query can see the logs.
			r.logRequestMu.Lock()
			waiter := r.logRequests[report.Payload.RequestID]
			r.logRequestMu.Unlock()
			if waiter != nil {
				select {
				case waiter <- report.Payload:
				default:
				}
			}
		case protocol.MessageAgentBackupContentReport:
			var report protocol.Message[protocol.BackupContentReportPayload]
			if err := json.Unmarshal(data, &report); err != nil {
				r.logger.Warn("failed to decode backup contents report", "cluster_id", clusterID, "error", err)
				continue
			}
			r.backupContentRequestMu.Lock()
			waiter := r.backupContentRequests[report.Payload.RequestID]
			r.backupContentRequestMu.Unlock()
			if waiter != nil {
				select {
				case waiter <- report.Payload:
				default:
				}
			}
		case protocol.MessageAgentTaskAccepted:
			var accepted protocol.Message[protocol.TaskAcceptedPayload]
			if err := json.Unmarshal(data, &accepted); err != nil {
				r.logger.Warn("failed to decode task accepted", "cluster_id", clusterID, "error", err)
				return
			}
			if strings.TrimSpace(accepted.Payload.TaskID) == "" {
				r.logger.Warn("ignored task accepted without task id", "cluster_id", clusterID, "command_id", accepted.Payload.CommandID)
				continue
			}
			existing, found, lookupErr := r.findTaskByID(clusterID, accepted.Payload.TaskID)
			if lookupErr != nil || !found || (accepted.Payload.CommandID != "" && existing.CommandID != "" && accepted.Payload.CommandID != existing.CommandID) {
				r.logger.Warn("ignored task accepted with invalid task identity", "cluster_id", clusterID, "task_id", accepted.Payload.TaskID, "command_id", accepted.Payload.CommandID)
				continue
			}
			_, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       accepted.Payload.TaskID,
				Status:       "accepted",
				Progress:     0,
				MarkAccepted: true,
			})
			if err != nil {
				r.logger.Error("failed to update accepted task", "task_id", accepted.Payload.TaskID, "error", err)
				return
			}
			_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: accepted.Payload.TaskID, Level: "info", Reason: "accepted", Message: "agent accepted task"})
		case protocol.MessageAgentTaskProgress:
			var progress protocol.Message[protocol.TaskProgressPayload]
			if err := json.Unmarshal(data, &progress); err != nil {
				r.logger.Warn("failed to decode task progress", "cluster_id", clusterID, "error", err)
				return
			}
			existing, ok, err := r.findTaskByID(clusterID, progress.Payload.TaskID)
			if err != nil || !ok {
				r.logger.Warn("ignored progress for unknown task", "cluster_id", clusterID, "task_id", progress.Payload.TaskID, "error", err)
				continue
			}
			if !existing.CompletedAt.IsZero() || !isActiveTaskStatus(existing.Status) {
				r.logger.Info("ignored late progress for terminal task", "task_id", existing.ID, "status", existing.Status)
				continue
			}
			if progress.Payload.CommandID != "" && existing.CommandID != "" && progress.Payload.CommandID != existing.CommandID {
				r.logger.Warn("ignored progress with mismatched command", "task_id", existing.ID, "command_id", progress.Payload.CommandID)
				continue
			}
			if incoming := progress.Payload.SizeProgressV2; incoming != nil {
				current := mapFromAny(existing.Payload["sizeProgressV2"])
				if currentSequence := int64FromAny(current["sequence"]); currentSequence > 0 && incoming.Sequence > 0 && incoming.Sequence <= currentSequence {
					continue
				}
			}
			_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:      progress.Payload.TaskID,
				Status:      progress.Payload.Status,
				Progress:    progress.Payload.Progress,
				Payload:     taskProgressPayloadPatch(progress.Payload),
				MarkStarted: true,
			})
			if err != nil {
				r.logger.Error("failed to update task progress", "task_id", progress.Payload.TaskID, "error", err)
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  progress.Payload.TaskID,
				Level:   "info",
				Reason:  "progress",
				Message: progress.Payload.Message,
				Payload: map[string]any{"velero": progress.Payload.Velero},
			})
		case protocol.MessageAgentVeleroEvent:
			var event protocol.Message[protocol.VeleroEventPayload]
			if err := json.Unmarshal(data, &event); err != nil {
				r.logger.Warn("failed to decode velero event", "cluster_id", clusterID, "error", err)
				return
			}
			task, err := r.handleVeleroBackupEvent(clusterID, event.Payload)
			if err != nil {
				if event.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, event.AgentID, event.MessageID, event.Type, event.Payload.TaskID, event.Payload.CommandID, "VELERO_EVENT_HANDLE_FAILED", err.Error(), true)
				}
				continue
			}
			if event.Payload.AckRequired {
				taskID := event.Payload.TaskID
				commandID := event.Payload.CommandID
				if taskID == "" && task.ID != "" {
					taskID = task.ID
				}
				if commandID == "" && task.CommandID != "" {
					commandID = task.CommandID
				}
				_ = r.writeEventAck(conn, clusterID, event.AgentID, event.MessageID, event.Type, taskID, commandID)
			}
		case protocol.MessageAgentTaskCompleted:
			var completed protocol.Message[protocol.TaskCompletedPayload]
			if err := json.Unmarshal(data, &completed); err != nil {
				r.logger.Warn("failed to decode task completed", "cluster_id", clusterID, "error", err)
				return
			}
			if strings.TrimSpace(completed.Payload.TaskID) == "" {
				r.logger.Warn("ignored task completion without task id", "cluster_id", clusterID, "command_id", completed.Payload.CommandID)
				continue
			}
			existingTask, ok, err := r.findTaskByID(clusterID, completed.Payload.TaskID)
			if err != nil {
				r.logger.Error("failed to load task before completion", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_COMPLETE_FAILED", err.Error(), true)
				}
				return
			}
			if !ok {
				r.logger.Error("task not found before completion", "task_id", completed.Payload.TaskID)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_NOT_FOUND", "task not found", true)
				}
				return
			}
			if existingTask.Type == "backup" && isForceStoppedBackupTask(existingTask) {
				_ = r.store.AddTaskEvent(store.TaskEventInput{
					TaskID:  existingTask.ID,
					Level:   "warning",
					Reason:  "completion_after_cancel",
					Message: "Ignored backup completion because force stop already finalized this sync task.",
					Payload: map[string]any{"velero": completed.Payload.Velero},
				})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			if existingTask.Type == "backup-cancel" {
				if err := r.finishBackupCancelTask(clusterID, existingTask, completed.Payload); err != nil {
					r.logger.Error("failed to finish backup cancel task", "task_id", completed.Payload.TaskID, "error", err)
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "BACKUP_CANCEL_FINISH_FAILED", err.Error(), true)
					}
					return
				}
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			if existingTask.Type == "agent-upgrade" || existingTask.Type == "velero-upgrade" {
				progress := 70
				reason := "waiting_for_reconnect"
				message := "agent deployment updated; waiting for the new agent to reconnect"
				if existingTask.Type == "velero-upgrade" {
					progress = 90
					reason = "waiting_for_verification"
					message = "velero rollout completed; waiting for server and node-agent digest verification"
				}
				_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
					TaskID:      existingTask.ID,
					Status:      "running",
					Progress:    progress,
					Payload:     taskCompletedPayloadPatch(completed.Payload),
					MarkStarted: true,
				})
				if err != nil {
					r.logger.Error("failed to mark agent upgrade waiting for reconnect", "task_id", existingTask.ID, "error", err)
					return
				}
				_ = r.addTaskEventIfChanged(store.TaskEventInput{
					TaskID: existingTask.ID, Level: "info", Reason: reason, Message: message,
				})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			patch := taskCompletedPayloadPatch(completed.Payload)
			existingTask, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:      completed.Payload.TaskID,
				Status:      "finalizing",
				Progress:    100,
				Payload:     patch,
				MarkStarted: true,
			})
			if err != nil {
				r.logger.Error("failed to mark task finalizing", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_FINALIZE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  completed.Payload.TaskID,
				Level:   "info",
				Reason:  "finalizing",
				Message: "finalizing restore point",
				Payload: map[string]any{"velero": completed.Payload.Velero},
			})
			if existingTask.Type == "backup" {
				taskForPoint := existingTask
				if taskForPoint.Payload == nil {
					taskForPoint.Payload = map[string]any{}
				}
				for key, value := range patch {
					taskForPoint.Payload[key] = value
				}
				if point, err := r.createRestorePointFromBackup(taskForPoint, completed.Payload.Velero); err != nil {
					r.logger.Error("failed to create restore point", "task_id", existingTask.ID, "error", err)
					_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
						TaskID:       completed.Payload.TaskID,
						Status:       "failed",
						Progress:     100,
						ErrorCode:    "RESTORE_POINT_CREATE_FAILED",
						ErrorMessage: err.Error(),
						MarkDone:     true,
					})
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "RESTORE_POINT_CREATE_FAILED", err.Error(), true)
					}
					continue
				} else if point.ID != "" {
					r.reconcileRetention(point.ProtectionPlanID, point.BackupTaskID)
				}
			}
			if existingTask.Type == "unregister" {
				if err := r.finishUnregisterTask(clusterID, existingTask); err != nil {
					_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: existingTask.ID, Status: "failed", Progress: 95, ErrorCode: "PLATFORM_CLEANUP_FAILED", ErrorMessage: err.Error(), MarkDone: true})
					_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: existingTask.ID, Level: "error", Reason: "PLATFORM_CLEANUP_FAILED", Message: err.Error()})
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "PLATFORM_CLEANUP_FAILED", err.Error(), true)
					}
					return
				}
				_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: existingTask.ID, Status: "succeeded", Progress: 100, Payload: patch, MarkDone: true})
				_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: existingTask.ID, Level: "info", Reason: "completed", Message: "cluster unregister completed"})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				return
			}
			task, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:   completed.Payload.TaskID,
				Status:   "succeeded",
				Progress: 100,
				Payload:  patch,
				MarkDone: true,
			})
			if err != nil {
				r.logger.Error("failed to complete task", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_COMPLETE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  completed.Payload.TaskID,
				Level:   "info",
				Reason:  "completed",
				Message: completed.Payload.Message,
				Payload: map[string]any{"velero": completed.Payload.Velero},
			})
			if task.Type == "storage-sync" && taskPayloadBool(task.Payload, "reconfigureStorage") {
				r.markClusterStorageBindingReady(task)
				r.finishProtectionPlanStorageReconfigure(task)
			} else if task.Type == "storage-sync" {
				r.markClusterStorageBindingReady(task)
				r.continueProtectionPlanActivationAfterStorage(task)
			}
			if task.Type == "schedule-sync" && task.ProtectionPlanID != "" {
				status := "active"
				if r.hasTargetStorageWarning(task.ProtectionPlanID) {
					status = "active_with_warning"
				}
				if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, status); err != nil {
					r.logger.Error("failed to mark protection plan active", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
				}
			}
			if task.Type == "retention-cleanup" {
				r.finishRetentionCleanupTask(task, completed.Payload.Velero)
			}
			if task.Type == "protection-cleanup" {
				r.finishProtectionCleanupTask(task, completed.Payload.Velero)
			}
			if completed.Payload.AckRequired {
				_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
			}
		case protocol.MessageAgentTaskFailed:
			var failed protocol.Message[protocol.TaskFailedPayload]
			if err := json.Unmarshal(data, &failed); err != nil {
				r.logger.Warn("failed to decode task failed", "cluster_id", clusterID, "error", err)
				return
			}
			if strings.TrimSpace(failed.Payload.TaskID) == "" {
				r.logger.Warn("ignored task failure without task id", "cluster_id", clusterID, "command_id", failed.Payload.CommandID)
				continue
			}
			errorMessage := detailedTaskFailureMessage(failed.Payload.Message, failed.Payload.Details)
			payloadPatch := taskFailurePayloadPatch(failed.Payload.Details)
			existingTask, _, lookupErr := r.findTaskByID(clusterID, failed.Payload.TaskID)
			if lookupErr == nil && existingTask.Type == "backup" && isForceStoppedBackupTask(existingTask) {
				_ = r.store.AddTaskEvent(store.TaskEventInput{
					TaskID:  existingTask.ID,
					Level:   "warning",
					Reason:  failed.Payload.ErrorCode,
					Message: "Ignored backup failure because force stop is in progress or already completed.",
					Payload: failed.Payload.Details,
				})
				if failed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
				}
				continue
			}
			task, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       failed.Payload.TaskID,
				Status:       "failed",
				Progress:     0,
				ErrorCode:    failed.Payload.ErrorCode,
				ErrorMessage: errorMessage,
				Payload:      payloadPatch,
				MarkDone:     true,
			})
			if err != nil {
				r.logger.Error("failed to mark task failed", "task_id", failed.Payload.TaskID, "error", err)
				if failed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID, "TASK_FAIL_UPDATE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  failed.Payload.TaskID,
				Level:   "error",
				Reason:  failed.Payload.ErrorCode,
				Message: errorMessage,
				Payload: failed.Payload.Details,
			})
			if task.Type == "retention-cleanup" {
				r.markRetentionCleanupFailed(task, failed.Payload.Message)
			}
			if task.Type == "protection-cleanup" {
				r.markProtectionCleanupFailed(task, failed.Payload.Message)
			}
			if task.Type == "backup-cancel" {
				r.markBackupCancelFailed(task, failed.Payload.Message)
			}
			if task.Type == "storage-sync" && task.ProtectionPlanID != "" {
				r.markClusterStorageBindingFailed(task, failed.Payload.ErrorCode, failed.Payload.Message)
				if r.retryStorageSyncTask(task, failed.Payload.Message) {
					if failed.Payload.AckRequired {
						_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
					}
					continue
				}
				if taskPayloadString(task.Payload, "activationRole") == "target" {
					r.finishTargetStorageSyncFailed(task)
				} else {
					if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "storage_failed"); err != nil {
						r.logger.Error("failed to mark protection plan storage failed", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
					}
				}
			} else if task.Type == "storage-sync" {
				r.markClusterStorageBindingFailed(task, failed.Payload.ErrorCode, failed.Payload.Message)
			}
			if task.Type == "schedule-sync" && task.ProtectionPlanID != "" {
				if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "schedule_failed"); err != nil {
					r.logger.Error("failed to mark protection plan schedule failed", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
				}
			}
			if failed.Payload.AckRequired {
				_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
			}
		default:
			r.logger.Warn("unsupported agent message", "cluster_id", clusterID, "type", meta.Type)
		}
	}
}
