package main

import (
	"context"
	"errors"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestValidateClusterBackupBackendKeepsProvidersIsolated(t *testing.T) {
	for _, valid := range [][2]string{
		{"native-kubernetes", "velero"},
		{"huaweicloud-cce", "velero"},
		{"openshift", "oadp"},
	} {
		if err := validateClusterBackupBackend(valid[0], valid[1]); err != nil {
			t.Fatalf("valid combination %q/%q rejected: %v", valid[0], valid[1], err)
		}
	}
	for _, invalid := range [][2]string{
		{"native-kubernetes", "oadp"},
		{"huaweicloud-cce", "oadp"},
		{"openshift", "velero"},
	} {
		if err := validateClusterBackupBackend(invalid[0], invalid[1]); err == nil {
			t.Fatalf("invalid combination %q/%q accepted", invalid[0], invalid[1])
		}
	}
}

type retryCredentialStore struct {
	attempts int
	cancel   context.CancelFunc
}

func (s *retryCredentialStore) Load(context.Context) (kube.AgentCredential, bool, error) {
	return kube.AgentCredential{}, false, nil
}
func (s *retryCredentialStore) Save(ctx context.Context, _ kube.AgentCredential) error {
	s.attempts++
	if s.attempts == 1 {
		return errors.New("temporary API outage")
	}
	return nil
}
func TestCredentialPersistenceRetriesWithoutTerminating(t *testing.T) {
	s := &retryCredentialStore{}
	persistCredentialWithRetry(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), s, kube.AgentCredential{ClusterID: "cluster", Credential: "credential"}, time.Millisecond)
	if s.attempts != 2 {
		t.Fatalf("attempts %d", s.attempts)
	}
}
func TestCredentialPersistenceStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &retryCredentialStore{}
	persistCredentialWithRetry(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), s, kube.AgentCredential{}, time.Millisecond)
	if s.attempts != 0 {
		t.Fatal("saved canceled credential")
	}
}
