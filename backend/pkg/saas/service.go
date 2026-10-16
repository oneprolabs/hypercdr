package saas

import (
	"context"
	"database/sql"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
	"time"
)

// Open uses Community's existing single-organization identities and sessions.
// No independent account database or alternate resource ownership is introduced.
func Open(ctx context.Context, databaseURL string, cfg Config) (*Service, func(), error) {
	repo, err := store.NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		repo.Close()
		return nil, nil, err
	}
	s := New(db, cfg, communityIdentity{repo})
	return s, func() { db.Close(); repo.Close() }, nil
}

type communityIdentity struct{ repo *store.PostgresStore }

func identity(u store.User) platform.Identity {
	return platform.Identity{ID: u.ID, TenantID: u.TenantID, TenantName: u.TenantName, Email: u.Email, DisplayName: u.DisplayName, Role: u.Role, Status: u.Status, AuthProvider: u.AuthProvider, TimeZone: u.TimeZone, Theme: u.Theme, SystemAdmin: u.SystemAdmin, MustChangePassword: u.MustChangePassword}
}
func (p communityIdentity) Authenticate(_ context.Context, email, password string) (platform.Identity, bool, error) {
	u, ok, e := p.repo.AuthenticateUser(store.UserAuthInput{Email: email, Password: password})
	return identity(u), ok, e
}
func (p communityIdentity) CreateSession(_ context.Context, id string, ttl time.Duration) (platform.IdentitySession, error) {
	s, e := p.repo.CreatePlatformSession(id, ttl)
	return platform.IdentitySession{Token: s.Token, ExpiresAt: s.ExpiresAt}, e
}
func (p communityIdentity) AuthenticateSession(_ context.Context, token string) (platform.Identity, bool, error) {
	u, ok, e := p.repo.AuthenticatePlatformSession(token)
	return identity(u), ok, e
}
func (p communityIdentity) DeleteSession(_ context.Context, token string) error {
	return p.repo.DeletePlatformSession(token)
}
