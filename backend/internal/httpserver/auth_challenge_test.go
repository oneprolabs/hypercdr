package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestAuthChallengeModeFallsBackToImageWithoutCompleteTurnstileConfig(t *testing.T) {
	r := &Router{cfg: config.Config{AuthChallengeMode: "turnstile", TurnstileSiteKey: "site"}}
	if got := r.authChallengeMode(); got != "image" {
		t.Fatalf("mode = %q, want image", got)
	}
}

func TestVerifyTurnstile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if req.Form.Get("secret") != "secret" || req.Form.Get("response") != "token" {
			t.Fatalf("unexpected form: %#v", req.Form)
		}
		io.WriteString(w, `{"success":true}`)
	}))
	defer server.Close()
	r := &Router{cfg: config.Config{TurnstileSecretKey: "secret", TurnstileVerifyURL: server.URL}, logger: slog.Default(), store: store.NewMemoryStore()}
	if ok, codes, _ := r.verifyTurnstile(httptest.NewRequest(http.MethodPost, "/", nil), "token"); !ok {
		t.Fatalf("expected valid Turnstile response, got codes=%v", codes)
	}
}

func TestVerifyTurnstileRejectsUnsuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"success":false}`)
	}))
	defer server.Close()
	r := &Router{cfg: config.Config{TurnstileSecretKey: "secret", TurnstileVerifyURL: server.URL}}
	if ok, _, _ := r.verifyTurnstile(httptest.NewRequest(http.MethodPost, "/", nil), "token"); ok {
		t.Fatal("expected unsuccessful Turnstile response to be rejected")
	}
}
