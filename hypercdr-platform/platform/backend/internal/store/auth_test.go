package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreUserRegistrationAndPasswordReset(t *testing.T) {
	repo := NewMemoryStore()
	user, err := repo.CreateUser(DefaultTenantID, "USER@example.com", "old-password")
	if err != nil || user.Email != "user@example.com" {
		t.Fatalf("create user: %#v, %v", user, err)
	}
	if !user.MustChangePassword {
		t.Fatal("new users must be required to change their temporary password")
	}
	if _, err := repo.CreateUser(DefaultTenantID, "user@example.com", "duplicate"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	token, found, err := repo.CreatePasswordResetToken("user@example.com", time.Minute)
	if err != nil || !found || token == "" {
		t.Fatalf("create reset token: %q, %v, %v", token, found, err)
	}
	resetUser, err := repo.ResetPassword(token, "new-password")
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if resetUser.MustChangePassword {
		t.Fatal("password recovery must clear the temporary-password requirement")
	}
	if _, ok, _ := repo.AuthenticateUser(UserAuthInput{Email: "user@example.com", Password: "new-password"}); !ok {
		t.Fatal("new password was not accepted")
	}
	if _, err := repo.ResetPassword(token, "reused"); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("expected one-time token rejection, got %v", err)
	}
}

func TestPasswordChangeRequirementCanBeClearedOrRestored(t *testing.T) {
	repo := NewMemoryStore()
	users, _ := repo.ListUsers()
	admin := users[0]
	if !admin.MustChangePassword {
		t.Fatal("built-in admin must change the initial password")
	}
	changed, found, err := repo.SetUserPassword(admin.ID, "secure-password", false)
	if err != nil || !found || changed.MustChangePassword {
		t.Fatalf("own password change did not clear requirement: %#v, %v, %v", changed, found, err)
	}
	session, err := repo.CreatePlatformSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	reset, found, err := repo.SetUserPassword(admin.ID, "temporary-password", true)
	if err != nil || !found || !reset.MustChangePassword {
		t.Fatalf("administrator reset did not restore requirement: %#v, %v, %v", reset, found, err)
	}
	if _, valid, err := repo.AuthenticatePlatformSession(session.Token); err != nil || valid {
		t.Fatalf("temporary password reset must invalidate existing sessions: valid=%v err=%v", valid, err)
	}
}

func TestMemoryStoreGoogleUserLinksExistingEmail(t *testing.T) {
	repo := NewMemoryStore()
	created, err := repo.CreateUser(DefaultTenantID, "user@example.com", "password")
	if err != nil {
		t.Fatal(err)
	}
	linked, err := repo.FindOrCreateGoogleUser("USER@example.com")
	if err != nil || linked.ID != created.ID {
		t.Fatalf("expected existing account link: %#v, %v", linked, err)
	}
}

func TestTenantAndSystemAdminMetadataStayConsistent(t *testing.T) {
	repo := NewMemoryStore()

	tenants, err := repo.ListTenants()
	if err != nil || len(tenants) != 1 {
		t.Fatalf("list tenants: %#v, %v", tenants, err)
	}
	if tenants[0].Name != "Admin" || tenants[0].UserCount != 1 {
		t.Fatalf("default tenant metadata is inconsistent: %#v", tenants[0])
	}

	users, err := repo.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("list users: %#v, %v", users, err)
	}
	admin := users[0]
	if admin.TenantName != "Admin" {
		t.Fatalf("admin tenant name=%q want Admin", admin.TenantName)
	}

	updated, found, err := repo.UpdateUser(UserUpdateInput{
		ID: admin.ID, TenantID: "another-tenant", Email: "changed@example.com",
		DisplayName: "Platform Administrator", Role: "operator", Status: "disabled",
	})
	if err != nil || !found {
		t.Fatalf("update admin: %#v, %v, %v", updated, found, err)
	}
	if updated.DisplayName != "Platform Administrator" || updated.TenantID != DefaultTenantID || updated.Email != DefaultAdminEmail || updated.Role != "admin" || updated.Status != "active" {
		t.Fatalf("protected admin fields changed unexpectedly: %#v", updated)
	}
}

func TestTenantDescriptionPersistsAndIsLimited(t *testing.T) {
	repo := NewMemoryStore()
	tenant, err := repo.CreateTenant(TenantInput{Name: "Customer", Description: "Customer production workloads", Status: "active"})
	if err != nil || tenant.Description != "Customer production workloads" {
		t.Fatalf("create tenant description: %#v, %v", tenant, err)
	}
	if _, err := repo.CreateTenant(TenantInput{Name: "Too long", Description: strings.Repeat("x", 501)}); err == nil {
		t.Fatal("expected tenant description length validation")
	}
}
