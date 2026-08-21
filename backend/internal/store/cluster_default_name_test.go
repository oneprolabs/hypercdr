package store

import (
	"testing"
	"time"
)

func TestRegisterClusterAppendsControlPlaneIPForTenantDuplicateName(t *testing.T) {
	repo := NewMemoryStore()
	firstToken, err := repo.CreateAgentToken(DefaultTenantID, "", "first", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := repo.RegisterCluster(RegisterClusterInput{Token: firstToken.Token, ClusterName: "k8s-master", ControlPlaneIP: "192.168.7.131"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "k8s-master" {
		t.Fatalf("first cluster name = %q", first.Name)
	}

	secondToken, err := repo.CreateAgentToken(DefaultTenantID, "", "second", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := repo.RegisterCluster(RegisterClusterInput{Token: secondToken.Token, ClusterName: "k8s-master", ControlPlaneIP: "192.168.7.136"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "k8s-master (192.168.7.136)" {
		t.Fatalf("second cluster name = %q", second.Name)
	}
}
