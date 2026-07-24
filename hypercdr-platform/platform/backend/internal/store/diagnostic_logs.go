package store

import "strings"
import "time"

func normalizeDiagnosticLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warning"
	case "error", "fatal":
		return "error"
	default:
		return "info"
	}
}

func redactDiagnosticDetails(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
		if isDiagnosticSecretKey(normalized) {
			result[key] = "[REDACTED]"
			continue
		}
		switch nested := item.(type) {
		case map[string]any:
			result[key] = redactDiagnosticDetails(nested)
		default:
			result[key] = item
		}
	}
	return result
}

func isDiagnosticSecretKey(key string) bool {
	for _, marker := range []string{"password", "token", "authorization", "secret", "credential", "accesskey", "serviceaccountkey"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func diagnosticLogFromInput(input DiagnosticLogInput, now time.Time) DiagnosticLog {
	scope := input.Scope
	if scope != "system" {
		scope = "tenant"
	}
	return DiagnosticLog{ID: newID(), TenantID: input.TenantID, Scope: scope, Level: normalizeDiagnosticLevel(input.Level), Component: strings.TrimSpace(input.Component), Operation: strings.TrimSpace(input.Operation), Message: strings.TrimSpace(input.Message), ClusterID: input.ClusterID, TaskID: input.TaskID, CommandID: input.CommandID, RequestID: input.RequestID, ErrorCode: input.ErrorCode, Status: input.Status, DurationMS: input.DurationMS, Details: redactDiagnosticDetails(input.Details), CreatedAt: now}
}

func diagnosticLogMatches(item DiagnosticLog, f DiagnosticLogFilter) bool {
	if f.Scope != "" && item.Scope != f.Scope || f.TenantID != "" && item.TenantID != f.TenantID || f.Level != "" && item.Level != f.Level || f.Component != "" && item.Component != f.Component || f.ClusterID != "" && item.ClusterID != f.ClusterID || f.TaskID != "" && item.TaskID != f.TaskID {
		return false
	}
	if !f.From.IsZero() && item.CreatedAt.Before(f.From) || !f.To.IsZero() && item.CreatedAt.After(f.To) {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	return q == "" || strings.Contains(strings.ToLower(item.Message+" "+item.Operation+" "+item.ErrorCode+" "+item.TaskID+" "+item.RequestID), q)
}

func paginateDiagnosticLogs(items []DiagnosticLog, f DiagnosticLogFilter) []DiagnosticLog {
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []DiagnosticLog{}
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
