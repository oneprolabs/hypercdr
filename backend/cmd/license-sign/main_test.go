package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/licensing"
)

func TestLicenseSigning(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "issuer.pem")
	claimsPath := filepath.Join(directory, "claims.json")
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := licensing.Claims{Version: 1, Product: "hypercdr", ID: uuid.NewString(), TenantID: uuid.NewString(), Revision: 1, MaxClusters: 3, IssuedAt: now.Add(-time.Minute), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	encodedClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claimsPath, encodedClaims, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--claims", claimsPath, "--private-key", keyPath, "--key-id", "test-issuer"}
	var output bytes.Buffer
	if err := run(args, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	verified, err := licensing.Verify(output.String(), claims.TenantID, map[string]ed25519.PublicKey{"test-issuer": publicKey}, now)
	if err != nil || verified.MaxClusters != 3 {
		t.Fatalf("signed license failed verification: %+v %v", verified, err)
	}
	if err := os.Chmod(keyPath, 0644); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(args, &output, io.Discard); err == nil || output.Len() != 0 {
		t.Fatal("unsafe private-key permissions accepted or license emitted")
	}
	if err := run(nil, io.Discard, io.Discard); err == nil {
		t.Fatal("missing signing inputs accepted")
	}
}
