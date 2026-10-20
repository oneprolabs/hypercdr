package httpserver

import (
	"context"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http/httptest"
	"testing"
)

func TestRequestActorUsesLoginAccountInsteadOfBoundEmail(t *testing.T) {
	for _, user := range []store.User{{ID: "user-1", Username: "admin", Email: "bound@example.test"}, {ID: "user-1", Username: "admin"}} {
		req := httptest.NewRequest("GET", "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
		if got := requestActor(req); got != "admin" {
			t.Fatalf("actor = %q, want fixed login account", got)
		}
	}
}
