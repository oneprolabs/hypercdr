package store

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

func drillTargetNamespaces(payload map[string]any, cleanup bool) []string {
	var namespaces []string
	if cleanup {
		encoded, _ := json.Marshal(payload["drillNamespaces"])
		_ = json.Unmarshal(encoded, &namespaces)
	} else {
		if namespace, ok := payload["targetNamespace"].(string); ok {
			namespaces = append(namespaces, namespace)
		}
		encoded, _ := json.Marshal(payload["targetNamespaces"])
		var mapping map[string]string
		_ = json.Unmarshal(encoded, &mapping)
		for _, namespace := range mapping {
			namespaces = append(namespaces, namespace)
		}
	}
	var result []string
	for _, namespace := range namespaces {
		if namespace = strings.TrimSpace(namespace); namespace != "" && !slices.Contains(result, namespace) {
			result = append(result, namespace)
		}
	}
	return result
}

func guardDrillCleanup(tx *sql.Tx, input TaskInput, tenantID string) (string, error) {
	if input.ClusterID == "" {
		return "", nil
	}
	cleanup := input.Type == "protection-cleanup"
	targets := drillTargetNamespaces(input.Payload, cleanup)
	if len(targets) == 0 {
		return "", nil
	}
	if _, err := tx.Exec(`select pg_advisory_xact_lock(hashtextextended($1,0))`, "drill-cleanup:"+input.ClusterID); err != nil {
		return "", err
	}
	var drillCreated time.Time
	drillID, _ := input.Payload["drillTaskId"].(string)
	if cleanup {
		var status string
		if err := tx.QueryRow(`select created_at,status from tasks where id=$1 and tenant_id=$2 and cluster_id=$3 and type='drill'`, drillID, tenantID, input.ClusterID).Scan(&drillCreated, &status); err != nil {
			return "", err
		}
		if slices.Contains([]string{"queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling"}, status) {
			return "", ErrProtectionPlanBusy
		}
	}
	rows, err := tx.Query(`select id::text,type,status,payload,created_at from tasks where tenant_id=$1 and cluster_id=$2 and type in ('drill','restore','takeover','protection-cleanup') and (status in ('queued','dispatched','accepted','running','syncing','finalizing','canceling') or ($3 and type in ('drill','restore','takeover') and created_at > $4) or ($3 and type='protection-cleanup' and payload->>'drillTaskId'=$5 and status='succeeded')) order by created_at desc,id desc`, tenantID, input.ClusterID, cleanup, drillCreated, drillID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var task Task
		var raw []byte
		if err := rows.Scan(&task.ID, &task.Type, &task.Status, &raw, &task.CreatedAt); err != nil {
			return "", err
		}
		if err := json.Unmarshal(raw, &task.Payload); err != nil {
			return "", err
		}
		isCleanup := task.Type == "protection-cleanup" && task.Payload["cleanupMode"] == "drill"
		if task.Type == "protection-cleanup" && !isCleanup {
			continue
		}
		active := slices.Contains([]string{"queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling"}, task.Status)
		overlaps := false
		for _, namespace := range drillTargetNamespaces(task.Payload, isCleanup) {
			overlaps = overlaps || slices.Contains(targets, namespace)
		}
		if !overlaps {
			continue
		}
		if cleanup && isCleanup && task.Payload["drillTaskId"] == drillID && (active || task.Status == "succeeded") {
			existingTargets := drillTargetNamespaces(task.Payload, true)
			slices.Sort(existingTargets)
			slices.Sort(targets)
			if slices.Equal(existingTargets, targets) {
				return task.ID, nil
			}
		}
		if (isCleanup && active) || (cleanup && !isCleanup && (active || task.CreatedAt.After(drillCreated))) {
			return "", ErrProtectionPlanBusy
		}
	}
	return "", rows.Err()
}
