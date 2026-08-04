package protocol

import (
	"time"
)

const Version = "v1"

const (
	MessageKindRequest  = "request"
	MessageKindResponse = "response"
	MessageKindEvent    = "event"
)

const (
	MessageAgentRegister            = "agent.register"
	MessageAgentHeartbeat           = "agent.heartbeat"
	MessageAgentInventoryReport     = "agent.inventory.report"
	MessageAgentTaskAccepted        = "agent.task.accepted"
	MessageAgentTaskProgress        = "agent.task.progress"
	MessageAgentTaskCompleted       = "agent.task.completed"
	MessageAgentTaskFailed          = "agent.task.failed"
	MessageAgentVeleroEvent         = "agent.velero.event"
	MessageAgentLogReport           = "agent.log.report"
	MessageAgentBackupContentReport = "agent.backup-content.report"

	MessagePlatformRegisterAccepted     = "platform.register.accepted"
	MessagePlatformRegisterRejected     = "platform.register.rejected"
	MessagePlatformTaskDispatch         = "platform.task.dispatch"
	MessagePlatformTaskCancel           = "platform.task.cancel"
	MessagePlatformInventoryRequest     = "platform.inventory.request"
	MessagePlatformLogRequest           = "platform.log.request"
	MessagePlatformBackupContentRequest = "platform.backup-content.request"
	MessagePlatformEventAck             = "platform.event.ack"
	MessagePlatformEventError           = "platform.event.error"
	MessageAgentMessageError            = "agent.message.error"
)

type Message[T any] struct {
	Version     string    `json:"version"`
	MessageID   string    `json:"messageId"`
	MessageKind string    `json:"messageKind"`
	Type        string    `json:"type"`
	TenantID    string    `json:"tenantId,omitempty"`
	ClusterID   string    `json:"clusterId,omitempty"`
	AgentID     string    `json:"agentId,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Payload     T         `json:"payload"`
}

func NewMessage[T any](messageKind string, messageType string, clusterID string, agentID string, payload T) Message[T] {
	return Message[T]{
		Version:     Version,
		MessageID:   newMessageID(),
		MessageKind: messageKind,
		Type:        messageType,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload:     payload,
	}
}

type RegisterPayload struct {
	RequestID       string         `json:"requestId,omitempty"`
	InstallToken    string         `json:"installToken,omitempty"`
	AgentCredential string         `json:"agentCredential,omitempty"`
	Cluster         ClusterSummary `json:"cluster"`
	Agent           AgentSummary   `json:"agent"`
	Velero          VeleroSummary  `json:"velero"`
}

type ClusterSummary struct {
	Fingerprint    string `json:"fingerprint,omitempty"`
	Name           string `json:"name"`
	KubeVersion    string `json:"kubeVersion"`
	NodeCount      int    `json:"nodeCount"`
	NamespaceCount int    `json:"namespaceCount"`
}

type AgentSummary struct {
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
	PodName   string `json:"podName"`
}

type VeleroSummary struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Status    string `json:"status"`
}

type RegisterAcceptedPayload struct {
	AckMessageID                    string          `json:"ackMessageId,omitempty"`
	AckType                         string          `json:"ackType,omitempty"`
	RequestID                       string          `json:"requestId,omitempty"`
	TenantID                        string          `json:"tenantId"`
	ClusterID                       string          `json:"clusterId"`
	ClusterName                     string          `json:"clusterName"`
	AgentCredential                 string          `json:"agentCredential"`
	HeartbeatIntervalSeconds        int             `json:"heartbeatIntervalSeconds"`
	InventoryResyncIntervalSeconds  int             `json:"inventoryResyncIntervalSeconds,omitempty"`
	InventoryChangeDebounceSeconds  int             `json:"inventoryChangeDebounceSeconds,omitempty"`
	InventoryMinPushIntervalSeconds int             `json:"inventoryMinPushIntervalSeconds,omitempty"`
	ProtocolVersion                 string          `json:"protocolVersion,omitempty"`
	Features                        map[string]bool `json:"features,omitempty"`
}

type RegisterRejectedPayload struct {
	AckMessageID string `json:"ackMessageId,omitempty"`
	AckType      string `json:"ackType,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
	ErrorCode    string `json:"errorCode,omitempty"`
	Retryable    bool   `json:"retryable,omitempty"`
}

