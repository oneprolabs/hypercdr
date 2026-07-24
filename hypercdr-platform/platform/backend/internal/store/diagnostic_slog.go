package store

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// DiagnosticSlogHandler mirrors warning and error records into the structured
// diagnostic index while preserving the normal stdout handler.
type DiagnosticSlogHandler struct {
	inner     slog.Handler
	store     Store
	component string
	attrs     []slog.Attr
	group     string
}

func NewDiagnosticSlogHandler(inner slog.Handler, repo Store, component string) slog.Handler {
	return &DiagnosticSlogHandler{inner: inner, store: repo, component: component}
}
func (h *DiagnosticSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}
func (h *DiagnosticSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	innerErr := h.inner.Handle(ctx, record)
	if record.Level < slog.LevelWarn {
		return innerErr
	}
	values := map[string]any{}
	for _, attr := range h.attrs {
		appendSlogAttr(values, attr)
	}
	record.Attrs(func(attr slog.Attr) bool { appendSlogAttr(values, attr); return true })
	input := DiagnosticLogInput{Scope: "system", Level: normalizeDiagnosticLevel(record.Level.String()), Component: h.component, Operation: stringValue(values, "operation"), Message: record.Message, ClusterID: stringValue(values, "cluster_id"), TaskID: stringValue(values, "task_id"), CommandID: stringValue(values, "command_id"), RequestID: stringValue(values, "request_id"), ErrorCode: stringValue(values, "error_code"), Details: redactDiagnosticDetails(values)}
	input.TenantID = stringValue(values, "tenant_id")
	if input.TenantID == "" && input.TaskID != "" {
		if tasks, err := h.store.ListTasks(""); err == nil {
			for _, task := range tasks {
				if task.ID == input.TaskID {
					input.TenantID = task.TenantID
					if input.ClusterID == "" {
						input.ClusterID = task.ClusterID
					}
					break
				}
			}
		}
	}
	if input.TenantID == "" && input.ClusterID != "" {
		if clusters, err := h.store.ListClusters(); err == nil {
			for _, cluster := range clusters {
				if cluster.ID == input.ClusterID {
					input.TenantID = cluster.TenantID
					break
				}
			}
		}
	}
	if input.TenantID != "" {
		input.Scope = "tenant"
	}
	_, storeErr := h.store.CreateDiagnosticLog(input)
	if innerErr != nil {
		return innerErr
	}
	return storeErr
}
func (h *DiagnosticSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.inner = h.inner.WithAttrs(attrs)
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}
func (h *DiagnosticSlogHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.inner = h.inner.WithGroup(name)
	clone.group = strings.Trim(strings.Join([]string{h.group, name}, "."), ".")
	return &clone
}
func appendSlogAttr(values map[string]any, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		for _, nested := range attr.Value.Group() {
			appendSlogAttr(values, nested)
		}
		return
	}
	if attr.Value.Kind() == slog.KindAny {
		if err, ok := attr.Value.Any().(error); ok {
			values[attr.Key] = err.Error()
			return
		}
	}
	values[attr.Key] = attr.Value.Any()
}
func stringValue(values map[string]any, key string) string {
	if value, ok := values[key]; ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}
