package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const DefaultTenantID = "00000000-0000-0000-0000-000000000001"

const DefaultAdminEmail = "admin"
const DefaultAdminPassword = "admin123"

const clusterConnectionStaleAfter = 10 * time.Minute

var (
	ErrTokenInvalid            = errors.New("install token is invalid")
	ErrTokenExpired            = errors.New("install token is expired")
	ErrTokenUsed               = errors.New("install token is already used")
	ErrUserExists              = errors.New("user already exists")
	ErrResetInvalid            = errors.New("password reset token is invalid or expired")
	ErrEmailSettingsNameExists = errors.New("SMTP configuration name already exists")
)

type ApplicationAlreadyProtectedError struct {
	ProtectionPlanID string
	ApplicationID    string
}

func (e *ApplicationAlreadyProtectedError) Error() string {
	return "application already belongs to protection plan " + e.ProtectionPlanID
}

type Store interface {
	ListTenants() ([]Tenant, error)
	GetTenant(id string) (Tenant, bool, error)
	CreateTenant(input TenantInput) (Tenant, error)
	UpdateTenant(id string, input TenantInput) (Tenant, bool, error)
	DeleteTenant(id string) (bool, bool, error)
	GetEmailSettings() (EmailSettings, bool, error)
	UpsertEmailSettings(input EmailSettingsInput) (EmailSettings, error)
	ListEmailSettings() ([]EmailSettings, error)
	GetEmailSettingsByID(id string) (EmailSettings, bool, error)
	CreateEmailSettings(input EmailSettingsInput) (EmailSettings, error)
	UpdateEmailSettings(id string, input EmailSettingsInput) (EmailSettings, bool, error)
	DeleteEmailSettings(id string) (bool, bool, error)
	SetDefaultEmailSettings(id string) (EmailSettings, bool, error)
	UpdateEmailSettingsTestResult(id, status, message string, testedAt time.Time) error
	GetPlatformSettings() (PlatformSettings, bool, error)
	UpsertPlatformSettings(input PlatformSettingsInput) (PlatformSettings, error)
	AuthenticateUser(input UserAuthInput) (User, bool, error)
	CreateUser(tenantID string, email string, password string) (User, error)
	ListUsers() ([]User, error)
	GetUser(id string) (User, bool, error)
	UpdateUser(input UserUpdateInput) (User, bool, error)
	DeleteUser(id string) (bool, error)
	SetUserPassword(id string, password string, mustChangePassword bool) (User, bool, error)
	GetAdminRecoveryEmail(userID string) (string, bool, error)
	SetAdminRecoveryEmail(userID, email string) (string, bool, error)
	CreatePlatformSession(userID string, ttl time.Duration) (PlatformSession, error)
	AuthenticatePlatformSession(token string) (User, bool, error)
	DeletePlatformSession(token string) error
	CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error)
	ResetPassword(token string, password string) (User, error)
	FindOrCreateGoogleUser(email string) (User, error)
	CreateAgentToken(tenantID, createdBy, description string, ttl time.Duration) (AgentToken, error)
	ValidateAgentToken(token string) error
	RegisterCluster(input RegisterClusterInput) (Cluster, string, error)
	AuthenticateAgentCredential(input AgentCredentialInput) (Cluster, bool, error)
	ListClusters() ([]Cluster, error)
	UpdateCluster(input ClusterUpdateInput) (Cluster, bool, error)
	SetClusterConnectionStatus(clusterID string, status string) (Cluster, bool, error)
	SetDefaultCluster(clusterID string) (Cluster, bool, error)
	DeleteCluster(clusterID string) (bool, error)
	ListApplications(clusterID string) ([]Application, error)
	UpdateApplication(input ApplicationUpdateInput) (Application, bool, error)
	ListTags() ([]Tag, error)
	CreateTag(tenantID, name string) (Tag, error)
	UpdateTag(id, name string) (Tag, bool, error)
	DeleteTag(id string) (bool, error)
	SetApplicationTags(applicationID string, tagIDs []string) (Application, bool, error)
	ApplyInventory(input InventoryInput) (Cluster, bool, error)
	UpdateHeartbeat(input HeartbeatInput) (Cluster, bool, error)
	CreateStorageRepository(input StorageRepositoryInput) (StorageRepository, error)
	UpdateStorageRepository(id string, input StorageRepositoryInput) (StorageRepository, bool, error)
	DeleteStorageRepository(id string) (bool, bool, error)
	ListStorageRepositories() ([]StorageRepository, error)
	GetStorageRepository(id string) (StorageRepository, bool, error)
	SetStorageRepositoryStatus(id string, status string, lastValidatedAt time.Time) (StorageRepository, bool, error)
	UpsertClusterStorageBinding(input ClusterStorageBindingInput) (ClusterStorageBinding, error)
	GetClusterStorageBinding(clusterID string, storageRepoID string, sourceClusterID string) (ClusterStorageBinding, bool, error)
	UpdateClusterStorageBindingStatus(input ClusterStorageBindingStatusInput) (ClusterStorageBinding, bool, error)
	CreatePolicy(input PolicyInput) (Policy, error)
	UpdatePolicy(id string, input PolicyInput) (Policy, bool, error)
	DeletePolicy(id string) (bool, bool, error)
	ListPolicies() ([]Policy, error)
	CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error)
	ListProtectionPlans(clusterID string) ([]ProtectionPlan, error)
	GetProtectionPlan(id string) (ProtectionPlan, bool, error)
	UpdateProtectionPlanStatus(id string, status string) (ProtectionPlan, bool, error)
	UpdateProtectionPlanStorageSize(id string, size map[string]any) (ProtectionPlan, bool, error)
	UpsertProtectionPlanSchedule(input ProtectionPlanScheduleInput) (ProtectionPlanSchedule, error)
	GetProtectionPlanSchedule(planID string) (ProtectionPlanSchedule, bool, error)
	ListDueProtectionPlanSchedules(now time.Time) ([]ProtectionPlanSchedule, error)
	MarkProtectionPlanScheduleFired(input ProtectionPlanScheduleFiredInput) (ProtectionPlanSchedule, bool, error)
	DisableProtectionPlanSchedule(planID string) error
	DeleteProtectionPlan(id string) (ProtectionPlan, bool, error)
	CleanupProtectionPlanRecords(id string) (ProtectionPlan, bool, error)
	GetApplication(id string) (Application, bool, error)
	CreateRestorePoint(input RestorePointInput) (RestorePoint, error)
	ListRestorePoints(filter RestorePointFilter) ([]RestorePoint, error)
	GetRestorePoint(id string) (RestorePoint, bool, error)
	UpdateRestorePointState(input RestorePointStateInput) (RestorePoint, bool, error)
	CreateTask(input TaskInput) (Task, error)
	ListTasks(clusterID string) ([]Task, error)
	UpdateTaskStatus(input TaskStatusInput) (Task, bool, error)
	AddTaskEvent(input TaskEventInput) error
	ListTaskEvents(taskID string) ([]TaskEvent, error)
	CreateDiagnosticLog(input DiagnosticLogInput) (DiagnosticLog, error)
	ListDiagnosticLogs(filter DiagnosticLogFilter) ([]DiagnosticLog, error)
	PurgeDiagnosticLogs(before time.Time) (int64, error)
	GetClusterLogCoverage(clusterID string, component string) (ClusterLogCoverage, bool, error)
	UpsertClusterLogCoverage(input ClusterLogCoverageInput) (ClusterLogCoverage, error)
	CreateAuditLog(input AuditLogInput) (AuditLog, error)
	ListAuditLogs(limit, offset int) ([]AuditLog, error)
	ListComponentReleases(component string) ([]ComponentRelease, error)
	GetActiveComponentRelease(component string) (ComponentRelease, bool, error)
	UpsertComponentRelease(input ComponentReleaseInput) (ComponentRelease, error)
	ActivateComponentRelease(id string, publishedBy string) (ComponentRelease, bool, error)
	ListPlatformReleases() ([]PlatformRelease, error)
	UpsertPlatformRelease(input PlatformReleaseInput) (PlatformRelease, error)
	ActivatePlatformRelease(id string, publishedBy string) (PlatformRelease, bool, error)
	ListPlatformUpgradeJobs() ([]PlatformUpgradeJob, error)
	CreatePlatformUpgradeJob(input PlatformUpgradeJobInput) (PlatformUpgradeJob, error)
	UpdatePlatformUpgradeJob(input PlatformUpgradeJobUpdate) (PlatformUpgradeJob, bool, error)
}

