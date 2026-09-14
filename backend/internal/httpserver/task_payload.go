package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"sort"
	"strings"
	"time"
)

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func boolPayload(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func stringSlicePayload(payload map[string]any, key string) []string {
	raw, ok := payload[key].([]any)
	if !ok {
		if stringsValue, ok := payload[key].([]string); ok {
			return stringsValue
		}
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func resourceSelectionPayload(payload map[string]any) store.ResourceSelection {
	switch raw := payload["resourceSelection"].(type) {
	case store.ResourceSelection:
		if raw.Mode == "" {
			raw.Mode = "all"
		}
		return raw
	case map[string]any:
		return store.ResourceSelection{
			Mode:            firstNonEmptyString(stringFromMap(raw, "mode"), "all"),
			NamespaceScoped: stringSlicePayload(raw, "namespaceScoped"),
			ClusterScoped:   stringSlicePayload(raw, "clusterScoped"),
		}
	default:
		return store.ResourceSelection{Mode: "all"}
	}
}

func protocolResourceSelection(selection store.ResourceSelection) protocol.ResourceSelection {
	return protocol.ResourceSelection{
		Mode:            selection.Mode,
		NamespaceScoped: selection.NamespaceScoped,
		ClusterScoped:   selection.ClusterScoped,
	}
}

func labelSelectorPayload(payload map[string]any) store.LabelSelector {
	raw, ok := payload["labelSelector"]
	if !ok || raw == nil {
		return store.LabelSelector{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return store.LabelSelector{}
	}
	var selector store.LabelSelector
	if err := json.Unmarshal(data, &selector); err != nil {
		return store.LabelSelector{}
	}
	return selector
}

func protocolLabelSelector(selector store.LabelSelector) protocol.LabelSelector {
	expressions := make([]protocol.LabelSelectorExpression, 0, len(selector.MatchExpressions))
	for _, expression := range selector.MatchExpressions {
		expressions = append(expressions, protocol.LabelSelectorExpression{
			Key: expression.Key, Operator: expression.Operator, Values: expression.Values,
		})
	}
	return protocol.LabelSelector{MatchLabels: selector.MatchLabels, MatchExpressions: expressions}
}

func stringMapPayload(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]any)
	if !ok {
		if typed, ok := payload[key].(map[string]string); ok {
			return typed
		}
		return nil
	}
	values := map[string]string{}
	for key, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func intMapPayload(payload map[string]any, key string) map[string]int {
	result := map[string]int{}
	if typed, ok := payload[key].(map[string]int); ok {
		for name, value := range typed {
			result[name] = value
		}
		return result
	}
	value, ok := payload[key].(map[string]any)
	if !ok {
		return result
	}
	for name, raw := range value {
		switch port := raw.(type) {
		case int:
			result[name] = port
		case int64:
			result[name] = int(port)
		case float64:
			result[name] = int(port)
		}
	}
	return result
}

func retentionCleanupCommandFromPayload(payload map[string]any) *protocol.RetentionCleanupCommand {
	points := retentionRestorePointsFromAny(payload["restorePoints"])
	return &protocol.RetentionCleanupCommand{
		PlanID:        stringPayload(payload, "planId"),
		RestorePoints: points,
	}
}

func protectionCleanupCommandFromPayload(payload map[string]any) *protocol.ProtectionCleanupCommand {
	return &protocol.ProtectionCleanupCommand{
		PlanID:               stringPayload(payload, "planId"),
		CleanupMode:          stringPayload(payload, "cleanupMode"),
		ScheduleName:         stringPayload(payload, "scheduleName"),
		BackupNamePrefix:     stringPayload(payload, "backupNamePrefix"),
		Namespace:            stringPayload(payload, "namespace"),
		SourceNamespaces:     stringSlicePayload(payload, "sourceNamespaces"),
		StorageRepo:          stringPayload(payload, "storageRepo"),
		CleanupObjectStorage: boolPayload(payload, "cleanupObjectStorage"),
		RestorePoints:        retentionRestorePointsFromAny(payload["restorePoints"]),
		RestoreNames:         stringSlicePayload(payload, "restoreNames"),
		DrillNamespaces:      stringSlicePayload(payload, "drillNamespaces"),
	}
}

func retentionRestorePointsFromAny(raw any) []protocol.RetentionRestorePoint {
	points := []protocol.RetentionRestorePoint{}
	appendPoint := func(values map[string]any) {
		point := protocol.RetentionRestorePoint{
			ID:               stringFromMap(values, "id"),
			VeleroBackupName: stringFromMap(values, "veleroBackupName"),
			Namespace:        stringFromMap(values, "namespace"),
		}
		if point.ID != "" && point.VeleroBackupName != "" {
			points = append(points, point)
		}
	}
	switch values := raw.(type) {
	case []any:
		for _, item := range values {
			if point, ok := item.(protocol.RetentionRestorePoint); ok {
				if point.ID != "" && point.VeleroBackupName != "" {
					points = append(points, point)
				}
				continue
			}
			if mapped, ok := item.(map[string]any); ok {
				appendPoint(mapped)
			}
		}
	case []map[string]any:
		for _, item := range values {
			appendPoint(item)
		}
	case []protocol.RetentionRestorePoint:
		for _, point := range values {
			if point.ID != "" && point.VeleroBackupName != "" {
				points = append(points, point)
			}
		}
	}
	return points
}

func (r *Router) scheduleSyncCommandFromPayload(payload map[string]any) (*protocol.ScheduleSyncCommand, error) {
	repoName := stringPayload(payload, "storageRepo")
	if repoName == "" {
		repoID := stringPayload(payload, "storageRepoId")
		if repoID != "" {
			repo, ok, err := r.store.GetStorageRepository(repoID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("storage repository not found")
			}
			repoName = repo.Name
		}
	}
	sourceNamespace := stringPayload(payload, "sourceNamespace")
	sourceNamespaces := stringSlicePayload(payload, "sourceNamespaces")
	if len(sourceNamespaces) == 0 && sourceNamespace != "" {
		sourceNamespaces = []string{sourceNamespace}
	}
	excludeRules := append(
		defaultExcludedResourcesForNamespaces(sourceNamespaces),
		excludeRulesPayload(payload)...,
	)
	return &protocol.ScheduleSyncCommand{
		PlanID:                  stringPayload(payload, "planId"),
		ScheduleName:            stringPayload(payload, "scheduleName"),
		Cron:                    stringPayload(payload, "cron"),
		SourceNamespace:         sourceNamespace,
		SourceNamespaces:        sourceNamespaces,
		Scope:                   stringPayload(payload, "scope"),
		IncludedResources:       stringSlicePayload(payload, "includedResources"),
		Selector:                protocolLabelSelector(labelSelectorPayload(payload)),
		StorageRepo:             repoName,
		IncludeClusterResources: boolPayload(payload, "includeClusterResources"),
		ExcludeResources:        excludeRules,
		ResourceSelection:       protocolResourceSelection(resourceSelectionPayload(payload)),
		Hooks:                   protocol.HookSet{},
	}, nil
}

func excludeRulesPayload(payload map[string]any) []protocol.ExcludeRule {
	raw, ok := payload["excludeRules"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	rules := make([]protocol.ExcludeRule, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rule := protocol.ExcludeRule{
			Group:    stringFromMap(m, "group"),
			Resource: stringFromMap(m, "resource"),
			Name:     stringFromMap(m, "name"),
			Version:  stringFromMap(m, "version"),
			Labels:   stringFromMap(m, "labels"),
		}
		if rule.Group == "" && rule.Resource == "" && rule.Name == "" && rule.Version == "" && rule.Labels == "" {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mapPayload(values map[string]any, key string) map[string]any {
	raw, ok := values[key].(map[string]any)
	if ok {
		return raw
	}
	stringsMap, ok := values[key].(map[string]string)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(stringsMap))
	for k, v := range stringsMap {
		out[k] = v
	}
	return out
}

func firstStringFromAny(value any) string {
	switch typed := value.(type) {
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}
	case []any:
		if len(typed) > 0 {
			value, _ := typed[0].(string)
			return value
		}
	}
	return ""
}

func stringArrayFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return uniqueNonEmptyStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		return uniqueNonEmptyStrings(values)
	default:
		return nil
	}
}

func firstStringFromStrings(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseTimeFromAny(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func veleroBackupSizeBytes(velero map[string]any) int64 {
	restorePointSize := mapFromAny(velero["restorePointSize"])
	if totalBytes := int64FromAny(restorePointSize["totalBytes"]); totalBytes > 0 {
		return totalBytes
	}
	size := mapFromAny(velero["size"])
	if totalBytes := int64FromAny(size["totalBytes"]); totalBytes > 0 {
		return totalBytes
	}
	if sizeBytes := int64FromAny(velero["sizeBytes"]); sizeBytes > 0 {
		return sizeBytes
	}
	return 0
}

func planStorageTotalBytesFromMetadata(metadata map[string]any) int64 {
	if len(metadata) == 0 {
		return 0
	}
	candidates := []map[string]any{
		mapFromAny(metadata["planStorageSize"]),
		mapFromAny(mapFromAny(metadata["velero"])["planStorageSize"]),
	}
	for _, candidate := range candidates {
		if totalBytes := int64FromAny(candidate["totalBytes"]); totalBytes != 0 {
			return totalBytes
		}
		if totalBytes := int64FromAny(candidate["total"]); totalBytes != 0 {
			return totalBytes
		}
		metadataBytes := int64FromAny(candidate["metadataBytes"])
		kopiaBytes := int64FromAny(candidate["kopiaBytes"])
		volumeBytes := int64FromAny(candidate["volumeBytes"])
		if metadataBytes != 0 || kopiaBytes != 0 || volumeBytes != 0 {
			return metadataBytes + kopiaBytes + volumeBytes
		}
	}
	return 0
}

func enrichRestorePointStorageIncrements(items []store.RestorePoint) []store.RestorePoint {
	grouped := map[string][]int{}
	for index, item := range items {
		if item.ProtectionPlanID == "" || !strings.EqualFold(item.Status, "available") {
			continue
		}
		grouped[item.ProtectionPlanID] = append(grouped[item.ProtectionPlanID], index)
	}
	for _, indexes := range grouped {
		sort.SliceStable(indexes, func(i, j int) bool {
			left := items[indexes[i]]
			right := items[indexes[j]]
			leftTime := left.CompletedAt
			if leftTime.IsZero() {
				leftTime = left.CreatedAt
			}
			rightTime := right.CompletedAt
			if rightTime.IsZero() {
				rightTime = right.CreatedAt
			}
			return leftTime.Before(rightTime)
		})
		var previousTotal int64
		for _, index := range indexes {
			currentTotal := planStorageTotalBytesFromMetadata(items[index].Metadata)
			if currentTotal == 0 {
				continue
			}
			delta := currentTotal
			hasPrevious := previousTotal != 0
			if hasPrevious {
				delta = currentTotal - previousTotal
			}
			if items[index].Metadata == nil {
				items[index].Metadata = map[string]any{}
			}
			items[index].Metadata["storageIncrementSize"] = map[string]any{
				"bytes":              delta,
				"planTotalBytes":     currentTotal,
				"previousTotalBytes": previousTotal,
				"hasPrevious":        hasPrevious,
			}
			previousTotal = currentTotal
		}
	}
	return items
}
