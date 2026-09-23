package httpserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

func (r *Router) componentTarget(ctx context.Context, component string) (store.ComponentRelease, error) {
	if r.cfg.ReleaseManifestPath != "" {
		return localManifestComponent(r.cfg.ReleaseManifestPath, component)
	}
	releases, err := r.store.ListPlatformReleases()
	if err != nil {
		return store.ComponentRelease{}, err
	}
	for _, release := range releases {
		if release.Status != "active" {
			continue
		}
		artifact, ok := release.ComponentManifest[component]
		if !ok || artifact.Image == "" || artifact.ImageDigest == "" {
			return store.ComponentRelease{}, fmt.Errorf("active platform release %s has no %s manifest entry", release.Version, component)
		}
		return store.ComponentRelease{ID: release.ID, TenantID: release.TenantID, Component: component, Version: artifact.Version, Image: artifact.Image, ImageDigest: artifact.ImageDigest, Status: "active", PublishedBy: release.PublishedBy, PublishedAt: release.PublishedAt, CreatedAt: release.CreatedAt, UpdatedAt: release.UpdatedAt}, nil
	}
	return store.ComponentRelease{}, fmt.Errorf("no active platform release manifest is available")
}

func validImageDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256:") {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func immutableImageReference(image, digest string) string {
	image, digest = strings.TrimSpace(image), strings.TrimSpace(digest)
	if image == "" || !validImageDigest(digest) {
		return image
	}
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	lastSlash := strings.LastIndexByte(image, '/')
	if colon := strings.LastIndexByte(image, ':'); colon > lastSlash {
		image = image[:colon]
	}
	return image + "@" + digest
}

