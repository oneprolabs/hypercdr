package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateCurrentUserTheme(t *testing.T) {
	repo := newTestStore(t)
	users, err := repo.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("users: %v", err)
	}
	router := &Router{store: repo, identityProvider: storeIdentityProvider{store: repo}}
	for _, tc := range []struct {
		body   string
		status int
		theme  string
	}{
		{`{"theme":"system"}`, http.StatusBadRequest, "light"},
		{`{"theme":"dark"}`, http.StatusOK, "dark"},
	} {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/me/theme", strings.NewReader(tc.body))
		req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, users[0]))
		w := httptest.NewRecorder()
		router.updateCurrentUserTheme(w, req)
		if w.Code != tc.status {
			t.Fatalf("body %s: status=%d want %d", tc.body, w.Code, tc.status)
		}
		u, found, err := repo.GetUser(users[0].ID)
		if err != nil || !found || u.Theme != tc.theme {
			t.Fatalf("body %s: user=%#v found=%v err=%v", tc.body, u, found, err)
		}
	}
}

// A custom edition must never mutate or return the Community identity with the
// same ID (whose password state can differ from the signed-in edition account).
type editionThemeTestProvider struct {
	EditionIdentityProvider
	user EditionIdentity
}

func (p *editionThemeTestProvider) SetTheme(_ context.Context, id, theme string) (EditionIdentity, bool, error) {
	if id != p.user.ID {
		return EditionIdentity{}, false, nil
	}
	p.user.Theme = theme
	return p.user, true, nil
}
func TestThemeUpdateUsesEditionIdentity(t *testing.T) {
	repo := newTestStore(t)
	users, err := repo.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("users: %v", err)
	}
	original := users[0]
	provider := &editionThemeTestProvider{user: EditionIdentity{ID: original.ID, Email: "enterprise@example.test", Theme: "light", MustChangePassword: false}}
	router := &Router{store: repo, identityProvider: provider}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/me/theme", strings.NewReader(`{"theme":"dark"}`))
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, original))
	w := httptest.NewRecorder()
	router.updateCurrentUserTheme(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result EditionIdentity
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Email != provider.user.Email || result.MustChangePassword || result.Theme != "dark" {
		t.Fatalf("wrong edition identity: %#v", result)
	}
	stored, _, err := repo.GetUser(original.ID)
	if err != nil || stored.Theme != original.Theme {
		t.Fatalf("Community identity changed: %#v %v", stored, err)
	}
}
