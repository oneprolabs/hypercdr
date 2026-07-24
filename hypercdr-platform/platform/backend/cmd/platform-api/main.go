package main

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

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		logger.Error("HCDR_DATABASE_URL is required; the platform now runs against PostgreSQL only")
		os.Exit(1)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	postgresStore, err := store.NewPostgresStore(connectCtx, cfg.DatabaseURL)
	connectCancel()
	if err != nil {
		logger.Error("failed to connect postgres", "error", err)
		os.Exit(1)
	}
	defer postgresStore.Close()
	repo := store.Store(postgresStore)
	settings, found, err := repo.GetPlatformSettings()
	if err != nil {
		logger.Error("failed to load platform settings", "error", err)
		os.Exit(1)
	}
	if !found {
		settings, err = repo.UpsertPlatformSettings(store.PlatformSettingsInput{ImageRegistry: cfg.ImageRegistry, AgentNamespace: cfg.AgentNamespace, VeleroVersion: cfg.VeleroVersion, PublicEndpoint: cfg.PublicBaseURL})
		if err != nil {
			logger.Error("failed to initialize platform settings", "error", err)
			os.Exit(1)
		}
	}
	// Persisted settings are authoritative after first installation. Environment
	// variables seed the row only once and cannot silently replace it on restart.
	cfg.ImageRegistry = settings.ImageRegistry
	cfg.AgentNamespace = settings.AgentNamespace
	cfg.VeleroVersion = settings.VeleroVersion
	if strings.TrimSpace(settings.PublicEndpoint) != "" {
		cfg.PublicBaseURL = settings.PublicEndpoint
	}
	logger.Info("using postgres repository", "addr", cfg.HTTPAddr)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.NewRouter(cfg, logger, repo),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled {
			if strings.TrimSpace(cfg.TLSCertFile) == "" || strings.TrimSpace(cfg.TLSKeyFile) == "" {
				errCh <- errors.New("HCDR_TLS_CERT_FILE and HCDR_TLS_KEY_FILE are required when HCDR_TLS_ENABLED=true")
				return
			}
			logger.Info("starting platform backend with TLS", "addr", cfg.HTTPAddr, "cert", cfg.TLSCertFile)
			errCh <- server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		logger.Info("starting platform backend", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("platform backend stopped")
}
