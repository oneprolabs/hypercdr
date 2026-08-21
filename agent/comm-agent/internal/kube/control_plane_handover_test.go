package kube

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func handoverFixture(t *testing.T) (*KubernetesControlPlaneHandoverManager, context.Context) {
	t.Helper()
	namespace := "hypercdr-agent"
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-comm-agent", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent-bootstrap", Namespace: namespace}, Data: map[string][]byte{"HCDR_PLATFORM_ENDPOINT": []byte("wss://community/ws/agent")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent-credential", Namespace: namespace}, Data: map[string][]byte{CredentialKeyClusterID: []byte("source-cluster"), CredentialKeySecret: []byte("source-credential")}},
	)
	return NewKubernetesControlPlaneHandoverManagerWithClient(client), context.Background()
}

func TestControlPlaneHandoverPrepareAndRollbackPreserveOriginalIdentity(t *testing.T) {
	manager, ctx := handoverFixture(t)
	deadline := time.Now().UTC().Add(10 * time.Minute)
	err := manager.Prepare(ctx, ControlPlaneHandoverInput{MigrationID: "migration-1", Namespace: "hypercdr-agent", PreviousEndpoint: "wss://community/ws/agent", PreviousClusterID: "source-cluster", PreviousCredential: "source-credential", TargetEndpoint: "wss://enterprise/ws/agent", TargetInstallToken: "target-token", RollbackDeadline: deadline})
	if err != nil {
		t.Fatal(err)
	}
	state, err := manager.load(ctx, "hypercdr-agent")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != HandoverStatusPrepared || state.PreviousClusterID != "source-cluster" || !state.RollbackDeadline.Equal(deadline) {
		t.Fatalf("unexpected state: %#v", state)
	}
	if err := manager.Rollback(ctx, "hypercdr-agent", "migration-1"); err != nil {
		t.Fatal(err)
	}
	credential, ok, err := NewSecretCredentialStoreWithClient(manager.client, "hypercdr-agent", "hypercdr-agent-credential").Load(ctx)
	if err != nil || !ok || credential.ClusterID != "source-cluster" || credential.Credential != "source-credential" {
		t.Fatalf("credential not restored: %#v, %v, %v", credential, ok, err)
	}
	bootstrap, err := manager.client.CoreV1().Secrets("hypercdr-agent").Get(ctx, "hypercdr-agent-bootstrap", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(bootstrap.Data["HCDR_PLATFORM_ENDPOINT"]) != "wss://community/ws/agent" {
		t.Fatalf("endpoint not restored: %s", bootstrap.Data["HCDR_PLATFORM_ENDPOINT"])
	}
}

func TestControlPlaneHandoverConfirmedStillExpiresBeforeManualCommit(t *testing.T) {
	manager, ctx := handoverFixture(t)
	deadline := time.Now().UTC().Add(time.Minute)
	if err := manager.Prepare(ctx, ControlPlaneHandoverInput{MigrationID: "migration-2", Namespace: "hypercdr-agent", PreviousEndpoint: "wss://community/ws/agent", PreviousClusterID: "source-cluster", PreviousCredential: "source-credential", TargetEndpoint: "wss://enterprise/ws/agent", TargetInstallToken: "target-token", RollbackDeadline: deadline}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Confirm(ctx, "hypercdr-agent", "migration-2"); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := manager.RollbackExpired(ctx, "hypercdr-agent", deadline.Add(time.Minute))
	if err != nil || !rolledBack {
		t.Fatalf("confirmed handover did not roll back at deadline: %v, %v", rolledBack, err)
	}
	if _, err := manager.load(ctx, "hypercdr-agent"); err == nil {
		t.Fatal("handover snapshot still exists after deadline rollback")
	}
}

func TestControlPlaneHandoverManualCommitStopsTimeoutRollbackUntilCleanup(t *testing.T) {
	manager, ctx := handoverFixture(t)
	deadline := time.Now().UTC().Add(time.Minute)
	if err := manager.Prepare(ctx, ControlPlaneHandoverInput{MigrationID: "migration-commit", Namespace: "hypercdr-agent", PreviousEndpoint: "wss://community/ws/agent", PreviousClusterID: "source-cluster", PreviousCredential: "source-credential", TargetEndpoint: "wss://enterprise/ws/agent", TargetInstallToken: "target-token", RollbackDeadline: deadline}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Confirm(ctx, "hypercdr-agent", "migration-commit"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Commit(ctx, "hypercdr-agent", "migration-commit"); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := manager.RollbackExpired(ctx, "hypercdr-agent", deadline.Add(time.Minute))
	if err != nil || rolledBack {
		t.Fatalf("committed handover rolled back at deadline: %v, %v", rolledBack, err)
	}
	state, err := manager.load(ctx, "hypercdr-agent")
	if err != nil || state.Status != HandoverStatusCommitted {
		t.Fatalf("committed decision was not persisted: %#v, %v", state, err)
	}
	if err := manager.Cleanup(ctx, "hypercdr-agent", "migration-commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.load(ctx, "hypercdr-agent"); err == nil {
		t.Fatal("handover snapshot still exists after cleanup")
	}
}
