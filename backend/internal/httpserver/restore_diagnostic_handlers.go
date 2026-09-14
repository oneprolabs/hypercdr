package httpserver

import (
	"encoding/json"
	"errors"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"slices"
	"strings"
	"time"
)

func (r *Router) getRestorePointContents(w http.ResponseWriter, req *http.Request) {
	point, ok, err := r.store.GetRestorePoint(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
		return
	}
	if !ok || !tenantVisible(req, point.TenantID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
		return
	}
	clusterID := point.SourceClusterID
	if !r.clusterVisible(req, clusterID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	if index, ok := restorePointContentIndex(point); ok && index.Status == "ready" && index.SchemaVersion >= restorePointContentIndexSchemaVersion {
		writeJSON(w, http.StatusOK, map[string]any{"restorePointId": point.ID, "veleroBackupName": point.VeleroBackupName, "clusterId": clusterID, "resources": index.Resources, "truncated": index.Truncated, "indexedAt": index.IndexedAt, "source": "index"})
		return
	}
	// Compatibility path for restore points created before content indexing
	// was introduced. The successful result is persisted, so subsequent opens
	// no longer depend on the source cluster or object storage.
	report, status, err := r.requestBackupContents(clusterID, point.VeleroBackupName, r.dataProtectionNamespaceForCluster(clusterID))
	if err != nil {
		r.persistRestorePointContentIndex(point, report, "failed", err.Error())
		writeJSON(w, status, map[string]any{"error": "backup_contents_unavailable", "message": err.Error(), "errorCode": report.ErrorCode})
		return
	}
	r.persistRestorePointContentIndex(point, report, "ready", "")
	writeJSON(w, http.StatusOK, map[string]any{"restorePointId": point.ID, "veleroBackupName": point.VeleroBackupName, "clusterId": clusterID, "resources": report.Resources, "truncated": report.Truncated, "indexedAt": time.Now().UTC(), "source": "indexed_now"})
}

type restorePointIndex struct {
	SchemaVersion    int                              `json:"schemaVersion,omitempty"`
	Status           string                           `json:"status"`
	Resources        []protocol.BackupResourceSummary `json:"resources"`
	Truncated        bool                             `json:"truncated,omitempty"`
	IndexedAt        time.Time                        `json:"indexedAt,omitempty"`
	GeneratorVersion string                           `json:"generatorVersion,omitempty"`
	LastError        string                           `json:"lastError,omitempty"`
	RetryAt          time.Time                        `json:"retryAt,omitempty"`
}

func readinessExpectationsFromCatalog(resources []protocol.BackupResourceSummary, sourceNamespaces []string) (bool, []string) {
	namespaces := map[string]struct{}{}
	for _, namespace := range sourceNamespaces {
		namespaces[strings.TrimSpace(namespace)] = struct{}{}
	}
	runtimeExpected := false
	pvcs := []string{}
	seenPVC := map[string]struct{}{}
	for _, resource := range resources {
		if resource.ClusterScoped {
			continue
		}
		if len(namespaces) > 0 {
			if _, included := namespaces[resource.Namespace]; !included {
				continue
			}
		}
		switch strings.ToLower(strings.TrimSpace(resource.Kind)) {
		case "deployment", "statefulset", "daemonset", "deploymentconfig", "replicaset", "job", "cronjob":
			runtimeExpected = true
		case "persistentvolumeclaim":
			if _, exists := seenPVC[resource.Name]; !exists && resource.Name != "" {
				seenPVC[resource.Name] = struct{}{}
				pvcs = append(pvcs, resource.Name)
			}
		}
	}
	slices.Sort(pvcs)
	return runtimeExpected, pvcs
}

func storageClassesFromCatalog(resources []protocol.BackupResourceSummary, sourceNamespaces []string) []string {
	namespaces := map[string]struct{}{}
	for _, namespace := range sourceNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespaces[namespace] = struct{}{}
		}
	}
	classes := map[string]struct{}{}
	for _, resource := range resources {
		if resource.ClusterScoped {
			continue
		}
		if len(namespaces) > 0 {
			if _, included := namespaces[resource.Namespace]; !included {
				continue
			}
		}
		for _, storageClass := range resource.StorageClasses {
			storageClass = strings.TrimSpace(storageClass)
			if storageClass != "" {
				classes[storageClass] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(classes))
	for storageClass := range classes {
		result = append(result, storageClass)
	}
	slices.Sort(result)
	return result
}

// Version 4 guarantees that cached catalogs were generated after JSON numeric
// Service ports were decoded correctly by comm-agent.
// Earlier catalogs can be structurally valid but cannot drive NodePort mapping UI.
const restorePointContentIndexSchemaVersion = 4

func restorePointContentIndex(point store.RestorePoint) (restorePointIndex, bool) {
	value, ok := point.Metadata["contentIndex"]
	if !ok {
		return restorePointIndex{}, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return restorePointIndex{}, false
	}
	var index restorePointIndex
	if json.Unmarshal(raw, &index) != nil || index.Status == "" {
		return restorePointIndex{}, false
	}
	index.Resources = normalizeBackupResourceSummaries(index.Resources)
	return index, true
}

// normalizeBackupResourceSummaries repairs catalogs produced by comm-agent
// versions that interpreted Velero's canonical scope directory as the
// resource name (for example namespaces.backupactions.actions.kio.kasten.io).
// It also removes API-version representations of the same Kubernetes object.
func normalizeBackupResourceSummaries(resources []protocol.BackupResourceSummary) []protocol.BackupResourceSummary {
	result := make([]protocol.BackupResourceSummary, 0, len(resources))
	seen := map[string]struct{}{}
	for _, item := range resources {
		if item.Resource == "namespaces" || item.Resource == "cluster" {
			apiGroup := ""
			if group, _, found := strings.Cut(item.APIVersion, "/"); found {
				apiGroup = group
			}
			if apiGroup == "" {
				item.Resource = item.Group
				item.Group = ""
			} else if suffix := "." + apiGroup; strings.HasSuffix(item.Group, suffix) {
				item.Resource = strings.TrimSuffix(item.Group, suffix)
				item.Group = apiGroup
			}
		}
		identity := strings.Join([]string{item.APIVersion, item.Kind, item.Namespace, item.Name}, "\x00")
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, item)
	}
	return result
}

