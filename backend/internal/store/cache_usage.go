package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type nodeCacheMeasurement struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Path       string `json:"path,omitempty"`
	MaxBytes   int64  `json:"maxBytes,omitempty"`
	UsedBytes  *int64 `json:"usedBytes,omitempty"`
	ObservedAt string `json:"observedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

type nodeCacheUsage struct {
	Name  string               `json:"name"`
	Index nodeCacheMeasurement `json:"index"`
	Drill nodeCacheMeasurement `json:"drill"`
}

type cacheUsageReport struct {
	Phase      string           `json:"phase"`
	Error      string           `json:"error,omitempty"`
	ObservedAt time.Time        `json:"observedAt"`
	Nodes      []nodeCacheUsage `json:"nodes"`
}

func mergeCacheUsage(previous, current nodeCacheMeasurement) nodeCacheMeasurement {
	if current.Error == "" || current.UsedBytes != nil {
		return current
	}
	if current.Path == "" {
		previous.Error = current.Error
		return previous
	}
	if current.Path == previous.Path {
		current.UsedBytes = previous.UsedBytes
		current.ObservedAt = previous.ObservedAt
	}
	return current
}

func validateCacheUsage(report cacheUsageReport) error {
	if report.ObservedAt.IsZero() || report.ObservedAt.After(time.Now().UTC().Add(5*time.Minute)) || (report.Phase != "succeeded" && report.Phase != "failed") || len(report.Nodes) > 10000 {
		return fmt.Errorf("invalid cache usage report")
	}
	seen := make(map[string]bool, len(report.Nodes))
	for _, node := range report.Nodes {
		if node.Name == "" || seen[node.Name] {
			return fmt.Errorf("invalid or duplicate cache usage node")
		}
		seen[node.Name] = true
		for _, cache := range []nodeCacheMeasurement{node.Index, node.Drill} {
			if cache.MaxBytes < 0 || (cache.UsedBytes != nil && *cache.UsedBytes < 0) {
				return fmt.Errorf("invalid cache usage size")
			}
			if cache.Path != "" && (!filepath.IsAbs(cache.Path) || filepath.Clean(cache.Path) != cache.Path || cache.Path == "/") {
				return fmt.Errorf("invalid cache usage path")
			}
			if cache.UsedBytes != nil {
				observed, err := time.Parse(time.RFC3339Nano, cache.ObservedAt)
				if err != nil || observed.IsZero() || observed.After(report.ObservedAt.Add(time.Minute)) || cache.Path == "" {
					return fmt.Errorf("invalid cache usage observation")
				}
			}
		}
	}
	return nil
}

func (s *PostgresStore) SaveClusterCacheUsage(clusterID string, report map[string]any) error {
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	var current cacheUsageReport
	if err := json.Unmarshal(raw, &current); err != nil {
		return err
	}
	if err := validateCacheUsage(current); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previousRaw []byte
	err = tx.QueryRow(`select coalesce(metadata->'cacheUsage', '{}'::jsonb) from clusters where id = $1 for update`, clusterID).Scan(&previousRaw)
	if err == sql.ErrNoRows {
		return fmt.Errorf("cluster not found")
	}
	if err != nil {
		return err
	}
	var previous cacheUsageReport
	if err := json.Unmarshal(previousRaw, &previous); err != nil {
		return err
	}
	if current.ObservedAt.Before(previous.ObservedAt) {
		return nil
	}
	byName := make(map[string]nodeCacheUsage, len(previous.Nodes))
	for _, node := range previous.Nodes {
		byName[node.Name] = node
	}
	for index, node := range current.Nodes {
		if old, exists := byName[node.Name]; exists {
			current.Nodes[index].Index = mergeCacheUsage(old.Index, node.Index)
			current.Nodes[index].Drill = mergeCacheUsage(old.Drill, node.Drill)
		}
	}
	if current.Phase == "failed" && len(current.Nodes) == 0 {
		current.Nodes = previous.Nodes
	}
	raw, err = json.Marshal(current)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`update clusters set metadata = coalesce(metadata, '{}'::jsonb) || jsonb_build_object('cacheUsage', $2::jsonb) where id = $1`, clusterID, raw); err != nil {
		return err
	}
	return tx.Commit()
}
