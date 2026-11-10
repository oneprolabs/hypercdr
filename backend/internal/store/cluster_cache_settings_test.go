package store

import "testing"

func TestClusterCacheSettingsDefaultsAndPersistence(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	settings, err := repo.GetClusterCacheSettings(cluster.ID)
	if err != nil || !settings.IndexEnabled || settings.IndexMaxGiB != 5 || settings.DrillEnabled {
		t.Fatalf("unexpected defaults: %+v %v", settings, err)
	}
	want := ClusterCacheSettings{RootPath: "/data/cache", IndexPath: "/data/cache/index", DrillPath: "/data/cache/drill-data", IndexEnabled: false, IndexMaxGiB: 12, DrillEnabled: true, DrillMaxGiB: 100}
	if err := repo.SaveClusterCacheSettings(cluster.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetClusterCacheSettings(cluster.ID)
	if err != nil || got != want {
		t.Fatalf("settings not persisted: %+v %v", got, err)
	}
	if err := repo.SaveClusterCacheSettings(cluster.ID, ClusterCacheSettings{RootPath: "/data/cache", IndexPath: "/data/cache/index", DrillPath: "/data/cache/drill-data", IndexEnabled: true, IndexMaxGiB: 0, DrillMaxGiB: 50}); err == nil {
		t.Fatal("invalid budget accepted")
	}
}
