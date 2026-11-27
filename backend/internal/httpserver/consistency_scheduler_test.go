package httpserver

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestCSIScheduleCreatesCapturedPerRunTaskWithoutNativeSchedule(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "isolated-scheduled")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "isolated", Type: "s3", Endpoint: "http://127.0.0.1:1", Bucket: "isolated"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(store.PolicyInput{Name: "hourly", ScheduleType: "interval", IntervalValue: 1, IntervalUnit: "hour", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, AppIDs: []string{app.ID}, StorageRepoID: storage.ID, PolicyID: policy.ID, Status: "active", DataProtection: store.DataProtectionSettings{Mode: "snapshot-crash-consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	now := time.Now().UTC()
	schedule, err := repo.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{ProtectionPlanID: plan.ID, NextFireAt: now.Add(-time.Minute), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	router.fireProtectionPlanSchedule(schedule, now)
	router.fireProtectionPlanSchedule(schedule, now.Add(time.Second))
	tasks, err := repo.ListTasks(clusterID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected one per-run task without native Schedule: %d, %v", len(tasks), err)
	}
	task := tasks[0]
	if task.Type != "backup" || task.CommandID == "" || taskPayloadString(task.Payload, "trigger") != "scheduled" || task.Payload["dataProtectionMode"] != "snapshot-crash-consistent" {
		t.Fatalf("scheduled task lost strict configuration: %+v", task)
	}
	if _, err := router.buildStoredTaskDispatch(task); err == nil {
		t.Fatal("offline scheduled task bypassed capability check")
	}
	connection := &websocket.Conn{}
	router.hub.set(clusterID, connection)
	router.hub.setSnapshotBackupSupport(clusterID, connection, true)
	message, err := router.buildStoredTaskDispatch(task)
	if err != nil || message.Payload.Backup == nil || message.Payload.Backup.ConsistencyGroup != nil || message.Payload.Backup.DataProtectionMode != "snapshot-crash-consistent" {
		t.Fatalf("scheduled dispatch lost CSI-only settings: %v", err)
	}
	if _, err := repo.UpdateProtectionPlan(plan.ID, store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, AppIDs: []string{app.ID}, StorageRepoID: storage.ID, PolicyID: policy.ID, Status: "active", DataProtection: store.DataProtectionSettings{Mode: "live-filesystem", RiskAcknowledgement: "live-filesystem-no-application-consistency-v1"}}); !errors.Is(err, store.ErrProtectionPlanBusy) {
		t.Fatalf("plan edit was not blocked while the strict run is queued: %v", err)
	}
	loaded, _, err := repo.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err = router.buildStoredTaskDispatch(loaded)
	if err != nil || message.Payload.Backup.DataProtectionMode != "snapshot-crash-consistent" || message.Payload.Backup.ConsistencyGroup != nil {
		t.Fatalf("rejected plan edit changed queued strict run: %v", err)
	}
	router.hub.set(clusterID, &websocket.Conn{})
	if _, err := router.buildStoredTaskDispatch(loaded); err == nil {
		t.Fatal("replacement socket inherited qualified scheduling capability")
	}
}
