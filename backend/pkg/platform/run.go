package platform

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/httpserver"
	"hypercdr-platform/platform/backend/internal/store"
)

// Run starts a complete HyperCDR control plane assembled for the requested
// edition. Enterprise code depends on this stable package, never internal code.
func Run(options Options) error {
	options = normalizeOptions(options)
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcLogTime}))
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return errors.New("HCDR_DATABASE_URL is required; the platform runs against PostgreSQL only")
	}
	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	postgresStore, err := store.NewPostgresStore(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer postgresStore.Close()
	secretCtx, secretCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := postgresStore.ConfigureSecretKey(secretCtx, cfg.SecretKey); err != nil {
		secretCancel()
		return err
	}
	secretCancel()
	migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer migrationCancel()
	if err := postgresStore.ApplyEditionMigrations(migrationCtx, editionMigrations(options.Migrations)); err != nil {
		return err
	}
	if options.DiagnosticSink != nil {
		postgresStore.SetDiagnosticLogWriter(diagnosticWriterAdapter{sink: options.DiagnosticSink})
	}
	repo := store.Store(postgresStore)
	logger = slog.New(store.NewDiagnosticSlogHandler(logger.Handler(), repo, "platform-api"))
	settings, found, err := repo.GetPlatformSettings()
	if err != nil {
		return err
	}
	if !found {
		settings, err = repo.UpsertPlatformSettings(store.PlatformSettingsInput{ImageRegistry: cfg.ImageRegistry, AgentNamespace: cfg.AgentNamespace, VeleroVersion: cfg.VeleroVersion, PublicEndpoint: cfg.PublicBaseURL})
		if err != nil {
			return err
		}
	} else if input, required := missingRegistryBackfill(settings, cfg); required {
		settings, err = repo.UpsertPlatformSettings(input)
		if err != nil {
			return err
		}
	}
	if namespace := strings.TrimSpace(options.AgentNamespace); namespace != "" && settings.AgentNamespace != namespace {
		settings, err = repo.UpsertPlatformSettings(store.PlatformSettingsInput{
			ImageRegistry:  settings.ImageRegistry,
			AgentNamespace: namespace,
			VeleroVersion:  settings.VeleroVersion,
			PublicEndpoint: settings.PublicEndpoint,
		})
		if err != nil {
			return err
		}
	}
	cfg.ImageRegistry, cfg.AgentNamespace, cfg.VeleroVersion = settings.ImageRegistry, settings.AgentNamespace, settings.VeleroVersion
	if strings.TrimSpace(settings.PublicEndpoint) != "" {
		cfg.PublicBaseURL = settings.PublicEndpoint
	}
	handler := httpserver.NewRouterWithProductInfo(cfg, logger, repo, httpserver.ProductInfo{
		Product: "HyperCDR", Edition: string(options.Edition), Capabilities: options.Capabilities, License: options.License,
	}, editionAuthorizer(options.Authorizer), httpserver.WithDiagnosticLogRetention(options.DiagnosticLogRetention), httpserver.WithExtensionRoutes(editionRoutes(options.Routes)), httpserver.WithIdentityProvider(editionIdentityProvider(options.IdentityProvider)), httpserver.WithAuditSink(editionAuditSink(options.AuditSink)))
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled {
			if strings.TrimSpace(cfg.TLSCertFile) == "" || strings.TrimSpace(cfg.TLSKeyFile) == "" {
				errCh <- errors.New("HCDR_TLS_CERT_FILE and HCDR_TLS_KEY_FILE are required when TLS is enabled")
				return
			}
			errCh <- server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		errCh <- server.ListenAndServe()
	}()
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stopCh:
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	ctx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return server.Shutdown(ctx)
}

func missingRegistryBackfill(settings store.PlatformSettings, cfg config.Config) (store.PlatformSettingsInput, bool) {
	if strings.TrimSpace(settings.ImageRegistry) != "" || strings.TrimSpace(cfg.ImageRegistry) == "" {
		return store.PlatformSettingsInput{}, false
	}
	return store.PlatformSettingsInput{
		ImageRegistry:  strings.TrimRight(strings.TrimSpace(cfg.ImageRegistry), "/"),
		AgentNamespace: settings.AgentNamespace,
		VeleroVersion:  settings.VeleroVersion,
		PublicEndpoint: settings.PublicEndpoint,
	}, true
}

type diagnosticWriterAdapter struct{ sink DiagnosticSink }

func (a diagnosticWriterAdapter) CreateDiagnosticLog(input store.DiagnosticLogInput) (store.DiagnosticLog, error) {
	event, err := a.sink.RecordDiagnostic(context.Background(), DiagnosticEvent{TenantID: input.TenantID, Scope: input.Scope, Level: input.Level, Component: input.Component, Operation: input.Operation, Message: input.Message, ClusterID: input.ClusterID, TaskID: input.TaskID, CommandID: input.CommandID, RequestID: input.RequestID, ErrorCode: input.ErrorCode, Status: input.Status, DurationMS: input.DurationMS, Details: input.Details, EventAt: input.EventAt, Fingerprint: input.Fingerprint})
	return store.DiagnosticLog{ID: event.ID, TenantID: event.TenantID, Scope: event.Scope, Level: event.Level, Component: event.Component, Operation: event.Operation, Message: event.Message, ClusterID: event.ClusterID, TaskID: event.TaskID, CommandID: event.CommandID, RequestID: event.RequestID, ErrorCode: event.ErrorCode, Status: event.Status, DurationMS: event.DurationMS, Details: event.Details, EventAt: event.EventAt, CreatedAt: event.CreatedAt, Fingerprint: event.Fingerprint}, err
}

