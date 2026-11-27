package httpserver

import (
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestApplicationConsistencyDispatchRequiresCapturedGroupAndCurrentProfile(t *testing.T) {
	for _, scenario := range []string{"valid", "no-advertisement", "wrong-profile", "no-snapshot", "wrong-version", "no-command", "schedule-sync", "wrong-tenant", "wrong-cluster", "out-of-scope"} {
		t.Run(scenario, func(t *testing.T) {
			group := &protocol.ConsistencyGroup{ID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", MaxPauseSeconds: 120,
				Workloads: []protocol.ConsistencyWorkload{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid"}}, Volumes: []protocol.ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc-uid", SnapshotClass: "class"}}}
			task := store.Task{ID: "task", CommandID: "command", TenantID: "tenant", ClusterID: "cluster", ProtectionPlanID: "plan", Type: "backup", Payload: map[string]any{"sourceNamespace": "app", "dataProtectionMode": "application-consistent", "consistencyGroup": group, "dataProtectionSnapshotVersion": 1}}
			plan := store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "cluster", DataProtection: store.DataProtectionSettings{Mode: "application-consistent", Group: group}}
			hub := newSessionHub()
			connection := &websocket.Conn{}
			hub.set("cluster", connection)
			capability := &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"qualified"}}
			switch scenario {
			case "no-advertisement":
				capability = nil
			case "wrong-profile":
				capability.Profiles = []string{"other"}
			case "no-snapshot":
				delete(task.Payload, "dataProtectionSnapshotVersion")
			case "wrong-version":
				task.Payload["dataProtectionSnapshotVersion"] = 2
			case "no-command":
				task.CommandID = ""
			case "schedule-sync":
				task.Type = "schedule-sync"
			case "wrong-tenant":
				plan.TenantID = "other"
			case "wrong-cluster":
				plan.SourceClusterID = "other"
			case "out-of-scope":
				group.Volumes[0].Namespace = "other"
			}
			hub.setApplicationConsistencySupport("cluster", connection, capability)
			router := &Router{hub: hub, store: consistencyPlanLookupStore{found: true, plan: plan}}
			_, err := router.buildStoredTaskDispatch(task)
			if err == nil {
				t.Fatalf("scenario=%s: %v", scenario, err)
			}
		})
	}
}

func TestStrictConsistencyCannotSilentlyUseLegacyDispatch(t *testing.T) {
	for _, taskType := range []string{"backup", "schedule-sync"} {
		for _, payload := range []map[string]any{
			{"dataProtectionMode": "application-consistent"},
			{"dataProtectionMode": "snapshot-crash-consistent"},
			{"dataProtectionMode": "unknown"},
			{"dataProtectionMode": map[string]any{}},
			{"consistencyGroup": map[string]any{"id": "group"}},
			{"dataProtectionMode": "live-filesystem", "consistencyGroup": map[string]any{}},
		} {
			if _, err := (&Router{}).buildStoredTaskDispatch(store.Task{Type: taskType, Payload: payload}); err == nil || !strings.Contains(err.Error(), "refusing legacy fallback") {
				t.Fatalf("%s silently accepted strict payload %#v: %v", taskType, payload, err)
			}
		}
	}
}

func TestLegacyBackupDispatchRemainsCompatible(t *testing.T) {
	for _, mode := range []string{"", "live-filesystem"} {
		message, err := (&Router{}).buildStoredTaskDispatch(store.Task{ID: "task", Type: "backup", Payload: map[string]any{"sourceNamespace": "app", "dataProtectionMode": mode}})
		if err != nil || message.Payload.Backup == nil || message.Payload.Backup.ConsistencyGroup != nil {
			t.Fatalf("legacy dispatch changed: %#v, %v", message, err)
		}
	}
}

func TestPersistedStrictPlanCannotLoseRequirementsInTaskPayload(t *testing.T) {
	repo := newTestStore(t)
	cluster := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, cluster, "strict-dispatch")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: cluster, AppID: app.ID, DataProtection: store.DataProtectionSettings{Mode: "snapshot-crash-consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo}
	for _, taskType := range []string{"backup", "schedule-sync"} {
		_, err := router.buildStoredTaskDispatch(store.Task{ID: "task", TenantID: plan.TenantID, ClusterID: cluster, ProtectionPlanID: plan.ID, Type: taskType, Payload: map[string]any{"sourceNamespace": app.Namespace}})
		if err == nil || !strings.Contains(err.Error(), "persisted strict data protection") {
			t.Fatalf("persisted requirements disappeared for %s: %v", taskType, err)
		}
	}
}
