package store

import (
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreUserRegistrationAndPasswordReset(t *testing.T) {
	repo := NewMemoryStore()
	user, err := repo.CreateUser("USER@example.com", "old-password")
	if err != nil || user.Email != "user@example.com" {
		t.Fatalf("create user: %#v, %v", user, err)
	}
	if _, err := repo.CreateUser("user@example.com", "duplicate"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	token, found, err := repo.CreatePasswordResetToken("user@example.com", time.Minute)
	if err != nil || !found || token == "" {
		t.Fatalf("create reset token: %q, %v, %v", token, found, err)
	}
	if _, err := repo.ResetPassword(token, "new-password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, ok, _ := repo.AuthenticateUser(UserAuthInput{Email: "user@example.com", Password: "new-password"}); !ok {
		t.Fatal("new password was not accepted")
	}
	if _, err := repo.ResetPassword(token, "reused"); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("expected one-time token rejection, got %v", err)
	}
}

func TestMemoryStoreGoogleUserLinksExistingEmail(t *testing.T) {
	repo := NewMemoryStore()
	created, err := repo.CreateUser("user@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	linked, err := repo.FindOrCreateGoogleUser("USER@example.com")
	if err != nil || linked.ID != created.ID {
		t.Fatalf("expected existing account link: %#v, %v", linked, err)
	}
}
