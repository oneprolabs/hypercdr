package store

import (
	"errors"
	"sync"
	"testing"
)

func TestClusterSyncSettingsValidation(t *testing.T) {
	for _, settings := range []ClusterSyncSettings{{MaxSyncTasks: 0, NodeTransferConcurrency: 4}, {MaxSyncTasks: 21, NodeTransferConcurrency: 4}, {MaxSyncTasks: 4, NodeTransferConcurrency: 0}, {MaxSyncTasks: 4, NodeTransferConcurrency: 33}} {
		if settings.Validate() == nil {
			t.Fatalf("accepted invalid settings %+v", settings)
		}
	}
	if err := DefaultClusterSyncSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedSyncCancellationAndRestart(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	first, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: first.ID, Status: "cancelled", MarkDone: true, OnlyQueuedSync: true})
	if err != nil || !changed || cancelled.Status != "cancelled" {
		t.Fatalf("queue cancellation: %+v %v %v", cancelled, changed, err)
	}
	if admitted, err := repo.AdmitSyncTask(first.ID, cluster.ID, cluster.TenantID); err != nil || admitted {
		t.Fatalf("cancelled task dispatched: %v %v", admitted, err)
	}
	second, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.AdmitSyncTask(second.ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
		t.Fatalf("second claim: %v %v", admitted, err)
	}
	if _, changed, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: second.ID, Status: "cancelled", MarkDone: true, OnlyQueuedSync: true}); err != nil || changed {
		t.Fatalf("cancelled admitted task locally: %v %v", changed, err)
	}
	var database string
	if err := repo.db.QueryRow(`select current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	restarted := schedulerSecondStore(t, database)
	if admitted, err := restarted.AdmitSyncTask(second.ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
		t.Fatalf("restart lost reserved slot: %v %v", admitted, err)
	}
}

func TestSyncAdmissionQueueAndRelease(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	settings, err := repo.GetClusterSyncSettings(cluster.ID)
	if err != nil || settings.MaxSyncTasks != 4 || settings.NodeTransferConcurrency != 4 {
		t.Fatalf("defaults: %+v %v", settings, err)
	}
	tasks := []Task{}
	for index := 0; index < 6; index++ {
		task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup", CommandID: NewPublicID()})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	for index, task := range tasks {
		admitted, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID)
		if err != nil || admitted != (index < 4) {
			t.Fatalf("task %d: admitted=%v err=%v", index, admitted, err)
		}
	}
	waiting, _, err := repo.GetTask(tasks[4].ID)
	if err != nil || waiting.Status != "queued" || !waiting.StartedAt.IsZero() || !waiting.DispatchedAt.IsZero() {
		t.Fatalf("waiting consumed execution time: %+v %v", waiting, err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: tasks[0].ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.AdmitSyncTask(tasks[5].ID, cluster.ID, cluster.TenantID); err != nil || admitted {
		t.Fatalf("FIFO bypass: %v %v", admitted, err)
	}
	if admitted, err := repo.AdmitSyncTask(tasks[4].ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
		t.Fatalf("did not release slot: %v %v", admitted, err)
	}
	if _, err := repo.AdmitSyncTask(tasks[5].ID, cluster.ID, NewPublicID()); !errors.Is(err, ErrTenantResourceMismatch) {
		t.Fatalf("foreign admission: %v", err)
	}
}

func TestSyncAdmissionConcurrentAndIdempotent(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	first, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for index := 0; index < 10; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if admitted, err := repo.AdmitSyncTask(first.ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
				t.Errorf("repeat claim %v %v", admitted, err)
			}
		}()
	}
	group.Wait()
	var count int
	if err := repo.db.QueryRow(`select count(*) from tasks where cluster_id=$1 and sync_admitted_at is not null`, cluster.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate reservations: %d %v", count, err)
	}
}

func TestSyncAdmissionCompetingTasksNeverExceedsLimit(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	tasks := []Task{}
	for index := 0; index < 12; index++ {
		task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup"})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	for round := 0; round < 4; round++ {
		var group sync.WaitGroup
		for _, task := range tasks {
			group.Add(1)
			go func(task Task) {
				defer group.Done()
				if _, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID); err != nil {
					t.Error(err)
				}
			}(task)
		}
		group.Wait()
		var count int
		if err := repo.db.QueryRow(`select count(*) from tasks where cluster_id=$1 and sync_admitted_at is not null`, cluster.ID).Scan(&count); err != nil || count > 4 {
			t.Fatalf("overcommitted slots: %d %v", count, err)
		}
	}
	var count int
	if err := repo.db.QueryRow(`select count(*) from tasks where cluster_id=$1 and sync_admitted_at is not null`, cluster.ID).Scan(&count); err != nil || count != 4 {
		t.Fatalf("failed to fill four slots: %d %v", count, err)
	}
}

func TestSyncSettingsWaitForIdleAndPreserveSave(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	settings := ClusterSyncSettings{MaxSyncTasks: 2, NodeTransferConcurrency: 6}
	if err := repo.SaveClusterSyncSettings(cluster.ID, settings); err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID); err != nil || admitted {
		t.Fatalf("started before runtime applied: %v %v", admitted, err)
	}
	apply, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "velero-upgrade", Payload: map[string]any{"syncSettingsApply": true, "concurrentBackups": 2, "nodeAgentConcurrency": 6}})
	if err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID); err != nil || admitted {
		t.Fatalf("started during runtime apply: %v %v", admitted, err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: apply.ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	observed, err := repo.GetClusterSyncSettings(cluster.ID)
	if err != nil || observed.AppliedMaxSyncTasks != 2 || observed.AppliedNodeTransferConcurrency != 6 {
		t.Fatalf("applied: %+v %v", observed, err)
	}
	if admitted, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
		t.Fatalf("did not resume: %v %v", admitted, err)
	}
	if _, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "velero-upgrade"}); !errors.Is(err, ErrCacheConfigurationBusy) {
		t.Fatalf("upgrade interrupted admitted task: %v", err)
	}
	settings.MaxSyncTasks = 1
	if err := repo.SaveClusterSyncSettings(cluster.ID, settings); err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.AdmitSyncTask(task.ID, cluster.ID, cluster.TenantID); err != nil || !admitted {
		t.Fatalf("lowering interrupted admitted task: %v %v", admitted, err)
	}
}