type PlatformSettings struct {
	TenantID       string    `json:"tenantId"`
	ImageRegistry  string    `json:"imageRegistry"`
	AgentNamespace string    `json:"agentNamespace"`
	VeleroVersion  string    `json:"veleroVersion"`
	PublicEndpoint string    `json:"publicEndpoint,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PlatformSettingsInput struct {
	ImageRegistry, AgentNamespace, VeleroVersion, PublicEndpoint string
}

type Tenant struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Status       string    `json:"status"`
	UserCount    int       `json:"userCount"`
	ClusterCount int       `json:"clusterCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type TenantInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type EmailSettings struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	IsDefault          bool       `json:"isDefault"`
	Enabled            bool       `json:"enabled"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	Security           string     `json:"security"`
	Username           string     `json:"username"`
	PasswordCiphertext string     `json:"-"`
	PasswordConfigured bool       `json:"passwordConfigured"`
	SenderName         string     `json:"senderName"`
	SenderEmail        string     `json:"senderEmail"`
	LastTestStatus     string     `json:"lastTestStatus"`
	LastTestedAt       *time.Time `json:"lastTestedAt,omitempty"`
	LastTestError      string     `json:"lastTestError,omitempty"`
	CreatedAt          time.Time  `json:"createdAt,omitempty"`
	UpdatedAt          time.Time  `json:"updatedAt,omitempty"`
}
type EmailSettingsInput struct {
	Name               string
	Enabled            bool
	Host               string
	Port               int
	Security           string
	Username           string
	PasswordCiphertext string
	SenderName         string
	SenderEmail        string
	UpdatedBy          string
}

type AuditLog struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenantId"`
	ActorID      string         `json:"actorId,omitempty"`
	Actor        string         `json:"actor"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   string         `json:"resourceId,omitempty"`
	ResourceName string         `json:"resourceName,omitempty"`
	Result       string         `json:"result"`
	Message      string         `json:"message,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type AuditLogInput struct {
	ActorID, Actor, Action, ResourceType, ResourceID, ResourceName, Result, Message string
	Payload                                                                         map[string]any
}