func (r *Router) listClusters(w http.ResponseWriter, req *http.Request) {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to list clusters", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	visibleClusters := clusters[:0]
	for _, item := range clusters {
		if tenantVisible(req, item.TenantID) {
			visibleClusters = append(visibleClusters, item)
		}
	}
	clusters = visibleClusters
	if req.URL.Query().Get("view") == "summary" {
		type clusterSummary struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		}
		items := make([]clusterSummary, 0, len(clusters))
		for _, item := range clusters {
			items = append(items, clusterSummary{ID: item.ID, IsDefault: item.IsDefault})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	agentTarget, agentTargetErr := r.componentTarget(req.Context(), "comm-agent")
	if agentTargetErr != nil {
		r.logger.Warn("failed to load comm-agent target release", "error", agentTargetErr)
	}
	veleroTarget, veleroTargetErr := r.componentTarget(req.Context(), "velero")
	if veleroTargetErr != nil {
		r.logger.Warn("failed to load velero target release", "error", veleroTargetErr)
	}
	oadpAgentTarget, oadpAgentTargetErr := r.componentTarget(req.Context(), "oadp-comm-agent")
	if oadpAgentTargetErr != nil {
		r.logger.Warn("failed to load oadp-comm-agent target release", "error", oadpAgentTargetErr)
	}
	oadpVeleroTarget, oadpVeleroTargetErr := r.componentTarget(req.Context(), "oadp-velero")
	if oadpVeleroTargetErr != nil {
		r.logger.Warn("failed to load OADP Velero target release", "error", oadpVeleroTargetErr)
	}
	upgradeTaskFilter := store.TaskFilter{
		Types:    []string{"agent-upgrade", "velero-upgrade"},
		Statuses: []string{"queued", "dispatched", "accepted", "running", "syncing", "finalizing"},
		Limit:    100,
		Summary:  true,
	}
	if user, ok := requestUser(req); ok && !user.SystemAdmin {
		upgradeTaskFilter.TenantID = user.TenantID
	}
	upgradeTasks, taskErr := r.store.ListTasksFiltered(upgradeTaskFilter)
	if taskErr != nil {
		r.logger.Warn("failed to load agent upgrade status", "error", taskErr)
	}
	for i := range clusters {
		clusterAgentTarget, clusterVeleroTarget := agentTarget, veleroTarget
		if normalizedClusterTypeForRouting(clusters[i].ClusterType) == "openshift" {
			clusterAgentTarget, clusterVeleroTarget = oadpAgentTarget, oadpVeleroTarget
		}
		clusters[i].RestoreCachePolicy = deriveRestoreCachePolicy(clusters[i].StorageClasses)
		if r.hub.has(clusters[i].ID) {
			clusters[i].ConnectionStatus = "online"
		} else {
			clusters[i].ConnectionStatus = "offline"
		}
		clusters[i].LatestAgentVersion = clusterAgentTarget.Version
		clusters[i].LatestAgentImage = clusterAgentTarget.Image
		clusters[i].LatestAgentImageDigest = clusterAgentTarget.ImageDigest
		clusters[i].AgentUpgradeAvailable = agentUpgradeIsAvailable(clusters[i], clusterAgentTarget)
		clusters[i].LatestVeleroVersion = clusterVeleroTarget.Version
		clusters[i].LatestVeleroImage = clusterVeleroTarget.Image
		clusters[i].LatestVeleroImageDigest = clusterVeleroTarget.ImageDigest
		if strings.TrimSpace(clusters[i].VeleroVersion) == "" &&
			strings.TrimSpace(clusters[i].VeleroImage) == immutableImageReference(clusterVeleroTarget.Image, clusterVeleroTarget.ImageDigest) {
			// OADP relatedImages are intentionally deployed by digest, so the
			// runtime cannot recover a human-readable tag from the Pod spec.
			// The immutable reference proves which qualified release is running.
			clusters[i].VeleroVersion = clusterVeleroTarget.Version
		}
		clusters[i].VeleroUpgradeAvailable = veleroUpgradeIsAvailable(clusters[i], clusterVeleroTarget)
		for _, task := range upgradeTasks {
			if task.ClusterID != clusters[i].ID || isTerminalTaskStatus(task.Status) {
				continue
			}
			if task.Type == "agent-upgrade" {
				clusters[i].AgentUpgradeStatus = "upgrading"
				clusters[i].AgentUpgradeProgress = task.Progress
			}
			if task.Type == "velero-upgrade" {
				clusters[i].VeleroUpgradeStatus = "upgrading"
				clusters[i].VeleroUpgradeProgress = task.Progress
			}
		}
		if clusters[i].AgentUpgradeAvailable && clusters[i].AgentUpgradeStatus == "" {
			clusters[i].AgentUpgradeStatus = "available"
		}
		if clusters[i].VeleroUpgradeAvailable && clusters[i].VeleroUpgradeStatus == "" {
			clusters[i].VeleroUpgradeStatus = "available"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": nonNilSlice(clusters),
	})
}

func deriveRestoreCachePolicy(storageClasses []store.ClusterStorageClass) store.RestoreCachePolicy {
	policy := store.RestoreCachePolicy{Mode: "automatic", ResidentThresholdMB: 1024, CacheLimitMB: 5120}
	for _, storageClass := range storageClasses {
		if !storageClass.Default {
			continue
		}
		if strings.TrimSpace(storageClass.Provisioner) == "" {
			policy.Reason = "Default StorageClass has no dynamic provisioner. Velero uses data mover ephemeral storage."
			return policy
		}
		if !strings.EqualFold(firstNonEmptyString(storageClass.ReclaimPolicy, "Delete"), "Delete") {
			policy.Reason = "Default StorageClass does not use reclaimPolicy Delete. Velero uses data mover ephemeral storage."
			return policy
		}
		policy.Enabled = true
		policy.StorageClass = storageClass.Name
		policy.Reason = "A compatible default StorageClass was selected automatically."
		return policy
	}
	policy.Reason = "No compatible default StorageClass was detected. Velero uses data mover ephemeral storage."
	return policy
}

func upgradeTargetIsNewer(currentVersion, targetVersion string, digestMismatch bool) bool {
	comparison, comparable := compareNumericVersions(targetVersion, currentVersion)
	if !comparable {
		return digestMismatch
	}
	return comparison > 0 || comparison == 0 && digestMismatch
}

func agentUpgradeIsAvailable(cluster store.Cluster, target store.ComponentRelease) bool {
	currentImage := strings.TrimSpace(cluster.AgentImage)
	targetImage := strings.TrimSpace(target.Image)
	imageMismatch := currentImage != "" && targetImage != "" && currentImage != targetImage
	return upgradeTargetIsNewer(cluster.AgentVersion, target.Version, imageMismatch)
}

func veleroUpgradeIsAvailable(cluster store.Cluster, target store.ComponentRelease) bool {
	identityMatches := strings.TrimSpace(cluster.VeleroImage) == strings.TrimSpace(target.Image) &&
		strings.TrimSpace(cluster.VeleroVersion) == strings.TrimSpace(target.Version)
	immutableIdentityMatches := strings.TrimSpace(cluster.VeleroImage) == immutableImageReference(target.Image, target.ImageDigest)
	digestMatches := strings.TrimSpace(target.ImageDigest) != "" &&
		cluster.VeleroImageDigest == target.ImageDigest && cluster.VeleroNodeAgentImageDigest == target.ImageDigest
	fullyReady := cluster.VeleroServerReady && cluster.VeleroNodeAgentDesired > 0 &&
		cluster.VeleroNodeAgentReady == cluster.VeleroNodeAgentDesired
	if (identityMatches || immutableIdentityMatches || digestMatches) && fullyReady {
		return false
	}
	currentImage := strings.TrimSpace(cluster.VeleroImage)
	targetImage := strings.TrimSpace(target.Image)
	imageMismatch := currentImage != "" && targetImage != "" && currentImage != targetImage
	return upgradeTargetIsNewer(cluster.VeleroVersion, target.Version, imageMismatch)
}

func compareNumericVersions(left, right string) (int, bool) {
	leftParts := numericVersionParts(left)
	rightParts := numericVersionParts(right)
	if len(leftParts) == 0 || len(rightParts) == 0 {
		return 0, false
	}
	length := max(len(leftParts), len(rightParts))
	for i := 0; i < length; i++ {
		leftPart, rightPart := 0, 0
		if i < len(leftParts) {
			leftPart = leftParts[i]
		}
		if i < len(rightParts) {
			rightPart = rightParts[i]
		}
		if leftPart < rightPart {
			return -1, true
		}
		if leftPart > rightPart {
			return 1, true
		}
	}
	return 0, true
}

func numericVersionParts(version string) []int {
	var parts []int
	for index := 0; index < len(version); {
		if version[index] < '0' || version[index] > '9' {
			index++
			continue
		}
		value := 0
		for index < len(version) && version[index] >= '0' && version[index] <= '9' {
			value = value*10 + int(version[index]-'0')
			index++
		}
		parts = append(parts, value)
	}
	return parts
}

func isTerminalTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func agentUpgradeTargetMatches(cluster store.Cluster, expectedImage, expectedVersion, expectedDigest string) bool {
	expectedImage = strings.TrimSpace(expectedImage)
	expectedVersion = strings.TrimSpace(expectedVersion)
	expectedDigest = strings.TrimSpace(expectedDigest)

	// A registry manifest digest and the runtime image ID reported by Kubernetes
	// are both valid sha256 values, but they identify different objects and are
	// not guaranteed to be equal. Prefer the immutable upgrade identity carried
	// by the running agent itself: its configured image and build version.
	identityMatches := expectedImage != "" && strings.TrimSpace(cluster.AgentImage) == expectedImage &&
		(expectedVersion == "" || strings.TrimSpace(cluster.AgentVersion) == expectedVersion)
	digestMatches := expectedDigest != "" && strings.TrimSpace(cluster.AgentImageDigest) == expectedDigest
	hasRuntimeIdentity := strings.TrimSpace(cluster.AgentImage) != "" && (expectedVersion == "" || strings.TrimSpace(cluster.AgentVersion) != "")
	if hasRuntimeIdentity {
		return identityMatches
	}
	return digestMatches
}

func (r *Router) completeAgentUpgradeAfterHeartbeat(cluster store.Cluster) {
	tasks, err := r.store.ListTasks(cluster.ID)
	if err != nil {
		r.logger.Warn("failed to reconcile agent upgrade after heartbeat", "cluster_id", cluster.ID, "error", err)
		return
	}
	for _, task := range tasks {
		if task.Type != "agent-upgrade" || isTerminalTaskStatus(task.Status) {
			continue
		}
		expectedDigest := strings.TrimSpace(stringPayload(task.Payload, "expectedDigest"))
		expectedImage := strings.TrimSpace(stringPayload(task.Payload, "image"))
		expectedVersion := strings.TrimSpace(stringPayload(task.Payload, "version"))
		if !agentUpgradeTargetMatches(cluster, expectedImage, expectedVersion, expectedDigest) {
			continue
		}
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: task.ID, Status: "succeeded", Progress: 100, MarkDone: true,
		}); err != nil {
			r.logger.Warn("failed to complete verified agent upgrade", "cluster_id", cluster.ID, "task_id", task.ID, "error", err)
			continue
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{
			TaskID: task.ID, Level: "info", Reason: "completed",
			Message: "new agent reconnected and reported the expected image and version",
			Payload: map[string]any{"image": cluster.AgentImage, "digest": cluster.AgentImageDigest, "version": cluster.AgentVersion},
		})
	}
}

