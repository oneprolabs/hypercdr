package config

import (
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr                 string
	TLSEnabled               bool
	TLSCertFile              string
	TLSKeyFile               string
	DatabaseURL              string
	PublicBaseURL            string
	AgentWSEndpoint          string
	AgentImage               string
	ImageRegistry            string
	AgentNamespace           string
	VeleroVersion            string
	VeleroImage              string
	VeleroAWSPlugin          string
	VeleroAzurePlugin        string
	VeleroGCPPlugin          string
	RegistryCAPath           string
	FrontendDir              string
	SecretKey                string
	ReleaseToken             string
	GoogleClientID           string
	GoogleClientSecret       string
	PasswordResetRevealToken bool
	SMTPHost                 string
	SMTPPort                 string
	SMTPUsername             string
	SMTPPassword             string
	SMTPFrom                 string
	LogLevel                 slog.Level
	DeployMode               string
	DeployDir                string
	UpgraderEndpoint         string
}

func Load() Config {
	imageRegistry := strings.TrimRight(getEnv("HCDR_IMAGE_REGISTRY", ""), "/")
	return Config{
		HTTPAddr:                 getEnv("HCDR_HTTP_ADDR", ":8080"),
		TLSEnabled:               parseBool("HCDR_TLS_ENABLED", false),
		TLSCertFile:              os.Getenv("HCDR_TLS_CERT_FILE"),
		TLSKeyFile:               os.Getenv("HCDR_TLS_KEY_FILE"),
		DatabaseURL:              os.Getenv("HCDR_DATABASE_URL"),
		PublicBaseURL:            strings.TrimRight(getEnv("HCDR_PUBLIC_BASE_URL", ""), "/"),
		AgentWSEndpoint:          getEnv("HCDR_AGENT_WS_ENDPOINT", ""),
		AgentImage:               getEnv("HCDR_AGENT_IMAGE", defaultImage(imageRegistry, "comm-agent:dev")),
		ImageRegistry:            imageRegistry,
		AgentNamespace:           getEnv("HCDR_AGENT_NAMESPACE", "hypercdr-agent"),
		VeleroVersion:            getEnv("HCDR_VELERO_VERSION", "v1.17.1"),
		VeleroImage:              getEnv("HCDR_VELERO_IMAGE", defaultImage(imageRegistry, "velero:v1.17.1-hcdr.1-20260716")),
		VeleroAWSPlugin:          getEnv("HCDR_VELERO_AWS_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-aws:v1.13.0")),
		VeleroAzurePlugin:        getEnv("HCDR_VELERO_AZURE_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-microsoft-azure:v1.13.0")),
		VeleroGCPPlugin:          getEnv("HCDR_VELERO_GCP_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-gcp:v1.13.0")),
		RegistryCAPath:           getEnv("HCDR_REGISTRY_CA_PATH", ""),
		FrontendDir:              os.Getenv("HCDR_FRONTEND_DIR"),
		SecretKey:                os.Getenv("HCDR_SECRET_KEY"),
		ReleaseToken:             strings.TrimSpace(os.Getenv("HCDR_RELEASE_TOKEN")),
		GoogleClientID:           os.Getenv("HCDR_GOOGLE_CLIENT_ID"),
		GoogleClientSecret:       os.Getenv("HCDR_GOOGLE_CLIENT_SECRET"),
		PasswordResetRevealToken: parseBool("HCDR_PASSWORD_RESET_REVEAL_TOKEN", false),
		SMTPHost:                 os.Getenv("HCDR_SMTP_HOST"),
		SMTPPort:                 getEnv("HCDR_SMTP_PORT", "587"),
		SMTPUsername:             os.Getenv("HCDR_SMTP_USERNAME"),
		SMTPPassword:             os.Getenv("HCDR_SMTP_PASSWORD"),
		SMTPFrom:                 getEnv("HCDR_SMTP_FROM", "HyperCDR <noreply@localhost>"),
		LogLevel:                 parseLogLevel(getEnv("HCDR_LOG_LEVEL", "info")),
		DeployMode:               getEnv("HCDR_DEPLOY_MODE", "development"),
		DeployDir:                getEnv("HCDR_DEPLOY_DIR", "/var/lib/hypercdr"),
		UpgraderEndpoint:         strings.TrimRight(getEnv("HCDR_UPGRADER_ENDPOINT", "http://127.0.0.1:18081"), "/"),
	}
}

func defaultImage(registry string, image string) string {
	registry = strings.TrimRight(strings.TrimSpace(registry), "/")
	if registry == "" {
		return ""
	}
	return registry + "/" + image
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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

func parseBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
