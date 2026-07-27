package config

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	PlatformEndpoint        string
	PlatformTLSSkipVerify   bool
	StateDir                string
	InstallToken            string
	AgentCredential         string
	ClusterID               string
	ClusterName             string
	AgentID                 string
	AgentVersion            string
	AgentImage              string
	Namespace               string
	PodName                 string
	ExecutorMode            string
	InventoryMode           string
	KubeconfigPath          string
	CredentialSecretEnabled bool
	CredentialSecretName    string
	HeartbeatInterval       time.Duration
	InventoryInterval       time.Duration
	LogLevel                slog.Level
}

func Load() Config {
	return Config{
		PlatformEndpoint:        getEnv("HCDR_PLATFORM_ENDPOINT", "ws://127.0.0.1:8080/ws/agent"),
		PlatformTLSSkipVerify:   parseBool("HCDR_PLATFORM_TLS_INSECURE_SKIP_VERIFY", false),
		StateDir:                getEnv("HCDR_AGENT_STATE_DIR", "/var/lib/hypercdr-agent"),
		InstallToken:            os.Getenv("HCDR_INSTALL_TOKEN"),
		AgentCredential:         os.Getenv("HCDR_AGENT_CREDENTIAL"),
		ClusterID:               os.Getenv("HCDR_CLUSTER_ID"),
		ClusterName:             getEnv("HCDR_CLUSTER_NAME", "unknown-cluster"),
		AgentID:                 getEnv("HCDR_AGENT_ID", getEnv("HOSTNAME", "comm-agent-local")),
		AgentVersion:            getEnv("HCDR_AGENT_VERSION", "v0.1.0-dev"),
		AgentImage:              getEnv("HCDR_AGENT_IMAGE", ""),
		Namespace:               getEnv("HCDR_AGENT_NAMESPACE", "hypercdr-agent"),
		PodName:                 getEnv("HCDR_POD_NAME", getEnv("HOSTNAME", "comm-agent-local")),
		ExecutorMode:            getEnv("HCDR_EXECUTOR_MODE", "dry-run"),
		InventoryMode:           getEnv("HCDR_INVENTORY_MODE", "static"),
		KubeconfigPath:          os.Getenv("HCDR_KUBECONFIG"),
		CredentialSecretEnabled: parseBool("HCDR_CREDENTIAL_SECRET_ENABLED", false),
		CredentialSecretName:    getEnv("HCDR_CREDENTIAL_SECRET_NAME", "hypercdr-agent-credential"),
		HeartbeatInterval:       parseDuration("HCDR_HEARTBEAT_INTERVAL", 30*time.Second),
		InventoryInterval:       parseDuration("HCDR_INVENTORY_INTERVAL", 5*time.Minute),
		LogLevel:                parseLogLevel(getEnv("HCDR_LOG_LEVEL", "info")),
	}
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