func (r *Router) completeVeleroUpgradeAfterHeartbeat(cluster store.Cluster) {
	tasks, err := r.store.ListTasks(cluster.ID)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.Type != "velero-upgrade" || isTerminalTaskStatus(task.Status) {
			continue
		}
		expectedDigest := strings.TrimSpace(stringPayload(task.Payload, "expectedDigest"))
		expectedImage := strings.TrimSpace(stringPayload(task.Payload, "image"))
		expectedVersion := strings.TrimSpace(stringPayload(task.Payload, "version"))
		allNodesReady := cluster.VeleroNodeAgentDesired > 0 && cluster.VeleroNodeAgentReady == cluster.VeleroNodeAgentDesired
		identityMatches := expectedImage != "" && strings.TrimSpace(cluster.VeleroImage) == expectedImage &&
			(expectedVersion == "" || strings.TrimSpace(cluster.VeleroVersion) == expectedVersion)
		digestMatches := expectedDigest != "" && cluster.VeleroImageDigest == expectedDigest && cluster.VeleroNodeAgentImageDigest == expectedDigest
		// Velero reports its upstream binary version (for example v1.18.2), while
		// HyperCDR release tags may carry a build suffix (v1.18.2-hcdr.1). The
		// immutable server and all-node image digests are authoritative when the
		// reported version intentionally omits that packaging suffix.
		verified := identityMatches || digestMatches
		if !verified || !cluster.VeleroServerReady || !allNodesReady {
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "succeeded", Progress: 100, MarkDone: true})
		_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "completed", Message: "velero server and all scheduled node agents report the expected image and version"})
	}
}

func (r *Router) resolveImageDigest(ctx context.Context, image string) (string, error) {
	image = strings.TrimSpace(image)
	now := time.Now()
	r.imageDigestMu.Lock()
	if r.imageDigests == nil {
		r.imageDigests = map[string]imageDigestCacheEntry{}
	}
	cached, ok := r.imageDigests[image]
	if ok && cached.Digest != "" && cached.ExpiresAt.After(now) {
		r.imageDigestMu.Unlock()
		return cached.Digest, nil
	}
	r.imageDigestMu.Unlock()

	registry, repository, reference, ok := splitContainerImage(image)
	if !ok {
		return "", fmt.Errorf("invalid image reference %q", image)
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // nolint:gosec -- lab Harbor commonly uses an internal CA.
	}
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, reference)
	digest, err := resolveRegistryManifestDigest(ctx, client, manifestURL, repository)
	if err != nil {
		return "", err
	}
	r.imageDigestMu.Lock()
	r.imageDigests[image] = imageDigestCacheEntry{Digest: digest, ExpiresAt: now.Add(imageDigestTTL)}
	r.imageDigestMu.Unlock()
	return digest, nil
}

func resolveRegistryManifestDigest(ctx context.Context, client *http.Client, manifestURL, repository string) (string, error) {
	// Do not offer index and single-manifest media types in one request. Some
	// registries negotiate a translated Docker representation when they are
	// mixed, so the same immutable OCI index can receive different header
	// digests between candidate registration and activation.
	type manifestMediaGroup struct {
		accept  string
		allowed []string
	}
	acceptGroups := []manifestMediaGroup{
		{
			accept:  "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json",
			allowed: []string{"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"},
		},
		{
			accept:  "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			allowed: []string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"},
		},
	}
	var lastStatus string
	for _, group := range acceptGroups {
		manifestResp, err := registryRequest(ctx, client, http.MethodHead, manifestURL, repository, group.accept)
		if err != nil {
			return "", err
		}
		lastStatus = manifestResp.Status
		digest := strings.TrimSpace(manifestResp.Header.Get("Docker-Content-Digest"))
		contentType := strings.TrimSpace(strings.Split(manifestResp.Header.Get("Content-Type"), ";")[0])
		_ = manifestResp.Body.Close()
		if manifestResp.StatusCode < http.StatusMultipleChoices && digest != "" && slices.Contains(group.allowed, contentType) {
			return digest, nil
		}
	}
	if lastStatus != "" {
		return "", fmt.Errorf("registry manifest request failed: %s", lastStatus)
	}
	return "", errors.New("registry manifest digest is empty")
}

var registryAuthParameterPattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)="([^"]*)"`)

func registryRequest(ctx context.Context, client *http.Client, method, targetURL, repository, accept string) (*http.Response, error) {
	request := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, targetURL, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return client.Do(req)
	}

	resp, err := request("")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	challenge := strings.TrimSpace(resp.Header.Get("WWW-Authenticate"))
	_ = resp.Body.Close()
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return nil, fmt.Errorf("registry authentication challenge is unsupported")
	}
	params := map[string]string{}
	for _, match := range registryAuthParameterPattern.FindAllStringSubmatch(challenge, -1) {
		params[strings.ToLower(match[1])] = match[2]
	}
	realm := strings.TrimSpace(params["realm"])
	if realm == "" {
		return nil, errors.New("registry authentication realm is missing")
	}
	tokenURL, err := url.Parse(realm)
	if err != nil {
		return nil, fmt.Errorf("invalid registry authentication realm: %w", err)
	}
	query := tokenURL.Query()
	if service := strings.TrimSpace(params["service"]); service != "" {
		query.Set("service", service)
	}
	scope := strings.TrimSpace(params["scope"])
	if scope == "" {
		scope = "repository:" + repository + ":pull"
	}
	query.Set("scope", scope)
	tokenURL.RawQuery = query.Encode()
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return nil, err
	}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return nil, err
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("registry token request failed: %s", tokenResp.Status)
	}
	var tokenBody struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenBody); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(tokenBody.Token)
	if token == "" {
		token = strings.TrimSpace(tokenBody.AccessToken)
	}
	if token == "" {
		return nil, errors.New("registry token is empty")
	}
	return request(token)
}

