package kube

import (
	"context"
	"time"
)

type ClusterReader interface {
	ReadCluster(ctx context.Context) (ClusterState, error)
}

type CapabilityReader interface {
	ReadNamespaceAPIs(ctx context.Context, namespace string) ([]NamespaceAPI, error)
}

type NamespaceAPI struct {
	Namespace string
	Group     string
	Version   string
	Resource  string
	Kind      string
	Count     int
}

type VeleroBackupWaiter interface {
	WaitForVeleroBackup(ctx context.Context, namespace string, name string, timeout time.Duration) error
}

// ResourceModifierWaiter provides a propagation barrier between creating the
// ConfigMap and creating the Velero Restore that references it. Velero validates
// Restore resources from an informer cache, so an immediately-created Restore
// can otherwise race the ConfigMap add event and become FailedValidation.
type ResourceModifierWaiter interface {
	WaitForResourceModifier(ctx context.Context, namespace string, name string, timeout time.Duration) error
}

type VeleroBackupDeletionWaiter interface {
	WaitForVeleroBackupDeleted(ctx context.Context, namespace string, name string, timeout time.Duration) error
}

type ClusterState struct {
	Name                 string
	KubeVersion          string
	Nodes                []Node
	StorageClasses       []StorageClass
	APIResources         []APIResource
	Capabilities         []NamedCapability
	CapabilitiesComplete bool
	Namespaces           []Namespace
	Workloads            []Workload
	Services             []NamespacedResource
	Ingresses            []NamespacedResource
	NetworkPolicies      []NamespacedResource
	ConfigMaps           []NamespacedResource
	Secrets              []NamespacedResource
	ServiceAccounts      []NamespacedResource
	PVCs                 []PVC
	OtherResources       []TypedNamespacedResource
	Velero               VeleroState
	CollectedAt          time.Time
}

type NamedCapability struct {
	Type   string
	Name   string
	Driver string
	Fields map[string]string
}

type APIResource struct {
	Group      string
	Version    string
	Resource   string
	Kind       string
	Namespaced bool
}

type Node struct {
	Name           string
	Labels         map[string]string
	Ready          bool
	KubeletVersion string
	Capacity       map[string]string
	AgeSeconds     int64
}

type StorageClass struct {
	Name                 string
	Provisioner          string
	ReclaimPolicy        string
	VolumeBindingMode    string
	AllowVolumeExpansion string
	Default              bool
	AgeSeconds           int64
}

type Namespace struct {
	Name       string
	Phase      string
	Labels     map[string]string
	AgeSeconds int64
}

type Workload struct {
	Namespace         string
	Name              string
	Kind              string
	Labels            map[string]string
	Ready             bool
	DesiredReplicas   int32
	ReadyReplicas     int32
	UpdatedReplicas   int32
	AvailableReplicas int32
	AgeSeconds        int64
	Containers        []string
	Images            []string
	Selector          string
	Fields            map[string]string
}

type NamespacedResource struct {
	Namespace  string
	Name       string
	Labels     map[string]string
	AgeSeconds int64
	Fields     map[string]string
}

type TypedNamespacedResource struct {
	Namespace  string
	Name       string
	Kind       string
	APIVersion string
	ShortName  string
	Labels     map[string]string
	AgeSeconds int64
	Fields     map[string]string
}

type PVC struct {
	Namespace     string
	Name          string
	Labels        map[string]string
	CapacityBytes int64
	AgeSeconds    int64
	Fields        map[string]string
}

type PVInfo struct {
	VolumeType string
}

type VeleroState struct {
	Status                  string
	BackupStorageLocations  []map[string]any
	VolumeSnapshotLocations []map[string]any
	RecentBackups           []map[string]any
	RecentRestores          []map[string]any
}
