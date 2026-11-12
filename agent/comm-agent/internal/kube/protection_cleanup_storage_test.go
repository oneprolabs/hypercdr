package kube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestCleanupStoragePreflightDetectsMissingBucketAndPermissions(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		code   string
	}{
		{"missing bucket", http.StatusNotFound, "NoSuchBucket"},
		{"access denied", http.StatusForbidden, "AccessDenied"},
		{"available", http.StatusOK, ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodHead {
					t.Errorf("unexpected object scan: %s", request.Method)
				}
				writer.Header().Set("x-amz-error-code", scenario.code)
				writer.WriteHeader(scenario.status)
			}))
			defer server.Close()
			bsl := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "BackupStorageLocation", "metadata": map[string]any{"name": "store", "namespace": "agent"}, "spec": map[string]any{"provider": "aws", "objectStorage": map[string]any{"bucket": "test"}, "credential": map[string]any{"name": "creds", "key": "cloud"}, "config": map[string]any{"s3Url": server.URL, "region": "us-east-1", "s3ForcePathStyle": "true"}}}}
			secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "creds", "namespace": "agent"}, "stringData": map[string]any{"cloud": "[default]\naws_access_key_id=test\naws_secret_access_key=test\n"}}}
			applier := NewDynamicManifestApplierWithClient(fake.NewSimpleDynamicClient(runtime.NewScheme(), bsl, secret))
			err := applier.CheckProtectionCleanupStorage(context.Background(), "agent", "store")
			if scenario.code == "" && err != nil {
				t.Fatal(err)
			}
			if scenario.code != "" && minio.ToErrorResponse(err).Code != scenario.code {
				t.Fatalf("unexpected error: %v", err)
			}
			if calls != 1 {
				t.Fatalf("definite result must not retry: %d requests", calls)
			}
		})
	}
}

func TestForceCleanupOnlySkipsRemoteStorageErrors(t *testing.T) {
	for _, code := range []string{"NoSuchBucket", "AccessDenied", "SignatureDoesNotMatch", "ServiceUnavailable"} {
		if !IsObjectStorageCleanupError(minio.ErrorResponse{Code: code}) {
			t.Fatalf("not classified as remote storage error: %s", code)
		}
	}
	for _, err := range []error{errors.New("invalid configuration"), apierrors.NewForbidden(schema.GroupResource{Resource: "backups"}, "backup", errors.New("denied")), context.Canceled} {
		if IsObjectStorageCleanupError(err) {
			t.Fatalf("unsafe to ignore: %v", err)
		}
	}
}

func TestBackupDeletionReportsVeleroFailureWithoutWaitingForTimeout(t *testing.T) {
	backup := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "current"}}}
	request := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "DeleteBackupRequest", "metadata": map[string]any{"name": "delete", "namespace": "agent"}, "spec": map[string]any{"backupName": "backup", "backupUID": "current"}, "status": map[string]any{"phase": "Processed", "errors": []any{"storage location unavailable: NoSuchBucket"}}}}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "velero.io", Version: "v1", Resource: "deletebackuprequests"}: "DeleteBackupRequestList"}, backup, request)
	started := time.Now()
	err := NewDynamicManifestApplierWithClient(client).WaitForVeleroBackupDeleted(context.Background(), "agent", "backup", 10*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "NoSuchBucket") || time.Since(started) > time.Second {
		t.Fatalf("did not fail promptly: %v", err)
	}
	if err := NewDynamicManifestApplierWithClient(client).WaitForVeleroBackupDeleted(context.Background(), "agent", "missing", time.Second); err != nil {
		t.Fatalf("already deleted must succeed: %v", err)
	}
}

func TestRetryRecreatesFailedVeleroDeleteRequest(t *testing.T) {
	request := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "DeleteBackupRequest", "metadata": map[string]any{"name": "delete", "namespace": "agent"}, "spec": map[string]any{"backupName": "backup"}, "status": map[string]any{"phase": "Processed", "errors": []any{"AccessDenied"}}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), request)
	manifest := Manifest{"apiVersion": "velero.io/v1", "kind": "DeleteBackupRequest", "metadata": map[string]any{"name": "delete", "namespace": "agent"}, "spec": map[string]any{"backupName": "backup"}}
	if _, err := NewDynamicManifestApplierWithClient(client).ApplyManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	deleted, created := false, false
	for _, action := range client.Actions() {
		deleted = deleted || action.GetVerb() == "delete"
		created = created || action.GetVerb() == "create"
	}
	if !deleted || !created {
		t.Fatal("failed deletion request was not recreated")
	}
}
