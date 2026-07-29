package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type Snapshot struct {
	Report protocol.InventoryReportPayload
	Hash   string
}

type Collector interface {
	Collect() (Snapshot, error)
}

type CapabilityCollector interface {
	CollectCapabilities(namespace string) (Snapshot, error)
}

type StaticCollector struct {
	cfg config.Config
}

func NewStaticCollector(cfg config.Config) *StaticCollector {
	return &StaticCollector{cfg: cfg}
}

func (c *StaticCollector) Collect() (Snapshot, error) {
	report := protocol.InventoryReportPayload{
		Full:        true,
		CollectedAt: time.Now().UTC(),
		Cluster: protocol.ClusterSummary{
			Name:           c.cfg.ClusterName,
			KubeVersion:    "unknown",
			NodeCount:      1,
			NamespaceCount: 3,
		},
		Nodes: []protocol.NodeInventory{
			{
				Name:           "sample-node",
				Role:           "worker",
				Status:         "ready",
				KubeletVersion: "unknown",
				Capacity: map[string]string{
					"cpu":    "unknown",
					"memory": "unknown",
				},
			},
		},
		Apps: []protocol.ApplicationInventory{
			{
				Namespace: "frontend-service",
				Status:    "running",
				Labels: map[string]string{
					"app":         "frontend",
					"environment": "demo",
				},
				Resources: protocol.ResourceSummary{
					Deployments:     3,
					Services:        2,
					Ingresses:       1,
					ConfigMaps:      2,
					Secrets:         1,
					PVCs:            1,
					PVCapacityBytes: 8 * 1024 * 1024 * 1024,
				},
			},
			{
				Namespace: "auth-db-primary",
				Status:    "running",
				Labels: map[string]string{
					"app":  "auth-db",
					"tier": "database",
				},
				Resources: protocol.ResourceSummary{
					StatefulSets:    1,
					Services:        1,
					ConfigMaps:      1,
					Secrets:         2,
					PVCs:            2,
					PVCapacityBytes: 24 * 1024 * 1024 * 1024,
				},
			},
			{
				Namespace: "payment-gateway",
				Status:    "running",
				Labels: map[string]string{
					"app":  "payment",
					"tier": "backend",
				},
				Resources: protocol.ResourceSummary{
					Deployments: 2,
					Services:    2,
					ConfigMaps:  3,
					Secrets:     1,
				},
			},
		},
		Velero: protocol.VeleroInventory{
			BackupStorageLocations:  []map[string]any{},
			VolumeSnapshotLocations: []map[string]any{},
			RecentBackups:           []map[string]any{},
			RecentRestores:          []map[string]any{},
		},
	}

	hash, err := hashReport(report)
	if err != nil {
		return Snapshot{}, err
	}
	report.InventoryHash = hash
	return Snapshot{Report: report, Hash: hash}, nil
}

func (c *StaticCollector) CollectCapabilities(namespace string) (Snapshot, error) {
	snapshot, err := c.Collect()
	if err == nil {
		snapshot.Report.Scope = "capabilities"
		snapshot.Report.Namespace = namespace
	}
	return snapshot, err
}

func hashReport(report protocol.InventoryReportPayload) (string, error) {
	normalized := struct {
		Cluster        protocol.ClusterSummary          `json:"cluster"`
		Nodes          []protocol.NodeInventory         `json:"nodes"`
		StorageClasses []protocol.StorageClassInventory `json:"storageClasses,omitempty"`
		Apps           []protocol.ApplicationInventory  `json:"applications"`
		Velero         protocol.VeleroInventory         `json:"velero"`
	}{
		Cluster:        report.Cluster,
		Nodes:          report.Nodes,
		StorageClasses: report.StorageClasses,
		Apps:           report.Apps,
		Velero:         report.Velero,
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	var stable any
	if err := json.Unmarshal(data, &stable); err != nil {
		return "", err
	}
	stripVolatileInventoryFields(stable)
	data, err = json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func stripVolatileInventoryFields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "ageSeconds")
		delete(typed, "AGE")
		for key, item := range typed {
			if key == "fields" {
				if fields, ok := item.(map[string]any); ok {
					delete(fields, "AGE")
				}
			}
			stripVolatileInventoryFields(item)
		}
	case []any:
		for _, item := range typed {
			stripVolatileInventoryFields(item)
		}
	}
}
