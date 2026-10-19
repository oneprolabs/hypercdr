package saas

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalAddressChangesApplyToSignupAndGoogleWithoutRestart(t *testing.T) {
	s, m := newTestService(t)
	// Use the real persisted platform row, as the page and CLI both do.
	_, err := s.db.Exec(`insert into platform_settings(tenant_id,image_registry,agent_namespace,velero_version,public_endpoint) values($1,'registry','hypercdr-agent','test','https://next.example') on conflict(tenant_id) do update set public_endpoint=excluded.public_endpoint`, store.DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "new@example.test"}, platform.Principal{})
	if w.Code != 403 || !strings.Contains(w.Body.String(), "https://next.example") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "/api/v1/saas/auth/signup/request", strings.NewReader(`{"email":"new@example.test"}`))
	r.Header.Set("Origin", "https://next.example")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	for _, route := range s.Routes() {
		if route.Pattern == "POST /api/v1/saas/auth/signup/request" {
			route.Handler(w, r, platform.Principal{})
		}
	}
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(m.messages[len(m.messages)-1], "https://next.example/signup?token=") {
		t.Fatal("mail uses old origin")
	}
	g := newGoogleProvider(Config{Origin: "https://old.example", GoogleClientID: "id", GoogleClientSecret: "secret"})
	s.google = g
	s.withCurrentOrigin(w, r, func(current *Service) {
		if current.google.OAuth.RedirectURL != "https://next.example/api/v1/saas/auth/google/callback" {
			t.Fatal(current.google.OAuth.RedirectURL)
		}
	})
	if g.OAuth.RedirectURL != "https://old.example/api/v1/saas/auth/google/callback" {
		t.Fatal("shared OAuth config was mutated")
	}
	w = call(t, s, "GET", "/api/v1/saas/auth/config", nil, platform.Principal{})
	var cfg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cfg)
	if cfg["publicUrl"] != "https://next.example" {
		t.Fatal(cfg)
	}
}
