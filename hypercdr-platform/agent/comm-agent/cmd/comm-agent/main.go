package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/executor"
	"hypercdr-platform/agent/comm-agent/internal/inventory"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/wsclient"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       cfg.LogLevel,
		ReplaceAttr: utcLogTime,
	}))

	logger.Info("comm-agent starting",
		"endpoint", cfg.PlatformEndpoint,
		"namespace", cfg.Namespace,
	)

	var applier kube.ManifestApplier
	var uninstaller kube.Uninstaller
	if strings.EqualFold(cfg.ExecutorMode, executor.ModeKubernetes) {
		dynamicApplier, err := kube.NewDynamicManifestApplier(cfg.KubeconfigPath)
		if err != nil {
			logger.Error("failed to initialize kubernetes manifest applier", "error", err)
			os.Exit(1)
		}
		applier = dynamicApplier
		logger.Info("kubernetes manifest applier initialized")
		kubeUninstaller, err := kube.NewKubernetesUninstaller(cfg.KubeconfigPath)
		if err != nil {
			logger.Error("failed to initialize kubernetes uninstaller", "error", err)
			os.Exit(1)
		}
		uninstaller = kubeUninstaller
	}

	var collector inventory.Collector
	if strings.EqualFold(cfg.InventoryMode, "kubernetes") {
		reader, err := kube.NewKubernetesClusterReader(cfg.KubeconfigPath, cfg.Namespace)
		if err != nil {
			logger.Error("failed to initialize kubernetes inventory reader", "error", err)
			os.Exit(1)
		}
		collector = inventory.NewKubernetesCollector(cfg, reader)
		logger.Info("kubernetes inventory collector initialized")
	}

	var credentialStore kube.CredentialStore
	if cfg.CredentialSecretEnabled {
		store, err := kube.NewSecretCredentialStore(cfg.KubeconfigPath, cfg.Namespace, cfg.CredentialSecretName)
		if err != nil {
			logger.Error("failed to initialize credential secret store", "error", err)
			os.Exit(1)
		}
		credentialStore = store
		if saved, ok, err := credentialStore.Load(context.Background()); err != nil {
			logger.Error("failed to load agent credential from secret", "error", err)
			os.Exit(1)
		} else if ok {
			cfg.ClusterID = saved.ClusterID
			cfg.AgentCredential = saved.Credential
			cfg.InstallToken = ""
			logger.Info("loaded agent credential from secret", "cluster_id", cfg.ClusterID)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for {
		client := wsclient.NewWithRuntimeDependencies(cfg, logger, applier, collector, uninstaller)
		accepted, err := client.Register()
		if err != nil {
			logger.Error("registration failed; retrying", "error", err)
			if !waitForRetry(ctx, logger) {
				return
			}
			continue
		}

		logger.Info("registration accepted",
			"tenant_id", accepted.TenantID,
			"cluster_id", accepted.ClusterID,
			"cluster_name", accepted.ClusterName,
			"heartbeat_interval_seconds", accepted.HeartbeatIntervalSeconds,
			"inventory_resync_interval_seconds", accepted.InventoryResyncIntervalSeconds,
		)
		cfg.ClusterID = accepted.ClusterID
		cfg.AgentCredential = accepted.AgentCredential
		cfg.InstallToken = ""
		if credentialStore != nil {
			if err := credentialStore.Save(context.Background(), kube.AgentCredential{
				ClusterID:  accepted.ClusterID,
				Credential: accepted.AgentCredential,
			}); err != nil {
				logger.Error("failed to persist agent credential", "error", err)
				os.Exit(1)
			}
			logger.Info("agent credential persisted", "secret", cfg.CredentialSecretName)
		}

		if err := client.RunHeartbeat(); err != nil {
			logger.Error("heartbeat loop failed; reconnecting", "error", err)
			if !waitForRetry(ctx, logger) {
				return
			}
			continue
		}
		return
	}
}

func utcLogTime(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC())
	}
	return attr
}

func waitForRetry(ctx context.Context, logger *slog.Logger) bool {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		logger.Info("stop signal received")
		return false
	case <-timer.C:
		return true
	}
}
