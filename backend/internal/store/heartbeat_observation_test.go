package store

import (
	"testing"
	"time"
)

func TestHeartbeatWithoutRuntimeObservationPreservesCacheConfiguration(t *testing.T) {
	store := newTestStore(t)
	token, err := store.CreateAgentToken(DefaultTenantID, "", "heartbeat-observation", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := store.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "heartbeat-observation", AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	updated, ok, err := store.UpdateHeartbeat(HeartbeatInput{ClusterID: cluster.ID, Status: "healthy", CacheState: map[string]any{"phase": "applied", "indexEnabled": true, "indexPath": "/cache/index", "observedAt": observed}})
	if err != nil || !ok {
		t.Fatalf("initial observation: %v %v", ok, err)
	}
	if updated.CacheState["indexPath"] != "/cache/index" {
		t.Fatal("cache observation was not stored")
	}
	updated, ok, err = store.UpdateHeartbeat(HeartbeatInput{ClusterID: cluster.ID, Status: "healthy", CacheObservationPending: true})
	if err != nil || !ok {
		t.Fatalf("lightweight heartbeat: %v %v", ok, err)
	}
	if updated.CacheState["indexPath"] != "/cache/index" || updated.CacheState["observedAt"] != observed {
		t.Fatal("lightweight heartbeat erased or refreshed old cache configuration")
	}
	updated, ok, err = store.UpdateHeartbeat(HeartbeatInput{ClusterID: cluster.ID, Status: "healthy"})
	if err != nil || !ok {
		t.Fatalf("legacy heartbeat: %v %v", ok, err)
	}
	if updated.CacheState != nil {
		t.Fatal("legacy unsupported agent retained stale cache capability")
	}
}
