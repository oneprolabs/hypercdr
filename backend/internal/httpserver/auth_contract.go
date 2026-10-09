package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strings"
	"time"
)

// Wire DTOs are shared by handlers and schema descriptions. Keep validation
// in handlers; this layer documents the serialized shape without duplicating it.
type loginRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	CaptchaID      string `json:"captchaId"`
	CaptchaCode    string `json:"captchaCode"`
	TurnstileToken string `json:"turnstileToken"`
}
type profileRequest struct {
	DisplayName string  `json:"displayName"`
	Email       string  `json:"email"`
	TimeZone    *string `json:"timeZone"`
}
type themeRequest struct {
	Theme string `json:"theme"`
}
type passwordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	RecoveryEmail   string `json:"recoveryEmail"`
}
type forgotPasswordRequest struct {
	Email string `json:"email"`
}
type resetPasswordRequest struct{ Token, Password string }
type captchaResponse struct {
	ID        string `json:"id"`
	Image     string `json:"image"`
	ExpiresAt string `json:"expiresAt" format:"date-time"`
}
type loginSessionResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt" format:"date-time"`
}
type loginResponse struct {
	User    EditionIdentity      `json:"user"`
	Session loginSessionResponse `json:"session"`
}
type logoutResponse struct {
	LoggedOut bool `json:"loggedOut"`
}
type messageResponse struct {
	Message string `json:"message"`
}
type passwordResetResponse struct {
	Message          string `json:"message"`
	ResetToken       string `json:"resetToken,omitempty"`
	ExpiresInSeconds int    `json:"expiresInSeconds,omitempty"`
}
type authConfigResponse struct {
	GoogleEnabled    bool   `json:"googleEnabled"`
	ChallengeMode    string `json:"challengeMode"`
	TurnstileSiteKey string `json:"turnstileSiteKey"`
	TurnstileEnabled bool   `json:"turnstileEnabled"`
	TimeZone         string `json:"timeZone"`
}
type turnstilePublicConfig struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	SiteKey    string `json:"site_key"`
}
type turnstileConfigResponse struct {
	Code string                `json:"code"`
	Data turnstilePublicConfig `json:"data"`
}

type payloadContract struct {
	Request, Response reflect.Type
	Required          []string
}

func authPayloadContract(pattern string) (payloadContract, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/auth/captcha":          {Response: reflect.TypeFor[captchaResponse]()},
		"POST /api/v1/auth/login":           {Request: reflect.TypeFor[loginRequest](), Response: reflect.TypeFor[loginResponse](), Required: []string{"email", "password"}},
		"POST /api/v1/auth/logout":          {Response: reflect.TypeFor[logoutResponse]()},
		"GET /api/v1/auth/me":               {Response: reflect.TypeFor[store.User]()},
		"PATCH /api/v1/auth/me":             {Request: reflect.TypeFor[profileRequest](), Response: reflect.TypeFor[EditionIdentity]()},
		"PATCH /api/v1/auth/me/theme":       {Request: reflect.TypeFor[themeRequest](), Response: reflect.TypeFor[EditionIdentity](), Required: []string{"theme"}},
		"POST /api/v1/auth/change-password": {Request: reflect.TypeFor[passwordChangeRequest](), Response: reflect.TypeFor[EditionIdentity](), Required: []string{"currentPassword", "newPassword"}},
		"POST /api/v1/auth/forgot-password": {Request: reflect.TypeFor[forgotPasswordRequest](), Response: reflect.TypeFor[passwordResetResponse]()},
		"POST /api/v1/auth/reset-password":  {Request: reflect.TypeFor[resetPasswordRequest](), Response: reflect.TypeFor[messageResponse](), Required: []string{"Token", "Password"}},
		"GET /api/v1/auth/config":           {Response: reflect.TypeFor[authConfigResponse]()},
		"GET /api/v1/auth/turnstile/config": {Response: reflect.TypeFor[turnstileConfigResponse]()},
	}
	c, ok := contracts[pattern]
	return c, ok
}

func wireSchema(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return map[string]any{"anyOf": []any{wireSchema(t.Elem()), map[string]any{"type": "null"}}}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": wireSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": wireSchema(t.Elem())}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("json")
			name, opts, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = wireSchema(field.Type)
			if format := field.Tag.Get("format"); format != "" {
				properties[name].(map[string]any)["format"] = format
			}
			if !strings.Contains(opts, "omitempty") {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required}
	default:
		return map[string]any{}
	}
}

func (r *Router) applyAuthPayloadContract(pattern string, operation map[string]any) bool {
	c, ok := authPayloadContract(pattern)
	if !ok {
		return false
	}
	if c.Request != nil {
		schema := wireSchema(c.Request)
		delete(schema, "required")
		if len(c.Required) > 0 {
			schema["required"] = c.Required
		}
		properties := schema["properties"].(map[string]any)
		if pattern == "POST /api/v1/auth/login" {
			if r.authChallengeMode() == "turnstile" {
				schema["required"] = []string{"email", "password", "turnstileToken"}
			} else {
				schema["required"] = []string{"email", "password", "captchaId", "captchaCode"}
			}
		}
		if pattern == "PATCH /api/v1/auth/me/theme" {
			properties["theme"].(map[string]any)["enum"] = []string{"light", "dark"}
		}
		if pattern == "POST /api/v1/auth/change-password" {
			properties["newPassword"].(map[string]any)["minLength"] = 8
			properties["newPassword"].(map[string]any)["maxLength"] = 128
		}
		if pattern == "POST /api/v1/auth/reset-password" {
			properties["Password"].(map[string]any)["minLength"] = 8
			properties["Password"].(map[string]any)["maxLength"] = 128
		}
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	responses := operation["responses"].(map[string]any)
	delete(responses, "2XX")
	responses["200"] = map[string]any{"description": "Successful response", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(c.Response)}}}
	operation["x-hypercdr-payload-contract"] = true
	return true
}