type ComponentRelease struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Component    string    `json:"component"`
	Version      string    `json:"version"`
	Image        string    `json:"image"`
	ImageDigest  string    `json:"imageDigest"`
	Status       string    `json:"status"`
	ReleaseNotes string    `json:"releaseNotes,omitempty"`
	PublishedBy  string    `json:"publishedBy,omitempty"`
	PublishedAt  time.Time `json:"publishedAt,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ComponentReleaseInput struct {
	Component    string
	Version      string
	Image        string
	ImageDigest  string
	Status       string
	ReleaseNotes string
	PublishedBy  string
}

type PlatformRelease struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenantId"`
	Version               string    `json:"version"`
	APIImage              string    `json:"apiImage"`
	APIImageDigest        string    `json:"apiImageDigest"`
	FrontendImage         string    `json:"frontendImage"`
	FrontendImageDigest   string    `json:"frontendImageDigest"`
	DatabaseSchemaVersion string    `json:"databaseSchemaVersion"`
	MinimumAgentVersion   string    `json:"minimumAgentVersion,omitempty"`
	RollbackSupported     bool      `json:"rollbackSupported"`
	ReleaseNotes          string    `json:"releaseNotes,omitempty"`
	Status                string    `json:"status"`
	PublishedBy           string    `json:"publishedBy,omitempty"`
	PublishedAt           time.Time `json:"publishedAt,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type PlatformReleaseInput struct {
	Version, APIImage, APIImageDigest, FrontendImage, FrontendImageDigest, DatabaseSchemaVersion, MinimumAgentVersion, ReleaseNotes, Status, PublishedBy string
	RollbackSupported                                                                                                                                    bool
}

type PlatformUpgradeJob struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenantId"`
	ReleaseID             string    `json:"releaseId"`
	FromVersion           string    `json:"fromVersion"`
	TargetVersion         string    `json:"targetVersion"`
	Status                string    `json:"status"`
	Step                  string    `json:"step"`
	Progress              int       `json:"progress"`
	APIImage              string    `json:"apiImage"`
	APIImageDigest        string    `json:"apiImageDigest"`
	FrontendImage         string    `json:"frontendImage"`
	FrontendImageDigest   string    `json:"frontendImageDigest"`
	DatabaseSchemaVersion string    `json:"databaseSchemaVersion"`
	RollbackSupported     bool      `json:"rollbackSupported"`
	BackupPath            string    `json:"backupPath,omitempty"`
	PreviousAPIImage      string    `json:"previousApiImage,omitempty"`
	PreviousFrontendImage string    `json:"previousFrontendImage,omitempty"`
	ErrorCode             string    `json:"errorCode,omitempty"`
	ErrorMessage          string    `json:"errorMessage,omitempty"`
	RequestedBy           string    `json:"requestedBy,omitempty"`
	ExecutorID            string    `json:"executorId,omitempty"`
	ExecutorHeartbeatAt   time.Time `json:"executorHeartbeatAt,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	StartedAt             time.Time `json:"startedAt,omitempty"`
	CompletedAt           time.Time `json:"completedAt,omitempty"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type PlatformUpgradeJobInput struct {
	Release                                                           PlatformRelease
	FromVersion, PreviousAPIImage, PreviousFrontendImage, RequestedBy string
}
type PlatformUpgradeJobUpdate struct {
	ID, Status, Step, BackupPath, ErrorCode, ErrorMessage, ExecutorID, PreviousAPIImage, PreviousFrontendImage string
	Progress                                                                                                   int
	MarkStarted, MarkDone                                                                                      bool
}

type UserAuthInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type User struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenantId"`
	TenantName         string `json:"tenantName"`
	Email              string `json:"email"`
	DisplayName        string `json:"displayName,omitempty"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	AuthProvider       string `json:"authProvider"`
	TimeZone           string `json:"timeZone,omitempty"`
	SystemAdmin        bool   `json:"systemAdmin,omitempty"`
	MustChangePassword bool   `json:"mustChangePassword"`
	RecoveryEmail      string `json:"recoveryEmail,omitempty"`
}

type UserUpdateInput struct {
	ID, TenantID, Email, DisplayName, Role, Status, TimeZone string
}

type PlatformSession struct {
	Token, UserID string
	ExpiresAt     time.Time
}

type AgentToken struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantId"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	Token       string    `json:"token"`
	Description string    `json:"description,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	UsedAt      time.Time `json:"usedAt,omitempty"`
	ClusterID   string    `json:"clusterId,omitempty"`
}

type Cluster struct {
	ID                         string                `json:"id"`
	TenantID                   string                `json:"tenantId"`
	Name                       string                `json:"name"`
	KubeVersion                string                `json:"kubeVersion"`
	Status                     string                `json:"status"`
	ConnectionStatus           string                `json:"connectionStatus"`
	NodeCount                  int                   `json:"nodeCount"`
	NamespaceCount             int                   `json:"namespaceCount"`
	ApplicationCount           int                   `json:"applicationCount"`
	ActiveTasks                int                   `json:"activeTasks"`
	AgentVersion               string                `json:"agentVersion"`
	AgentImage                 string                `json:"agentImage,omitempty"`
	AgentImageID               string                `json:"agentImageId,omitempty"`
	AgentImageDigest           string                `json:"agentImageDigest,omitempty"`
	LatestAgentVersion         string                `json:"latestAgentVersion,omitempty"`
	LatestAgentImage           string                `json:"latestAgentImage,omitempty"`
	LatestAgentImageDigest     string                `json:"latestAgentImageDigest,omitempty"`
	AgentUpgradeAvailable      bool                  `json:"agentUpgradeAvailable,omitempty"`
	AgentUpgradeStatus         string                `json:"agentUpgradeStatus,omitempty"`
	AgentUpgradeProgress       int                   `json:"agentUpgradeProgress,omitempty"`
	VeleroVersion              string                `json:"veleroVersion,omitempty"`
	VeleroStatus               string                `json:"veleroStatus"`
	VeleroImage                string                `json:"veleroImage,omitempty"`
	VeleroImageDigest          string                `json:"veleroImageDigest,omitempty"`
	VeleroServerReady          bool                  `json:"veleroServerReady,omitempty"`
	VeleroNodeAgentDesired     int32                 `json:"veleroNodeAgentDesired,omitempty"`
	VeleroNodeAgentReady       int32                 `json:"veleroNodeAgentReady,omitempty"`
	VeleroNodeAgentImageDigest string                `json:"veleroNodeAgentImageDigest,omitempty"`
	LatestVeleroVersion        string                `json:"latestVeleroVersion,omitempty"`
	LatestVeleroImage          string                `json:"latestVeleroImage,omitempty"`
	LatestVeleroImageDigest    string                `json:"latestVeleroImageDigest,omitempty"`
	VeleroUpgradeAvailable     bool                  `json:"veleroUpgradeAvailable,omitempty"`
	VeleroUpgradeStatus        string                `json:"veleroUpgradeStatus,omitempty"`
	VeleroUpgradeProgress      int                   `json:"veleroUpgradeProgress,omitempty"`
	InventoryHash              string                `json:"inventoryHash,omitempty"`
	Nodes                      []ClusterNode         `json:"nodes,omitempty"`
	StorageClasses             []ClusterStorageClass `json:"storageClasses,omitempty"`
	RestoreCachePolicy         RestoreCachePolicy    `json:"restoreCachePolicy"`
	APIResources               []ClusterAPIResource  `json:"apiResources,omitempty"`
	NamespaceAPIs              []ClusterNamespaceAPI `json:"namespaceAPIs,omitempty"`
	Capabilities               []ClusterCapability   `json:"capabilities,omitempty"`
	CapabilitiesCollectedAt    time.Time             `json:"capabilitiesCollectedAt,omitempty"`
	CapabilitiesComplete       bool                  `json:"capabilitiesComplete,omitempty"`
	Role                       string                `json:"role"`
	IsDefault                  bool                  `json:"isDefault"`
	RegisteredAt               time.Time             `json:"registeredAt"`
	LastSeenAt                 time.Time             `json:"lastSeenAt"`
}

