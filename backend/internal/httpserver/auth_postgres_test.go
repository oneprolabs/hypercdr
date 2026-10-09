package httpserver

import (
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostgresRouterRejectsAnonymousAndRevokedSessions(t *testing.T) {
	repo := newTestStore(t)
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), repo))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/clusters")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.StatusCode)
	}
	session, err := repo.CreatePlatformSession(testAdmin(t, repo).ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/clusters", nil)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("session status=%d want %d", response.StatusCode, want)
		}
	}
	check(http.StatusOK)
	if err := repo.DeletePlatformSession(session.Token); err != nil {
		t.Fatal(err)
	}
	check(http.StatusUnauthorized)
}

func TestPostgresRouterEnforcesRoleAndTemporaryPassword(t *testing.T) {
	repo := newTestStore(t)
	user, err := repo.CreateUser(store.DefaultTenantID, "operator@example.com", "temporary-password")
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), repo))
	defer server.Close()
	request := func(method, path string, token string) int {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if got := request(http.MethodGet, "/api/v1/clusters", session.Token); got != http.StatusForbidden {
		t.Fatalf("temporary-password status=%d", got)
	}
	if _, found, err := repo.SetUserPassword(user.ID, "permanent-password", false); err != nil || !found {
		t.Fatalf("change password: %v", err)
	}
	session, err = repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(http.MethodGet, "/api/v1/clusters", session.Token); got != http.StatusOK {
		t.Fatalf("operator read status=%d", got)
	}
	if got := request(http.MethodPatch, "/api/v1/users/"+user.ID, session.Token); got != http.StatusForbidden {
		t.Fatalf("operator admin mutation status=%d", got)
	}
}
