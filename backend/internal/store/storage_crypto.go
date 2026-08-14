package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func (s *PostgresStore) ConfigureSecretKey(ctx context.Context, secretKey string) error {
	s.secretKey = strings.TrimSpace(secretKey)
	if s.secretKey == "" {
		return errors.New("HCDR_SECRET_KEY is required to protect storage repository credentials")
	}
	rows, err := s.db.QueryContext(ctx, `select id,secret_payload from storage_repositories where secret_ciphertext='' and secret_payload<>'{}'::jsonb`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type legacy struct {
		id      string
		payload []byte
	}
	var items []legacy
	for rows.Next() {
		var item legacy
		if err = rows.Scan(&item.id, &item.payload); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		ciphertext, encryptErr := encryptStoragePayload(item.payload, s.secretKey)
		for index := range item.payload {
			item.payload[index] = 0
		}
		if encryptErr != nil {
			return encryptErr
		}
		if _, err = s.db.ExecContext(ctx, `update storage_repositories set secret_ciphertext=$2,secret_payload='{}'::jsonb,updated_at=now() where id=$1 and secret_ciphertext=''`, item.id, ciphertext); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) encodeStorageSecret(secret map[string]string) (string, error) {
	if len(secret) == 0 {
		return "", nil
	}
	if s.secretKey == "" {
		return "", errors.New("storage credential encryption is not configured")
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return "", err
	}
	ciphertext, err := encryptStoragePayload(raw, s.secretKey)
	for index := range raw {
		raw[index] = 0
	}
	return ciphertext, err
}

func (s *PostgresStore) decodeStorageSecret(ciphertext string, legacy []byte) (map[string]string, error) {
	secret := map[string]string{}
	if strings.TrimSpace(ciphertext) == "" {
		if len(legacy) > 0 {
			_ = json.Unmarshal(legacy, &secret)
		}
		return secret, nil
	}
	if s.secretKey == "" {
		return nil, errors.New("storage credential decryption is not configured")
	}
	raw, err := decryptStoragePayload(ciphertext, s.secretKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt storage repository credentials: %w", err)
	}
	defer func() {
		for index := range raw {
			raw[index] = 0
		}
	}()
	if err = json.Unmarshal(raw, &secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func encryptStoragePayload(value []byte, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, value, nil)), nil
}

func decryptStoragePayload(value, secret string) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, errors.New("invalid encrypted storage payload")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}
