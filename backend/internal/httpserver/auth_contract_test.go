package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
)

func TestEveryMountedAuthRouteHasPayloadContract(t *testing.T) {
	r := &Router{mux: http.NewServeMux()}
	r.mountAuthRoutes()
	for _, route := range r.routeContracts {
		c, ok := authPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing auth contract: %s", route.Pattern)
		}
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		if !r.applyAuthPayloadContract(route.Pattern, op) {
			t.Fatal(route.Pattern)
		}
		if _, exists := op["responses"].(map[string]any)["2XX"]; exists {
			t.Fatal("placeholder auth success")
		}
	}
	schema := wireSchema(reflect.TypeFor[loginResponse]())
	user := schema["properties"].(map[string]any)["user"].(map[string]any)
	for key := range user["properties"].(map[string]any) {
		if strings.Contains(strings.ToLower(key), "password") && key != "mustChangePassword" {
			t.Fatal("password field exposed", key)
		}
	}
}

func TestAuthContractMatchesActualCaptchaLoginConfigAndLogout(t *testing.T) {
	repo := newTestStore(t)
	handler := NewRouter(config.Config{AuthChallengeMode: "image"}, slog.Default(), repo)
	server := httptest.NewServer(handler)
	defer server.Close()
	defer handler.(interface{ Close(context.Context) error }).Close(context.Background())
	read := func(method, path, body, token string) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, response.StatusCode, raw)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		c, ok := authPayloadContract(method + " " + path)
		if !ok {
			t.Fatal("missing contract", path)
		}
		validateWireObject(t, decoded, wireSchema(c.Response))
		return decoded
	}
	captcha := read("GET", "/api/v1/auth/captcha", "", "")
	svg, err := base64.StdEncoding.DecodeString(strings.SplitN(captcha["image"].(string), ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	code := ""
	for _, match := range regexp.MustCompile(`<text[^>]*>([^<]+)</text>`).FindAllSubmatch(svg, -1) {
		code += string(match[1])
	}
	body, _ := json.Marshal(loginRequest{Email: "admin", Password: "admin123", CaptchaID: captcha["id"].(string), CaptchaCode: code})
	login := read("POST", "/api/v1/auth/login", string(body), "")
	token := login["session"].(map[string]any)["token"].(string)
	read("GET", "/api/v1/auth/me", "", token)
	read("GET", "/api/v1/auth/config", "", token)
	read("GET", "/api/v1/auth/turnstile/config", "", token)
	read("PATCH", "/api/v1/auth/me/theme", `{"theme":"dark"}`, token)
	read("PATCH", "/api/v1/auth/me", `{"displayName":"Contract Admin"}`, token)
	read("POST", "/api/v1/auth/forgot-password", `{"email":""}`, "")
	read("POST", "/api/v1/auth/logout", "{}", token)
}

func validateWireObject(t *testing.T, value any, schema map[string]any) {
	t.Helper()
	if choices, ok := schema["anyOf"].([]any); ok {
		if value == nil {
			return
		}
		validateWireObject(t, value, choices[0].(map[string]any))
		return
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected object, got %T", value)
		}
		for _, key := range schema["required"].([]string) {
			if _, ok := object[key]; !ok {
				t.Fatalf("missing required %s", key)
			}
		}
		properties := schema["properties"].(map[string]any)
		for key, val := range object {
			child, ok := properties[key]
			if !ok {
				t.Fatalf("undocumented field %s", key)
			}
			validateWireObject(t, val, child.(map[string]any))
		}
	case "string":
		if _, ok := value.(string); !ok {
			t.Fatalf("expected string got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			t.Fatalf("expected boolean got %T", value)
		}
	case "integer", "number":
		if _, ok := value.(float64); !ok {
			t.Fatalf("expected number got %T", value)
		}
	}
}
