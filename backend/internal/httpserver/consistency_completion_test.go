package httpserver

import (
	"errors"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestBackupConsistencyAdmissionRejectsUnprovenClaimsBeforePublication(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		request, result map[string]any
	}{
		{"legacy-claims-achieved", nil, map[string]any{"achievedConsistency": "application-consistent"}},
		{"legacy-claims-record", nil, map[string]any{"consistency": map[string]any{"phase": "completed"}}},
		{"strict-native-only", map[string]any{"dataProtectionMode": "application-consistent", "consistencyGroup": map[string]any{}}, map[string]any{"kind": "Backup", "name": "native", "phase": "Completed"}},
		{"strict-missing-group", map[string]any{"dataProtectionMode": "application-consistent"}, nil},
		{"strict-empty-evidence", map[string]any{"dataProtectionMode": "application-consistent", "consistencyGroup": map[string]any{}}, map[string]any{"requestedConsistency": "application-consistent", "achievedConsistency": "application-consistent"}},
		{"omitted-mode-with-group", map[string]any{"consistencyGroup": map[string]any{}}, nil},
		{"unknown-mode", map[string]any{"dataProtectionMode": "unknown"}, nil},
		{"invalid-mode", map[string]any{"dataProtectionMode": map[string]any{}}, nil},
		{"snapshot-missing-mode", map[string]any{"dataProtectionSnapshotVersion": 1, "consistencyGroup": nil}, nil},
		{"snapshot-empty-mode", map[string]any{"dataProtectionSnapshotVersion": 1, "dataProtectionMode": "", "consistencyGroup": nil}, nil},
		{"snapshot-missing-group", map[string]any{"dataProtectionSnapshotVersion": 1, "dataProtectionMode": "live-filesystem"}, nil},
		{"snapshot-unknown-version", map[string]any{"dataProtectionSnapshotVersion": 2, "dataProtectionMode": "live-filesystem", "consistencyGroup": nil}, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			task := store.Task{ID: "task", Type: "backup", Payload: scenario.request}
			if err := validateTaskConsistencyCompletion(task, scenario.result); !errors.Is(err, store.ErrBackupResultConflict) {
				t.Fatalf("unproven completion accepted: %v", err)
			}
			router := &Router{}
			if err := router.acceptBackupCompletion(task, protocol.TaskCompletedPayload{Velero: scenario.result}); !errors.Is(err, store.ErrBackupResultConflict) {
				t.Fatalf("completion admission bypass: %v", err)
			}
			if _, err := router.createRestorePointFromBackup(task, scenario.result); !errors.Is(err, store.ErrBackupResultConflict) {
				t.Fatalf("restore-point publication bypass: %v", err)
			}
		})
	}
}

type consistencyPlanLookupStore struct {
	store.Store
	plan  store.ProtectionPlan
	found bool
	err   error
}

func (repo consistencyPlanLookupStore) GetProtectionPlan(string) (store.ProtectionPlan, bool, error) {
	return repo.plan, repo.found, repo.err
}

func TestLegacyConsistencyCompletionChecksPersistedPlan(t *testing.T) {
	task := store.Task{ID: "legacy", Type: "backup", TenantID: "tenant", ClusterID: "cluster", ProtectionPlanID: "plan"}
	for _, scenario := range []struct {
		name  string
		plan  store.ProtectionPlan
		found bool
	}{
		{"application", store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "cluster", DataProtection: store.DataProtectionSettings{Mode: "application-consistent"}}, true},
		{"snapshot", store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "cluster", DataProtection: store.DataProtectionSettings{Mode: "snapshot-crash-consistent"}}, true},
		{"other-tenant", store.ProtectionPlan{TenantID: "other", SourceClusterID: "cluster"}, true},
		{"other-cluster", store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "other"}, true},
		{"missing", store.ProtectionPlan{}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			router := &Router{store: consistencyPlanLookupStore{plan: scenario.plan, found: scenario.found}}
			if err := router.acceptBackupCompletion(task, protocol.TaskCompletedPayload{}); !errors.Is(err, store.ErrBackupResultConflict) {
				t.Fatalf("legacy completion bypass: %v", err)
			}
			if _, err := router.createRestorePointFromBackup(task, nil); !errors.Is(err, store.ErrBackupResultConflict) {
				t.Fatalf("legacy publication bypass: %v", err)
			}
		})
	}
	router := &Router{store: consistencyPlanLookupStore{found: true, plan: store.ProtectionPlan{TenantID: "tenant", SourceClusterID: "cluster", DataProtection: store.DataProtectionSettings{Mode: "live-filesystem"}}}}
	if err := router.validateBackupConsistencyCompletion(task, nil); err != nil {
		t.Fatalf("legacy FSB rejected: %v", err)
	}
	lookupFailure := errors.New("plan lookup unavailable")
	router.store = consistencyPlanLookupStore{err: lookupFailure}
	if err := router.validateBackupConsistencyCompletion(task, nil); !errors.Is(err, lookupFailure) {
		t.Fatalf("lookup failure did not fail closed: %v", err)
	}
	task.Payload = map[string]any{"dataProtectionSnapshotVersion": 1, "dataProtectionMode": "live-filesystem", "consistencyGroup": nil}
	if err := router.validateBackupConsistencyCompletion(task, nil); err != nil {
		t.Fatalf("captured historical configuration depended on current plan: %v", err)
	}
}

func TestLegacyBackupConsistencyAdmissionRemainsCompatible(t *testing.T) {
	for _, request := range []map[string]any{nil, {"dataProtectionMode": "live-filesystem"}} {
		if err := validateTaskConsistencyCompletion(store.Task{Type: "backup", Payload: request}, map[string]any{"kind": "Backup", "name": "legacy", "phase": "Completed"}); err != nil {
			t.Fatal(err)
		}
	}
}
