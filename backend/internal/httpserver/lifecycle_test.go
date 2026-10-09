package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestRouterBackgroundWorkersStopBeforeStoreClose(t *testing.T) {
	repo := newTestStore(t)
	handler := NewRouter(config.Config{RegistrationSessionDir: t.TempDir()}, slog.Default(), repo)
	closer, ok := handler.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("router has no lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := closer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(ctx); err != nil {
		t.Fatal("shutdown not idempotent", err)
	}
}

func TestKubeconfigUploadOwnerCannotBeImpersonatedWithinTenant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Router{cceRegistrationUploads: map[string]cceKubeconfigUpload{
		"upload": {ID: "upload", TenantID: store.DefaultTenantID, OwnerID: "owner", Path: path, ExpiresAt: time.Now().Add(time.Minute)},
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster-registrations/cce/kubeconfigs/upload", nil)
	req.SetPathValue("id", "upload")
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "other", TenantID: store.DefaultTenantID}))
	w := httptest.NewRecorder()
	r.deleteCCEKubeconfig(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner status %d", w.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("non-owner deleted upload", err)
	}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "owner", TenantID: store.DefaultTenantID}))
	w = httptest.NewRecorder()
	r.deleteCCEKubeconfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner status %d", w.Code)
	}
}
