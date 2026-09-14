package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (r *Router) diagnosticLogFilter(req *http.Request) (store.DiagnosticLogFilter, error) {
	q := req.URL.Query()
	user, ok := requestUser(req)
	filter := store.DiagnosticLogFilter{TenantID: q.Get("tenantId"), Scope: q.Get("scope"), Source: q.Get("source"), Level: q.Get("level"), Component: q.Get("component"), ClusterID: q.Get("clusterId"), TaskID: q.Get("taskId"), Query: strings.TrimSpace(q.Get("q"))}
	if filter.Source != "" && filter.Source != "platform" && filter.Source != "cluster" {
		return filter, errors.New("invalid diagnostic log source")
	}
	if filter.Source == "platform" {
		filter.ClusterID = ""
	}
	if filter.Source == "cluster" && filter.Scope == "system" {
		return filter, errors.New("system scope is only available for platform logs")
	}
	filter.Limit, _ = strconv.Atoi(q.Get("limit"))
	filter.Offset, _ = strconv.Atoi(q.Get("offset"))
	if filter.Limit <= 0 || filter.Limit > 5000 {
		filter.Limit = 200
	}
	if value := q.Get("from"); value != "" {
		filter.From, _ = time.Parse(time.RFC3339, value)
	}
	if value := q.Get("to"); value != "" {
		filter.To, _ = time.Parse(time.RFC3339, value)
	}
	if ok && !user.SystemAdmin {
		filter.TenantID = user.TenantID
		filter.Scope = "tenant"
	}
	if filter.Scope == "system" && (!ok || !user.SystemAdmin) {
		return filter, errors.New("system administrator permission is required")
	}
	if filter.ClusterID != "" {
		clusters, err := r.store.ListClusters()
		if err != nil {
			return filter, err
		}
		found := false
		for _, cluster := range clusters {
			if cluster.ID == filter.ClusterID && (user.SystemAdmin || cluster.TenantID == user.TenantID) && (filter.TenantID == "" || cluster.TenantID == filter.TenantID) {
				found = true
				break
			}
		}
		if !found {
			return filter, errors.New("cluster not found")
		}
	}
	return filter, nil
}

