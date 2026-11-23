package saas

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/licensing"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func TestTenantLicenseActivationAPI(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "license-owner@example.test", "Licensed organization")
	service.cfg.SaaS = false
	admin := principal(owner)
	member := platform.Principal{ID: owner.ID, TenantID: owner.TenantID, Role: "operator"}
	path := "/api/v1/saas/license"
	if response := call(t, service, "GET", path, nil, member); response.Code != 200 {
		t.Fatalf("member usage read: %d", response.Code)
	}
	if response := call(t, service, "POST", path, map[string]string{"license": "invalid"}, member); response.Code != 403 {
		t.Fatalf("member activation: %d", response.Code)
	}
	if response := call(t, service, "POST", path, map[string]string{"license": "invalid"}, admin); response.Code != 503 {
		t.Fatalf("missing issuer key: %d", response.Code)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service.cfg.LicensePublicKeys = map[string]ed25519.PublicKey{"test-issuer": publicKey}
	if response := call(t, service, "POST", path, map[string]string{"license": "invalid"}, admin); response.Code != 400 {
		t.Fatalf("invalid license: %d", response.Code)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := licensing.Claims{Version: 1, Product: "hypercdr", ID: uuid.NewString(), TenantID: owner.TenantID, Revision: 1, MaxClusters: 5, IssuedAt: now.Add(-time.Minute), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	makeLicense := func(claims licensing.Claims) string {
		payload, _ := json.Marshal(claims)
		envelope, _ := json.Marshal(licensing.Envelope{KeyID: "test-issuer", Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))})
		return string(envelope)
	}
	for attempt := 0; attempt < 2; attempt++ {
		encoded := makeLicense(claims)
		if attempt == 1 {
			var formatted bytes.Buffer
			if err := json.Indent(&formatted, []byte(encoded), "", "  "); err != nil {
				t.Fatal(err)
			}
			encoded = formatted.String()
		}
		response := call(t, service, "POST", path, map[string]string{"license": encoded}, admin)
		if response.Code != 200 {
			t.Fatalf("activation attempt %d: %d %s", attempt, response.Code, response.Body.String())
		}
		var result struct {
			Usage struct {
				Plan        string `json:"plan"`
				MaxClusters int    `json:"maxClusters"`
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Usage.Plan != "commercial" || result.Usage.MaxClusters != 5 {
			t.Fatalf("effective entitlement not returned: %s", response.Body.String())
		}
	}
	claims.ID = uuid.NewString()
	if response := call(t, service, "POST", path, map[string]string{"license": makeLicense(claims)}, admin); response.Code != 409 {
		t.Fatalf("same revision replacement: %d", response.Code)
	}
	claims.TenantID = uuid.NewString()
	if response := call(t, service, "POST", path, map[string]string{"license": makeLicense(claims)}, admin); response.Code != 400 {
		t.Fatalf("wrong-tenant license: %d", response.Code)
	}
}