type HeartbeatPayload struct {
	AckRequired                bool   `json:"ackRequired,omitempty"`
	Status                     string `json:"status"`
	AgentVersion               string `json:"agentVersion"`
	AgentImage                 string `json:"agentImage,omitempty"`
	AgentImageID               string `json:"agentImageId,omitempty"`
	AgentImageDigest           string `json:"agentImageDigest,omitempty"`
	VeleroStatus               string `json:"veleroStatus,omitempty"`
	VeleroVersion              string `json:"veleroVersion,omitempty"`
	VeleroImage                string `json:"veleroImage,omitempty"`
	VeleroImageDigest          string `json:"veleroImageDigest,omitempty"`
	VeleroServerReady          bool   `json:"veleroServerReady,omitempty"`
	VeleroNodeAgentDesired     int32  `json:"veleroNodeAgentDesired,omitempty"`
	VeleroNodeAgentReady       int32  `json:"veleroNodeAgentReady,omitempty"`
	VeleroNodeAgentImageDigest string `json:"veleroNodeAgentImageDigest,omitempty"`
	ActiveTasks                int    `json:"activeTasks"`
	LastInventoryAt            string `json:"lastInventoryAt,omitempty"`
}

type InventoryReportPayload struct {
	AckRequired          bool                       `json:"ackRequired,omitempty"`
	AckMessageID         string                     `json:"ackMessageId,omitempty"`
	AckType              string                     `json:"ackType,omitempty"`
	RequestID            string                     `json:"requestId,omitempty"`
	Scope                string                     `json:"scope,omitempty"`
	Reason               string                     `json:"reason,omitempty"`
	Namespace            string                     `json:"namespace,omitempty"`
	Full                 bool                       `json:"full"`
	CollectedAt          time.Time                  `json:"collectedAt"`
	InventoryHash        string                     `json:"inventoryHash"`
	Cluster              ClusterSummary             `json:"cluster"`
	Nodes                []NodeInventory            `json:"nodes"`
	StorageClasses       []StorageClassInventory    `json:"storageClasses,omitempty"`
	APIResources         []APIResourceInventory     `json:"apiResources,omitempty"`
	NamespaceAPIs        []NamespaceAPIInventory    `json:"namespaceAPIs,omitempty"`
	Capabilities         []NamedCapabilityInventory `json:"capabilities,omitempty"`
	CapabilitiesComplete bool                       `json:"capabilitiesComplete,omitempty"`
	Apps                 []ApplicationInventory     `json:"applications"`
	Velero               VeleroInventory            `json:"velero"`
}

type NamedCapabilityInventory struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Driver string            `json:"driver,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

type NamespaceAPIInventory struct {
	Namespace string `json:"namespace"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Count     int    `json:"count"`
}

// APIResourceInventory records one resource endpoint actually served by the
// cluster. Readiness checks use this evidence instead of assuming that a CRD
// name alone makes every stored API version restorable.
type APIResourceInventory struct {
	Group      string `json:"group,omitempty"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

type NodeInventory struct {
	Name           string            `json:"name"`
	Role           string            `json:"role,omitempty"`
	Status         string            `json:"status"`
	KubeletVersion string            `json:"kubeletVersion,omitempty"`
	Capacity       map[string]string `json:"capacity,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds,omitempty"`
}

type StorageClassInventory struct {
	Name                 string `json:"name"`
	Provisioner          string `json:"provisioner"`
	ReclaimPolicy        string `json:"reclaimPolicy"`
	VolumeBindingMode    string `json:"volumeBindingMode"`
	AllowVolumeExpansion string `json:"allowVolumeExpansion"`
	Default              bool   `json:"default,omitempty"`
	AgeSeconds           int64  `json:"ageSeconds,omitempty"`
}

type ApplicationInventory struct {
	Namespace  string            `json:"namespace"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels,omitempty"`
	AgeSeconds int64             `json:"ageSeconds,omitempty"`
	Resources  ResourceSummary   `json:"resources"`
}

