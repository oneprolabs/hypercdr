package store

import (
	"context"
	"net/url"
	"os"
	"testing"
)

func TestSchedulerLockAcrossProcessesAndHandoff(t *testing.T) {
	first := newTestStore(t)
	// A distinct pool represents the overlapping blue/green API process.
	var database string
	if err := first.db.QueryRow(`select current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	// Reuse the isolated test DSN through the helper's database, not business DSN.
	second := schedulerSecondStore(t, database)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := first.RunSchedulerExclusive(func() { close(entered); <-release })
		done <- err
	}()
	<-entered
	acquired, err := second.RunSchedulerExclusive(func() { t.Error("overlapping scheduler tick ran") })
	close(release)
	if err != nil || acquired {
		t.Fatalf("overlap: acquired=%v err=%v", acquired, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ran := false
	acquired, err = second.RunSchedulerExclusive(func() { ran = true })
	if err != nil || !acquired || !ran {
		t.Fatalf("handoff: acquired=%v ran=%v err=%v", acquired, ran, err)
	}
}

func TestSchedulerLockReleasedAfterPanic(t *testing.T) {
	repo := newTestStore(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected callback panic")
			}
		}()
		_, _ = repo.RunSchedulerExclusive(func() { panic("test failure") })
	}()
	acquired, err := repo.RunSchedulerExclusive(func() {})
	if err != nil || !acquired {
		t.Fatalf("lock leaked: acquired=%v err=%v", acquired, err)
	}
}

func schedulerSecondStore(t *testing.T, database string) *PostgresStore {
	t.Helper()
	u, err := url.Parse(os.Getenv("HCDR_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + database
	second, err := NewPostgresStoreWithoutMigrations(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	return second
}
