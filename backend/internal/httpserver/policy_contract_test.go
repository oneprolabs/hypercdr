package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyContractsCoverMountedRoutesAndHideTenantInput(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/policies") {
			continue
		}
		count++
		c, status, ok := policyPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing contract %s", route.Pattern)
		}
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		applyPolicyPayloadContract(route.Pattern, op)
		if c.Request != nil {
			if _, exists := wireSchema(c.Request)["properties"].(map[string]any)["tenantId"]; exists {
				t.Fatal("client-controlled tenant")
			}
		}
		if route.Pattern == "POST /api/v1/policies" && status != 201 {
			t.Fatal("wrong create status")
		}
	}
	if count != 4 {
		t.Fatalf("coverage %d", count)
	}
}

func TestPolicyActualCreateListUpdateDeleteMatchContracts(t *testing.T) {
	repo := newTestStore(t)
	r := &Router{store: repo, logger: slog.Default()}
	actor := testAdmin(t, repo)
	run := func(pattern, path, body, id string, handler http.HandlerFunc, status int) map[string]any {
		t.Helper()
		method, _, _ := strings.Cut(pattern, " ")
		req := tenantRequest(httptest.NewRequest(method, path, strings.NewReader(body)), actor)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		var decoded map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		c, _, _ := policyPayloadContract(pattern)
		validateWireObject(t, decoded, wireSchema(c.Response))
		return decoded
	}
	created := run("POST /api/v1/policies", "/api/v1/policies", `{"name":"contract-policy","scheduleType":"manual","composition":"full","retentionCount":7}`, "", r.createPolicy, 201)
	id := created["id"].(string)
	if created["tenantId"] != store.DefaultTenantID {
		t.Fatal("wrong tenant")
	}
	run("GET /api/v1/policies", "/api/v1/policies", "", "", r.listPolicies, 200)
	run("PATCH /api/v1/policies/{id}", "/api/v1/policies/"+id, `{"name":"updated-policy","scheduleType":"manual","composition":"full"}`, id, r.tenantGuard("policy", r.updatePolicy), 200)
	run("DELETE /api/v1/policies/{id}", "/api/v1/policies/"+id, "", id, r.tenantGuard("policy", r.deletePolicy), 200)
}
