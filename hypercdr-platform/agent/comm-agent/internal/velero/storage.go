package velero

import (
	"fmt"
	"strings"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type BackupStorageLocationManifest struct {
	APIVersion string                            `json:"apiVersion"`
	Kind       string                            `json:"kind"`
	Metadata   ManifestMetadata                  `json:"metadata"`
	Spec       BackupStorageLocationManifestSpec `json:"spec"`
}

type SecretManifest struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   ManifestMetadata  `json:"metadata"`
	Type       string            `json:"type"`
	StringData map[string]string `json:"stringData"`
}

type StorageManifests struct {
	Secret                *SecretManifest
	BackupStorageLocation BackupStorageLocationManifest
}

type BackupStorageLocationManifestSpec struct {
	Provider            string            `json:"provider"`
	ObjectStorage       ObjectStorageSpec `json:"objectStorage"`
	Config              map[string]string `json:"config,omitempty"`
	Credential          *CredentialRef    `json:"credential,omitempty"`
	Default             bool              `json:"default,omitempty"`
	ValidationFrequency string            `json:"validationFrequency,omitempty"`
	AccessMode          string            `json:"accessMode,omitempty"`
}

type ObjectStorageSpec struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix,omitempty"`
}

type CredentialRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type StorageBuildInput struct {
	TaskID         string
	CommandID      string
	AgentNamespace string
	Command        protocol.StorageSyncCommand
}

func BuildBackupStorageLocationManifest(input StorageBuildInput) (BackupStorageLocationManifest, error) {
	if input.Command.Name == "" {
		return BackupStorageLocationManifest{}, fmt.Errorf("storage repository name is required")
	}
	if input.Command.Bucket == "" {
		return BackupStorageLocationManifest{}, fmt.Errorf("storage repository bucket is required")
	}
	agentNamespace := input.AgentNamespace
	if agentNamespace == "" {
		agentNamespace = "hypercdr-agent"
	}

	config := map[string]string{}
	typeName := strings.ToLower(strings.TrimSpace(input.Command.Type))
	isAzure := typeName == "azure"
	isGCS := typeName == "gcp" || typeName == "gcs" || typeName == "google cloud"
	if input.Command.Endpoint != "" && !isAzure && !isGCS {
		config["s3Url"] = normalizeS3URL(input.Command.Endpoint, input.Command.TLSEnabled)
	}
	region := normalizedS3Region(input.Command.Type, input.Command.Region)
	if region != "" {
		config["region"] = region
	}
	if !input.Command.TLSEnabled {
		if !isAzure && !isGCS {
			config["insecureSkipTLSVerify"] = "true"
		}
	}
	if isAzure && input.Command.Credentials != nil && input.Command.Credentials.AccountName != "" {
		config["storageAccount"] = input.Command.Credentials.AccountName
	}
	for key, value := range input.Command.Config {
		if text, ok := value.(string); ok && text != "" {
			if mapped, handled, drop, override := mapUIStorageConfig(key, text); handled {
				if drop {
					continue
				}
				valueToWrite := text
				if override != "" {
					valueToWrite = override
				}
				config[mapped] = valueToWrite
				continue
			}
			if !isVeleroBSLConfigKey(key) {
				continue
			}
			config[key] = text
		}
	}

	manifest := BackupStorageLocationManifest{
		APIVersion: "velero.io/v1",
		Kind:       "BackupStorageLocation",
		Metadata: ManifestMetadata{
			Name:      sanitizeName(input.Command.Name),
			Namespace: agentNamespace,
			Labels: map[string]string{
				"hypercdr.io/task-id":       input.TaskID,
				"hypercdr.io/command-id":    input.CommandID,
				"hypercdr.io/repository-id": input.Command.RepositoryID,
			},
		},
		Spec: BackupStorageLocationManifestSpec{
			Provider: providerName(input.Command.Type),
			ObjectStorage: ObjectStorageSpec{
				Bucket: input.Command.Bucket,
			},
			Config:              config,
			ValidationFrequency: "1m",
			AccessMode:          "ReadWrite",
		},
	}
	if input.Command.SecretRef != "" {
		manifest.Spec.Credential = &CredentialRef{
			Name: input.Command.SecretRef,
			Key:  "cloud",
		}
	}
	if prefix, ok := input.Command.Config["prefix"].(string); ok && prefix != "" {
		manifest.Spec.ObjectStorage.Prefix = prefix
	}
	return manifest, nil
}

