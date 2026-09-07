package store

import (
	"errors"
	"testing"
	"time"
)

func registerDefaultInvariantCluster(t *testing.T, repo *MemoryStore, description string) Cluster {
	t.Helper()
	token, err := repo.CreateAgentToken(DefaultTenantID, "", description, time.Hour)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	cluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: description})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}
	return cluster
}

func findDefaultInvariantCluster(t *testing.T, repo *MemoryStore, id string) (Cluster, bool) {
	t.Helper()
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	for _, cluster := range clusters {
		if cluster.ID == id {
			return cluster, true
		}
	}
	return Cluster{}, false
}

func TestDefaultClusterCannotBeCleared(t *testing.T) {
	repo := NewMemoryStore()
	cluster := registerDefaultInvariantCluster(t, repo, "first")
	value := false
	if _, _, err := repo.UpdateCluster(ClusterUpdateInput{ID: cluster.ID, IsDefault: &value}); !errors.Is(err, ErrDefaultClusterRequired) {
		t.Fatalf("expected ErrDefaultClusterRequired, got %v", err)
	}
	stored, found := findDefaultInvariantCluster(t, repo, cluster.ID)
	if !found || !stored.IsDefault {
		t.Fatalf("default cluster changed: found=%v default=%v", found, stored.IsDefault)
	}
}

func TestRegistrationRepairsMissingDefault(t *testing.T) {
	repo := NewMemoryStore()
	first := registerDefaultInvariantCluster(t, repo, "first")
	repo.mu.Lock()
	first.IsDefault = false // simulate historical data created before the invariant
	repo.clusters[first.ID] = first
	repo.mu.Unlock()

	second := registerDefaultInvariantCluster(t, repo, "second")
	if !second.IsDefault {
		t.Fatal("new registration did not repair missing default")
	}
}

func TestDeletionRepairsMissingDefault(t *testing.T) {
	repo := NewMemoryStore()
	first := registerDefaultInvariantCluster(t, repo, "first")
	second := registerDefaultInvariantCluster(t, repo, "second")
	repo.mu.Lock()
	first.IsDefault = false // simulate historical data created before the invariant
	repo.clusters[first.ID] = first
	repo.mu.Unlock()

	if deleted, err := repo.DeleteCluster(second.ID); err != nil || !deleted {
		t.Fatalf("DeleteCluster: deleted=%v err=%v", deleted, err)
	}
	stored, found := findDefaultInvariantCluster(t, repo, first.ID)
	if !found || !stored.IsDefault {
		t.Fatalf("remaining cluster was not elected: found=%v default=%v", found, stored.IsDefault)
	}
}

func TestSwitchingDefaultKeepsExactlyOne(t *testing.T) {
	repo := NewMemoryStore()
	first := registerDefaultInvariantCluster(t, repo, "first")
	second := registerDefaultInvariantCluster(t, repo, "second")
	if _, found, err := repo.SetDefaultCluster(second.ID); err != nil || !found {
		t.Fatalf("SetDefaultCluster: found=%v err=%v", found, err)
	}
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, cluster := range clusters {
		if cluster.IsDefault {
			defaults++
			if cluster.ID != second.ID {
				t.Fatalf("unexpected default cluster %s (first=%s)", cluster.ID, first.ID)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default, got %d", defaults)
	}
}
