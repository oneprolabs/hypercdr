package kube

import (
	"context"
	"time"
)

type PlanCapacityTarget struct {
	PlanID           string
	StorageLocation  string
	BackupNamePrefix string
	Namespaces       []string
}

type PlanCapacityResult struct {
	PlanID      string
	Stats       PlanObjectStorageStats
	CollectedAt time.Time
	Error       error
}

func (a *DynamicManifestApplier) CollectPlanCapacity(ctx context.Context, namespace string, plans []PlanCapacityTarget) []PlanCapacityResult {
	repositories := map[string]PlanObjectStorageStats{}
	results := make([]PlanCapacityResult, 0, len(plans))
	for _, plan := range plans {
		stats, err := a.planObjectStorageStats(ctx, namespace, plan.StorageLocation, plan.BackupNamePrefix, plan.Namespaces, repositories)
		results = append(results, PlanCapacityResult{PlanID: plan.PlanID, Stats: stats, CollectedAt: time.Now().UTC(), Error: err})
	}
	return results
}
