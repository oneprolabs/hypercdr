package store

import (
	"errors"
	"sync"
	"testing"
)

func TestCacheConfigurationGateSerializesConcurrentTaskCreation(t *testing.T) {
	for _, taskType := range []string{"backup", "drill", "storage-sync", "restore"} {
		t.Run(taskType, func(t *testing.T) {
			repo := newTestStore(t)
			cluster, _ := seedPlanApplication(t, repo)
			start := make(chan struct{})
			results := make(chan error, 2)
			var workers sync.WaitGroup
			for _, kind := range []string{taskType, "cache-settings"} {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					_, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: kind})
					results <- err
				}()
			}
			close(start)
			workers.Wait()
			close(results)
			accepted, rejected := 0, 0
			for err := range results {
				if err == nil {
					accepted++
				} else if errors.Is(err, ErrCacheConfigurationBusy) {
					rejected++
				} else {
					t.Fatal(err)
				}
			}
			if accepted != 1 || rejected != 1 {
				t.Fatalf("accepted=%d rejected=%d; want one of each", accepted, rejected)
			}
		})
	}
}

func TestCacheCleanupDoesNotBlockNewWork(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	if _, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "cache-cleanup"}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"backup", "drill"} {
		if _, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: kind}); err != nil {
			t.Fatalf("cleanup blocked %s: %v", kind, err)
		}
	}
}

func TestCacheCleanupCreationReusesParentTask(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	input := TaskInput{ClusterID: cluster.ID, Type: "cache-cleanup", Payload: map[string]any{"cacheSettingsTaskId": "configuration-event"}}
	first, err := repo.CreateTask(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate cleanup tasks: %s and %s", first.ID, second.ID)
	}
}
