package main

import (
	"context"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/internal/testdb"
	"testing"
)

func newTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	repo, err := store.NewPostgresStore(context.Background(), testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Error(err)
		}
	})
	return repo
}