type RestoreCachePolicy struct {
	Mode                string `json:"mode"`
	Enabled             bool   `json:"enabled"`
	StorageClass        string `json:"storageClass,omitempty"`
	ResidentThresholdMB int    `json:"residentThresholdMB,omitempty"`
	CacheLimitMB        int    `json:"cacheLimitMB,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

func applyClusterConnectionFreshness(cluster *Cluster) {
	if cluster == nil {
		return
	}
	if cluster.ConnectionStatus == "online" && !cluster.LastSeenAt.IsZero() && time.Since(cluster.LastSeenAt) > clusterConnectionStaleAfter {
		cluster.ConnectionStatus = "offline"
	}
}

type ClusterNode struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	Roles          string            `json:"roles,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds,omitempty"`
	KubeletVersion string            `json:"kubeletVersion,omitempty"`
	Capacity       map[string]string `json:"capacity,omitempty"`
}

type ClusterStorageClass struct {
	Name                 string `json:"name"`
	Provisioner          string `json:"provisioner"`
	ReclaimPolicy        string `json:"reclaimPolicy"`
	VolumeBindingMode    string `json:"volumeBindingMode"`
	AllowVolumeExpansion string `json:"allowVolumeExpansion"`
	Default              bool   `json:"default,omitempty"`
	AgeSeconds           int64  `json:"ageSeconds,omitempty"`
}

type ClusterAPIResource struct {
	Group      string `json:"group,omitempty"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

type ClusterNamespaceAPI struct {
	Scope     string `json:"scope,omitempty"`
	Namespace string `json:"namespace"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Count     int    `json:"count"`
}

type ClusterCapability struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Driver string            `json:"driver,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

type ClusterUpdateInput struct {
	ID        string `json:"-"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	IsDefault *bool  `json:"isDefault,omitempty"`
}

type Application struct {
	ID               string            `json:"id"`
	ClusterID        string            `json:"clusterId"`
	Namespace        string            `json:"namespace"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	Labels           map[string]string `json:"labels,omitempty"`
	WorkloadCount    int               `json:"workloadCount"`
	ServiceCount     int               `json:"serviceCount"`
	IngressCount     int               `json:"ingressCount"`
	ConfigMapCount   int               `json:"configMapCount"`
	SecretCount      int               `json:"secretCount"`
	PVCCount         int               `json:"pvcCount"`
	PVCapacityBytes  int64             `json:"pvCapacityBytes"`
	ResourceSummary  map[string]any    `json:"resourceSummary,omitempty"`
	LastCollectedAt  time.Time         `json:"lastCollectedAt"`
	ProtectionStatus string            `json:"protectionStatus"`
	Tags             []string          `json:"tags,omitempty"`
}

type Tag struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ApplicationUpdateInput struct {
	ID               string `json:"-"`
	ProtectionStatus string `json:"protectionStatus"`
}

type RegisterClusterInput struct {
	Token         string
	ClusterName   string
	KubeVersion   string
	AgentVersion  string
	VeleroVersion string
	VeleroStatus  string
}

type AgentCredentialInput struct {
	ClusterID  string
	Credential string
}

type HeartbeatInput struct {
	ClusterID                  string
	Status                     string
	KubeVersion                string
	AgentVersion               string
	AgentImage                 string
	AgentImageID               string
	AgentImageDigest           string
	VeleroStatus               string
	VeleroVersion              string
	VeleroImage                string
	VeleroImageDigest          string
	VeleroServerReady          bool
	VeleroNodeAgentDesired     int32
	VeleroNodeAgentReady       int32
	VeleroNodeAgentImageDigest string
	NodeCount                  int
	NamespaceCount             int
	ApplicationCount           int
	ActiveTasks                int
	InventoryHash              string
}

type InventoryInput struct {
	ClusterID            string
	KubeVersion          string
	VeleroStatus         string
	NodeCount            int
	NamespaceCount       int
	Nodes                []ClusterNode
	StorageClasses       []ClusterStorageClass
	APIResources         []ClusterAPIResource
	NamespaceAPIs        []ClusterNamespaceAPI
	Capabilities         []ClusterCapability
	CapabilityScan       bool
	CapabilityNamespace  string
	CapabilitiesComplete bool
	Apps                 []Application
	CollectedAt          time.Time
	Hash                 string
}

func mergeNamespaceAPIs(existing, scanned []ClusterNamespaceAPI, namespace string) []ClusterNamespaceAPI {
	if strings.TrimSpace(namespace) == "" {
		return scanned
	}
	merged := make([]ClusterNamespaceAPI, 0, len(existing)+len(scanned))
	for _, resource := range existing {
		// Cluster-scoped catalog entries are still relationships discovered for
		// one namespace. Keep other namespaces independent, and discard legacy
		// unowned cluster entries so they cannot leak into every application.
		if resource.Namespace == namespace || (resource.Scope == "cluster" && resource.Namespace == "") {
			continue
		}
		merged = append(merged, resource)
	}
	return append(merged, scanned...)
}

type StorageRepository struct {
	ID              string            `json:"id"`
	TenantID        string            `json:"tenantId"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Endpoint        string            `json:"endpoint,omitempty"`
	Bucket          string            `json:"bucket,omitempty"`
	Region          string            `json:"region,omitempty"`
	TLSEnabled      bool              `json:"tlsEnabled"`
	Status          string            `json:"status"`
	Config          map[string]any    `json:"config,omitempty"`
	SecretRef       string            `json:"secretRef,omitempty"`
	Secret          map[string]string `json:"-"`
	LastValidatedAt time.Time         `json:"lastValidatedAt,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type StorageRepositoryInput struct {
	TenantID          string         `json:"-"`
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	Endpoint          string         `json:"endpoint"`
	Bucket            string         `json:"bucket"`
	Region            string         `json:"region"`
	TLSEnabled        bool           `json:"tlsEnabled"`
	Config            map[string]any `json:"config"`
	SecretRef         string         `json:"secretRef"`
	AccessKey         string         `json:"accessKey"`
	SecretKey         string         `json:"secretKey"`
	AccountName       string         `json:"accountName"`
	AccountKey        string         `json:"accountKey"`
	ServiceAccountKey string         `json:"serviceAccountKey"`
}

type ClusterStorageBinding struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenantId"`
	ClusterID        string    `json:"clusterId"`
	StorageRepoID    string    `json:"storageRepoId"`
	SourceClusterID  string    `json:"sourceClusterId"`
	BSLName          string    `json:"bslName"`
	ObjectPrefix     string    `json:"objectPrefix,omitempty"`
	Status           string    `json:"status"`
	RetryCount       int       `json:"retryCount"`
	LastSyncedAt     time.Time `json:"lastSyncedAt,omitempty"`
	LastSuccessAt    time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorCode    string    `json:"lastErrorCode,omitempty"`
	LastErrorMessage string    `json:"lastErrorMessage,omitempty"`
	RepoUpdatedAt    time.Time `json:"repoUpdatedAt,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ClusterStorageBindingInput struct {
	ClusterID       string
	StorageRepoID   string
	SourceClusterID string
	BSLName         string
	ObjectPrefix    string
	Status          string
	RetryCount      int
	RepoUpdatedAt   time.Time
}

type ClusterStorageBindingStatusInput struct {
	ClusterID        string
	StorageRepoID    string
	SourceClusterID  string
	Status           string
	RetryCount       int
	LastSyncedAt     time.Time
	LastSuccessAt    time.Time
	LastErrorCode    string
	LastErrorMessage string
	RepoUpdatedAt    time.Time
}

type Policy struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantId"`
	Name           string    `json:"name"`
	Composition    string    `json:"composition"`
	ScheduleType   string    `json:"scheduleType"`
	IntervalValue  int       `json:"intervalValue,omitempty"`
	IntervalUnit   string    `json:"intervalUnit,omitempty"`
	Hour           int       `json:"hour,omitempty"`
	Minute         int       `json:"minute,omitempty"`
	WeekDay        int       `json:"weekDay,omitempty"`
	MonthDay       int       `json:"monthDay,omitempty"`
	RetentionCount int       `json:"retentionCount,omitempty"`
	RetentionDays  int       `json:"retentionDays,omitempty"`
	Status         string    `json:"status"`
	BoundCount     int       `json:"boundCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PolicyInput struct {
	TenantID       string `json:"-"`
	Name           string `json:"name"`
	Composition    string `json:"composition"`
	ScheduleType   string `json:"scheduleType"`
	IntervalValue  int    `json:"intervalValue"`
	IntervalUnit   string `json:"intervalUnit"`
	Hour           int    `json:"hour"`
	Minute         int    `json:"minute"`
	WeekDay        int    `json:"weekDay"`
	MonthDay       int    `json:"monthDay"`
	RetentionCount int    `json:"retentionCount"`
	RetentionDays  int    `json:"retentionDays"`
	Status         string `json:"status"`
}

type ProtectionPlan struct {
	ID                   string            `json:"id"`
	TenantID             string            `json:"tenantId"`
	SourceClusterID      string            `json:"sourceClusterId"`
	AppID                string            `json:"appId"`
	AppIDs               []string          `json:"appIds"`
	ScopeType            string            `json:"scopeType"`
	IncludedResources    []string          `json:"includedResources,omitempty"`
	ResourceSelection    ResourceSelection `json:"resourceSelection"`
	LabelSelector        LabelSelector     `json:"labelSelector,omitempty"`
	IncludeClusterScoped bool              `json:"includeClusterScoped"`
	StorageRepoID        string            `json:"storageRepoId,omitempty"`
	PolicyID             string            `json:"policyId,omitempty"`
	TargetClusterID      string            `json:"targetClusterId,omitempty"`
	ExcludedResources    []string          `json:"excludedResources,omitempty"`
	PreHooks             []map[string]any  `json:"preHooks,omitempty"`
	PostHooks            []map[string]any  `json:"postHooks,omitempty"`
	PlanStorageSize      map[string]any    `json:"planStorageSize,omitempty"`
	NextFireAt           time.Time         `json:"nextFireAt,omitempty"`
	ScheduleEnabled      bool              `json:"scheduleEnabled,omitempty"`
	Status               string            `json:"status"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
}

type ProtectionPlanInput struct {
	TenantID             string            `json:"-"`
	SourceClusterID      string            `json:"sourceClusterId"`
	AppID                string            `json:"appId"`
	AppIDs               []string          `json:"appIds"`
	ScopeType            string            `json:"scopeType"`
	IncludedResources    []string          `json:"includedResources"`
	ResourceSelection    ResourceSelection `json:"resourceSelection"`
	LabelSelector        LabelSelector     `json:"labelSelector"`
	IncludeClusterScoped bool              `json:"includeClusterScoped"`
	StorageRepoID        string            `json:"storageRepoId"`
	PolicyID             string            `json:"policyId"`
	TargetClusterID      string            `json:"targetClusterId"`
	ExcludedResources    []string          `json:"excludedResources"`
	PreHooks             []map[string]any  `json:"preHooks"`
	PostHooks            []map[string]any  `json:"postHooks"`
	Status               string            `json:"status"`
}

// ResourceSelection captures the user-facing resource-type selection used by
// current protection plans. "all" deliberately has no type arrays: it keeps
// Velero's native all-resource defaults rather than encoding an ambiguous
// empty selection. "custom" carries independent namespace/cluster scopes.
// Legacy fields above remain only for rolling-agent protocol compatibility;
// legacy Filter plans are migrated to the scoped model.
type ResourceSelection struct {
	Mode            string   `json:"mode"`
	NamespaceScoped []string `json:"namespaceScoped,omitempty"`
	ClusterScoped   []string `json:"clusterScoped,omitempty"`
}

type LabelSelector struct {
	MatchLabels      map[string]string         `json:"matchLabels,omitempty"`
	MatchExpressions []LabelSelectorExpression `json:"matchExpressions,omitempty"`
}

type LabelSelectorExpression struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type ProtectionPlanSchedule struct {
	ProtectionPlanID string    `json:"protectionPlanId"`
	LastFiredAt      time.Time `json:"lastFiredAt,omitempty"`
	NextFireAt       time.Time `json:"nextFireAt,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ProtectionPlanScheduleInput struct {
	ProtectionPlanID string
	NextFireAt       time.Time
	Enabled          bool
}

type ProtectionPlanScheduleFiredInput struct {
	ProtectionPlanID string
	LastFiredAt      time.Time
	NextFireAt       time.Time
}

type Task struct {
	ID               string         `json:"id"`
	TenantID         string         `json:"tenantId"`
	ClusterID        string         `json:"clusterId"`
	AppID            string         `json:"appId,omitempty"`
	ProtectionPlanID string         `json:"protectionPlanId,omitempty"`
	RestorePointID   string         `json:"restorePointId,omitempty"`
	Type             string         `json:"type"`
	Status           string         `json:"status"`
	Progress         int            `json:"progress"`
	CommandID        string         `json:"commandId,omitempty"`
	ErrorCode        string         `json:"errorCode,omitempty"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
	Payload          map[string]any `json:"payload,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	DispatchedAt     time.Time      `json:"dispatchedAt,omitempty"`
	AcceptedAt       time.Time      `json:"acceptedAt,omitempty"`
	StartedAt        time.Time      `json:"startedAt,omitempty"`
	CompletedAt      time.Time      `json:"completedAt,omitempty"`
}

type RestorePoint struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenantId"`
	ProtectionPlanID  string         `json:"protectionPlanId,omitempty"`
	SourceClusterID   string         `json:"sourceClusterId"`
	AppID             string         `json:"appId,omitempty"`
	StorageRepoID     string         `json:"storageRepoId,omitempty"`
	DisplayName       string         `json:"displayName"`
	VeleroBackupName  string         `json:"veleroBackupName"`
	PointType         string         `json:"pointType"`
	Status            string         `json:"status"`
	SizeBytes         int64          `json:"sizeBytes,omitempty"`
	StartedAt         time.Time      `json:"startedAt,omitempty"`
	CompletedAt       time.Time      `json:"completedAt,omitempty"`
	ExpiresAt         time.Time      `json:"expiresAt,omitempty"`
	SourceNamespace   string         `json:"sourceNamespace,omitempty"`
	LabelSelector     string         `json:"labelSelector,omitempty"`
	BackupTaskID      string         `json:"backupTaskId,omitempty"`
	BackupStorageName string         `json:"backupStorageName,omitempty"`
	TaskCreatedAt     time.Time      `json:"taskCreatedAt,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	SizeMetricsV2     map[string]any `json:"sizeMetricsV2,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
}

type RestorePointInput struct {
	ProtectionPlanID  string
	SourceClusterID   string
	AppID             string
	StorageRepoID     string
	DisplayName       string
	TaskCreatedAt     time.Time
	VeleroBackupName  string
	PointType         string
	Status            string
	SizeBytes         int64
	StartedAt         time.Time
	CompletedAt       time.Time
	ExpiresAt         time.Time
	SourceNamespace   string
	LabelSelector     string
	BackupTaskID      string
	BackupStorageName string
	Metadata          map[string]any
	SizeMetricsV2     map[string]any
}

type RestorePointStateInput struct {
	ID       string
	Status   string
	Metadata map[string]any
}

type RestorePointFilter struct {
	ClusterID        string
	AppID            string
	ProtectionPlanID string
	IncludeDeleted   bool
}

type TaskInput struct {
	ClusterID        string         `json:"clusterId"`
	AppID            string         `json:"appId"`
	ProtectionPlanID string         `json:"protectionPlanId"`
	RestorePointID   string         `json:"restorePointId"`
	Type             string         `json:"type"`
	Status           string         `json:"status"`
	CommandID        string         `json:"commandId"`
	Payload          map[string]any `json:"payload"`
}

type TaskStatusInput struct {
	TaskID         string
	RestorePointID string
	Status         string
	Progress       int
	ErrorCode      string
	ErrorMessage   string
	Payload        map[string]any
	MarkAccepted   bool
	MarkStarted    bool
	MarkDone       bool
}

type TaskEventInput struct {
	TaskID  string
	Level   string
	Reason  string
	Message string
	Payload map[string]any
}

type TaskEvent struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"taskId"`
	Level     string         `json:"level"`
	Reason    string         `json:"reason,omitempty"`
	Message   string         `json:"message"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type DiagnosticLog struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenantId,omitempty"`
	Scope       string         `json:"scope"`
	Level       string         `json:"level"`
	Component   string         `json:"component"`
	Operation   string         `json:"operation,omitempty"`
	Message     string         `json:"message"`
	ClusterID   string         `json:"clusterId,omitempty"`
	TaskID      string         `json:"taskId,omitempty"`
	CommandID   string         `json:"commandId,omitempty"`
	RequestID   string         `json:"requestId,omitempty"`
	ErrorCode   string         `json:"errorCode,omitempty"`
	Status      string         `json:"status,omitempty"`
	DurationMS  int64          `json:"durationMs,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	EventAt     time.Time      `json:"eventAt"`
	CreatedAt   time.Time      `json:"createdAt"`
	Fingerprint string         `json:"-"`
}

type DiagnosticLogInput struct {
	TenantID, Scope, Level, Component, Operation, Message      string
	ClusterID, TaskID, CommandID, RequestID, ErrorCode, Status string
	DurationMS                                                 int64
	Details                                                    map[string]any
	EventAt                                                    time.Time
	Fingerprint                                                string
}

type DiagnosticLogFilter struct {
	TenantID, Scope, Source, Level, Component, ClusterID, TaskID, Query string
	From, To                                                            time.Time
	Limit, Offset                                                       int
}

type ClusterLogCoverage struct {
	ClusterID       string    `json:"clusterId"`
	TenantID        string    `json:"tenantId"`
	Component       string    `json:"component"`
	CoveredFrom     time.Time `json:"coveredFrom"`
	CoveredTo       time.Time `json:"coveredTo"`
	LastCollectedAt time.Time `json:"lastCollectedAt"`
	LastRequestID   string    `json:"lastRequestId,omitempty"`
	LastEntryCount  int       `json:"lastEntryCount"`
	Truncated       bool      `json:"truncated"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type ClusterLogCoverageInput struct {
	ClusterID, TenantID, Component, RequestID string
	CoveredFrom, CoveredTo, CollectedAt       time.Time
	EntryCount                                int
	Truncated                                 bool
}

func NewPublicID() string {
	return newID()
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func normalizeClusterRole(role string) string {
	switch role {
	case "source", "target", "both":
		return role
	default:
		return "both"
	}
}

func storageSecretName(repositoryID string) string {
	if len(repositoryID) >= 8 {
		return "hypercdr-repo-" + repositoryID[:8]
	}
	return "hypercdr-repo"
}

func storageSecretPayload(input StorageRepositoryInput) map[string]string {
	secret := map[string]string{}
	if input.AccessKey != "" {
		secret["accessKey"] = input.AccessKey
	}
	if input.SecretKey != "" {
		secret["secretKey"] = input.SecretKey
	}
	if input.AccountName != "" {
		secret["accountName"] = input.AccountName
	}
	if input.AccountKey != "" {
		secret["accountKey"] = input.AccountKey
	}
	if input.ServiceAccountKey != "" {
		secret["serviceAccountKey"] = input.ServiceAccountKey
	}
	return secret
}

func normalizeStorageRegionValue(region string) string {
	region = strings.TrimSpace(region)
	switch strings.ToLower(region) {
	case "n/a", "na", "-":
		return ""
	default:
		return region
	}
}