func (r *Router) persistRestorePointContentIndex(point store.RestorePoint, report protocol.BackupContentReportPayload, status string, message string) {
	index := restorePointIndex{SchemaVersion: restorePointContentIndexSchemaVersion, Status: status, Resources: normalizeBackupResourceSummaries(report.Resources), Truncated: report.Truncated, GeneratorVersion: r.clusterAgentVersion(point.SourceClusterID), LastError: message}
	if status == "ready" {
		index.IndexedAt = time.Now().UTC()
		index.LastError = ""
	} else {
		index.RetryAt = time.Now().UTC().Add(5 * time.Minute)
	}
	if _, _, err := r.store.UpdateRestorePointState(store.RestorePointStateInput{ID: point.ID, Status: point.Status, Metadata: map[string]any{"contentIndex": index}}); err != nil {
		r.logger.Error("failed to persist restore point content index", "restore_point_id", point.ID, "error", err)
	}
}

func (r *Router) clusterAgentVersion(clusterID string) string {
	clusters, err := r.store.ListClusters()
	if err != nil {
		return ""
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return cluster.AgentVersion
		}
	}
	return ""
}

func (r *Router) scheduleRestorePointContentIndex(point store.RestorePoint) {
	if point.ID == "" || point.Status != "available" {
		return
	}
	if index, ok := restorePointContentIndex(point); ok && index.Status == "ready" && index.SchemaVersion >= restorePointContentIndexSchemaVersion {
		currentVersion := r.clusterAgentVersion(point.SourceClusterID)
		if currentVersion == "" || index.GeneratorVersion == currentVersion {
			return
		}
	} else if ok && index.Status == "failed" && time.Now().UTC().Before(index.RetryAt) {
		return
	}
	r.contentIndexMu.Lock()
	if _, running := r.contentIndexing[point.ID]; running {
		r.contentIndexMu.Unlock()
		return
	}
	r.contentIndexing[point.ID] = struct{}{}
	r.contentIndexMu.Unlock()
	go func() {
		defer func() {
			r.contentIndexMu.Lock()
			delete(r.contentIndexing, point.ID)
			r.contentIndexMu.Unlock()
		}()
		r.contentIndexSlots <- struct{}{}
		defer func() { <-r.contentIndexSlots }()
		delays := []time.Duration{0, 5 * time.Second, 30 * time.Second}
		for attempt, delay := range delays {
			if delay > 0 {
				time.Sleep(delay)
			}
			report, _, err := r.requestBackupContents(point.SourceClusterID, point.VeleroBackupName, r.dataProtectionNamespaceForCluster(point.SourceClusterID))
			if err == nil {
				r.persistRestorePointContentIndex(point, report, "ready", "")
				r.logger.Info("restore point content index created", "restore_point_id", point.ID, "resources", len(report.Resources), "attempt", attempt+1)
				return
			}
			r.persistRestorePointContentIndex(point, report, "failed", err.Error())
			r.logger.Warn("restore point content indexing failed", "restore_point_id", point.ID, "attempt", attempt+1, "error", err)
		}
	}()
}

