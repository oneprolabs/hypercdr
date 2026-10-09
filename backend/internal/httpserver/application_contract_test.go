package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplicationAndTagContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/applications") && !strings.HasPrefix(path, "/api/v1/tags") {
			continue
		}
		count++
		c, status, ok := applicationPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing payload contract: %s", route.Pattern)
		}
		op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
		if !applyApplicationPayloadContract(route.Pattern, op) {
			t.Fatal(route.Pattern)
		}
		if route.Pattern == "POST /api/v1/tags" && status != 201 {
			t.Fatal("wrong creation status")
		}
		if _, placeholder := op["responses"].(map[string]any)["2XX"]; placeholder {
			t.Fatal("placeholder success")
		}
	}
	if count != 7 {
		t.Fatalf("unexpected domain coverage %d", count)
	}
}

func TestTagCreateResponseAndRequestMatchContract(t *testing.T) {
	repo := newTestStore(t)
	r := &Router{store: repo}
	req := tenantRequest(httptest.NewRequest("POST", "/api/v1/tags", strings.NewReader(`{"name":"contract-tag"}`)), testAdmin(t, repo))
	w := httptest.NewRecorder()
	r.createTag(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	c, _, _ := applicationPayloadContract("POST /api/v1/tags")
	validateWireObject(t, value, wireSchema(c.Response))
	listRequest := tenantRequest(httptest.NewRequest("GET", "/api/v1/tags", nil), testAdmin(t, repo))
	listResult := httptest.NewRecorder()
	r.listTags(listResult, listRequest)
	var listed map[string]any
	if err := json.Unmarshal(listResult.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	listContract, _, _ := applicationPayloadContract("GET /api/v1/tags")
	validateWireObject(t, listed, wireSchema(listContract.Response))
	if value["tenantId"] != store.DefaultTenantID {
		t.Fatal("wrong tenant in response")
	}
}