func splitContainerImage(image string) (registry string, repository string, reference string, ok bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", "", "", false
	}
	if strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		return "", "", "", false
	}
	firstSlash := strings.IndexByte(image, '/')
	if firstSlash <= 0 || firstSlash == len(image)-1 {
		return "", "", "", false
	}
	registry = image[:firstSlash]
	remainder := image[firstSlash+1:]
	reference = "latest"
	if at := strings.LastIndex(remainder, "@"); at >= 0 {
		repository = remainder[:at]
		reference = remainder[at+1:]
	} else if colon := strings.LastIndex(remainder, ":"); colon >= 0 && colon > strings.LastIndex(remainder, "/") {
		repository = remainder[:colon]
		reference = remainder[colon+1:]
	} else {
		repository = remainder
	}
	return registry, repository, reference, registry != "" && repository != "" && reference != ""
}

func imageVersion(image string) string {
	_, _, reference, ok := splitContainerImage(image)
	if !ok {
		return strings.TrimSpace(image)
	}
	if strings.HasPrefix(reference, "sha256:") && len(reference) > 19 {
		return "sha256:" + reference[7:19]
	}
	return reference
}

func (r *Router) updateCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	var input store.ClusterUpdateInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	input.ID = clusterID
	cluster, ok, err := r.store.UpdateCluster(input)
	if err != nil {
		if errors.Is(err, store.ErrDefaultClusterRequired) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "default_cluster_required", "message": err.Error()})
			return
		}
		r.logger.Error("failed to update cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (r *Router) setDefaultCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	cluster, ok, err := r.store.SetDefaultCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to set default cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "set_default_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (r *Router) requestClusterInventory(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	var body struct {
		RequestID                  string `json:"requestId"`
		Scope                      string `json:"scope"`
		Namespace                  string `json:"namespace"`
		IncludeDetails             bool   `json:"includeDetails"`
		Reason                     string `json:"reason"`
		IncludeRecentVeleroObjects bool   `json:"includeRecentVeleroObjects"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to list clusters for inventory request", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	found := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	if body.RequestID == "" {
		body.RequestID = store.NewPublicID()
	}
	if body.Scope == "" {
		body.Scope = "summary"
	}
	// The UI-facing name describes the user action; the agent protocol uses
	// capabilities for the scoped, dynamic API object-count scan.
	if body.Scope == "namespaceResources" {
		body.Scope = "capabilities"
	}
	if body.Reason == "" {
		body.Reason = "user_refresh"
	}
	now := time.Now().UTC()
	conn, ok := r.hub.get(clusterID)
	if !ok {
		status := inventoryRequestStatus{
			RequestID:   body.RequestID,
			ClusterID:   clusterID,
			Scope:       body.Scope,
			Namespace:   body.Namespace,
			Status:      "failed",
			ErrorCode:   "AGENT_OFFLINE",
			Message:     "agent is not connected; inventory request was not sent",
			CreatedAt:   now,
			CompletedAt: now,
		}
		r.setInventoryRequestStatus(status)
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "agent_offline",
			"message":   "agent is not connected; inventory request was not sent",
			"requestId": body.RequestID,
		})
		return
	}
	messageID := store.NewPublicID()
	r.setInventoryRequestStatus(inventoryRequestStatus{
		RequestID: body.RequestID,
		MessageID: messageID,
		ClusterID: clusterID,
		Scope:     body.Scope,
		Namespace: body.Namespace,
		Status:    "pending",
		CreatedAt: now,
	})
	message := protocol.Message[protocol.InventoryRequestPayload]{
		Version:     protocol.Version,
		MessageID:   messageID,
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformInventoryRequest,
		ClusterID:   clusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.InventoryRequestPayload{
			RequestID:                  body.RequestID,
			Scope:                      body.Scope,
			Namespace:                  body.Namespace,
			IncludeDetails:             body.IncludeDetails,
			Reason:                     body.Reason,
			IncludeRecentVeleroObjects: body.IncludeRecentVeleroObjects,
		},
	}
	if err := conn.WriteJSON(message); err != nil {
		r.logger.Error("failed to dispatch inventory request", "cluster_id", clusterID, "request_id", body.RequestID, "error", err)
		failed := inventoryRequestStatus{
			RequestID:   body.RequestID,
			MessageID:   messageID,
			ClusterID:   clusterID,
			Scope:       body.Scope,
			Namespace:   body.Namespace,
			Status:      "failed",
			ErrorCode:   "DISPATCH_FAILED",
			Message:     err.Error(),
			CreatedAt:   now,
			CompletedAt: time.Now().UTC(),
		}
		r.setInventoryRequestStatus(failed)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "queued_failed",
			"warning":   "inventory request could not be sent: " + err.Error(),
			"messageId": messageID,
			"requestId": body.RequestID,
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "sent",
		"messageId": messageID,
		"requestId": body.RequestID,
		"scope":     body.Scope,
		"namespace": body.Namespace,
	})
}

func (r *Router) getClusterInventoryRequest(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	requestID := req.PathValue("requestId")
	if clusterID == "" || requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id_required"})
		return
	}
	status, ok := r.getInventoryRequestStatus(requestID)
	if !ok || status.ClusterID != clusterID {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "inventory_request_not_found"})
		return
	}
	if status.Status == "pending" && time.Since(status.CreatedAt) > 60*time.Second {
		status.Status = "timeout"
		status.ErrorCode = "INVENTORY_REQUEST_TIMEOUT"
		status.Message = "agent did not respond before timeout"
		status.CompletedAt = time.Now().UTC()
		r.setInventoryRequestStatus(status)
	}
	writeJSON(w, http.StatusOK, status)
}

func (r *Router) setInventoryRequestStatus(status inventoryRequestStatus) {
	if status.RequestID == "" {
		return
	}
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	r.inventory[status.RequestID] = status
}

func (r *Router) getInventoryRequestStatus(requestID string) (inventoryRequestStatus, bool) {
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	status, ok := r.inventory[requestID]
	return status, ok
}

func (r *Router) completeInventoryRequest(clusterID string, payload protocol.InventoryReportPayload) {
	if payload.RequestID == "" {
		return
	}
	status, ok := r.getInventoryRequestStatus(payload.RequestID)
	if !ok || status.ClusterID != clusterID {
		return
	}
	status.Status = "succeeded"
	status.Message = "inventory updated"
	status.CompletedAt = time.Now().UTC()
	r.setInventoryRequestStatus(status)
}

func (r *Router) failInventoryRequest(clusterID string, payload protocol.MessageErrorPayload) {
	if payload.RequestID == "" {
		return
	}
	status, ok := r.getInventoryRequestStatus(payload.RequestID)
	if !ok || status.ClusterID != clusterID {
		return
	}
	status.Status = "failed"
	status.ErrorCode = payload.ErrorCode
	status.Message = payload.Message
	status.CompletedAt = time.Now().UTC()
	r.setInventoryRequestStatus(status)
}

func (r *Router) deleteCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if req.URL.Query().Get("force") != "true" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "cluster_unregister_required",
			"message": "use POST /api/v1/clusters/{id}/unregister for agent self-uninstall, or DELETE with force=true for platform-only cleanup",
		})
		return
	}
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to delete cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "clusterId": clusterID})
}

func (r *Router) forceCleanupCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if !r.clusterExists(clusterID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "force_remove_precheck_failed", "message": err.Error()})
		return
	}
	if audit.SourcePlanCount > 0 || audit.TargetPlanCount > 0 {
		plans, listErr := r.store.ListProtectionPlans("")
		if listErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "force_remove_dependency_cleanup_failed", "message": listErr.Error()})
			return
		}
		if cleanupErr := r.cleanupUnregisterProtectionRelationships(clusterID, plans); cleanupErr != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "force_remove_dependency_cleanup_failed", "message": cleanupErr.Error(), "precheck": audit})
			return
		}
	}
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to force cleanup cluster platform records", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_cluster_failed", "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "cleaned",
		"clusterId": clusterID,
		"warning":   "Platform records and HyperCDR relationships were removed. Source backup objects and cluster-side resources were not deleted and may require manual cleanup.",
	})
}

type unregisterAudit struct {
	ClusterID            string   `json:"clusterId"`
	AgentOnline          bool     `json:"agentOnline"`
	DefaultCluster       bool     `json:"defaultCluster"`
	SourcePlanCount      int      `json:"sourcePlanCount"`
	TargetPlanCount      int      `json:"targetPlanCount"`
	RestorePointCount    int      `json:"restorePointCount"`
	StorageRepositoryIDs []string `json:"storageRepositoryIds"`
	ActiveTaskCount      int      `json:"activeTaskCount"`
	ActiveTaskTypes      []string `json:"activeTaskTypes"`
	UnregisterActive     bool     `json:"unregisterActive"`
	ObjectStorageNeeded  bool     `json:"objectStorageNeeded"`
	Stage                string   `json:"stage"`
	Allowed              bool     `json:"allowed"`
	Blockers             []string `json:"blockers"`
}

func (r *Router) auditClusterUnregister(clusterID string) (unregisterAudit, error) {
	audit := unregisterAudit{
		ClusterID:            clusterID,
		StorageRepositoryIDs: []string{},
		ActiveTaskTypes:      []string{},
		Blockers:             []string{},
		Allowed:              true,
		Stage:                "registered",
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		return audit, err
	}
	found := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			found = true
			audit.DefaultCluster = cluster.IsDefault
			break
		}
	}
	if !found {
		return audit, errors.New("cluster not found")
	}
	_, audit.AgentOnline = r.hub.get(clusterID)
	plans, err := r.store.ListProtectionPlans("")
	if err != nil {
		return audit, err
	}
	repositoryIDs := map[string]struct{}{}
	for _, plan := range plans {
		if plan.SourceClusterID == clusterID {
			audit.SourcePlanCount++
			if plan.StorageRepoID != "" {
				repositoryIDs[plan.StorageRepoID] = struct{}{}
			}
		}
		if plan.TargetClusterID == clusterID {
			audit.TargetPlanCount++
		}
	}
	repositories, err := r.store.ListStorageRepositories()
	if err != nil {
		return audit, err
	}
	for _, repository := range repositories {
		if _, ok, bindingErr := r.store.GetClusterStorageBinding(clusterID, repository.ID, clusterID); bindingErr != nil {
			return audit, bindingErr
		} else if ok {
			repositoryIDs[repository.ID] = struct{}{}
		}
	}
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{ClusterID: clusterID, IncludeDeleted: true})
	if err != nil {
		return audit, err
	}
	for _, point := range points {
		if strings.EqualFold(point.Status, "deleted") {
			continue
		}
		audit.RestorePointCount++
		if point.StorageRepoID != "" {
			repositoryIDs[point.StorageRepoID] = struct{}{}
		}
	}
	for id := range repositoryIDs {
		audit.StorageRepositoryIDs = append(audit.StorageRepositoryIDs, id)
	}
	sort.Strings(audit.StorageRepositoryIDs)
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return audit, err
	}
	types := map[string]struct{}{}
	for _, task := range tasks {
		if !isActiveTaskStatus(task.Status) {
			continue
		}
		if task.Type == "unregister" {
			audit.UnregisterActive = true
		} else {
			audit.ActiveTaskCount++
			types[task.Type] = struct{}{}
		}
	}
	for taskType := range types {
		audit.ActiveTaskTypes = append(audit.ActiveTaskTypes, taskType)
	}
	sort.Strings(audit.ActiveTaskTypes)
	audit.ObjectStorageNeeded = len(audit.StorageRepositoryIDs) > 0
	switch {
	case audit.ActiveTaskCount > 0:
		audit.Stage = "active_tasks"
	case audit.TargetPlanCount > 0:
		audit.Stage = "target_in_use"
	case audit.RestorePointCount > 0:
		audit.Stage = "protected_with_restore_points"
	case audit.SourcePlanCount > 0:
		audit.Stage = "configured_without_restore_points"
	default:
		audit.Stage = "registered_without_dr"
	}
	if audit.UnregisterActive {
		audit.Blockers = append(audit.Blockers, "An unregister task is already active for this cluster.")
	}
	if !audit.AgentOnline {
		audit.Blockers = append(audit.Blockers, "The cluster agent is offline. Reconnect it for normal unregister, or use Force Remove if the cluster is permanently unavailable.")
	}
	if audit.ActiveTaskCount > 0 {
		audit.Blockers = append(audit.Blockers, "Wait for active cluster tasks to finish before unregistering.")
	}
	if audit.TargetPlanCount > 0 {
		audit.Blockers = append(audit.Blockers, "This cluster is used as a DR target. Change or remove those DR configurations first.")
	}
	audit.Allowed = len(audit.Blockers) == 0
	return audit, nil
}

func (r *Router) precheckUnregisterCluster(w http.ResponseWriter, req *http.Request) {
	audit, err := r.auditClusterUnregister(req.PathValue("id"))
	if err != nil {
		if err.Error() == "cluster not found" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "unregister_precheck_failed", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, audit)
}

func (r *Router) unregisterCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	var body struct {
		DeleteVelero     *bool  `json:"deleteVelero"`
		DeleteNamespace  *bool  `json:"deleteNamespace"`
		DeleteBackupData bool   `json:"deleteBackupData"`
		Reason           string `json:"reason"`
	}
	if req.Body != nil {
		if err := decodeJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
			return
		}
	}
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		if err.Error() == "cluster not found" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "unregister_precheck_failed", "message": err.Error()})
		return
	}
	// Checking "clean up data" is an explicit consent to remove the
	// cluster's HyperCDR protection relationships as well. This keeps
	// unregister simple while retaining a deliberate destructive action.
	// Explicit cleanup consent may resolve protection-data and target-reference
	// blockers. It must never bypass operational blockers that make a normal
	// cluster-side uninstall unsafe or impossible.
	cleanupConsentResolvesBlockers := body.DeleteBackupData && audit.AgentOnline && audit.ActiveTaskCount == 0 && !audit.UnregisterActive
	if !audit.Allowed && !cleanupConsentResolvesBlockers {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "unregister_precheck_blocked", "message": strings.Join(audit.Blockers, " "), "precheck": audit})
		return
	}
	deleteVelero := true
	if body.DeleteVelero != nil {
		deleteVelero = *body.DeleteVelero
	}
	deleteNamespace := true
	if body.DeleteNamespace != nil {
		deleteNamespace = *body.DeleteNamespace
	}
	if audit.RestorePointCount > 0 && !body.DeleteBackupData {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "backup_data_decision_required",
			"message":  "This cluster has restore points. Confirm deletion of backup data, or remove/archive its DR configurations before unregistering.",
			"precheck": audit,
		})
		return
	}
	cleanupObjectStorage := audit.ObjectStorageNeeded && (audit.RestorePointCount == 0 || body.DeleteBackupData)
	cleanupProtectionRelationships := body.DeleteBackupData && (audit.SourcePlanCount > 0 || audit.TargetPlanCount > 0)
	// When object storage is involved, preserve platform relationships until
	// remote backup deletion succeeds. A failed storage cleanup must leave the
	// source plan and restore-point metadata usable for a retry.
	if cleanupProtectionRelationships && !cleanupObjectStorage {
		plans, listErr := r.store.ListProtectionPlans("")
		if listErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "unregister_dependency_cleanup_failed", "message": listErr.Error()})
			return
		}
		if cleanupErr := r.cleanupUnregisterProtectionRelationships(clusterID, plans); cleanupErr != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "unregister_dependency_cleanup_failed", "message": cleanupErr.Error()})
			return
		}
	}
	namespace := r.agentNamespaceForCluster(clusterID)
	// OADP owns the Velero installation and cluster-scoped CRDs on OpenShift.
	// Unregister the dedicated openshift-adp namespace, but never run the
	// community Velero/CRD removal path against an operator-managed install.
	if r.clusterType(clusterID) == "openshift" {
		deleteVelero = false
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: clusterID,
		Type:      "unregister",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"requestedBy":          requestActor(req),
			"clusterId":            clusterID,
			"namespace":            namespace,
			"deleteVelero":         deleteVelero,
			"deleteNamespace":      deleteNamespace,
			"deleteBackupData":     body.DeleteBackupData,
			"cleanupObjectStorage": cleanupObjectStorage,
			"storageRepositoryIds": audit.StorageRepositoryIDs,
			"unregisterStage":      "prechecking",
			"reason":               body.Reason,
		},
	})
	if err != nil {
		r.logger.Error("failed to create unregister task", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	if cleanupObjectStorage {
		go r.cleanupAndDispatchUnregister(task, audit.StorageRepositoryIDs, cleanupProtectionRelationships)
		writeJSON(w, http.StatusAccepted, task)
		return
	}

	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; unregister will be dispatched after reconnect",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "agent is offline; unregister task remains queued",
		})
		return
	}

	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch unregister task", "cluster_id", clusterID, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "unregister task created but dispatch failed",
		})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 20,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "unregister task dispatched to agent",
	})
	writeJSON(w, http.StatusAccepted, task)
}

func (r *Router) cleanupUnregisterProtectionRelationships(clusterID string, plans []store.ProtectionPlan) error {
	for _, plan := range plans {
		if plan.SourceClusterID != clusterID && plan.TargetClusterID != clusterID {
			continue
		}
		var ok bool
		var err error
		if plan.SourceClusterID == clusterID {
			_, ok, err = r.store.DeleteProtectionPlan(plan.ID)
		} else {
			// Target-only cleanup preserves the source plan, schedule, restore
			// points, and object-storage data. A future drill must select a new
			// target explicitly.
			_, ok, err = r.store.ClearProtectionPlanTargetCluster(plan.ID, clusterID)
		}
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("a protection relationship changed during unregister; refresh and retry")
		}
	}
	return nil
}

func (r *Router) cleanupAndDispatchUnregister(task store.Task, repositoryIDs []string, cleanupProtectionRelationships bool) {
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 10})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "cleaning_object_storage", Message: "cleaning backup data before cluster-side uninstall"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cleanupResults, cleanupErr := r.cleanupClusterObjectStorageRepositories(ctx, task.ClusterID, repositoryIDs)
	cancel()
	if cleanupErr != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", Progress: 10, ErrorCode: "OBJECT_STORAGE_CLEANUP_FAILED", ErrorMessage: cleanupErr.Error(), MarkDone: true})
		_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: "OBJECT_STORAGE_CLEANUP_FAILED", Message: cleanupErr.Error(), Payload: map[string]any{"cleanupResult": cleanupResults}})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "object_storage_cleaned", Message: "backup data cleanup completed", Payload: map[string]any{"cleanupResult": cleanupResults}})
	if cleanupProtectionRelationships {
		plans, err := r.store.ListProtectionPlans("")
		if err == nil {
			err = r.cleanupUnregisterProtectionRelationships(task.ClusterID, plans)
		}
		if err != nil {
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", Progress: 10, ErrorCode: "UNREGISTER_DEPENDENCY_CLEANUP_FAILED", ErrorMessage: err.Error(), MarkDone: true})
			_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: "UNREGISTER_DEPENDENCY_CLEANUP_FAILED", Message: err.Error()})
			return
		}
	}
	conn, ok := r.hub.get(task.ClusterID)
	if !ok || conn == nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", Progress: 10, ErrorCode: "AGENT_OFFLINE", ErrorMessage: "agent disconnected after precheck; unregister will be dispatched after reconnect"})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", Progress: 10, ErrorCode: "DISPATCH_FAILED", ErrorMessage: err.Error()})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched", Progress: 20})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "dispatched", Message: "unregister task dispatched to agent"})
}

func (r *Router) upgradeClusterAgent(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Repair bool `json:"repair"`
	}
	if req.Body != nil {
		decoder := json.NewDecoder(io.LimitReader(req.Body, 1<<20))
		if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
			return
		}
	}
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if !r.clusterExists(clusterID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	targetName := "comm-agent"
	clusterType := r.clusterType(clusterID)
	if clusterType == "openshift" {
		targetName = "oadp-comm-agent"
	}
	target, targetErr := r.componentTarget(req.Context(), targetName)
	if targetErr != nil || target.Image == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "agent_image_not_configured", "message": "Target agent image is not configured"})
		return
	}
	targetImage, targetDigest := target.Image, target.ImageDigest
	if clusterType == "openshift" {
		targetImage = immutableImageReference(targetImage, targetDigest)
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	for _, cluster := range clusters {
		if !input.Repair && cluster.ID == clusterID && agentUpgradeTargetMatches(cluster, targetImage, target.Version, targetDigest) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_already_current", "message": "Comm Agent already uses the target image."})
			return
		}
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	blockedTypes := map[string]bool{"backup": true, "restore": true, "drill": true, "takeover": true, "failback": true, "retention-cleanup": true, "protection-cleanup": true, "agent-upgrade": true, "velero-upgrade": true}
	for _, task := range tasks {
		if blockedTypes[task.Type] && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cluster_task_active", "message": "Wait for active backup, restore, drill, cleanup, or upgrade tasks to finish before upgrading Comm Agent."})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_offline", "message": "Cluster agent is offline. Reconnect the agent before upgrading."})
		return
	}
	commandID := store.NewPublicID()
	namespace := r.agentNamespaceForCluster(clusterID)
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: clusterID,
		Type:      "agent-upgrade",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"operation":      map[bool]string{true: "repair", false: "upgrade"}[input.Repair],
			"requestedBy":    requestActor(req),
			"clusterId":      clusterID,
			"namespace":      namespace,
			"image":          targetImage,
			"version":        target.Version,
			"releaseId":      target.ID,
			"expectedDigest": targetDigest,
			"deploymentName": "hypercdr-comm-agent",
			// Both provider deployments intentionally keep the same Kubernetes
			// container contract; only the OpenShift image/binary is independent.
			"containerName":     "comm-agent",
			"rolloutAnnotation": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		r.logger.Error("failed to create agent upgrade task", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, task)
	go r.dispatchComponentUpgrade(conn, task, "agent upgrade task dispatched")
}

func (r *Router) upgradeClusterVelero(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Repair bool `json:"repair"`
	}
	if req.Body != nil {
		decoder := json.NewDecoder(io.LimitReader(req.Body, 1<<20))
		if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
			return
		}
	}
	clusterID := req.PathValue("id")
	clusters, listErr := r.store.ListClusters()
	if listErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	var cluster store.Cluster
	found := false
	for _, item := range clusters {
		if item.ID == clusterID {
			cluster = item
			found = true
			break
		}
	}
	if clusterID == "" || !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	if cluster.ClusterType == "openshift" {
		r.upgradeOpenShiftOADP(w, req, cluster, input.Repair)
		return
	}
	if cluster.VeleroImageDigest == "" || cluster.VeleroNodeAgentImageDigest == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "velero_runtime_unreported", "message": "Upgrade the comm-agent first so it can report the Velero server and node-agent image digests."})
		return
	}
	target, targetErr := r.componentTarget(req.Context(), "velero")
	if targetErr != nil || target.Image == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "velero_image_not_configured"})
		return
	}
	targetImage, targetDigest := target.Image, target.ImageDigest
	awsPlugin, awsErr := r.componentTarget(req.Context(), "velero-plugin-for-aws")
	azurePlugin, azureErr := r.componentTarget(req.Context(), "velero-plugin-for-microsoft-azure")
	gcpPlugin, gcpErr := r.componentTarget(req.Context(), "velero-plugin-for-gcp")
	if awsErr != nil || azureErr != nil || gcpErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "release_manifest_plugins_missing", "message": "The active HyperCDR release does not contain a complete Velero plugin manifest."})
		return
	}
	veleroIdentityMatches := strings.TrimSpace(cluster.VeleroImage) == targetImage && strings.TrimSpace(cluster.VeleroVersion) == strings.TrimSpace(target.Version)
	veleroDigestMatches := cluster.VeleroImageDigest == targetDigest && cluster.VeleroNodeAgentImageDigest == targetDigest
	if !input.Repair && (veleroIdentityMatches || veleroDigestMatches) && cluster.VeleroServerReady && cluster.VeleroNodeAgentDesired > 0 && cluster.VeleroNodeAgentReady == cluster.VeleroNodeAgentDesired {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "velero_already_current", "message": "Velero server and all node agents already use the target image."})
		return
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	blockedTypes := map[string]bool{"backup": true, "restore": true, "drill": true, "takeover": true, "failback": true, "retention-cleanup": true, "protection-cleanup": true, "agent-upgrade": true, "velero-upgrade": true}
	for _, task := range tasks {
		if blockedTypes[task.Type] && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cluster_task_active", "message": "Wait for active backup, restore, drill, cleanup, or upgrade tasks to finish before upgrading Velero."})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_offline", "message": "Cluster agent is offline. Reconnect the agent before upgrading Velero."})
		return
	}
	cacheStorageClass := ""
	for _, storageClass := range cluster.StorageClasses {
		if storageClass.Default && strings.TrimSpace(storageClass.Provisioner) != "" && strings.EqualFold(firstNonEmptyString(storageClass.ReclaimPolicy, "Delete"), "Delete") {
			cacheStorageClass = storageClass.Name
			break
		}
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "velero-upgrade", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"requestedBy": requestActor(req),
		"operation":   map[bool]string{true: "repair", false: "upgrade"}[input.Repair],
		"clusterId":   clusterID, "namespace": r.dataProtectionNamespaceForCluster(clusterID), "image": targetImage, "version": target.Version, "releaseId": target.ID,
		"expectedDigest": targetDigest, "deploymentName": "velero", "daemonSetName": "node-agent",
		"awsPluginImage": awsPlugin.Image, "azurePluginImage": azurePlugin.Image, "gcpPluginImage": gcpPlugin.Image,
		"concurrentBackups": 2, "nodeAgentConcurrency": 2, "prepareQueueLength": 4,
		"cacheStorageClass": cacheStorageClass, "cacheResidentThresholdMB": 1024, "cacheLimitMB": 5120,
		"crdsUrl": func() string {
			// Packaging-only HyperCDR builds do not change the upstream Velero
			// CRDs. Reapplying them can block an otherwise simple image repair.
			if strings.HasPrefix(strings.TrimSpace(target.Version), strings.TrimSpace(cluster.VeleroVersion)) {
				return ""
			}
			return strings.TrimRight(r.cfg.PublicBaseURL, "/") + veleroCRDsPath
		}(),
	}})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	dispatchMessage := "Velero upgrade task dispatched to cluster agent"
	if input.Repair {
		dispatchMessage = "Velero repair task dispatched to cluster agent"
	}
	writeJSON(w, http.StatusAccepted, task)
	go r.dispatchComponentUpgrade(conn, task, dispatchMessage)
}

func (r *Router) upgradeOpenShiftOADP(w http.ResponseWriter, req *http.Request, cluster store.Cluster, repair bool) {
	veleroTarget, veleroErr := r.componentTarget(req.Context(), "oadp-velero")
	catalogTarget, catalogErr := r.componentTarget(req.Context(), "oadp-catalog")
	if veleroErr != nil || catalogErr != nil || strings.TrimSpace(veleroTarget.Image) == "" || strings.TrimSpace(catalogTarget.Image) == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "oadp_release_manifest_incomplete", "message": "The active release must contain immutable oadp-velero and oadp-catalog targets."})
		return
	}
	if !repair && !veleroUpgradeIsAvailable(cluster, veleroTarget) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "oadp_already_current", "message": "OADP Velero and node-agent already use the target release."})
		return
	}
	tasks, err := r.store.ListTasks(cluster.ID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_tasks_failed"})
		return
	}
	blocked := map[string]bool{"backup": true, "restore": true, "drill": true, "takeover": true, "failback": true, "retention-cleanup": true, "protection-cleanup": true, "agent-upgrade": true, "velero-upgrade": true}
	for _, task := range tasks {
		if blocked[task.Type] && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, 409, map[string]any{"error": "cluster_task_active", "message": "Wait for active backup, restore, drill, cleanup, or upgrade tasks to finish before upgrading OADP."})
			return
		}
	}
	conn, ok := r.hub.get(cluster.ID)
	if !ok {
		writeJSON(w, 409, map[string]any{"error": "agent_offline", "message": "OpenShift agent is offline. Reconnect it before upgrading OADP."})
		return
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "velero-upgrade", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"requestedBy": requestActor(req), "operation": map[bool]string{true: "repair", false: "upgrade"}[repair], "clusterId": cluster.ID,
		"namespace": "openshift-adp", "oadp": true, "catalogImage": immutableImageReference(catalogTarget.Image, catalogTarget.ImageDigest), "oadpPackage": "oadp-operator", "oadpChannel": "stable-1.3", "oadpTargetCsv": "oadp-operator.v1.3.10",
		"image": immutableImageReference(veleroTarget.Image, veleroTarget.ImageDigest), "version": veleroTarget.Version, "releaseId": veleroTarget.ID, "expectedDigest": veleroTarget.ImageDigest,
	}})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_task_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, task)
	go r.dispatchComponentUpgrade(conn, task, "OADP upgrade task dispatched to OpenShift agent")
}

func (r *Router) dispatchComponentUpgrade(conn *websocket.Conn, task store.Task, message string) {
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch component upgrade task", "cluster_id", task.ClusterID, "task_id", task.ID, "type", task.Type, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", Progress: 0, ErrorCode: "DISPATCH_FAILED", ErrorMessage: err.Error()})
		_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "warning", Reason: "dispatch_failed", Message: "Upgrade task was created but could not be dispatched; it remains queued for retry."})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched", Progress: 0})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "dispatched", Message: message})
}

func (r *Router) clusterExists(clusterID string) bool {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to verify cluster exists", "cluster_id", clusterID, "error", err)
		return false
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return true
		}
	}
	return false
}

func (r *Router) agentNamespace() string {
	if r.cfg.AgentNamespace != "" {
		return r.cfg.AgentNamespace
	}
	return "hypercdr-agent"
}

func (r *Router) clusterType(clusterID string) string {
	clusters, err := r.store.ListClusters()
	if err != nil {
		return ""
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return normalizedClusterTypeForRouting(cluster.ClusterType)
		}
	}
	return ""
}

func normalizedClusterTypeForRouting(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func clusterTypesDRCompatible(sourceType, targetType string) bool {
	return (normalizedClusterTypeForRouting(sourceType) == "openshift") == (normalizedClusterTypeForRouting(targetType) == "openshift")
}

func (r *Router) clustersDRCompatible(sourceClusterID, targetClusterID string) bool {
	return clusterTypesDRCompatible(r.clusterType(sourceClusterID), r.clusterType(targetClusterID))
}

func (r *Router) agentNamespaceForCluster(clusterID string) string {
	if r.clusterType(clusterID) == "openshift" {
		return "openshift-adp"
	}
	return r.agentNamespace()
}

// dataProtectionNamespaceForCluster resolves the namespace that owns the
// Velero/OADP resources. It is intentionally separate from the agent
// namespace even though both map to the same namespace in phase 1.
func (r *Router) dataProtectionNamespaceForCluster(clusterID string) string {
	if r.clusterType(clusterID) == "openshift" {
		return "openshift-adp"
	}
	return r.agentNamespace()
}
