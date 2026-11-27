package httpserver

import (
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestConsistencyRestoreCapabilityIsBoundToCurrentConnection(t *testing.T) {
	hub := newSessionHub()
	first, replacement := &websocket.Conn{}, &websocket.Conn{}
	hub.set("target", first)
	if hub.supportsConsistencyRestore("target") {
		t.Fatal("old Agent gained restore support")
	}
	hub.setConsistencyRestoreSupport("target", first, true)
	if !hub.supportsConsistencyRestore("target") {
		t.Fatal("current capability missing")
	}
	hub.set("target", replacement)
	hub.setConsistencyRestoreSupport("target", first, true)
	if hub.supportsConsistencyRestore("target") {
		t.Fatal("replaced socket restored stale capability")
	}
	hub.setConsistencyRestoreSupport("target", replacement, true)
	hub.remove("target", replacement)
	if hub.supportsConsistencyRestore("target") {
		t.Fatal("disconnected capability remained usable")
	}
}

func TestConsistencyRestoreRejectsUnboundPointAndOldAgentBeforeLookup(t *testing.T) {
	for _, scenario := range []string{"old-agent", "wrong-tenant", "wrong-backup", "wrong-point", "missing-source"} {
		t.Run(scenario, func(t *testing.T) {
			point := store.RestorePoint{ID: "point", TenantID: "tenant", BackupTaskID: "source", VeleroBackupName: "backup", Metadata: map[string]any{"velero": map[string]any{"achievedConsistency": "application-consistent"}}}
			task := store.Task{RestorePointID: "point", TenantID: "tenant", ClusterID: "target"}
			command := &protocol.RestoreCommand{VeleroBackupName: "backup"}
			switch scenario {
			case "wrong-tenant":
				task.TenantID = "other"
			case "wrong-backup":
				command.VeleroBackupName = "other"
			case "wrong-point":
				task.RestorePointID = "other"
			case "missing-source":
				point.BackupTaskID = ""
			}
			if err := (&Router{hub: newSessionHub()}).attachConsistencyRestoreEvidence(task, point, command); err == nil {
				t.Fatal("unverified restore accepted")
			}
		})
	}
}
