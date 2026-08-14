package httpserver

import (
	"context"
	"errors"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

type storeIdentityProvider struct{ store store.Store }
type storeAuditSink struct{ store store.Store }

func (s storeAuditSink) RecordAudit(_ context.Context, event EditionAuditEvent) error {
	_, err := s.store.CreateAuditLog(store.AuditLogInput{ActorID: event.ActorID, Actor: event.Actor, Action: event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID, ResourceName: event.ResourceName, Result: event.Result, Message: event.Message, Payload: map[string]any{"httpStatus": event.HTTPStatus}})
	return err
}

func identityFromStore(user store.User) EditionIdentity {
	return EditionIdentity{ID: user.ID, TenantID: user.TenantID, TenantName: user.TenantName, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role, Status: user.Status, AuthProvider: user.AuthProvider, TimeZone: user.TimeZone, SystemAdmin: user.SystemAdmin, MustChangePassword: user.MustChangePassword}
}

func storeUserFromIdentity(user EditionIdentity) store.User {
	return store.User{ID: user.ID, TenantID: user.TenantID, TenantName: user.TenantName, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role, Status: user.Status, AuthProvider: user.AuthProvider, TimeZone: user.TimeZone, SystemAdmin: user.SystemAdmin, MustChangePassword: user.MustChangePassword}
}

func (p storeIdentityProvider) Authenticate(_ context.Context, email, password string) (EditionIdentity, bool, error) {
	user, found, err := p.store.AuthenticateUser(store.UserAuthInput{Email: email, Password: password})
	return identityFromStore(user), found, err
}
func (p storeIdentityProvider) CreateSession(_ context.Context, userID string, ttl time.Duration) (EditionIdentitySession, error) {
	session, err := p.store.CreatePlatformSession(userID, ttl)
	return EditionIdentitySession{Token: session.Token, ExpiresAt: session.ExpiresAt}, err
}
func (p storeIdentityProvider) AuthenticateSession(_ context.Context, token string) (EditionIdentity, bool, error) {
	user, found, err := p.store.AuthenticatePlatformSession(token)
	return identityFromStore(user), found, err
}
func (p storeIdentityProvider) DeleteSession(_ context.Context, token string) error {
	return p.store.DeletePlatformSession(token)
}
func (p storeIdentityProvider) UpdateProfile(_ context.Context, input EditionIdentityProfileUpdate) (EditionIdentity, bool, error) {
	current, found, err := p.store.GetUser(input.ID)
	if err != nil || !found {
		return EditionIdentity{}, found, err
	}
	user, found, err := p.store.UpdateUser(store.UserUpdateInput{ID: current.ID, TenantID: current.TenantID, Email: input.Email, DisplayName: input.DisplayName, Role: current.Role, Status: current.Status, TimeZone: input.TimeZone})
	return identityFromStore(user), found, err
}
func (p storeIdentityProvider) SetPassword(_ context.Context, id, password string, mustChange bool) (EditionIdentity, bool, error) {
	user, found, err := p.store.SetUserPassword(id, password, mustChange)
	return identityFromStore(user), found, err
}
func (p storeIdentityProvider) CreatePasswordResetToken(_ context.Context, email string, ttl time.Duration) (string, bool, error) {
	return p.store.CreatePasswordResetToken(email, ttl)
}
func (p storeIdentityProvider) ResetPassword(_ context.Context, token, password string) (bool, error) {
	_, err := p.store.ResetPassword(token, password)
	if errors.Is(err, store.ErrResetInvalid) {
		return false, nil
	}
	return err == nil, err
}
