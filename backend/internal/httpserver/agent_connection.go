package httpserver

import (
	"errors"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
	"time"
)

func (r *Router) agentWebSocket(w http.ResponseWriter, req *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(req *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		r.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	var register protocol.Message[protocol.RegisterPayload]
	if err := conn.ReadJSON(&register); err != nil {
		r.logger.Warn("failed to read register message", "error", err)
		return
	}

	if register.Type != protocol.MessageAgentRegister {
		_ = conn.WriteJSON(newRejectedMessage("EXPECTED_REGISTER", "first message must be agent.register"))
		return
	}

	var cluster store.Cluster
	var credential string
	isNewRegistration := false
	if register.Payload.AgentCredential != "" {
		authenticated, ok, err := r.store.AuthenticateAgentCredential(store.AgentCredentialInput{
			ClusterID:  register.ClusterID,
			Credential: register.Payload.AgentCredential,
		})
		if err != nil {
			r.logger.Error("failed to authenticate agent credential", "cluster_id", register.ClusterID, "error", err)
			_ = conn.WriteJSON(newRejectedMessage("CREDENTIAL_AUTH_FAILED", "failed to authenticate agent credential"))
			return
		}
		if !ok {
			_ = conn.WriteJSON(newRejectedMessage("CREDENTIAL_INVALID", "agent credential is invalid"))
			return
		}
		cluster = authenticated
		credential = register.Payload.AgentCredential
	} else {
		registered, issuedCredential, err := r.store.RegisterCluster(store.RegisterClusterInput{
			Token:          register.Payload.InstallToken,
			ClusterName:    register.Payload.Cluster.Name,
			ControlPlaneIP: register.Payload.Cluster.ControlPlaneIP,
			KubeVersion:    register.Payload.Cluster.KubeVersion,
			AgentVersion:   register.Payload.Agent.Version,
			VeleroVersion:  register.Payload.Velero.Version,
			VeleroStatus:   register.Payload.Velero.Status,
			NodeCount:      register.Payload.Cluster.NodeCount,
			ClusterType:    register.Payload.Cluster.ClusterType,
			CloudProvider:  register.Payload.Cluster.CloudProvider,
			CloudRegion:    register.Payload.Cluster.CloudRegion,
			CloudClusterID: register.Payload.Cluster.CloudClusterID,
		})
		if err != nil {
			reason := "TOKEN_INVALID"
			if errors.Is(err, store.ErrTokenExpired) {
				reason = "TOKEN_EXPIRED"
			}
			if errors.Is(err, store.ErrTokenUsed) {
				reason = "TOKEN_USED"
			}
			_ = conn.WriteJSON(newRejectedMessage(reason, err.Error()))
			return
		}
		if r.editionAdmission != nil {
			decision := r.editionAdmission(req.Context(), EditionAdmissionRequest{Operation: "cluster.register", TenantID: registered.TenantID, ClusterID: registered.ID, WorkerNodes: register.Payload.Cluster.NodeCount})
			if !decision.Allowed {
				if _, deleteErr := r.store.DeleteCluster(registered.ID); deleteErr != nil {
					r.logger.Error("failed to roll back license-rejected cluster registration", "cluster_id", registered.ID, "error", deleteErr)
					decision.Message += "; platform rollback failed and residual cluster records may remain"
				}
				reason := strings.TrimSpace(decision.Code)
				if reason == "" {
					reason = "LICENSE_CAPACITY_EXCEEDED"
				}
				_ = conn.WriteJSON(newRejectedMessage(reason, decision.Message))
				return
			}
		}
		cluster = registered
		credential = issuedCredential
		isNewRegistration = true
	}

	accepted := protocol.Message[protocol.RegisterAcceptedPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformRegisterAccepted,
		TenantID:    cluster.TenantID,
		ClusterID:   cluster.ID,
		AgentID:     register.AgentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.RegisterAcceptedPayload{
			AckMessageID:                    register.MessageID,
			AckType:                         protocol.MessageAgentRegister,
			RequestID:                       register.Payload.RequestID,
			TenantID:                        cluster.TenantID,
			ClusterID:                       cluster.ID,
			ClusterName:                     cluster.Name,
			AgentCredential:                 credential,
			HeartbeatIntervalSeconds:        30,
			InventoryResyncIntervalSeconds:  300,
			InventoryChangeDebounceSeconds:  8,
			InventoryMinPushIntervalSeconds: 15,
			ProtocolVersion:                 protocol.Version,
			Features: map[string]bool{
				"taskDispatch":    true,
				"inventoryReport": true,
				"veleroEvent":     true,
				"sizeReport":      true,
			},
		},
	}
	if err := conn.WriteJSON(accepted); err != nil {
		r.logger.Warn("failed to write register accepted", "cluster_id", cluster.ID, "error", err)
		return
	}
	if isNewRegistration {
		task, err := r.store.CreateTask(store.TaskInput{
			ClusterID: cluster.ID,
			Type:      "register",
			Status:    "succeeded",
			Payload: map[string]any{
				"agentId":       register.AgentID,
				"clusterId":     cluster.ID,
				"clusterName":   cluster.Name,
				"kubeVersion":   register.Payload.Cluster.KubeVersion,
				"agentVersion":  register.Payload.Agent.Version,
				"veleroVersion": register.Payload.Velero.Version,
				"veleroStatus":  register.Payload.Velero.Status,
			},
		})
		if err != nil {
			r.logger.Warn("failed to record register task", "cluster_id", cluster.ID, "error", err)
		} else {
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:   task.ID,
				Status:   "succeeded",
				Progress: 100,
				MarkDone: true,
			})
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  task.ID,
				Level:   "info",
				Reason:  "register.accepted",
				Message: "platform accepted agent registration and issued cluster identity",
				Payload: map[string]any{
					"clusterId": cluster.ID,
					"agentId":   register.AgentID,
				},
			})
		}
	}
	r.logger.Info("agent registered", "cluster_id", cluster.ID, "cluster", cluster.Name)

	r.hub.set(cluster.ID, conn)
	r.configureAgentConnection(conn, cluster.ID)
	pingDone := make(chan struct{})
	go r.pingAgentConnection(conn, cluster.ID, pingDone)
	defer func() {
		close(pingDone)
		r.hub.remove(cluster.ID, conn)
		if _, _, err := r.store.SetClusterConnectionStatus(cluster.ID, "offline"); err != nil {
			r.logger.Warn("failed to mark agent offline", "cluster_id", cluster.ID, "error", err)
		}
	}()
	r.redispatchPendingTasks(cluster.ID, conn)
	r.readAgentMessages(conn, cluster.ID)
}

