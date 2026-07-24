package httpserver

import (
	"log/slog"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestEmailSettingsPasswordIsEncryptedAndNotExposed(t *testing.T) {
	r := &Router{cfg: config.Config{SecretKey: "test-secret-key"}, logger: slog.Default(), store: store.NewMemoryStore()}
	ciphertext, err := r.encryptSetting("smtp-password")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || ciphertext == "smtp-password" {
		t.Fatal("SMTP password was not encrypted")
	}
	plain, err := r.decryptSetting(ciphertext)
	if err != nil || plain != "smtp-password" {
		t.Fatalf("decrypt=%q err=%v", plain, err)
	}
	item, err := r.store.UpsertEmailSettings(store.EmailSettingsInput{Enabled: true, Host: "smtp.example.com", Port: 587, Security: "starttls", Username: "mailer", PasswordCiphertext: ciphertext, SenderName: "HyperCDR", SenderEmail: "noreply@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !item.PasswordConfigured || item.PasswordCiphertext != ciphertext {
		t.Fatal("password configuration state was not preserved")
	}
}
