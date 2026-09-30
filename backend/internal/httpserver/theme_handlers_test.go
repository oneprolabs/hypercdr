package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestUpdateCurrentUserTheme(t *testing.T) {
	repo := store.NewMemoryStore()
	users, err := repo.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("users: %v", err)
	}
	router := &Router{store: repo}
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
