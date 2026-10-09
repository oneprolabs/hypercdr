package httpserver

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestOldUploadTimerDoesNotDeleteExtendedRegistrationCredentials(t *testing.T) {
	r := &Router{cceRegistrationUploads: map[string]cceKubeconfigUpload{}}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(validCCEKubeconfig), 0600); err != nil {
		t.Fatal(err)
	}
	id := "ccer_abcdefghijklmnopqrstuvwxyz123456"
	r.cceRegistrationUploads[id] = cceKubeconfigUpload{ID: id, Path: path, ExpiresAt: time.Now().Add(time.Hour)}
	r.expireCCEKubeconfig(id, time.Now().Add(-time.Second))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("old timer deleted extended credentials: %v", err)
	}
	if _, ok := r.cceRegistrationUploads[id]; !ok {
		t.Fatal("old timer removed extended session")
	}
}

func TestCredentialJanitorPreservesActiveRequestsAcrossRestartAndFailsExpiredTasks(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	base := t.TempDir()
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	createFile := func() (string, string) {
		t.Helper()
		id, err := secureRegistrationID()
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(base, id)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "kubeconfig")
		if err := os.WriteFile(path, []byte(validCCEKubeconfig), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		return id, path
	}
	activeID, activePath := createFile()
	expiredID, expiredPath := createFile()
	liveID, livePath := createFile()
	_, orphanPath := createFile()
	for _, input := range []store.TaskInput{
		{TenantID: actor.TenantID, Type: "cluster-registration", Status: "running", Payload: map[string]any{"ownerId": actor.ID, "sessionId": activeID, "credentialExpiresAt": now.Add(30 * time.Minute).Format(time.RFC3339Nano)}},
		{TenantID: actor.TenantID, Type: "cluster-registration", Status: "queued", Payload: map[string]any{"ownerId": actor.ID, "sessionId": expiredID, "credentialExpiresAt": now.Add(-time.Minute).Format(time.RFC3339Nano)}},
	} {
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
	}
	// The active request has no in-memory upload after restart. A separate
	// in-memory upload has an extended expiry but still an old file mtime.
	r := &Router{store: repo, logger: slog.Default(), cfg: config.Config{RegistrationSessionDir: base}, cceRegistrationUploads: map[string]cceKubeconfigUpload{liveID: {ID: liveID, Path: livePath, ExpiresAt: now.Add(time.Hour)}}}
	r.cleanupOrphanedCCEKubeconfigs(now)
	for _, path := range []string{activePath, livePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("active credentials lost: %v", err)
		}
	}
	for _, path := range []string{expiredPath, orphanPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expired credentials remain: %v", err)
		}
	}
	tasks, err := repo.ListTasksFiltered(store.TaskFilter{TenantID: actor.TenantID, RegistrationSessionID: expiredID})
	if err != nil || len(tasks) != 1 || tasks[0].Status != "failed" || tasks[0].ErrorCode != "REGISTRATION_SESSION_EXPIRED" || tasks[0].CompletedAt.IsZero() {
		t.Fatalf("expired registration has no retryable failure: %+v %v", tasks, err)
	}
	// A transient store outage must not make a live credential look orphaned.
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	r.cleanupOrphanedCCEKubeconfigs(now.Add(2 * time.Hour))
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("store outage removed credentials: %v", err)
	}
}
