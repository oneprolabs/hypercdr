package config

import (
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr                      string
	TLSEnabled                    bool
	TLSCertFile                   string
	TLSKeyFile                    string
	DatabaseURL                   string
	BaseURL                       string
	PublicBaseURL                 string
	AgentWSEndpoint               string
	AgentPrivateWSEndpoint        string
	AgentPublicWSEndpoint         string
	AgentImage                    string
	ImageRegistry                 string
	AgentNamespace                string
	VeleroVersion                 string
	VeleroImage                   string
	VeleroAWSPlugin               string
	VeleroAzurePlugin             string
	VeleroGCPPlugin               string
	RegistryCAPath                string
	FrontendDir                   string
	SecretKey                     string
	ReleaseToken                  string
	GoogleClientID                string
	GoogleClientSecret            string
	PasswordResetRevealToken      bool
	SMTPHost                      string
	SMTPPort                      string
	SMTPUsername                  string
	SMTPPassword                  string
	SMTPFrom                      string
	LogLevel                      slog.Level
	DeployMode                    string
	DeployDir                     string
	UpgraderEndpoint              string
	RegistrationExecutorEndpoint  string
	RegistrationExecutorToken     string
	RegistrationSessionDir        string
	RegistrationExecutorImage     string
	RegistrationExecutorNamespace string
	RegistrationSessionPVC        string
	RegistrationConfigSecret      string
	RegistrationKubernetesAPI     string
	RegistrationServiceTokenPath  string
	RegistrationServiceCAPath     string
	RegistrationPlatformInstance  string
	ReleaseCenterURL              string
	ReleaseCenterToken            string
	ReleaseCenterCAFile           string
	ReleaseCenterSyncInterval     string
	AuthChallengeMode             string
	TurnstileSiteKey              string
	TurnstileSecretKey            string
	TurnstileVerifyURL            string
}