func (r *Router) configureAgentConnection(conn *websocket.Conn, clusterID string) {
	_ = conn.SetReadDeadline(time.Now().Add(agentPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(agentPongWait))
	})
	conn.SetCloseHandler(func(code int, text string) error {
		r.logger.Info("agent websocket close received", "cluster_id", clusterID, "code", code, "text", text)
		return nil
	})
}

func (r *Router) pingAgentConnection(conn *websocket.Conn, clusterID string, done <-chan struct{}) {
	ticker := time.NewTicker(agentPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			deadline := time.Now().Add(5 * time.Second)
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
				r.logger.Info("agent websocket ping failed", "cluster_id", clusterID, "error", err)
				_ = conn.Close()
				return
			}
		}
	}
}

func (r *Router) writeEventAck(conn *websocket.Conn, clusterID string, agentID string, ackMessageID string, ackType string, taskID string, commandID string) error {
	message := protocol.Message[protocol.EventAckPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformEventAck,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.EventAckPayload{
			AckMessageID: ackMessageID,
			AckType:      ackType,
			TaskID:       taskID,
			CommandID:    commandID,
			Persisted:    true,
		},
	}
	return conn.WriteJSON(message)
}

func (r *Router) writeEventError(conn *websocket.Conn, clusterID string, agentID string, ackMessageID string, ackType string, taskID string, commandID string, code string, messageText string, retryable bool) error {
	message := protocol.Message[protocol.EventErrorPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformEventError,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.EventErrorPayload{
			AckMessageID: ackMessageID,
			AckType:      ackType,
			TaskID:       taskID,
			CommandID:    commandID,
			ErrorCode:    code,
			Message:      messageText,
			Retryable:    retryable,
		},
	}
	return conn.WriteJSON(message)
}

func (r *Router) finishUnregisterTask(clusterID string, task store.Task) error {
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to clean cluster after unregister", "cluster_id", clusterID, "task_id", task.ID, "error", err)
		return err
	}
	if !ok {
		r.logger.Warn("unregister completed for unknown cluster", "cluster_id", clusterID, "task_id", task.ID)
		return nil
	}
	r.logger.Info("cluster cleaned after agent unregister", "cluster_id", clusterID, "task_id", task.ID)
	return nil
}