type ResourceSummary struct {
	Deployments     int                `json:"deployments"`
	StatefulSets    int                `json:"statefulsets"`
	DaemonSets      int                `json:"daemonsets"`
	Jobs            int                `json:"jobs,omitempty"`
	CronJobs        int                `json:"cronjobs,omitempty"`
	Services        int                `json:"services"`
	Ingresses       int                `json:"ingresses"`
	NetworkPolicies int                `json:"networkPolicies,omitempty"`
	ConfigMaps      int                `json:"configmaps"`
	Secrets         int                `json:"secrets"`
	ServiceAccounts int                `json:"serviceAccounts,omitempty"`
	PVCs            int                `json:"pvcs"`
	PVCapacityBytes int64              `json:"pvCapacityBytes"`
	DRSupport       *DRSupportSummary  `json:"drSupport,omitempty"`
	Categories      []ResourceCategory `json:"categories,omitempty"`
}

type DRSupportSummary struct {
	Status string           `json:"status"`
	Reason string           `json:"reason,omitempty"`
	Checks []DRSupportCheck `json:"checks,omitempty"`
}

type DRSupportCheck struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Provisioner  string `json:"provisioner,omitempty"`
	Volume       string `json:"volume,omitempty"`
	VolumeType   string `json:"volumeType,omitempty"`
}

type ResourceCategory struct {
	Key   string                `json:"key"`
	Label string                `json:"label"`
	Total int                   `json:"total"`
	Items []ResourceKindSummary `json:"items,omitempty"`
}

type ResourceKindSummary struct {
	Kind      string        `json:"kind"`
	ShortName string        `json:"shortName,omitempty"`
	Count     int           `json:"count"`
	Resources []ResourceRef `json:"resources,omitempty"`
}

type ResourceRef struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	Kind              string            `json:"kind,omitempty"`
	APIVersion        string            `json:"apiVersion,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Ready             string            `json:"ready,omitempty"`
	DesiredReplicas   int32             `json:"desiredReplicas,omitempty"`
	ReadyReplicas     int32             `json:"readyReplicas,omitempty"`
	UpdatedReplicas   int32             `json:"updatedReplicas,omitempty"`
	AvailableReplicas int32             `json:"availableReplicas,omitempty"`
	AgeSeconds        int64             `json:"ageSeconds,omitempty"`
	Containers        []string          `json:"containers,omitempty"`
	Images            []string          `json:"images,omitempty"`
	Selector          string            `json:"selector,omitempty"`
	Fields            map[string]string `json:"fields,omitempty"`
}

type VeleroInventory struct {
	Status                  string           `json:"status"`
	BackupStorageLocations  []map[string]any `json:"backupStorageLocations"`
	VolumeSnapshotLocations []map[string]any `json:"volumeSnapshotLocations"`
	RecentBackups           []map[string]any `json:"recentBackups"`
	RecentRestores          []map[string]any `json:"recentRestores"`
}

type InventoryRequestPayload struct {
	RequestID                  string `json:"requestId,omitempty"`
	Scope                      string `json:"scope,omitempty"`
	Namespace                  string `json:"namespace,omitempty"`
	IncludeDetails             bool   `json:"includeDetails,omitempty"`
	Reason                     string `json:"reason,omitempty"`
	IncludeRecentVeleroObjects bool   `json:"includeRecentVeleroObjects,omitempty"`
}

type LogRequestPayload struct {
	RequestID string    `json:"requestId"`
	Component string    `json:"component"`
	Since     time.Time `json:"since"`
	TailLines int64     `json:"tailLines"`
}
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Component string    `json:"component"`
	Pod       string    `json:"pod,omitempty"`
	Node      string    `json:"node,omitempty"`
	Message   string    `json:"message"`
}
type LogReportPayload struct {
	RequestID string     `json:"requestId"`
	Component string     `json:"component"`
	Entries   []LogEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
	ErrorCode string     `json:"errorCode,omitempty"`
	Message   string     `json:"message,omitempty"`
}

type BackupContentRequestPayload struct {
	RequestID        string `json:"requestId"`
	VeleroBackupName string `json:"veleroBackupName"`
	VeleroNamespace  string `json:"veleroNamespace"`
}

type BackupResourceSummary struct {
	APIVersion     string   `json:"apiVersion"`
	Kind           string   `json:"kind"`
	Namespace      string   `json:"namespace,omitempty"`
	Name           string   `json:"name"`
	Group          string   `json:"group,omitempty"`
	Resource       string   `json:"resource,omitempty"`
	ClusterScoped  bool     `json:"clusterScoped"`
	Images         []string `json:"images,omitempty"`
	StorageClasses []string `json:"storageClasses,omitempty"`
}

type BackupContentReportPayload struct {
	RequestID string                  `json:"requestId"`
	Resources []BackupResourceSummary `json:"resources"`
	Truncated bool                    `json:"truncated,omitempty"`
	ErrorCode string                  `json:"errorCode,omitempty"`
	Message   string                  `json:"message,omitempty"`
}

type TaskCancelPayload struct {
	TaskID          string `json:"taskId,omitempty"`
	CommandID       string `json:"commandId,omitempty"`
	TargetTaskID    string `json:"targetTaskId,omitempty"`
	TargetCommandID string `json:"targetCommandId,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type MessageErrorPayload struct {
	AckMessageID string `json:"ackMessageId,omitempty"`
	AckType      string `json:"ackType,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	CommandID    string `json:"commandId,omitempty"`
	ErrorCode    string `json:"errorCode"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
}

