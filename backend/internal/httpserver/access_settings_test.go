package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platformaccess"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlatformAccessRequiresSystemAdminAndPersists(t *testing.T) {
	repo := newTestStore(t)
	_, err := repo.UpsertPlatformSettings(store.PlatformSettingsInput{InstanceID: "stable-instance", ImageRegistry: "registry.example", AgentNamespace: "hypercdr-agent", VeleroVersion: "test", PublicEndpoint: "https://old.example"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo}
	for _, actor := range []store.User{{Role: "admin"}, {Role: "operator"}} {
		w := httptest.NewRecorder()
		r.accessSettings(w, tenantRequest(httptest.NewRequest("PATCH", "/api/v1/platform/access", strings.NewReader(`{"publicUrl":"https://evil.example"}`)), actor))
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	actor := store.User{Role: "admin", SystemAdmin: true}
	req := tenantRequest(httptest.NewRequest("PATCH", "/api/v1/platform/access", strings.NewReader(`{"publicUrl":"https://app.example.com/"}`)), actor)
	// The admin can repair configuration while connected through an old/IP entry.
	req.Header.Set("Origin", "https://192.168.1.2:12443")
	w := httptest.NewRecorder()
	r.accessSettings(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://app.example.com/api/v1/saas/auth/google/callback") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	s, _, _ := repo.GetPlatformSettings()
	if s.InstanceID != "stable-instance" || s.ImageRegistry != "registry.example" || s.PublicEndpoint != "https://app.example.com" {
		t.Fatal(s)
	}
	// The command uses the same persistence path; a newly constructed router sees it.
	if _, err := platformaccess.Save(repo, "https://next.example"); err != nil {
		t.Fatal(err)
	}
	r2 := &Router{store: repo}
	if r2.publicBaseURL(req) != "https://next.example" {
		t.Fatal("saved address did not survive router recreation")
	}
	w = httptest.NewRecorder()
	r.accessSettings(w, tenantRequest(httptest.NewRequest("PATCH", "/api/v1/platform/access", strings.NewReader(`{"publicUrl":"http://evil.example"}`)), actor))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