func Load() Config {
	imageRegistry := strings.TrimRight(getEnv("HCDR_IMAGE_REGISTRY", ""), "/")
	return Config{
		HTTPAddr:                      getEnv("HCDR_HTTP_ADDR", ":8080"),
		TLSEnabled:                    parseBool("HCDR_TLS_ENABLED", false),
		TLSCertFile:                   os.Getenv("HCDR_TLS_CERT_FILE"),
		TLSKeyFile:                    os.Getenv("HCDR_TLS_KEY_FILE"),
		DatabaseURL:                   os.Getenv("HCDR_DATABASE_URL"),
		BaseURL:                       strings.TrimRight(getEnv("HCDR_BASE_URL", ""), "/"),
		PublicBaseURL:                 strings.TrimRight(getEnv("HCDR_PUBLIC_BASE_URL", ""), "/"),
		AgentWSEndpoint:               getEnv("HCDR_AGENT_WS_ENDPOINT", ""),
		AgentPrivateWSEndpoint:        getEnv("HCDR_AGENT_PRIVATE_WS_ENDPOINT", ""),
		AgentPublicWSEndpoint:         getEnv("HCDR_AGENT_PUBLIC_WS_ENDPOINT", ""),
		AgentImage:                    getEnv("HCDR_AGENT_IMAGE", defaultImage(imageRegistry, "comm-agent:dev")),
		ImageRegistry:                 imageRegistry,
		AgentNamespace:                getEnv("HCDR_AGENT_NAMESPACE", "hypercdr-agent"),
		VeleroVersion:                 getEnv("HCDR_VELERO_VERSION", "v1.18.2"),
		VeleroImage:                   getEnv("HCDR_VELERO_IMAGE", defaultImage(imageRegistry, "velero:v1.18.2-hcdr.4")),
		VeleroAWSPlugin:               getEnv("HCDR_VELERO_AWS_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-aws:v1.13.0")),
		VeleroAzurePlugin:             getEnv("HCDR_VELERO_AZURE_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-microsoft-azure:v1.13.0")),
		VeleroGCPPlugin:               getEnv("HCDR_VELERO_GCP_PLUGIN_IMAGE", defaultImage(imageRegistry, "velero-plugin-for-gcp:v1.13.0")),
		RegistryCAPath:                getEnv("HCDR_REGISTRY_CA_PATH", ""),
		FrontendDir:                   os.Getenv("HCDR_FRONTEND_DIR"),
		SecretKey:                     os.Getenv("HCDR_SECRET_KEY"),
		ReleaseToken:                  strings.TrimSpace(os.Getenv("HCDR_RELEASE_TOKEN")),
		GoogleClientID:                os.Getenv("HCDR_GOOGLE_CLIENT_ID"),
		GoogleClientSecret:            os.Getenv("HCDR_GOOGLE_CLIENT_SECRET"),
		PasswordResetRevealToken:      parseBool("HCDR_PASSWORD_RESET_REVEAL_TOKEN", false),
		SMTPHost:                      os.Getenv("HCDR_SMTP_HOST"),
		SMTPPort:                      getEnv("HCDR_SMTP_PORT", "587"),
		SMTPUsername:                  os.Getenv("HCDR_SMTP_USERNAME"),
		SMTPPassword:                  os.Getenv("HCDR_SMTP_PASSWORD"),
		SMTPFrom:                      getEnv("HCDR_SMTP_FROM", "HyperCDR <noreply@localhost>"),
		LogLevel:                      parseLogLevel(getEnv("HCDR_LOG_LEVEL", "info")),
		DeployMode:                    getEnv("HCDR_DEPLOY_MODE", "development"),
		DeployDir:                     getEnv("HCDR_DEPLOY_DIR", "/var/lib/hypercdr"),
		UpgraderEndpoint:              strings.TrimRight(getEnv("HCDR_UPGRADER_ENDPOINT", "http://127.0.0.1:18081"), "/"),
		RegistrationExecutorEndpoint:  strings.TrimRight(getEnv("HCDR_REGISTRATION_EXECUTOR_ENDPOINT", "http://127.0.0.1:18082"), "/"),
		RegistrationExecutorToken:     strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_EXECUTOR_TOKEN")),
		RegistrationSessionDir:        getEnv("HCDR_REGISTRATION_SESSION_DIR", "/var/lib/hypercdr/registration-sessions"),
		RegistrationExecutorImage:     strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_EXECUTOR_IMAGE")),
		RegistrationExecutorNamespace: getEnv("HCDR_REGISTRATION_EXECUTOR_NAMESPACE", "default"),
		RegistrationSessionPVC:        getEnv("HCDR_REGISTRATION_SESSION_PVC", "hypercdr-registration-sessions"),
		RegistrationConfigSecret:      strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_CONFIG_SECRET")),
		RegistrationKubernetesAPI:     strings.TrimRight(strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_KUBERNETES_API")), "/"),
		RegistrationServiceTokenPath:  getEnv("HCDR_REGISTRATION_SERVICE_TOKEN_PATH", "/var/run/secrets/kubernetes.io/serviceaccount/token"),
		RegistrationServiceCAPath:     getEnv("HCDR_REGISTRATION_SERVICE_CA_PATH", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"),
		RegistrationPlatformInstance:  strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_PLATFORM_INSTANCE")),
		ReleaseCenterURL:              strings.TrimRight(os.Getenv("HCDR_RELEASE_CENTER_URL"), "/"),
		ReleaseCenterToken:            strings.TrimSpace(os.Getenv("HCDR_RELEASE_CENTER_TOKEN")),
		ReleaseCenterCAFile:           os.Getenv("HCDR_RELEASE_CENTER_CA_FILE"),
		ReleaseCenterSyncInterval:     getEnv("HCDR_RELEASE_CENTER_SYNC_INTERVAL", "3600"),
		AuthChallengeMode:             getEnv("HCDR_AUTH_CHALLENGE_MODE", "image"),
		TurnstileSiteKey:              strings.TrimSpace(os.Getenv("HCDR_TURNSTILE_SITE_KEY")),
		TurnstileSecretKey:            strings.TrimSpace(os.Getenv("HCDR_TURNSTILE_SECRET_KEY")),
		TurnstileVerifyURL:            getEnv("HCDR_TURNSTILE_VERIFY_URL", "https://challenges.cloudflare.com/turnstile/v0/siteverify"),
	}
}

func defaultImage(registry string, image string) string {
	registry = strings.TrimRight(strings.TrimSpace(registry), "/")
	if registry == "" {
		return ""
	}
	parts := strings.Split(registry, "/")
	if len(parts) == 3 && (parts[0] == "docker.io" || strings.HasSuffix(parts[0], ".aliyuncs.com")) {
		return registry + ":" + strings.Replace(image, ":", "-", 1)
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
