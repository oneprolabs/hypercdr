package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func validateRegistrationResponse(t *testing.T, pattern string, value any) {
	t.Helper()
	c, _, ok := registrationPayloadContract(pattern)
	if !ok || c.Response == nil {
		t.Fatalf("missing registration response: %s", pattern)
	}
	validateWireObject(t, value, wireSchema(c.Response))
}

func TestRegistrationAndTokenContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		registration := strings.Contains(route.Pattern, "/cluster-registrations/")
		token := strings.Contains(route.Pattern, "/agent-tokens") || route.Pattern == "POST /api/v1/disaster-handovers/validate"
		if !registration && !token {
			continue
		}
		op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
		ok := false
		if registration {
			ok = applyRegistrationPayloadContract(route.Pattern, op)
		} else {
			ok = applyTokenPayloadContract(route.Pattern, op)
		}
		if !ok {
			t.Fatalf("missing payload: %s", route.Pattern)
		}
		if _, exists := op["responses"].(map[string]any)["2XX"]; exists {
			t.Fatalf("placeholder: %s", route.Pattern)
		}
		if strings.HasSuffix(route.Pattern, "/validate") {
			if security, ok := op["security"].([]any); !ok || len(security) != 0 {
				t.Fatalf("validation requires platform login: %s", route.Pattern)
			}
			if op["x-hypercdr-token-purpose"] == nil {
				t.Fatal("missing purpose restriction")
			}
		}
		count++
	}
	if count != 11 {
		t.Fatalf("coverage = %d, want 11", count)
	}
}

func TestAgentTokenActualResponsesMatchContractAndValidationDoesNotConsume(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	r := &Router{store: repo, logger: slog.Default()}
	create := httptest.NewRecorder()
	req := tenantRequest(httptest.NewRequest("POST", "/api/v1/agent-tokens", strings.NewReader(`{"clusterType":"openshift"}`)), actor)
	r.createAgentToken(create, req)
	if create.Code != 201 {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(create.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	validateWireObject(t, body, wireSchema(reflect.TypeFor[agentTokenResponse]()))
	token := body["token"].(string)
	check := func(value string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		r.validateAgentToken(w, httptest.NewRequest("POST", "/api/v1/agent-tokens/validate", strings.NewReader(value)))
		if w.Code != status {
			t.Fatalf("validation: %d %s", w.Code, w.Body.String())
		}
		var result any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		applyTokenPayloadContract("POST /api/v1/agent-tokens/validate", op)
		schema := op["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		validateWireObject(t, result, schema)
	}
	encoded, _ := json.Marshal(map[string]string{"token": token})
	check(string(encoded), 200)
	check(string(encoded), 200)
	check(`{}`, 400)
	check(`{"token":"invalid"}`, 401)
	expired, err := repo.CreateAgentToken(actor.TenantID, actor.ID, "expired", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(map[string]string{"token": expired.Token})
	check(string(encoded), 410)
	// Registration tokens must never pass the disaster-handover purpose check.
	w := httptest.NewRecorder()
	encoded, _ = json.Marshal(map[string]string{"token": token})
	r.validateDisasterHandoverToken(w, httptest.NewRequest("POST", "/api/v1/disaster-handovers/validate", strings.NewReader(string(encoded))))
	if w.Code != 401 {
		t.Fatalf("cross-purpose token accepted: %d", w.Code)
	}
}
