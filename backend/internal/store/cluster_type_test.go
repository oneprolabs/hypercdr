package store

import (
	"strings"
	"testing"
	"time"
)

func TestCCERegistrationPersistsPlatformIdentity(t *testing.T) {
	repo := NewMemoryStore()
	token, err := repo.CreateAgentToken(DefaultTenantID, "", "cce", time.Hour, "huaweicloud-cce")
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterType: "huaweicloud-cce", ClusterName: "cce-production", CloudProvider: "huaweicloud", CloudRegion: "cn-north-4", CloudClusterID: "cce-production"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ClusterType != "huaweicloud-cce" || cluster.CloudProvider != "huaweicloud" || cluster.CloudRegion != "cn-north-4" || cluster.CloudClusterID != "cce-production" {
		t.Fatalf("unexpected CCE identity: %#v", cluster)
	}
}

func TestRegistrationRejectsTokenClusterTypeMismatch(t *testing.T) {
	repo := NewMemoryStore()
	token, err := repo.CreateAgentToken(DefaultTenantID, "", "cce", time.Hour, "huaweicloud-cce")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterType: "native-kubernetes", ClusterName: "wrong-type"})
	if err == nil || !strings.Contains(err.Error(), "cluster type") {
		t.Fatalf("expected cluster type mismatch, got %v", err)
	}
}

func TestLegacyRegistrationDefaultsToNativeKubernetes(t *testing.T) {
	repo := NewMemoryStore()
	token, err := repo.CreateAgentToken(DefaultTenantID, "", "legacy", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "native"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ClusterType != "native-kubernetes" {
		t.Fatalf("cluster type = %q", cluster.ClusterType)
	}
}