func normalizedS3Region(storageType, region string) string {
	region = strings.TrimSpace(region)
	switch strings.ToLower(region) {
	case "n/a", "na", "-":
		region = ""
	}
	typeName := strings.ToLower(strings.TrimSpace(storageType))
	if region == "" && (typeName == "s3" || typeName == "s3-compatible" || typeName == "s3 compatible") {
		return "us-east-1"
	}
	return region
}

func normalizeS3URL(endpoint string, tlsEnabled bool) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || strings.Contains(endpoint, "://") {
		return endpoint
	}
	if tlsEnabled {
		return "https://" + endpoint
	}
	return "http://" + endpoint
}

func BuildStorageManifests(input StorageBuildInput) (StorageManifests, error) {
	bsl, err := BuildBackupStorageLocationManifest(input)
	if err != nil {
		return StorageManifests{}, err
	}
	manifests := StorageManifests{BackupStorageLocation: bsl}
	if input.Command.Credentials == nil || input.Command.SecretRef == "" {
		return manifests, nil
	}
	cloudCredentials := awsCredentialsFile(input.Command.Credentials.AccessKey, input.Command.Credentials.SecretKey)
	typeName := strings.ToLower(strings.TrimSpace(input.Command.Type))
	if typeName == "azure" {
		cloudCredentials = azureCredentialsFile(input.Command.Credentials.AccountKey)
	}
	if typeName == "gcp" || typeName == "gcs" || typeName == "google cloud" {
		cloudCredentials = strings.TrimSpace(input.Command.Credentials.ServiceAccountKey)
	}
	if cloudCredentials == "" {
		return manifests, nil
	}
	manifests.Secret = &SecretManifest{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata: ManifestMetadata{
			Name:      input.Command.SecretRef,
			Namespace: bsl.Metadata.Namespace,
			Labels: map[string]string{
				"hypercdr.io/task-id":       input.TaskID,
				"hypercdr.io/command-id":    input.CommandID,
				"hypercdr.io/repository-id": input.Command.RepositoryID,
			},
		},
		Type: "Opaque",
		StringData: map[string]string{
			"cloud": cloudCredentials,
		},
	}
	return manifests, nil
}

func azureCredentialsFile(accountKey string) string {
	if strings.TrimSpace(accountKey) == "" {
		return ""
	}
	return "AZURE_STORAGE_ACCOUNT_ACCESS_KEY=" + strings.TrimSpace(accountKey) + "\n"
}

func awsCredentialsFile(accessKey string, secretKey string) string {
	if accessKey == "" && secretKey == "" {
		return ""
	}
	return fmt.Sprintf("[default]\naws_access_key_id=%s\naws_secret_access_key=%s\n", accessKey, secretKey)
}

func providerName(value string) string {
	switch value {
	case "aws", "AWS", "s3", "S3", "S3-Compatible", "S3 Compatible", "s3-compatible", "s3 compatible":
		return "aws"
	case "azure", "Azure":
		return "azure"
	case "gcp", "GCP", "gcs", "GCS", "Google Cloud", "google cloud":
		return "gcp"
	default:
		if value == "" {
			return "aws"
		}
		return value
	}
}

// mapUIStorageConfig translates HyperCDR storage-repository UI config fields
// into Velero BackupStorageLocation spec.config entries. The first result is
// the mapped key (or the original key if unchanged), the second indicates the
// key was recognised, and the third (drop) tells the caller not to write the
// key into the Velero BSL config at all.
func mapUIStorageConfig(key string, value string) (string, bool, bool, string) {
	switch key {
	case "prefix":
		return "", true, true, ""
	case "urlStyle", "url_style":
		switch value {
		case "path", "path-style", "virtualHost", "virtual-hosted":
			return "s3ForcePathStyle", true, false, "true"
		}
		return "", true, true, ""
	case "bucket", "caCert":
		return "", true, true, ""
	}
	return "", false, false, ""
}

// isVeleroBSLConfigKey reports whether key is a known Velero AWS-plugin
// BackupStorageLocation spec.config field. Anything not on this list (UI-only
// flags) is dropped to avoid Velero validation errors.
func isVeleroBSLConfigKey(key string) bool {
	switch key {
	case "region", "s3Url", "publicUrl", "kmsKeyId",
		"customerKeyEncryptionFile", "customerKeyEncryptionSecret",
		"s3ForcePathStyle", "signatureVersion", "credentialsFile",
		"profile", "serverSideEncryption", "insecureSkipTLSVerify",
		"enableSharedConfig", "tagging", "checksumAlgorithm", "caCert":
		return true
	}
	return false
}
