package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestFileLevelRiskAcknowledgementPersistenceAndTaskSnapshot(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	acceptedAt := time.Now().UTC().Truncate(time.Microsecond)
	settings := DataProtectionSettings{Mode: "live-filesystem", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement, RiskAcceptance: &DataProtectionRiskAcceptance{ActorID: "test-actor", AcceptedAt: acceptedAt}}
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: settings})
	if err != nil {
		t.Fatal(err)
	}
	if plan.DataProtection.RiskAcknowledgement != LiveFilesystemRiskAcknowledgement {
		t.Fatal("risk acknowledgement was not persisted")
	}
	task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", Payload: map[string]any{"dataProtectionRiskAcknowledgement": "caller-injection"}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Payload["dataProtectionRiskAcknowledgement"] != LiveFilesystemRiskAcknowledgement {
		t.Fatal("task did not snapshot the persisted acknowledgement")
	}
	encoded, err := json.Marshal(task.Payload["dataProtectionRiskAcceptance"])
	if err != nil {
		t.Fatal(err)
	}
	var acceptance DataProtectionRiskAcceptance
	if err := json.Unmarshal(encoded, &acceptance); err != nil || acceptance.ActorID != "test-actor" || !acceptance.AcceptedAt.Equal(acceptedAt) {
		t.Fatalf("task did not capture acceptance identity and UTC time: %v", err)
	}
	if _, err := repo.db.Exec(`update tasks set payload=payload - 'dataProtectionRiskAcceptance' where id=$1`, task.ID); err == nil {
		t.Fatal("direct SQL removed immutable risk acceptance")
	}
	if _, err := repo.db.Exec(`update tasks set payload=payload - 'dataProtectionRiskAcknowledgement' where id=$1`, task.ID); err == nil {
		t.Fatal("direct SQL removed immutable risk acknowledgement")
	}
	for _, invalid := range []string{
		`{"mode":"live-filesystem","riskAcknowledgement":"live-filesystem-no-application-consistency-v1","riskAcceptance":null}`,
		`{"mode":"live-filesystem","riskAcknowledgement":"live-filesystem-no-application-consistency-v1","riskAcceptance":{"actorId":"","acceptedAt":"2026-11-25T00:00:00Z"}}`,
		`{"mode":"live-filesystem","riskAcknowledgement":null}`,
		`{"mode":"live-filesystem","riskAcknowledgement":"unknown-version"}`,
		`{"mode":"snapshot-crash-consistent","riskAcknowledgement":"live-filesystem-no-application-consistency-v1"}`,
	} {
		if _, err := repo.db.Exec(`update protection_plans set data_protection=$1::jsonb where id=$2`, invalid, plan.ID); err == nil {
			t.Fatalf("direct SQL accepted invalid risk evidence: %s", invalid)
		}
	}
}

