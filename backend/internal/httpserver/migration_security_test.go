package httpserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMigrationListRequiresSystemAdministrator(t *testing.T) {
	repo := newTestStore(t)
	admin := testAdmin(t, repo)
	user, err := repo.CreateUser(admin.TenantID, "migration-operator@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(user.ID, "test-password", false); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(config.Config{}, slog.Default(), repo)
	req := httptest.NewRequest("GET", "/api/v1/community-migrations", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("ordinary user reads installation migration history: %d %s", w.Code, w.Body.String())
	}
}

func TestMigrationContractsMountedSessionBindingAndResponses(t *testing.T) {
	repo := newTestStore(t)
	if _, err := repo.UpsertPlatformSettings(store.PlatformSettingsInput{AgentNamespace: "hypercdr-agent", PublicEndpoint: "https://isolated.example"}); err != nil {
		t.Fatal(err)
	}
	admin := testAdmin(t, repo)
	var router *Router
	handler := NewRouterWithProductInfo(config.Config{ReleaseToken: "isolated-release"}, slog.Default(), repo, ProductInfo{Edition: "community"}, RouterOption(func(r *Router) { router = r }))
	count := 0
	for _, route := range router.routeContracts {
		if !strings.Contains(route.Pattern, "/community-migrations") {
			continue
		}
		count++
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}, "parameters": []any{}}
		if !applyMigrationPayloadContract(route.Pattern, op) {
			t.Fatalf("missing %s", route.Pattern)
		}
	}
	if count != 13 {
		t.Fatalf("coverage %d", count)
	}
	platform, err := repo.CreatePlatformSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	run := func(method, path, body, authorization string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	authorization := run("POST", "/api/v1/community-migrations/authorizations", "", "Bearer "+platform.Token, 201)
	validateWireObject(t, authorization, wireSchema(reflect.TypeFor[migrationAuthorizationResponse]()))
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	body, _ := json.Marshal(migrationSessionRequest{Token: authorization["token"].(string), TargetInstanceID: "isolated-target", ProtocolVersion: communityMigrationProtocolV1, TargetPublicKey: keyPEM})
	opened := run("POST", "/api/v1/community-migrations/source/sessions", string(body), "", 201)
	validateWireObject(t, opened, wireSchema(reflect.TypeFor[migrationSessionResponse]()))
	run("POST", "/api/v1/community-migrations/source/sessions", string(body), "", 409)
	id := opened["id"].(string)
	token := opened["sessionToken"].(string)
	for _, route := range router.routeContracts {
		if !strings.Contains(route.Pattern, "/source/{id}") {
			continue
		}
		method, path, _ := strings.Cut(route.Pattern, " ")
		path = strings.ReplaceAll(path, "{id}", id)
		path = strings.ReplaceAll(path, "{table}", "clusters")
		for _, identity := range []string{"", "Migration wrong-token", "Bearer " + platform.Token} {
			run(method, path, `{}`, identity, 401)
		}
		foreignPath := strings.Replace(path, id, "11111111-1111-1111-1111-111111111111", 1)
		run(method, foreignPath, `{}`, "Migration "+token, 401)
	}
	inventory := run("GET", "/api/v1/community-migrations/source/"+id+"/inventory", "", "Migration "+token, 200)
	validateWireObject(t, inventory, wireSchema(reflect.TypeFor[migrationInventoryResponse]()))
	freeze := run("POST", "/api/v1/community-migrations/source/"+id+"/freeze", `{}`, "Migration "+token, 200)
	validateWireObject(t, freeze, wireSchema(reflect.TypeFor[store.CommunityMigrationSession]()))
	backup := run("POST", "/api/v1/community-migrations/source/"+id+"/backup", `{}`, "Migration "+token, 200)
	validateWireObject(t, backup, wireSchema(reflect.TypeFor[migrationBackupResponse]()))
	manifest := run("GET", "/api/v1/community-migrations/source/"+id+"/manifest", "", "Migration "+token, 200)
	validateWireObject(t, manifest, wireSchema(reflect.TypeFor[store.CommunityMigrationExportManifest]()))
	credentials := run("GET", "/api/v1/community-migrations/source/"+id+"/credentials", "", "Migration "+token, 200)
	validateWireObject(t, credentials, wireSchema(reflect.TypeFor[migrationCredentialsResponse]()))
	smtp := run("GET", "/api/v1/community-migrations/source/"+id+"/smtp", "", "Migration "+token, 200)
	validateWireObject(t, smtp, wireSchema(reflect.TypeFor[migrationSMTPResponse]()))
	run("GET", "/api/v1/community-migrations/source/"+id+"/export/clusters?limit=invalid", "", "Migration "+token, 400)
	run("POST", "/api/v1/community-migrations/source/"+id+"/commit", `{}`, "Migration "+token, 409)
	rollback := run("POST", "/api/v1/community-migrations/source/"+id+"/rollback", `{}`, "Migration "+token, 200)
	validateWireObject(t, rollback, wireSchema(reflect.TypeFor[store.CommunityMigrationSession]()))
	history := run("GET", "/api/v1/community-migrations", "", "Bearer "+platform.Token, 200)
	validateWireObject(t, history, wireSchema(reflect.TypeFor[listResponse[store.CommunityMigrationSession]]()))
	// List records deliberately contain no usable session credential.
	for _, item := range history["items"].([]any) {
		if item.(map[string]any)["SessionToken"] != "" {
			t.Fatal("migration history exposes session credential")
		}
	}
}
