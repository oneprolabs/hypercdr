package store

import "testing"

func TestStorageCredentialEncryptionRoundTrip(t *testing.T) {
	plain := []byte(`{"accessKey":"access","secretKey":"secret"}`)
	ciphertext, err := encryptStoragePayload(plain, "platform-secret")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == string(plain) || ciphertext == "" {
		t.Fatal("storage credential was not encrypted")
	}
	decoded, err := decryptStoragePayload(ciphertext, "platform-secret")
	if err != nil || string(decoded) != string(plain) {
		t.Fatalf("decoded=%q err=%v", decoded, err)
	}
	if _, err = decryptStoragePayload(ciphertext, "wrong-secret"); err == nil {
		t.Fatal("wrong secret decrypted storage credentials")
	}
}

func TestEncodeStorageSecretRequiresConfiguredKey(t *testing.T) {
	store := &PostgresStore{}
	if _, err := store.encodeStorageSecret(map[string]string{"secretKey": "secret"}); err == nil {
		t.Fatal("unencrypted storage secret accepted")
	}
	store.secretKey = "configured-secret"
	ciphertext, err := store.encodeStorageSecret(map[string]string{"secretKey": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := store.decodeStorageSecret(ciphertext, nil)
	if err != nil || decoded["secretKey"] != "secret" {
		t.Fatalf("decoded=%v err=%v", decoded, err)
	}
}
