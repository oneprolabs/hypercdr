package store

import "testing"

func TestClusterCacheSettingsDefaultsAndPersistence(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	settings, err := repo.GetClusterCacheSettings(cluster.ID)
	if err != nil || !settings.Enabled || settings.MaxGiB != 5 {
		t.Fatalf("unexpected defaults: %+v %v", settings, err)
	}
	want := ClusterCacheSettings{Enabled: false, MaxGiB: 12}
	if err := repo.SaveClusterCacheSettings(cluster.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetClusterCacheSettings(cluster.ID)
	if err != nil || got != want {
		t.Fatalf("settings not persisted: %+v %v", got, err)
	}
	if err := repo.SaveClusterCacheSettings(cluster.ID, ClusterCacheSettings{Enabled: true, MaxGiB: 0}); err == nil {
		t.Fatal("invalid budget accepted")
	}
}
