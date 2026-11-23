package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func signed(t *testing.T, claims Claims, key ed25519.PrivateKey) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(Envelope{KeyID: "issuer-1", Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))})
	if err != nil {
		t.Fatal(err)
	}
	return string(envelope)
}

func TestVerifyTenantLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := Claims{Version: 1, Product: "hypercdr", ID: uuid.NewString(), TenantID: uuid.NewString(), Revision: 1, MaxClusters: 5, IssuedAt: now.Add(-time.Hour), ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	keys := map[string]ed25519.PublicKey{"issuer-1": publicKey}
	verified, err := Verify(signed(t, claims, privateKey), claims.TenantID, keys, now)
	if err != nil || verified.MaxClusters != 5 || verified.KeyID != "issuer-1" {
		t.Fatalf("valid license: %+v %v", verified, err)
	}
	cases := []struct {
		name   string
		mutate func(*Claims)
	}{
		{"product", func(value *Claims) { value.Product = "other-product" }},
		{"version", func(value *Claims) { value.Version = 2 }},
		{"tenant", func(value *Claims) { value.TenantID = uuid.NewString() }},
		{"invalid_id", func(value *Claims) { value.ID = "not-a-license-id" }},
		{"revision", func(value *Claims) { value.Revision = 0 }},
		{"free_capacity", func(value *Claims) { value.MaxClusters = 1 }},
		{"negative_capacity", func(value *Claims) { value.MaxClusters = -1 }},
		{"excessive_capacity", func(value *Claims) { value.MaxClusters = 1000001 }},
		{"expired", func(value *Claims) { value.ValidUntil = now }},
		{"not_yet_valid", func(value *Claims) { value.ValidFrom = now.Add(time.Second) }},
		{"future_issuer", func(value *Claims) { value.IssuedAt = now.Add(time.Second) }},
		{"missing_issued_at", func(value *Claims) { value.IssuedAt = time.Time{} }},
		{"invalid_period", func(value *Claims) { value.ValidFrom = value.ValidUntil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			invalid := claims
			test.mutate(&invalid)
			if _, err := Verify(signed(t, invalid, privateKey), claims.TenantID, keys, now); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	for _, raw := range []string{signed(t, claims, otherKey), "{}", "null", "[]", signed(t, claims, privateKey) + "{}"} {
		if _, err := Verify(raw, claims.TenantID, keys, now); err == nil {
			t.Fatal("malformed or wrongly signed license accepted")
		}
	}
	if _, err := Verify(signed(t, claims, privateKey), claims.TenantID, nil, now); err == nil {
		t.Fatal("untrusted signing key accepted")
	}
}

func TestLicensePublicKeyConfiguration(t *testing.T) {
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]string{"issuer-1": base64.StdEncoding.EncodeToString(publicKey)})
	keys, err := ParsePublicKeys(string(raw))
	if err != nil || !publicKey.Equal(keys["issuer-1"]) {
		t.Fatalf("public key configuration: %v", err)
	}
	for _, invalid := range []string{"[]", "invalid", `{"issuer":"invalid"}`, `{"":"AA=="}`, string(raw) + "{}"} {
		if _, err := ParsePublicKeys(invalid); err == nil {
			t.Fatalf("invalid keys accepted: %s", invalid)
		}
	}
}
