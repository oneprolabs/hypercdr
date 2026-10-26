package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestBackupProtocolBlackBoxMessagePermutations(t *testing.T) {
	for _, order := range [][]string{
		{"result", "event", "inventory"}, {"result", "inventory", "event"},
		{"event", "result", "inventory"}, {"event", "inventory", "result"},
		{"inventory", "result", "event"}, {"inventory", "event", "result"},
	} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			repo := newTestStore(t)
			handler := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
			server := httptest.NewServer(handler)
			defer server.Close()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := handler.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "protocol-blackbox", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/ws/agent", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(12 * time.Second))
			if err := conn.WriteJSON(protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, Type: protocol.MessageAgentRegister, AgentID: "protocol-agent", Payload: protocol.RegisterPayload{InstallToken: token.Token, Cluster: protocol.ClusterSummary{Name: "protocol-test"}, Agent: protocol.AgentSummary{Version: "v1-test"}, Velero: protocol.VeleroSummary{Status: "ready"}}}); err != nil {
				t.Fatal(err)
			}
			var registered protocol.Message[protocol.RegisterAcceptedPayload]
			if err := conn.ReadJSON(&registered); err != nil || registered.Type != protocol.MessagePlatformRegisterAccepted {
				t.Fatalf("registration: %v %v", registered, err)
			}
			clusterID := registered.Payload.ClusterID
			app := seedSchedulerApplication(t, repo, clusterID, "demo")
			plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
			if err != nil {
				t.Fatal(err)
			}
			commandID := store.NewPublicID()
			task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", CommandID: commandID, Payload: map[string]any{"veleroBackupName": "protocol-backup"}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := repo.CreatePlatformSession(testAdmin(t, repo).ID, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			get := func(path string) map[string]any {
				t.Helper()
				req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
				req.Header.Set("Authorization", "Bearer "+session.Token)
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("GET %s: %d", path, response.StatusCode)
				}
				var body map[string]any
				if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				return body
			}
			if order[0] == "result" && order[1] == "event" {
				victimCluster := seedSchedulerCluster(t, repo)
				victimApp := seedSchedulerApplication(t, repo, victimCluster, "victim")
				victimPlan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: victimCluster, AppID: victimApp.ID, Status: "active"})
				if err != nil {
					t.Fatal(err)
				}
				victim, err := repo.CreateTask(store.TaskInput{ClusterID: victimCluster, ProtectionPlanID: victimPlan.ID, Type: "backup", Status: "running", CommandID: store.NewPublicID()})
				if err != nil {
					t.Fatal(err)
				}
				if err := conn.WriteJSON(protocol.Message[protocol.TaskFailedPayload]{Version: protocol.Version, MessageID: "forged-failure", Type: protocol.MessageAgentTaskFailed, Payload: protocol.TaskFailedPayload{AckRequired: true, TaskID: victim.ID, CommandID: victim.CommandID, ErrorCode: "FORGED_FAILURE"}}); err != nil {
					t.Fatal(err)
				}
				for {
					var rejected protocol.Message[protocol.EventErrorPayload]
					if err := conn.ReadJSON(&rejected); err != nil {
						t.Fatal(err)
					}
					if rejected.Payload.AckMessageID == "forged-failure" {
						if rejected.Type != protocol.MessagePlatformEventError || rejected.Payload.Retryable {
							t.Fatal("forged failure was not rejected permanently")
						}
						break
					}
				}
				if state := get("/api/v1/tasks/" + victim.ID); state["status"] != "running" {
					t.Fatal("Agent changed another cluster's task")
				}
			}
			labels := map[string]string{"hypercdr.io/plan-id": plan.ID, "hypercdr.io/task-id": task.ID, "hypercdr.io/command-id": commandID}
			event := protocol.VeleroEventPayload{AckRequired: true, TaskID: task.ID, CommandID: commandID, PlanID: plan.ID, BackupName: "protocol-backup", Phase: "Completed", EventType: "backup_completed", Labels: labels, Velero: map[string]any{"kind": "Backup", "name": "protocol-backup"}}
			sequence := 0
			var resultMessage protocol.Message[any]
			send := func(kind string) {
				t.Helper()
				sequence++
				messageID := fmt.Sprintf("message-%d", sequence)
				messageType := protocol.MessageAgentVeleroEvent
				var payload any = event
				if kind == "inventory" {
					messageType = protocol.MessageAgentInventoryReport
					payload = protocol.InventoryReportPayload{CollectedAt: time.Now().UTC(), Scope: "namespace", Namespace: "demo", Apps: []protocol.ApplicationInventory{{Namespace: "demo", Status: "ready"}}, Velero: protocol.VeleroInventory{RecentBackups: []map[string]any{{"name": "protocol-backup", "phase": "Completed", "labels": labels}}}}
				}
				if kind == "result" {
					messageType = protocol.MessageAgentTaskCompleted
					payload = protocol.TaskCompletedPayload{AckRequired: true, TaskID: task.ID, CommandID: commandID, Status: "succeeded", Operation: "backup", Progress: 100, Velero: map[string]any{"kind": "Backup", "name": "protocol-backup"}, SizeMetricsV2: &protocol.SizeMetricsV2{SchemaVersion: 2, Operation: "backup", Logical: protocol.SizeValueV2{Known: true, TotalBytes: 1024}, NewData: protocol.SizeValueV2{Known: true, TotalBytes: 0}}}
				}
				message := protocol.Message[any]{Version: protocol.Version, Type: messageType, MessageKind: protocol.MessageKindEvent, MessageID: messageID, ClusterID: clusterID, Payload: payload}
				if kind == "result" {
					resultMessage = message
				}
				if err := conn.WriteJSON(message); err != nil {
					t.Fatal(err)
				}
				if kind == "inventory" {
					messageID += "-barrier"
					if err := conn.WriteJSON(protocol.Message[protocol.VeleroEventPayload]{Version: protocol.Version, Type: protocol.MessageAgentVeleroEvent, MessageID: messageID, ClusterID: clusterID, Payload: event}); err != nil {
						t.Fatal(err)
					}
				}
				for {
					var receipt protocol.Message[protocol.EventAckPayload]
					if err := conn.ReadJSON(&receipt); err != nil {
						t.Fatal(err)
					}
					if receipt.Type == protocol.MessagePlatformEventError {
						t.Fatalf("message rejected: %+v", receipt)
					}
					if receipt.Type == protocol.MessagePlatformEventAck && receipt.Payload.AckMessageID == messageID {
						if !receipt.Payload.Persisted {
							t.Fatal("ACK before persistence")
						}
						break
					}
				}
			}
			for _, invalid := range []protocol.Message[protocol.TaskFailedPayload]{
				{Version: "v2", Type: protocol.MessageAgentTaskFailed},
				{ClusterID: "other-cluster", Type: protocol.MessageAgentTaskFailed},
				{MessageKind: protocol.MessageKindRequest, Type: protocol.MessageAgentTaskFailed},
			} {
				invalid.Payload = protocol.TaskFailedPayload{TaskID: task.ID, CommandID: commandID, ErrorCode: "INVALID_ENVELOPE"}
				if err := conn.WriteJSON(invalid); err != nil {
					t.Fatal(err)
				}
			}
			if err := conn.WriteJSON(protocol.Message[protocol.TaskProgressPayload]{Version: protocol.Version, Type: protocol.MessageAgentTaskProgress, Payload: protocol.TaskProgressPayload{TaskID: task.ID, CommandID: commandID, Status: "succeeded", Progress: 100}}); err != nil {
				t.Fatal(err)
			}
			send("event")
			if state := get("/api/v1/tasks/" + task.ID); state["status"] != "running" {
				t.Fatalf("progress or observation changed outcome: %v", state)
			}
			resultSeen := false
			for _, kind := range order {
				send(kind)
				resultSeen = resultSeen || kind == "result"
				items := get("/api/v1/restore-points?protectionPlanId=" + plan.ID)["items"].([]any)
				if !resultSeen && len(items) != 0 {
					t.Fatal("observation published a restore point")
				}
				if resultSeen && len(items) != 1 {
					t.Fatalf("result cardinality=%d", len(items))
				}
			}
			for _, kind := range []string{"result", "event", "inventory"} {
				send(kind)
			}
			if err := conn.WriteJSON(resultMessage); err != nil {
				t.Fatal(err)
			}
			for {
				var receipt protocol.Message[protocol.EventAckPayload]
				if err := conn.ReadJSON(&receipt); err != nil {
					t.Fatal(err)
				}
				if receipt.Type == protocol.MessagePlatformEventError {
					t.Fatalf("replayed result rejected: %v", receipt)
				}
				if receipt.Type == protocol.MessagePlatformEventAck && receipt.Payload.AckMessageID == resultMessage.MessageID {
					break
				}
			}
			items := get("/api/v1/restore-points?protectionPlanId=" + plan.ID)["items"].([]any)
			if len(items) != 1 {
				t.Fatal("duplicate point")
			}
			point := items[0].(map[string]any)
			metrics := point["sizeMetricsV2"].(map[string]any)
			if metrics["newData"].(map[string]any)["known"] != true {
				t.Fatalf("known zero was lost: %v", metrics)
			}
			if err := conn.WriteJSON(protocol.Message[protocol.TaskFailedPayload]{Version: protocol.Version, MessageID: "conflicting-failure", Type: protocol.MessageAgentTaskFailed, Payload: protocol.TaskFailedPayload{AckRequired: true, TaskID: task.ID, CommandID: commandID, ErrorCode: "LATE_FAILURE"}}); err != nil {
				t.Fatal(err)
			}
			for {
				var rejection protocol.Message[protocol.EventErrorPayload]
				if err := conn.ReadJSON(&rejection); err != nil {
					t.Fatal(err)
				}
				if rejection.Payload.AckMessageID == "conflicting-failure" {
					if rejection.Type != protocol.MessagePlatformEventError || rejection.Payload.ErrorCode != "BACKUP_RESULT_CONFLICT" {
						t.Fatal("late failure was not rejected as a result conflict")
					}
					break
				}
			}
			state := get("/api/v1/tasks/" + task.ID)
			if state["status"] != "succeeded" || state["restorePointId"] != point["id"] {
				t.Fatalf("task and point disagree: %v %v", state, point)
			}
		})
	}
}
