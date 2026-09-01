package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresCreateTaskWithoutClusterID(t *testing.T) {
	dsn := os.Getenv("HCDR_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hypercdr:hypercdr@127.0.0.1:5432/hypercdr?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Skipf("postgres not available, skipping: %v", err)
	}
	defer repo.Close()

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
