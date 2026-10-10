package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestSupportBundleRequiresSystemAdministrator(t *testing.T) {
	repo := newTestStore(t)
	admin := testAdmin(t, repo)
	directory := t.TempDir()
	t.Setenv("HCDR_SUPPORT_BUNDLE_DIR", directory)
	name := "hcdr-support-bundle-security-fixture.tar.gz"
	if err := os.WriteFile(filepath.Join(directory, name), []byte("isolated diagnostic fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(config.Config{}, slog.Default(), repo)
	for _, role := range []string{"operator", "admin"} {
		user, err := repo.CreateUser(admin.TenantID, role+"-bundle@example.com", "test-password")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.SetUserPassword(user.ID, "test-password", false); err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.UpdateUser(store.UserUpdateInput{ID: user.ID, TenantID: user.TenantID, Email: user.Email, Role: role, Status: "active"}); err != nil {
			t.Fatal(err)
		}
		session, err := repo.CreatePlatformSession(user.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct{ method, path, body string }{
			{"GET", "/api/v1/support-bundles/" + name + "/download", ""},
			{"POST", "/api/v1/support-bundles", `{}`},
			{"DELETE", "/api/v1/support-bundles/" + name, ""},
		} {
			req := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
			req.Header.Set("Authorization", "Bearer "+session.Token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatalf("%s %s: %d %s", role, item.method, w.Code, w.Body.String())
			}
		}
	}
	session, err := repo.CreatePlatformSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/support-bundles/"+name+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "isolated diagnostic fixture") {
		t.Fatalf("admin download %d", w.Code)
	}
	for _, item := range []struct {
		rangeValue string
		status     int
	}{{"bytes=0-6", 206}, {"bytes=999-1000", 416}} {
		req := httptest.NewRequest("GET", "/api/v1/support-bundles/"+name+"/download", nil)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		req.Header.Set("Range", item.rangeValue)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != item.status {
			t.Fatalf("range %s: %d", item.rangeValue, w.Code)
		}
	}
	req = httptest.NewRequest("DELETE", "/api/v1/support-bundles/"+name, nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("delete %d", w.Code)
	}
}

func TestSupportContractsMountedAndWireResponses(t *testing.T) {
	router := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	router.routes()
	count := 0
	for _, route := range router.routeContracts {
		if !strings.Contains(route.Pattern, "/api/v1/support-bundles") {
			continue
		}
		count++
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}, "parameters": []any{}}
		if !applySupportPayloadContract(route.Pattern, op) {
			t.Fatalf("missing %s", route.Pattern)
		}
		if _, ok := op["responses"].(map[string]any)["2XX"]; ok {
			t.Fatal("placeholder remains")
		}
	}
	if count != 3 {
		t.Fatalf("coverage %d", count)
	}
	// Decoder rejection is tested without collecting any host or cluster data.
	handler := &Router{}
	for _, body := range []string{`{`, `{} {}`, `{"description":"valid"} garbage`, strings.Repeat(" ", 14<<20) + `{}`} {
		req := httptest.NewRequest("POST", "/api/v1/support-bundles", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler.createSupportBundle(w, req)
		if w.Code != 400 {
			t.Fatalf("invalid body status %d", w.Code)
		}
	}
	for _, method := range []string{"GET", "DELETE"} {
		req := httptest.NewRequest(method, "/api/v1/support-bundles/invalid", nil)
		req.SetPathValue("name", "invalid")
		w := httptest.NewRecorder()
		if method == "GET" {
			handler.downloadSupportBundle(w, req)
		} else {
			handler.deleteSupportBundle(w, req)
		}
		if w.Code != 404 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("missing file error %d %s", w.Code, w.Header().Get("Content-Type"))
		}
	}
}
