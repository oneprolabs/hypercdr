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
	}
	cfg.ImageRegistry, cfg.AgentNamespace, cfg.VeleroVersion = settings.ImageRegistry, settings.AgentNamespace, settings.VeleroVersion
	if strings.TrimSpace(settings.PublicEndpoint) != "" {
		cfg.PublicBaseURL = settings.PublicEndpoint
	}
	handler := httpserver.NewRouterWithProductInfo(cfg, logger, repo, httpserver.ProductInfo{
		Product: "HyperCDR", Edition: string(options.Edition), Capabilities: options.Capabilities, License: options.License,
	})
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

func utcLogTime(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC())
	}
	return attr
}