func (r *Router) requestBackupContents(clusterID string, backupName string, veleroNamespace string) (protocol.BackupContentReportPayload, int, error) {
	conn, ok := r.hub.get(clusterID)
	if !ok {
		return protocol.BackupContentReportPayload{}, http.StatusConflict, errors.New("Cluster is offline. Restore point contents cannot be inspected.")
	}
	if strings.TrimSpace(veleroNamespace) == "" {
		veleroNamespace = "hypercdr-agent"
	}
	requestID := store.NewPublicID()
	waiter := make(chan protocol.BackupContentReportPayload, 1)
	r.backupContentRequestMu.Lock()
	r.backupContentRequests[requestID] = waiter
	r.backupContentRequestMu.Unlock()
	defer func() {
		r.backupContentRequestMu.Lock()
		delete(r.backupContentRequests, requestID)
		r.backupContentRequestMu.Unlock()
	}()
	message := protocol.Message[protocol.BackupContentRequestPayload]{Version: protocol.Version, MessageID: store.NewPublicID(), MessageKind: protocol.MessageKindRequest, Type: protocol.MessagePlatformBackupContentRequest, ClusterID: clusterID, Timestamp: time.Now().UTC(), Payload: protocol.BackupContentRequestPayload{RequestID: requestID, VeleroBackupName: backupName, VeleroNamespace: veleroNamespace}}
	if err := conn.WriteJSON(message); err != nil {
		return protocol.BackupContentReportPayload{}, http.StatusBadGateway, err
	}
	select {
	case report := <-waiter:
		if report.ErrorCode != "" {
			return report, http.StatusBadGateway, errors.New(report.Message)
		}
		return report, http.StatusOK, nil
	case <-time.After(50 * time.Second):
		return protocol.BackupContentReportPayload{}, http.StatusGatewayTimeout, errors.New("The cluster agent did not return restore point contents in time.")
	}
}

func (r *Router) recordClusterLogCoverage(clusterID, component string, requestedSince time.Time, requestID string, report protocol.LogReportPayload) (store.ClusterLogCoverage, error) {
	clusters, err := r.store.ListClusters()
	if err != nil {
		return store.ClusterLogCoverage{}, err
	}
	tenantID := ""
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			tenantID = cluster.TenantID
			break
		}
	}
	if tenantID == "" {
		return store.ClusterLogCoverage{}, errors.New("cluster not found")
	}
	coveredFrom := requestedSince.UTC()
	if report.Truncated && len(report.Entries) > 0 {
		coveredFrom = report.Entries[0].Timestamp.UTC()
		for _, entry := range report.Entries[1:] {
			if entry.Timestamp.Before(coveredFrom) {
				coveredFrom = entry.Timestamp.UTC()
			}
		}
	}
	now := time.Now().UTC()
	return r.store.UpsertClusterLogCoverage(store.ClusterLogCoverageInput{ClusterID: clusterID, TenantID: tenantID, Component: component, RequestID: requestID, CoveredFrom: coveredFrom, CoveredTo: now, CollectedAt: now, EntryCount: len(report.Entries), Truncated: report.Truncated})
}

func (r *Router) searchClusterLogs(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	var body struct {
		Component string    `json:"component"`
		From      time.Time `json:"from"`
		To        time.Time `json:"to"`
	}
	if decodeJSON(req, &body) != nil || !map[string]bool{"comm-agent": true, "velero": true, "node-agent": true}[body.Component] {
		writeJSON(w, 400, map[string]any{"error": "invalid_log_search"})
		return
	}
	now := time.Now().UTC()
	if body.To.IsZero() || body.To.After(now) {
		body.To = now
	}
	if body.From.IsZero() || !body.From.Before(body.To) {
		body.From = body.To.Add(-time.Hour)
	}
	coverage, found, err := r.store.GetClusterLogCoverage(clusterID, body.Component)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "log_coverage_read_failed"})
		return
	}
	if found && !body.From.Before(coverage.CoveredFrom) && !body.To.After(coverage.CoveredTo) {
		writeJSON(w, 200, map[string]any{"status": "ready", "collected": false, "coverageComplete": true, "coverage": coverage})
		return
	}
	collectFrom := clusterLogCollectFrom(body.From, now)
	report, coverage, requestID, status, collectErr := r.collectClusterLogsRange(clusterID, body.Component, collectFrom, clusterLogTailLines)
	if collectErr != nil {
		writeJSON(w, status, map[string]any{"error": "automatic_log_collection_failed", "message": collectErr.Error(), "requestId": requestID})
		return
	}
	complete := !body.From.Before(coverage.CoveredFrom) && !body.To.After(coverage.CoveredTo)
	result := map[string]any{"status": "ready", "collected": true, "count": len(report.Entries), "truncated": report.Truncated, "coverageComplete": complete, "coverage": coverage}
	if !complete {
		result["message"] = "The platform collected all logs still retained by the cluster. Earlier logs are no longer available."
	}
	writeJSON(w, 200, result)
}

func clusterLogCollectFrom(requestedFrom, now time.Time) time.Time {
	earliestCollectable := now.Add(-24 * time.Hour)
	if requestedFrom.Before(earliestCollectable) {
		return earliestCollectable
	}
	return requestedFrom
}

func sanitizeDiagnosticMessage(message string) string {
	message = strings.TrimSpace(message)
	var object map[string]any
	if json.Unmarshal([]byte(message), &object) == nil {
		for key := range object {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") || strings.Contains(normalized, "accesskey") {
				object[key] = "[REDACTED]"
			}
		}
		if value, err := json.Marshal(object); err == nil {
			return string(value)
		}
	}
	if index := strings.Index(strings.ToLower(message), "bearer "); index >= 0 {
		return message[:index] + "Bearer [REDACTED]"
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func decodeJSON(req *http.Request, target any) error {
	defer req.Body.Close()
	return json.NewDecoder(req.Body).Decode(target)
}
