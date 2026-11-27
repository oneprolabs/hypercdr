package httpserver

import (
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestSnapshotBackupDispatchRequiresCurrentEvidenceCapability(t *testing.T) {
	for _, scenario := range []string{"valid", "old-agent", "replacement", "missing-version", "schedule", "wrong-tenant", "unexpected-group"} {
		t.Run(scenario, func(t *testing.T) {
			hub := newSessionHub()
			connection := &websocket.Conn{}
			hub.set("cluster", connection)
			hub.setSnapshotBackupSupport("cluster", connection, scenario != "old-agent")
			task := store.Task{ID: "task", CommandID: "command", Type: "backup", TenantID: "tenant", ClusterID: "cluster", ProtectionPlanID: "plan", Payload: map[string]any{"dataProtectionSnapshotVersion": 1, "dataProtectionMode": "snapshot-crash-consistent", "consistencyGroup": nil, "sourceNamespace": "app"}}
			plan := store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "cluster", DataProtection: store.DataProtectionSettings{Mode: "snapshot-crash-consistent"}}
			switch scenario {
			case "replacement":
				hub.set("cluster", &websocket.Conn{})
			case "missing-version":
				delete(task.Payload, "dataProtectionSnapshotVersion")
			case "schedule":
				task.Type = "schedule-sync"
			case "wrong-tenant":
				plan.TenantID = "other"
			case "unexpected-group":
				task.Payload["consistencyGroup"] = map[string]any{}
			}
			router := &Router{hub: hub, store: consistencyPlanLookupStore{found: true, plan: plan}}
			message, err := router.buildStoredTaskDispatch(task)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected dispatch admission: %v", err)
			}
			if err == nil && (message.Payload.Backup.DataProtectionMode != "snapshot-crash-consistent" || message.Payload.Backup.ConsistencyGroup != nil) {
				t.Fatal("snapshot command changed methods")
			}
		})
	}
}
