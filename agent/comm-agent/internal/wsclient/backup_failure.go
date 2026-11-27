package wsclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type backupResultReader interface {
	GetBackupResultSummary(context.Context, string, string, string) (kube.RestoreResultSummary, error)
}

func (c *Client) enrichBackupFailure(ctx context.Context, object kube.AppliedObject, payload map[string]any, status kube.ManifestStatus, message string) (string, map[string]any) {
	reader, ok := c.statsReader.(backupResultReader)
	if !ok || status.Phase == "FailedValidation" {
		return message, payload
	}
	storageLocation := backupStorageLocationFromPayload(payload)
	if storageLocation == "" {
		return message, payload
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	summary, err := reader.GetBackupResultSummary(readCtx, object.Namespace, storageLocation, object.Name)
	if err != nil {
		payload["backupResultsReadError"] = err.Error()
		return fmt.Sprintf("Velero backup %s with %d error(s). Detailed results could not be read: %s", status.Phase, status.Errors, err), payload
	}
	payload["backupResults"] = map[string]any{"key": summary.Key, "errorCount": summary.ErrorCount, "errors": summary.Errors}
	if len(summary.Errors) > 0 {
		message = strings.Join(summary.Errors, "\n")
	}
	return message, payload
}

type backupFailureRule struct {
	code     string
	patterns []string
}

var backupFailureRules = []backupFailureRule{
	{"SYNC_API_READ_FAILED", []string{"error to list dataupload", "the server was unable to return a response in the time allotted", "etcdserver: request timed out"}},
	{"SYNC_STORAGE_AUTH_FAILED", []string{"invalidaccesskeyid", "signaturedoesnotmatch", "expiredtoken", "accessdenied", "access denied", "invalid credentials"}},
	{"SYNC_STORAGE_CAPACITY_EXCEEDED", []string{"no space left on device", "insufficient storage", "quota exceeded", "disk full"}},
	{"SYNC_TLS_FAILED", []string{"x509:", "tls handshake", "certificate has expired"}},
	{"SYNC_API_PERMISSION_DENIED", []string{"forbidden", "cannot list resource", "cannot create resource"}},
	{"SYNC_REPOSITORY_UNAVAILABLE", []string{"repository not initialized", "repository not found", "repository is not initialized", "repository is locked"}},
	{"SYNC_CSI_RUNTIME_UNAVAILABLE", []string{"csi snapshot execution is unavailable", "csi runtime checker unavailable"}},
	{"SYNC_CSI_PREREQUISITES_FAILED", []string{"snapshot prerequisites failed", "snapshot prerequisite checker unavailable"}},
	{"SYNC_CSI_SNAPSHOT_FAILED", []string{"failed to create snapshot", "failed to create volumesnapshot", "snapshot creation failed", "snapshot handle is empty"}},
	{"SYNC_VOLUME_MOUNT_FAILED", []string{"failedattachvolume", "failedmount", "multi-attach error", "mountvolume.setup failed", "failed to mount"}},
	{"SYNC_WORKER_RESOURCE_UNAVAILABLE", []string{"insufficient cpu", "insufficient memory", "failedscheduling", "oomkilled", "imagepullbackoff", "errimagepull"}},
	{"SYNC_SOURCE_VOLUME_UNAVAILABLE", []string{"persistentvolumeclaim not found", "pvc is not bound", "read-only file system", "input/output error"}},
	{"SYNC_NETWORK_UNAVAILABLE", []string{"connection refused", "connection reset", "no such host", "network is unreachable", "i/o timeout", "use of closed network connection"}},
}

func backupFailureDetails(code, message string, details map[string]any) (string, map[string]any) {
	result := cloneVeleroPayload(details)
	if result == nil {
		result = map[string]any{}
	}
	result["originalErrorCode"] = code
	result["originalMessage"] = message
	velero := wsMapFromAny(result["velero"])
	status := wsMapFromAny(velero["status"])
	evidence := []string{message, stringFromAny(status["message"]), stringFromAny(status["reason"])}
	for _, raw := range backupFailureItems(wsMapFromAny(velero["volumeProgress"])["items"]) {
		if strings.EqualFold(stringFromAny(raw["phase"]), "Failed") {
			evidence = append(evidence, stringFromAny(raw["message"]))
		}
	}
	text := strings.ToLower(strings.Join(evidence, "\n"))
	if code == "SNAPSHOT_COMPLETION_UNPROVEN" || code == "LOCAL_LEDGER_WRITE_FAILED" {
		return code, result
	}
	for _, rule := range backupFailureRules {
		for _, pattern := range rule.patterns {
			if strings.Contains(text, pattern) {
				return rule.code, result
			}
		}
	}
	switch strings.ToUpper(code) {
	case "TASK_DEADLINE_EXCEEDED", "BACKUP_STATUS_TIMEOUT":
		return "SYNC_DEADLINE_EXCEEDED", result
	case "BACKUP_STATUS_READ_FAILED":
		return "SYNC_API_READ_FAILED", result
	case "BACKUP_FAILED", "VOLUME_BACKUP_FAILED", "BACKUP_SUBMIT_FAILED", "BACKUP_MANIFEST_INVALID", "BACKUP_COMMAND_INVALID":
		return "SYNC_BACKUP_FAILED", result
	}
	return code, result
}

func backupFailureItems(value any) []map[string]any {
	var items []map[string]any
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		for _, raw := range typed {
			items = append(items, wsMapFromAny(raw))
		}
	}
	return items
}

func backupVolumeFailureMessage(volume map[string]any) string {
	var failures []string
	for _, item := range backupFailureItems(volume["items"]) {
		if strings.EqualFold(stringFromAny(item["phase"]), "Failed") {
			failures = append(failures, fmt.Sprintf("%s/%s (%s): %s", stringFromAny(item["namespace"]), stringFromAny(item["volume"]), stringFromAny(item["name"]), stringFromAny(item["message"])))
		}
	}
	if len(failures) == 0 {
		return "A volume backup failed. Inspect DataUpload or PodVolumeBackup events and node-agent logs."
	}
	return strings.Join(failures, "; ")
}