func TestDataProtectionRiskAcknowledgementValidation(t *testing.T) {
	for _, settings := range []DataProtectionSettings{
		{Mode: "live-filesystem", RiskAcceptance: &DataProtectionRiskAcceptance{ActorID: "actor", AcceptedAt: time.Now().UTC()}},
		{Mode: "live-filesystem", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement, RiskAcceptance: &DataProtectionRiskAcceptance{ActorID: "actor"}},
		{Mode: "live-filesystem", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement, RiskAcceptance: &DataProtectionRiskAcceptance{AcceptedAt: time.Now().UTC()}},
		{Mode: "live-filesystem", RiskAcknowledgement: "unknown-version"},
		{Mode: "snapshot-crash-consistent", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement},
		{Mode: "application-consistent", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement},
	} {
		if err := validateDataProtectionRisk(settings); !errors.Is(err, ErrDataProtectionSettings) {
			t.Fatalf("invalid risk evidence accepted: %+v: %v", settings, err)
		}
	}
	for _, settings := range []DataProtectionSettings{
		{Mode: "live-filesystem"},
		{Mode: "live-filesystem", RiskAcknowledgement: LiveFilesystemRiskAcknowledgement},
		{Mode: "snapshot-crash-consistent"},
	} {
		if err := validateDataProtectionRisk(settings); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskDataProtectionSnapshotsLockedPlanInsteadOfCallerClaims(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	settings := DataProtectionSettings{Mode: "snapshot-crash-consistent"}
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: settings})
	if err != nil {
		t.Fatal(err)
	}
	caller := map[string]any{"dataProtectionMode": "live-filesystem", "consistencyGroup": nil, "sourceNamespace": app.Namespace}
	var tasks []Task
	for _, taskType := range []string{"backup", "schedule-sync"} {
		task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: taskType, Payload: caller})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	if caller["dataProtectionMode"] != "live-filesystem" || caller["consistencyGroup"] != nil || caller["dataProtectionSnapshotVersion"] != nil {
		t.Fatal("task creation mutated caller payload")
	}
	for _, task := range tasks {
		for _, payload := range []map[string]any{
			{"dataProtectionMode": "live-filesystem"},
			{"consistencyGroup": map[string]any{"id": "replacement"}},
		} {
			if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", MarkDone: true, Payload: payload}); err == nil {
				t.Fatalf("application snapshot tampering accepted for %s: %v", task.Type, payload)
			}
		}
		if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "failed", ErrorCode: "TEST_FINISHED"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpdateProtectionPlan(plan.ID, ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: DataProtectionSettings{Mode: "live-filesystem"}}); err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		loaded, found, err := repo.GetTask(task.ID)
		if err != nil || !found {
			t.Fatalf("task not persisted: %v", err)
		}
		if loaded.Payload["dataProtectionMode"] != "snapshot-crash-consistent" || loaded.Payload["dataProtectionSnapshotVersion"] != float64(1) {
			t.Fatalf("task lost creation-time protection configuration: %v", loaded.Payload)
		}
		if loaded.Payload["consistencyGroup"] != nil {
			t.Fatal("CSI-only task acquired an application coordination group")
		}
	}
	legacy, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", Payload: map[string]any{"dataProtectionMode": "application-consistent", "consistencyGroup": settings.Group}})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Payload["dataProtectionMode"] != "live-filesystem" || legacy.Payload["consistencyGroup"] != nil {
		t.Fatal("caller overrode current plan configuration")
	}
}

func TestTaskDataProtectionSnapshotRejectsStatusAndSQLTampering(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []map[string]any{
		{"dataProtectionSnapshotVersion": nil},
		{"dataProtectionSnapshotVersion": 2},
		{"dataProtectionMode": "application-consistent"},
		{"consistencyGroup": map[string]any{"id": "injected"}},
	} {
		if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", MarkDone: true, Payload: payload}); err == nil {
			t.Fatalf("status update accepted snapshot tampering: %v", payload)
		}
	}
	for _, expression := range []string{
		`payload - 'dataProtectionSnapshotVersion'`,
		`payload - 'dataProtectionMode'`,
		`payload - 'consistencyGroup'`,
		`'{}'::jsonb`,
		`'null'::jsonb`,
	} {
		if _, err := repo.db.Exec(`update tasks set status='succeeded', payload=`+expression+` where id=$1`, task.ID); err == nil {
			t.Fatalf("direct SQL accepted snapshot removal: %s", expression)
		}
	}
	loaded, found, err := repo.GetTask(task.ID)
	if err != nil || !found {
		t.Fatalf("load task: %v, %v", found, err)
	}
	originalPayload, err := json.Marshal(task.Payload)
	if err != nil {
		t.Fatal(err)
	}
	loadedPayload, err := json.Marshal(loaded.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.Status || string(loadedPayload) != string(originalPayload) {
		t.Fatal("rejected update was not atomic")
	}
	updated, found, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 20, Payload: map[string]any{"dataProtectionSnapshotVersion": 1, "dataProtectionMode": "live-filesystem", "consistencyGroup": nil, "phase": "uploading"}})
	if err != nil || !found {
		t.Fatalf("normal progress update rejected: %v, %v", found, err)
	}
	if updated.Status != "running" || updated.Payload["phase"] != "uploading" || updated.Payload["dataProtectionMode"] != "live-filesystem" {
		t.Fatal("normal progress update lost task state")
	}
}