type VeleroEventPayload struct {
	AckRequired        bool              `json:"ackRequired,omitempty"`
	EventType          string            `json:"eventType"`
	BackupName         string            `json:"backupName"`
	Namespace          string            `json:"namespace,omitempty"`
	PlanID             string            `json:"planId,omitempty"`
	TaskID             string            `json:"taskId,omitempty"`
	CommandID          string            `json:"commandId,omitempty"`
	SourceClusterID    string            `json:"sourceClusterId,omitempty"`
	SourceNamespace    string            `json:"sourceNamespace,omitempty"`
	ScheduleName       string            `json:"scheduleName,omitempty"`
	Phase              string            `json:"phase"`
	Progress           int               `json:"progress"`
	Message            string            `json:"message,omitempty"`
	ResourceVersion    string            `json:"resourceVersion,omitempty"`
	StorageLocation    string            `json:"storageLocation,omitempty"`
	IncludedNamespaces []string          `json:"includedNamespaces,omitempty"`
	StartedAt          time.Time         `json:"startedAt,omitempty"`
	CompletedAt        time.Time         `json:"completedAt,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Velero             map[string]any    `json:"velero,omitempty"`
}

type TaskDispatchPayload struct {
	RequestMessageID  string                    `json:"-"`
	TaskID            string                    `json:"taskId"`
	CommandID         string                    `json:"commandId"`
	Type              string                    `json:"type"`
	Deadline          time.Time                 `json:"deadline"`
	Backup            *BackupCommand            `json:"backup,omitempty"`
	BackupCancel      *BackupCancelCommand      `json:"backupCancel,omitempty"`
	Restore           *RestoreCommand           `json:"restore,omitempty"`
	StorageSync       *StorageSyncCommand       `json:"storageSync,omitempty"`
	ScheduleSync      *ScheduleSyncCommand      `json:"scheduleSync,omitempty"`
	RetentionCleanup  *RetentionCleanupCommand  `json:"retentionCleanup,omitempty"`
	ProtectionCleanup *ProtectionCleanupCommand `json:"protectionCleanup,omitempty"`
	AgentUpgrade      *AgentUpgradeCommand      `json:"agentUpgrade,omitempty"`
	VeleroUpgrade     *VeleroUpgradeCommand     `json:"veleroUpgrade,omitempty"`
	Unregister        *UnregisterCommand        `json:"unregister,omitempty"`
}

type AgentUpgradeCommand struct {
	ClusterID         string `json:"clusterId"`
	Namespace         string `json:"namespace"`
	Image             string `json:"image"`
	Version           string `json:"version,omitempty"`
	ExpectedDigest    string `json:"expectedDigest,omitempty"`
	DeploymentName    string `json:"deploymentName,omitempty"`
	ContainerName     string `json:"containerName,omitempty"`
	RolloutAnnotation string `json:"rolloutAnnotation,omitempty"`
}

type VeleroUpgradeCommand struct {
	ClusterID        string `json:"clusterId"`
	Namespace        string `json:"namespace"`
	Image            string `json:"image"`
	Version          string `json:"version,omitempty"`
	ExpectedDigest   string `json:"expectedDigest,omitempty"`
	DeploymentName   string `json:"deploymentName,omitempty"`
	DaemonSetName    string `json:"daemonSetName,omitempty"`
	AWSPluginImage   string `json:"awsPluginImage,omitempty"`
	AzurePluginImage string `json:"azurePluginImage,omitempty"`
	GCPPluginImage   string `json:"gcpPluginImage,omitempty"`
}

type UnregisterCommand struct {
	ClusterID       string `json:"clusterId"`
	Namespace       string `json:"namespace"`
	DeleteVelero    bool   `json:"deleteVelero"`
	DeleteNamespace bool   `json:"deleteNamespace"`
	Reason          string `json:"reason,omitempty"`
}

type BackupCommand struct {
	PlanID                  string        `json:"planId,omitempty"`
	Trigger                 string        `json:"trigger,omitempty"`
	SourceClusterID         string        `json:"sourceClusterId,omitempty"`
	SourceNamespace         string        `json:"sourceNamespace"`
	SourceNamespaces        []string      `json:"sourceNamespaces,omitempty"`
	VeleroBackupName        string        `json:"veleroBackupName,omitempty"`
	Scope                   string        `json:"scope"`
	IncludedResources       []string      `json:"includedResources,omitempty"`
	LabelSelector           LabelSelector `json:"labelSelector,omitempty"`
	StorageRepo             string        `json:"storageRepo"`
	IncludeClusterResources bool          `json:"includeClusterResources"`
	ExcludedResources       []string      `json:"excludedResources,omitempty"`
	Hooks                   HookSet       `json:"hooks"`
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

type BackupCancelCommand struct {
	PlanID           string `json:"planId"`
	TargetTaskID     string `json:"targetTaskId"`
	VeleroBackupName string `json:"veleroBackupName,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type ScheduleSyncCommand struct {
	PlanID                  string        `json:"planId"`
	ScheduleName            string        `json:"scheduleName"`
	Cron                    string        `json:"cron"`
	SourceNamespace         string        `json:"sourceNamespace"`
	SourceNamespaces        []string      `json:"sourceNamespaces,omitempty"`
	Scope                   string        `json:"scope"`
	LabelSelector           string        `json:"labelSelector,omitempty"`
	StorageRepo             string        `json:"storageRepo"`
	IncludeClusterResources bool          `json:"includeClusterResources"`
	ExcludeResources        []ExcludeRule `json:"excludeResources"`
	Hooks                   HookSet       `json:"hooks"`
}