func (r *Router) listDiagnosticLogs(w http.ResponseWriter, req *http.Request) {
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "diagnostic_log_scope_forbidden", "message": err.Error()})
		return
	}
	items, err := r.store.ListDiagnosticLogs(filter)
	if err != nil {
		r.logger.Error("list diagnostic logs failed", "error", err)
		writeJSON(w, 500, map[string]any{"error": "list_diagnostic_logs_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items), "limit": filter.Limit, "offset": filter.Offset})
}

func (r *Router) exportDiagnosticLogs(w http.ResponseWriter, req *http.Request) {
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "diagnostic_log_scope_forbidden", "message": err.Error()})
		return
	}
	filter.Limit = 5000
	filter.Offset = 0
	items, err := r.store.ListDiagnosticLogs(filter)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "export_diagnostic_logs_failed"})
		return
	}
	location := time.UTC
	timezone := strings.TrimSpace(req.URL.Query().Get("timezone"))
	if timezone != "" {
		if requestedLocation, loadErr := time.LoadLocation(timezone); loadErr == nil {
			location = requestedLocation
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	name := "hypercdr-diagnostic-logs.log"
	if filter.Source != "" {
		name = "hypercdr-" + filter.Source + "-logs.log"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	fmt.Fprintf(w, "# HyperCDR Diagnostic Log Export\n# Source: %s\n# Time zone: %s\n# Exported at: %s\n# Records: %d\n\n", valueOrDefault(filter.Source, "all"), location.String(), time.Now().In(location).Format(time.RFC3339), len(items))
	for _, item := range items {
		fmt.Fprintf(w, "%s [%s] [%s] %s\n", item.EventAt.In(location).Format("2006-01-02 15:04:05.000 MST"), strings.ToUpper(item.Level), item.Component, item.Message)
		fields := []string{"scope=" + item.Scope}
		for _, field := range []struct{ key, value string }{
			{"tenant_id", item.TenantID}, {"cluster_id", item.ClusterID}, {"operation", item.Operation},
			{"status", item.Status}, {"error_code", item.ErrorCode}, {"task_id", item.TaskID},
			{"command_id", item.CommandID}, {"request_id", item.RequestID},
		} {
			if field.value != "" {
				fields = append(fields, field.key+"="+strconv.Quote(field.value))
			}
		}
		if item.DurationMS > 0 {
			fields = append(fields, fmt.Sprintf("duration_ms=%d", item.DurationMS))
		}
		fmt.Fprintf(w, "  Context: %s\n", strings.Join(fields, " "))
		if len(item.Details) > 0 {
			if details, marshalErr := json.MarshalIndent(item.Details, "    ", "  "); marshalErr == nil {
				fmt.Fprintf(w, "  Data:\n    %s\n", details)
			}
		}
		fmt.Fprintln(w)
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (r *Router) diagnosticLogSources(w http.ResponseWriter, req *http.Request) {
	user, ok := requestUser(req)
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "authentication_required"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_log_sources_failed"})
		return
	}
	items := []map[string]any{}
	for _, cluster := range clusters {
		if user.SystemAdmin || cluster.TenantID == user.TenantID {
			items = append(items, map[string]any{"id": cluster.ID, "tenantId": cluster.TenantID, "name": cluster.Name, "connectionStatus": cluster.ConnectionStatus})
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (r *Router) collectClusterLogs(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	var body struct {
		Component string    `json:"component"`
		Since     time.Time `json:"since"`
		TailLines int64     `json:"tailLines"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	allowed := map[string]bool{"comm-agent": true, "velero": true, "node-agent": true}
	if !allowed[body.Component] {
		writeJSON(w, 400, map[string]any{"error": "unsupported_log_component", "message": "Component must be comm-agent, velero, or node-agent."})
		return
	}
	if body.Since.IsZero() {
		body.Since = time.Now().UTC().Add(-30 * time.Minute)
	}
	if body.Since.Before(time.Now().UTC().Add(-24 * time.Hour)) {
		writeJSON(w, 400, map[string]any{"error": "log_range_too_large", "message": "Cluster log collection is limited to the last 24 hours."})
		return
	}
	if body.TailLines <= 0 || body.TailLines > clusterLogTailLines {
		body.TailLines = 1000
	}
	report, coverage, requestID, status, err := r.collectClusterLogsRange(clusterID, body.Component, body.Since, body.TailLines)
	if err != nil {
		writeJSON(w, status, map[string]any{"error": "log_collection_failed", "message": err.Error(), "requestId": requestID})
		return
	}
	writeJSON(w, 200, map[string]any{"requestId": requestID, "status": "completed", "count": len(report.Entries), "truncated": report.Truncated, "coverage": coverage, "message": "Cluster logs collected."})
}

func (r *Router) collectClusterLogsRange(clusterID, component string, since time.Time, tailLines int64) (protocol.LogReportPayload, store.ClusterLogCoverage, string, int, error) {
	r.logCollectMu.Lock()
	defer r.logCollectMu.Unlock()
	report, requestID, status, err := r.requestClusterLogs(clusterID, component, since, tailLines)
	if err != nil {
		return report, store.ClusterLogCoverage{}, requestID, status, err
	}
	coverage, err := r.recordClusterLogCoverage(clusterID, component, since, requestID, report)
	if err != nil {
		return report, store.ClusterLogCoverage{}, requestID, http.StatusInternalServerError, err
	}
	return report, coverage, requestID, status, nil
}

func (r *Router) requestClusterLogs(clusterID, component string, since time.Time, tailLines int64) (protocol.LogReportPayload, string, int, error) {
	conn, ok := r.hub.get(clusterID)
	if !ok {
		return protocol.LogReportPayload{}, "", 409, errors.New("Cluster is offline. Logs cannot be collected in real time.")
	}
	requestID := store.NewPublicID()
	waiter := make(chan protocol.LogReportPayload, 1)
	r.logRequestMu.Lock()
	r.logRequests[requestID] = waiter
	r.logRequestMu.Unlock()
	defer func() { r.logRequestMu.Lock(); delete(r.logRequests, requestID); r.logRequestMu.Unlock() }()
	message := protocol.Message[protocol.LogRequestPayload]{Version: protocol.Version, MessageID: store.NewPublicID(), MessageKind: protocol.MessageKindRequest, Type: protocol.MessagePlatformLogRequest, ClusterID: clusterID, Timestamp: time.Now().UTC(), Payload: protocol.LogRequestPayload{RequestID: requestID, Component: component, Since: since, TailLines: tailLines}}
	if err := conn.WriteJSON(message); err != nil {
		return protocol.LogReportPayload{}, requestID, 502, err
	}
	select {
	case report := <-waiter:
		if report.ErrorCode != "" {
			return report, requestID, 502, errors.New(report.Message)
		}
		return report, requestID, 200, nil
	case <-time.After(25 * time.Second):
		return protocol.LogReportPayload{}, requestID, 504, errors.New("The cluster agent did not respond to automatic log collection.")
	}
}