func TestPlanDataProtectionPersistenceAndLegacyUpdate(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	settings := DataProtectionSettings{Mode: "snapshot-crash-consistent"}
	input := ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: settings}
	plan, err := repo.CreateProtectionPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	check := func(plan ProtectionPlan) {
		t.Helper()
		if !reflect.DeepEqual(plan.DataProtection, settings) {
			t.Fatalf("persisted configuration changed: %#v", plan.DataProtection)
		}
	}
	check(plan)
	loaded, found, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !found {
		t.Fatalf("get: %v %v", found, err)
	}
	check(loaded)
	listed, err := repo.ListProtectionPlans(cluster.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %v %v", listed, err)
	}
	check(listed[0])
	input.DataProtection = DataProtectionSettings{}
	updated, err := repo.UpdateProtectionPlan(plan.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	check(updated)
	loaded, _, err = repo.GetProtectionPlan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	check(loaded)
	deleted, found, err := repo.DeleteProtectionPlan(plan.ID)
	if err != nil || !found {
		t.Fatalf("delete: %v %v", found, err)
	}
	check(deleted)
}

func TestPlanDataProtectionRejectsInvalidConfiguration(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	for _, settings := range []DataProtectionSettings{
		{Mode: "unknown"}, {Mode: "application-consistent"},
		{Mode: "live-filesystem", Group: &protocol.ConsistencyGroup{ID: "group"}},
		{Mode: "application-consistent", Group: &protocol.ConsistencyGroup{ID: "incomplete"}},
		{Mode: "application-consistent", Group: &protocol.ConsistencyGroup{ID: "outside", Strategy: "offline-v1", ApplicationProfile: "profile", MaxPauseSeconds: 120,
			Workloads: []protocol.ConsistencyWorkload{{Namespace: "other", Kind: "Deployment", Name: "writer", UID: "writer"}},
			Volumes:   []protocol.ConsistencyVolume{{Namespace: app.Namespace, Name: "data", UID: "data", SnapshotClass: "snapshots"}}}},
	} {
		_, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: settings})
		if !errors.Is(err, ErrDataProtectionSettings) {
			t.Fatalf("invalid configuration accepted: %#v: %v", settings, err)
		}
	}
}

func TestDecodePlanDataProtectionFailsClosed(t *testing.T) {
	for _, payload := range []string{`null`, `{}`, `{"mode":"unknown"}`, `{"mode":"application-consistent"}`, `{"mode":"application-consistent","group":{}}`, `{"mode":"live-filesystem","group":{}}`, `{broken`} {
		if err := decodePlanDataProtection([]byte(payload), &ProtectionPlan{}); err == nil {
			t.Fatalf("corrupt configuration accepted: %s", payload)
		}
	}
}

func TestPlanDataProtectionDatabaseConstraint(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID})
	if err != nil {
		t.Fatal(err)
	}
	if plan.DataProtection.Mode != "live-filesystem" {
		t.Fatal("legacy plan did not receive live-filesystem default")
	}
	for _, payload := range []string{`null`, `{}`, `{"mode":null}`, `{"mode":1}`, `{"mode":"unknown"}`, `{"mode":"application-consistent"}`, `{"mode":"application-consistent","group":null}`, `{"mode":"live-filesystem","group":{}}`} {
		if _, err := repo.db.Exec(`update protection_plans set data_protection=$2 where id=$1`, plan.ID, payload); err == nil {
			t.Fatalf("database accepted invalid configuration %s", payload)
		}
	}
}
