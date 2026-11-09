package controller

import (
	"testing"
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	velerotest "github.com/vmware-tanzu/velero/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOnDemandRequestExpiryAndAcknowledgement(t *testing.T) {
	location := &velerov1.BackupStorageLocation{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "velero", Annotations: map[string]string{storageAccessPolicy: "on-demand-v1"}}}
	if !onDemandStorage(location) || storageRequestPending(location, "validate") {
		t.Fatal("idle location has a pending request")
	}
	location.Annotations["hypercdr.io/validate-request"] = time.Now().Add(-6 * time.Minute).UTC().Format(time.RFC3339Nano)
	if storageRequestPending(location, "validate") {
		t.Fatal("expired request was accepted")
	}
	location.Annotations["hypercdr.io/validate-request"] = time.Now().UTC().Format(time.RFC3339Nano)
	if !storageRequestPending(location, "validate") {
		t.Fatal("fresh request ignored")
	}
	scheme := runtime.NewScheme()
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(location).Build()
	if err := completeStorageRequest(t.Context(), cli, location, "validate", false); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(t.Context(), client.ObjectKeyFromObject(location), location); err != nil {
		t.Fatal(err)
	}
	if storageRequestPending(location, "validate") || location.Annotations["hypercdr.io/validate-result"] != "failed" {
		t.Fatal("failed request was not consumed")
	}
}

func TestOnDemandIdleControllersDoNotOpenStorage(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	location := &velerov1.BackupStorageLocation{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "velero", Annotations: map[string]string{storageAccessPolicy: "on-demand-v1"}}, Status: velerov1.BackupStorageLocationStatus{Phase: velerov1.BackupStorageLocationPhaseAvailable}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(location).Build()
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(location)}
	validation := &backupStorageLocationReconciler{ctx: t.Context(), client: cli, log: velerotest.NewLogger()}
	if _, err := validation.Reconcile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	syncer := &backupSyncReconciler{client: cli, namespace: "velero", logger: velerotest.NewLogger()}
	if syncer.locationFilterFunc(location) {
		t.Fatal("idle location was scheduled for backup sync")
	}
	if _, err := syncer.Reconcile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}

func TestOnDemandIdleRepositoryDoesNotMaintainOrReconnect(t *testing.T) {
	for _, phase := range []velerov1.BackupRepositoryPhase{velerov1.BackupRepositoryPhaseReady, velerov1.BackupRepositoryPhaseNotReady, velerov1.BackupRepositoryPhaseNew} {
		scheme := runtime.NewScheme()
		if err := velerov1.AddToScheme(scheme); err != nil {
			t.Fatal(err)
		}
		location := &velerov1.BackupStorageLocation{ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "velero", Annotations: map[string]string{storageAccessPolicy: "on-demand-v1"}}}
		repo := &velerov1.BackupRepository{ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: "velero", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))}, Spec: velerov1.BackupRepositorySpec{BackupStorageLocation: "store"}, Status: velerov1.BackupRepositoryStatus{Phase: phase}}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(location, repo).Build()
		reconciler := &BackupRepoReconciler{Client: cli, namespace: "velero", logger: velerotest.NewLogger()}
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(repo)}); err != nil {
			t.Fatal(err)
		}
	}
}
