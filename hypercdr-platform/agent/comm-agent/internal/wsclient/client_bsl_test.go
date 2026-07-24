package wsclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type manifestStatusResult struct {
	status kube.ManifestStatus
	err    error
}

type sequenceManifestStatusReader struct {
	results []manifestStatusResult
	calls   int
}

func (r *sequenceManifestStatusReader) GetManifestStatus(context.Context, kube.AppliedObject) (kube.ManifestStatus, error) {
	index := r.calls
	r.calls++
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	return r.results[index].status, r.results[index].err
}

func TestRequireBackupStorageLocationRetriesTransientReadErrors(t *testing.T) {
	reader := &sequenceManifestStatusReader{results: []manifestStatusResult{
		{err: errors.New("etcdserver: request timed out")},
		{err: errors.New("context deadline exceeded")},
		{status: kube.ManifestStatus{Phase: "Available"}},
	}}
	client := &Client{cfg: config.Config{Namespace: "hypercdr-agent"}, statusReader: reader}

	if err := client.requireBackupStorageLocationWithRetry(context.Background(), "repo", 3, 0); err != nil {
		t.Fatalf("expected transient BSL reads to recover, got %v", err)
	}
	if reader.calls != 3 {
		t.Fatalf("BSL read calls = %d, want 3", reader.calls)
	}
}

func TestRequireBackupStorageLocationDoesNotRetryNotFound(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "backupstoragelocations"}, "repo")
	reader := &sequenceManifestStatusReader{results: []manifestStatusResult{{err: notFound}}}
	client := &Client{cfg: config.Config{Namespace: "hypercdr-agent"}, statusReader: reader}

	err := client.requireBackupStorageLocationWithRetry(context.Background(), "repo", 3, 0)
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected precise BSL not configured error, got %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("BSL read calls = %d, want 1", reader.calls)
	}
}

func TestRequireBackupStorageLocationReportsTemporaryAPIOutageAfterRetries(t *testing.T) {
	reader := &sequenceManifestStatusReader{results: []manifestStatusResult{{err: errors.New("etcdserver: request timed out")}}}
	client := &Client{cfg: config.Config{Namespace: "hypercdr-agent"}, statusReader: reader}

	err := client.requireBackupStorageLocationWithRetry(context.Background(), "repo", 3, 0)
	if err == nil || !strings.Contains(err.Error(), "Kubernetes API is temporarily unavailable after 3 retries") {
		t.Fatalf("expected temporary Kubernetes API error, got %v", err)
	}
	if strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("temporary API failure was incorrectly reported as missing BSL: %v", err)
	}
	if reader.calls != 4 {
		t.Fatalf("BSL read calls = %d, want 4", reader.calls)
	}
}

func TestRequireBackupStorageLocationDoesNotRetryUnavailablePhase(t *testing.T) {
	reader := &sequenceManifestStatusReader{results: []manifestStatusResult{{status: kube.ManifestStatus{Phase: "Unavailable", Message: "credentials rejected"}}}}
	client := &Client{cfg: config.Config{Namespace: "hypercdr-agent"}, statusReader: reader}

	err := client.requireBackupStorageLocationWithRetry(context.Background(), "repo", 3, 0)
	if err == nil || !strings.Contains(err.Error(), "is Unavailable: credentials rejected") {
		t.Fatalf("expected BSL phase error, got %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("BSL read calls = %d, want 1", reader.calls)
	}
}
