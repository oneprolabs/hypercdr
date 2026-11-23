package licensing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidLicense = errors.New("license is invalid or does not belong to this organization")

type Claims struct {
	Version     int       `json:"version"`
	Product     string    `json:"product"`
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantId"`
	Revision    int64     `json:"revision"`
	MaxClusters int       `json:"maxClusters"`
	IssuedAt    time.Time `json:"issuedAt"`
	ValidFrom   time.Time `json:"validFrom"`
	ValidUntil  time.Time `json:"validUntil"`
}

type Envelope struct {
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type Verified struct {
	Claims
	KeyID   string
	Encoded string
}

func Verify(encoded, tenantID string, keys map[string]ed25519.PublicKey, now time.Time) (Verified, error) {
	if len(encoded) == 0 || len(encoded) > 16384 {
		return Verified{}, ErrInvalidLicense
	}
	var envelope Envelope
	if decode([]byte(encoded), &envelope) != nil {
		return Verified{}, ErrInvalidLicense
	}
	key := keys[envelope.KeyID]
	payload, payloadErr := base64.RawURLEncoding.DecodeString(envelope.Payload)
	signature, signatureErr := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if len(key) != ed25519.PublicKeySize || payloadErr != nil || signatureErr != nil || !ed25519.Verify(key, payload, signature) {
		return Verified{}, ErrInvalidLicense
	}
	var claims Claims
	if decode(payload, &claims) != nil || claims.Version != 1 || claims.Product != "hypercdr" || claims.TenantID != tenantID || claims.Revision <= 0 || claims.MaxClusters < 2 || claims.MaxClusters > 1000000 {
		return Verified{}, ErrInvalidLicense
	}
	if _, err := uuid.Parse(claims.ID); err != nil {
		return Verified{}, ErrInvalidLicense
	}
	if _, err := uuid.Parse(claims.TenantID); err != nil {
		return Verified{}, ErrInvalidLicense
	}
	if claims.IssuedAt.IsZero() || claims.ValidFrom.IsZero() || claims.ValidUntil.IsZero() || claims.IssuedAt.After(now) || claims.ValidFrom.After(now) || !claims.ValidUntil.After(now) || !claims.ValidUntil.After(claims.ValidFrom) || claims.IssuedAt.After(claims.ValidUntil) {
		return Verified{}, ErrInvalidLicense
	}
	canonical, err := json.Marshal(envelope)
	if err != nil {
		return Verified{}, ErrInvalidLicense
	}
	return Verified{Claims: claims, KeyID: envelope.KeyID, Encoded: string(canonical)}, nil
}

func Sign(claims Claims, keyID string, key ed25519.PrivateKey, now time.Time) (string, error) {
	if len(key) != ed25519.PrivateKeySize || strings.TrimSpace(keyID) == "" || len(keyID) > 64 {
		return "", ErrInvalidLicense
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	envelope := Envelope{KeyID: keyID, Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	verified, err := Verify(string(encoded), claims.TenantID, map[string]ed25519.PublicKey{keyID: key.Public().(ed25519.PublicKey)}, now)
	return verified.Encoded, err
}

func ParsePublicKeys(raw string) (map[string]ed25519.PublicKey, error) {
	keys := map[string]ed25519.PublicKey{}
	if strings.TrimSpace(raw) == "" {
		return keys, nil
	}
	var encoded map[string]string
	if decode([]byte(raw), &encoded) != nil {
		return nil, errors.New("HCDR_LICENSE_PUBLIC_KEYS must be a JSON object of key IDs and base64 public keys")
	}
	for keyID, value := range encoded {
		key, err := base64.StdEncoding.DecodeString(value)
		if strings.TrimSpace(keyID) == "" || len(keyID) > 64 || err != nil || len(key) != ed25519.PublicKeySize {
			return nil, errors.New("HCDR_LICENSE_PUBLIC_KEYS contains an invalid Ed25519 public key")
		}
		keys[keyID] = ed25519.PublicKey(key)
	}
	return keys, nil
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalidLicense
	}
	return nil
}