type RetentionCleanupCommand struct {
	PlanID        string                  `json:"planId"`
	RestorePoints []RetentionRestorePoint `json:"restorePoints"`
}

type RetentionRestorePoint struct {
	ID               string `json:"id"`
	VeleroBackupName string `json:"veleroBackupName"`
	Namespace        string `json:"namespace,omitempty"`
}

type ProtectionCleanupCommand struct {
	PlanID               string                  `json:"planId"`
	CleanupMode          string                  `json:"cleanupMode,omitempty"`
	ScheduleName         string                  `json:"scheduleName,omitempty"`
	BackupNamePrefix     string                  `json:"backupNamePrefix,omitempty"`
	Namespace            string                  `json:"namespace,omitempty"`
	SourceNamespaces     []string                `json:"sourceNamespaces,omitempty"`
	StorageRepo          string                  `json:"storageRepo,omitempty"`
	CleanupObjectStorage bool                    `json:"cleanupObjectStorage,omitempty"`
	RestorePoints        []RetentionRestorePoint `json:"restorePoints,omitempty"`
	RestoreNames         []string                `json:"restoreNames,omitempty"`
}

type RestoreCommand struct {
	RestorePointID         string            `json:"restorePointId"`
	VeleroBackupName       string            `json:"veleroBackupName"`
	StorageRepo            string            `json:"storageRepo,omitempty"`
	SourceNamespace        string            `json:"sourceNamespace"`
	SourceNamespaces       []string          `json:"sourceNamespaces,omitempty"`
	TargetNamespace        string            `json:"targetNamespace"`
	TargetNamespaces       map[string]string `json:"targetNamespaces,omitempty"`
	TargetMode             string            `json:"targetMode"`
	RestoreMode            string            `json:"restoreMode"`
	ArtifactMode           string            `json:"artifactMode"`
	ConflictPolicy         string            `json:"conflictPolicy"`
	IncludeClusterScoped   bool              `json:"includeClusterScoped"`
	UseTransforms          bool              `json:"useTransforms"`
	TransformPreset        string            `json:"transformPreset"`
	StorageProfileMode     string            `json:"storageProfileMode"`
	AlternateProfileID     string            `json:"alternateProfileId,omitempty"`
	IncludedResources      []string          `json:"includedResources,omitempty"`
	ExcludedResources      []string          `json:"excludedResources,omitempty"`
	StorageClassMappings   map[string]string `json:"storageClassMappings,omitempty"`
	ImageMappings          map[string]string `json:"imageMappings,omitempty"`
	WaitForWorkloads       bool              `json:"waitForWorkloads"`
	RunValidation          bool              `json:"runValidation"`
	ForceStart             bool              `json:"forceStart,omitempty"`
	ContentCatalogLoaded   bool              `json:"contentCatalogLoaded,omitempty"`
	PersistentDataExpected bool              `json:"persistentDataExpected,omitempty"`
}