func editionRoutes(items []Route) []httpserver.ExtensionRoute {
	result := make([]httpserver.ExtensionRoute, 0, len(items))
	for _, item := range items {
		handler := item.Handler
		result = append(result, httpserver.ExtensionRoute{
			Pattern: item.Pattern,
			Handler: func(w http.ResponseWriter, req *http.Request, principal httpserver.EditionPrincipal) {
				handler(w, req, Principal{ID: principal.ID, TenantID: principal.TenantID, Email: principal.Email, Role: principal.Role, SystemAdmin: principal.SystemAdmin})
			},
		})
	}
	return result
}

func editionMigrations(items []Migration) []store.EditionMigration {
	result := make([]store.EditionMigration, 0, len(items))
	for _, item := range items {
		result = append(result, store.EditionMigration{Version: item.Version, SQL: item.SQL})
	}
	return result
}

func editionAuthorizer(authorizer Authorizer) httpserver.EditionAuthorizer {
	if authorizer == nil {
		return nil
	}
	return func(ctx context.Context, request httpserver.EditionAuthorizationRequest) httpserver.EditionAuthorizationDecision {
		decision := authorizer.Authorize(ctx, AuthorizationRequest{
			Method: request.Method,
			Path:   request.Path,
			Principal: Principal{
				ID: request.Principal.ID, TenantID: request.Principal.TenantID,
				Email: request.Principal.Email, Role: request.Principal.Role,
				SystemAdmin: request.Principal.SystemAdmin,
			},
		})
		return httpserver.EditionAuthorizationDecision{Allowed: decision.Allowed, Code: decision.Code, Message: decision.Message}
	}
}

type identityProviderAdapter struct{ provider IdentityProvider }

func editionIdentityProvider(provider IdentityProvider) httpserver.EditionIdentityProvider {
	if provider == nil {
		return nil
	}
	return identityProviderAdapter{provider: provider}
}

type auditSinkAdapter struct{ sink AuditSink }

func editionAuditSink(sink AuditSink) httpserver.EditionAuditSink {
	if sink == nil {
		return nil
	}
	return auditSinkAdapter{sink: sink}
}
func (a auditSinkAdapter) RecordAudit(ctx context.Context, event httpserver.EditionAuditEvent) error {
	return a.sink.RecordAudit(ctx, AuditEvent{TenantID: event.TenantID, ActorID: event.ActorID, Actor: event.Actor, Action: event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID, ResourceName: event.ResourceName, Result: event.Result, Message: event.Message, HTTPStatus: event.HTTPStatus})
}

func (a identityProviderAdapter) Authenticate(ctx context.Context, email, password string) (httpserver.EditionIdentity, bool, error) {
	identity, found, err := a.provider.Authenticate(ctx, email, password)
	return editionIdentity(identity), found, err
}
func (a identityProviderAdapter) CreateSession(ctx context.Context, id string, ttl time.Duration) (httpserver.EditionIdentitySession, error) {
	session, err := a.provider.CreateSession(ctx, id, ttl)
	return httpserver.EditionIdentitySession{Token: session.Token, ExpiresAt: session.ExpiresAt}, err
}
func (a identityProviderAdapter) AuthenticateSession(ctx context.Context, token string) (httpserver.EditionIdentity, bool, error) {
	identity, found, err := a.provider.AuthenticateSession(ctx, token)
	return editionIdentity(identity), found, err
}
func (a identityProviderAdapter) DeleteSession(ctx context.Context, token string) error {
	return a.provider.DeleteSession(ctx, token)
}
func (a identityProviderAdapter) UpdateProfile(ctx context.Context, input httpserver.EditionIdentityProfileUpdate) (httpserver.EditionIdentity, bool, error) {
	identity, found, err := a.provider.UpdateProfile(ctx, IdentityProfileUpdate{ID: input.ID, Email: input.Email, DisplayName: input.DisplayName, TimeZone: input.TimeZone})
	return editionIdentity(identity), found, err
}
func (a identityProviderAdapter) SetPassword(ctx context.Context, id, password string, mustChange bool) (httpserver.EditionIdentity, bool, error) {
	identity, found, err := a.provider.SetPassword(ctx, id, password, mustChange)
	return editionIdentity(identity), found, err
}
func (a identityProviderAdapter) CreatePasswordResetToken(ctx context.Context, email string, ttl time.Duration) (string, bool, error) {
	return a.provider.CreatePasswordResetToken(ctx, email, ttl)
}
func (a identityProviderAdapter) ResetPassword(ctx context.Context, token, password string) (bool, error) {
	return a.provider.ResetPassword(ctx, token, password)
}
func editionIdentity(identity Identity) httpserver.EditionIdentity {
	return httpserver.EditionIdentity{ID: identity.ID, TenantID: identity.TenantID, TenantName: identity.TenantName, Email: identity.Email, DisplayName: identity.DisplayName, Role: identity.Role, Status: identity.Status, AuthProvider: identity.AuthProvider, TimeZone: identity.TimeZone, SystemAdmin: identity.SystemAdmin, MustChangePassword: identity.MustChangePassword}
}

func utcLogTime(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC())
	}
	return attr
}
