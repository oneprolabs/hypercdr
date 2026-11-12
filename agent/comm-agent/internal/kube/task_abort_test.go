package kube

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestStopBackupDeletionRequestsIsScopedAndDoesNotPretendAnInFlightDeletionStopped(t *testing.T) {
	resource := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "deletebackuprequests"}
	for _, phase := range []string{"New", "InProgress", "Processed"} {
		t.Run(phase, func(t *testing.T) {
			request := func(name, backup string) *unstructured.Unstructured {
				return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "DeleteBackupRequest", "metadata": map[string]any{"name": name, "namespace": "agent", "uid": name}, "spec": map[string]any{"backupName": backup}, "status": map[string]any{"phase": phase}}}
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{resource: "DeleteBackupRequestList"}, request("target", "backup"), request("other", "unrelated"))
			applier := NewDynamicManifestApplierWithClient(client)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := applier.StopBackupDeletionRequests(ctx, "agent", []string{"backup"})
			if phase == "InProgress" && err == nil {
				t.Fatal("running Velero deletion was falsely confirmed stopped")
			}
			if phase != "InProgress" && err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resource(resource).Namespace("agent").Get(context.Background(), "other", metav1.GetOptions{}); err != nil {
				t.Fatal("unrelated deletion request was removed")
			}
			_, remaining := client.Resource(resource).Namespace("agent").Get(context.Background(), "target", metav1.GetOptions{})
			if phase != "InProgress" && !apierrors.IsNotFound(remaining) {
				t.Fatal("target deletion request remained")
			}
		})
	}
}