type StorageSyncCommand struct {
	RepositoryID string         `json:"repositoryId"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Endpoint     string         `json:"endpoint,omitempty"`
	Bucket       string         `json:"bucket"`
	Region       string         `json:"region,omitempty"`
	TLSEnabled   bool           `json:"tlsEnabled"`
	SecretRef    string         `json:"secretRef,omitempty"`
	Credentials  *S3Credentials `json:"credentials,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
}

type S3Credentials struct {
	AccessKey         string `json:"accessKey,omitempty"`
	SecretKey         string `json:"secretKey,omitempty"`
	AccountName       string `json:"accountName,omitempty"`
	AccountKey        string `json:"accountKey,omitempty"`
	ServiceAccountKey string `json:"serviceAccountKey,omitempty"`
}

type ExcludeRule struct {
	Group    string `json:"group"`
	Resource string `json:"resource"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Labels   string `json:"labels"`
}

type HookSet struct {
	Pre  []HookScript `json:"pre"`
	Post []HookScript `json:"post"`
}

type HookScript struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Entry   bool   `json:"entry"`
}

type TaskAcceptedPayload struct {
	AckMessageID string    `json:"ackMessageId,omitempty"`
	AckType      string    `json:"ackType,omitempty"`
	TaskID       string    `json:"taskId"`
	CommandID    string    `json:"commandId"`
	AcceptedAt   time.Time `json:"acceptedAt"`
}

type TaskProgressPayload struct {
	AckRequired         bool           `json:"ackRequired,omitempty"`
	TaskID              string         `json:"taskId"`
	CommandID           string         `json:"commandId,omitempty"`
	Status              string         `json:"status"`
	Progress            int            `json:"progress"`
	TotalBytes          int64          `json:"totalBytes,omitempty"`
	SyncedBytes         int64          `json:"syncedBytes,omitempty"`
	SpeedBytesPerSecond float64        `json:"speedBytesPerSecond,omitempty"`
	Percent             float64        `json:"percent,omitempty"`
	EtaSeconds          int64          `json:"etaSeconds,omitempty"`
	Message             string         `json:"message"`
	Velero              map[string]any `json:"velero,omitempty"`
}

type TaskCompletedPayload struct {
	AckRequired bool           `json:"ackRequired,omitempty"`
	TaskID      string         `json:"taskId"`
	CommandID   string         `json:"commandId,omitempty"`
	Status      string         `json:"status"`
	Operation   string         `json:"operation,omitempty"`
	Progress    int            `json:"progress"`
	Message     string         `json:"message"`
	Size        map[string]any `json:"size,omitempty"`
	Velero      map[string]any `json:"velero,omitempty"`
}

type TaskFailedPayload struct {
	AckRequired  bool           `json:"ackRequired,omitempty"`
	AckMessageID string         `json:"ackMessageId,omitempty"`
	AckType      string         `json:"ackType,omitempty"`
	TaskID       string         `json:"taskId"`
	CommandID    string         `json:"commandId,omitempty"`
	ErrorCode    string         `json:"errorCode"`
	Message      string         `json:"message"`
	Details      map[string]any `json:"details,omitempty"`
	Retryable    bool           `json:"retryable,omitempty"`
}

type EventAckPayload struct {
	AckMessageID string `json:"ackMessageId"`
	AckType      string `json:"ackType,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	CommandID    string `json:"commandId,omitempty"`
	Persisted    bool   `json:"persisted"`
}

type EventErrorPayload struct {
	AckMessageID string `json:"ackMessageId"`
	AckType      string `json:"ackType,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	CommandID    string `json:"commandId,omitempty"`
	ErrorCode    string `json:"errorCode"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
}
