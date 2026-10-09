package store

import (
	"testing"
)

func TestPostgresCreateTaskWithoutClusterID(t *testing.T) {
	repo := newTestStore(t)

	task, err := repo.CreateTask(TaskInput{
		TenantID: DefaultTenantID,
		Type:     "cluster-registration",
		Status:   "queued",
		Payload:  map[string]any{"test": "task-without-cluster"},
	})
	if err != nil {
		t.Fatalf("CreateTask without ClusterID: %v", err)
	}
	defer repo.db.Exec(`delete from tasks where id=$1`, task.ID)
	if task.ClusterID != "" {
		t.Fatalf("ClusterID = %q, want empty", task.ClusterID)
	}
}
