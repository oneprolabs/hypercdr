package store

import "testing"

func TestMemoryStoreTaskProgressDoesNotRegress(t *testing.T) {
	repo := NewMemoryStore()
	task, err := repo.CreateTask(TaskInput{
		Type:   "drill",
		Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.UpdateTaskStatus(TaskStatusInput{
		TaskID:   task.ID,
		Status:   "running",
		Progress: 5,
	})
	if err != nil || !ok {
		t.Fatalf("UpdateTaskStatus to 5 failed: ok=%v err=%v", ok, err)
	}
	if task.Progress != 5 {
		t.Fatalf("progress=%d, want 5", task.Progress)
	}

	task, ok, err = repo.UpdateTaskStatus(TaskStatusInput{
		TaskID:   task.ID,
		Status:   "running",
		Progress: 0,
	})
	if err != nil || !ok {
		t.Fatalf("UpdateTaskStatus to 0 failed: ok=%v err=%v", ok, err)
	}
	if task.Progress != 5 {
		t.Fatalf("progress regressed to %d, want 5", task.Progress)
	}

	task, ok, err = repo.UpdateTaskStatus(TaskStatusInput{
		TaskID:   task.ID,
		Status:   "succeeded",
		Progress: 100,
	})
	if err != nil || !ok {
		t.Fatalf("UpdateTaskStatus to 100 failed: ok=%v err=%v", ok, err)
	}
	if task.Progress != 100 {
		t.Fatalf("progress=%d, want 100", task.Progress)
	}
}
