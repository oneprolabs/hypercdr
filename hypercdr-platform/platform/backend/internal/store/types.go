package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

const DefaultTenantID = "00000000-0000-0000-0000-000000000001"

const DefaultAdminEmail = "admin"
const DefaultAdminPassword = "admin123"

const clusterConnectionStaleAfter = 10 * time.Minute

var (
	ErrTokenInvalid = errors.New("install token is invalid")
	ErrTokenExpired = errors.New("install token is expired")
	ErrTokenUsed    = errors.New("install token is already used")
	ErrUserExists   = errors.New("user already exists")
	ErrResetInvalid = errors.New("password reset token is invalid or expired")
)

type Store interface {
	AuthenticateUser(input UserAuthInput) (User, bool, error)
	CreateUser(email string, password string) (User, error)
	CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error)
	ResetPassword(token string, password string) (User, error)
	FindOrCreateGoogleUser(email string) (User, error)
	CreateAgentToken(description string, ttl time.Duration) (AgentToken, error)
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
	ApplyInventory(input InventoryInput) (Cluster, bool, error)
	UpdateHeartbeat(input HeartbeatInput) (Cluster, bool, error)
	CreateStorageRepository(input StorageRepositoryInput) (StorageRepository, error)
	ListStorageRepositories() ([]StorageRepository, error)
	GetStorageRepository(id string) (StorageRepository, bool, error)
	SetStorageRepositoryStatus(id string, status string, lastValidatedAt time.Time) (StorageRepository, bool, error)
	UpsertClusterStorageBinding(input ClusterStorageBindingInput) (ClusterStorageBinding, error)
	GetClusterStorageBinding(clusterID string, storageRepoID string, sourceClusterID string) (ClusterStorageBinding, bool, error)
	UpdateClusterStorageBindingStatus(input ClusterStorageBindingStatusInput) (ClusterStorageBinding, bool, error)
	CreatePolicy(input PolicyInput) (Policy, error)
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
}

type UserAuthInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type User struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

type AgentToken struct {
	ID          string    `json:"id"`
	Token       string    `json:"token"`
	Description string    `json:"description,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	UsedAt      time.Time `json:"usedAt,omitempty"`
	ClusterID   string    `json:"clusterId,omitempty"`
}

type Cluster struct {
	ID                     string                `json:"id"`
	TenantID               string                `json:"tenantId"`
	Name                   string                `json:"name"`
	KubeVersion            string                `json:"kubeVersion"`
	Status                 string                `json:"status"`
	ConnectionStatus       string                `json:"connectionStatus"`
	NodeCount              int                   `json:"nodeCount"`
	NamespaceCount         int                   `json:"namespaceCount"`
	ApplicationCount       int                   `json:"applicationCount"`
	ActiveTasks            int                   `json:"activeTasks"`
	AgentVersion           string                `json:"agentVersion"`
	AgentImage             string                `json:"agentImage,omitempty"`
	AgentImageID           string                `json:"agentImageId,omitempty"`
	AgentImageDigest       string                `json:"agentImageDigest,omitempty"`
	LatestAgentVersion     string                `json:"latestAgentVersion,omitempty"`
	LatestAgentImage       string                `json:"latestAgentImage,omitempty"`
	LatestAgentImageDigest string                `json:"latestAgentImageDigest,omitempty"`
	AgentUpgradeAvailable  bool                  `json:"agentUpgradeAvailable,omitempty"`
	AgentUpgradeStatus     string                `json:"agentUpgradeStatus,omitempty"`
	AgentUpgradeProgress   int                   `json:"agentUpgradeProgress,omitempty"`
	VeleroVersion          string                `json:"veleroVersion,omitempty"`
	VeleroStatus           string                `json:"veleroStatus"`
	InventoryHash          string                `json:"inventoryHash,omitempty"`
	Nodes                  []ClusterNode         `json:"nodes,omitempty"`
	StorageClasses         []ClusterStorageClass `json:"storageClasses,omitempty"`
	Role                   string                `json:"role"`
	IsDefault              bool                  `json:"isDefault"`
	RegisteredAt           time.Time             `json:"registeredAt"`
	LastSeenAt             time.Time             `json:"lastSeenAt"`
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
	ClusterID        string
	Status           string
	KubeVersion      string
	AgentVersion     string
	AgentImage       string
	AgentImageID     string
	AgentImageDigest string
	VeleroStatus     string
	NodeCount        int
	NamespaceCount   int
	ApplicationCount int
	ActiveTasks      int
	InventoryHash    string
}

type InventoryInput struct {
	ClusterID      string
	KubeVersion    string
	VeleroStatus   string
	NodeCount      int
	NamespaceCount int
	Nodes          []ClusterNode
	StorageClasses []ClusterStorageClass
	Apps           []Application
	CollectedAt    time.Time
	Hash           string
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
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Endpoint   string         `json:"endpoint"`
	Bucket     string         `json:"bucket"`
	Region     string         `json:"region"`
	TLSEnabled bool           `json:"tlsEnabled"`
	Config     map[string]any `json:"config"`
	SecretRef  string         `json:"secretRef"`
	AccessKey  string         `json:"accessKey"`
	SecretKey  string         `json:"secretKey"`
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
	ID                   string           `json:"id"`
	TenantID             string           `json:"tenantId"`
	SourceClusterID      string           `json:"sourceClusterId"`
	AppID                string           `json:"appId"`
	AppIDs               []string         `json:"appIds"`
	ScopeType            string           `json:"scopeType"`
	IncludedResources    []string         `json:"includedResources,omitempty"`
	LabelSelector        LabelSelector    `json:"labelSelector,omitempty"`
	IncludeClusterScoped bool             `json:"includeClusterScoped"`
	StorageRepoID        string           `json:"storageRepoId,omitempty"`
	PolicyID             string           `json:"policyId,omitempty"`
	TargetClusterID      string           `json:"targetClusterId,omitempty"`
	ExcludedResources    []string         `json:"excludedResources,omitempty"`
	PreHooks             []map[string]any `json:"preHooks,omitempty"`
	PostHooks            []map[string]any `json:"postHooks,omitempty"`
	PlanStorageSize      map[string]any   `json:"planStorageSize,omitempty"`
	NextFireAt           time.Time        `json:"nextFireAt,omitempty"`
	ScheduleEnabled      bool             `json:"scheduleEnabled,omitempty"`
	Status               string           `json:"status"`
	CreatedAt            time.Time        `json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
}

type ProtectionPlanInput struct {
	SourceClusterID      string           `json:"sourceClusterId"`
	AppID                string           `json:"appId"`
	AppIDs               []string         `json:"appIds"`
	ScopeType            string           `json:"scopeType"`
	IncludedResources    []string         `json:"includedResources"`
	LabelSelector        LabelSelector    `json:"labelSelector"`
	IncludeClusterScoped bool             `json:"includeClusterScoped"`
	StorageRepoID        string           `json:"storageRepoId"`
	PolicyID             string           `json:"policyId"`
	TargetClusterID      string           `json:"targetClusterId"`
	ExcludedResources    []string         `json:"excludedResources"`
	PreHooks             []map[string]any `json:"preHooks"`
	PostHooks            []map[string]any `json:"postHooks"`
	Status               string           `json:"status"`
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
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
}

type RestorePointInput struct {
	ProtectionPlanID  string
	SourceClusterID   string
	AppID             string
	StorageRepoID     string
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
	return secret
}
