package httpserver

import (
	"hypercdr-platform/platform/backend/internal/buildinfo"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *Router) healthz(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (r *Router) readyz(w http.ResponseWriter, req *http.Request) {
	status := "degraded"
	if r.cfg.DatabaseURL != "" {
		status = "ok"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"databaseSet": r.cfg.DatabaseURL != "",
	})
}

func (r *Router) platformVersion(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, platformVersionResponse{Version: buildinfo.Version, GitCommit: buildinfo.GitCommit, BuildTime: buildinfo.BuildTime, DatabaseSchemaVersion: buildinfo.SchemaVersion, DeployMode: r.cfg.DeployMode})
}

func (r *Router) listPlatformReleases(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformReleases()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_platform_releases_failed"})
		return
	}
	writeJSON(w, 200, listResponse[store.PlatformRelease]{Items: nonNilSlice(items)})
}

func (r *Router) getPlatformRelease(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformReleases()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_platform_releases_failed"})
		return
	}
	for _, item := range items {
		if item.ID == req.PathValue("id") {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "release_not_found"})
}

var requiredReleaseComponents = []string{
	"platform-api", "platform-frontend", "cluster-registration-executor",
	"comm-agent", "velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp",
	"oadp-comm-agent", "oadp-operator", "oadp-velero", "oadp-openshift-plugin", "oadp-aws-plugin",
	"oadp-restore-helper", "oadp-bundle", "oadp-catalog",
}

func (r *Router) createPlatformRelease(w http.ResponseWriter, req *http.Request) {
	var body platformReleaseRequest
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Version) == "" {
		writeJSON(w, 400, map[string]any{"error": "version_required"})
		return
	}
	manifest := body.ComponentManifest
	for _, name := range requiredReleaseComponents {
		component, ok := manifest[name]
		if !ok || strings.TrimSpace(component.Version) == "" || strings.TrimSpace(component.Image) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "release_manifest_incomplete", "message": "Release manifest is missing component " + name + "."})
			return
		}
		digest, resolveErr := r.resolveImageDigest(req.Context(), component.Image)
		if resolveErr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "release_component_unavailable", "component": name, "message": resolveErr.Error()})
			return
		}
		if component.ImageDigest != "" && component.ImageDigest != digest {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "release_component_digest_changed", "component": name})
			return
		}
		component.ImageDigest = digest
		manifest[name] = component
	}
	apiImage, apiDigest := manifest["platform-api"].Image, manifest["platform-api"].ImageDigest
	frontendImage, frontendDigest := manifest["platform-frontend"].Image, manifest["platform-frontend"].ImageDigest
	schema := strings.TrimSpace(body.DatabaseSchemaVersion)
	if schema == "" {
		schema = buildinfo.SchemaVersion
	}
	status, publishedBy := "candidate", ""
	if releases, listErr := r.store.ListPlatformReleases(); listErr == nil && len(releases) == 0 {
		status, publishedBy = "active", "system"
	}
	item, err := r.store.UpsertPlatformRelease(store.PlatformReleaseInput{Version: body.Version, APIImage: apiImage, APIImageDigest: apiDigest, FrontendImage: frontendImage, FrontendImageDigest: frontendDigest, ComponentManifest: manifest, DatabaseSchemaVersion: schema, MinimumAgentVersion: body.MinimumAgentVersion, RollbackSupported: body.RollbackSupported, ReleaseNotes: body.ReleaseNotes, Status: status, PublishedBy: publishedBy})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_platform_release_failed"})
		return
	}
	writeJSON(w, 201, item)
}

func (r *Router) platformPrecheck(releaseID string) ([]platformPrecheckItem, bool, store.PlatformRelease) {
	items, _ := r.store.ListPlatformReleases()
	var release store.PlatformRelease
	for _, v := range items {
		if v.ID == releaseID {
			release = v
		}
	}
	clusters, _ := r.store.ListClusters()
	tasks, _ := r.store.ListTasks("")
	activeTasks := 0
	for _, t := range tasks {
		if !isTerminalTaskStatus(t.Status) {
			activeTasks++
		}
	}
	offline := 0
	for _, c := range clusters {
		if !r.hub.has(c.ID) {
			offline++
		}
	}
	manifestComplete := release.ID != ""
	for _, name := range requiredReleaseComponents {
		component, ok := release.ComponentManifest[name]
		if !ok || component.Version == "" || component.Image == "" || !validImageDigest(component.ImageDigest) {
			manifestComplete = false
			break
		}
	}
	checks := []platformPrecheckItem{
		{ID: "release", Label: "Release package is registered", Passed: release.ID != "", Blocking: true},
		{ID: "manifest", Label: "Complete immutable component manifest", Passed: manifestComplete, Blocking: true},
		{ID: "mode", Label: "Formal deployment mode", Passed: r.cfg.DeployMode != "development", Detail: r.cfg.DeployMode, Blocking: true},
		{ID: "tasks", Label: "No active DR tasks", Passed: activeTasks == 0, Detail: activeTasks, Blocking: true},
		{ID: "agents", Label: "Some registered agents are offline", Passed: offline == 0, Detail: offline, Blocking: false},
		{ID: "version", Label: "Target differs from running version", Passed: release.Version != "" && release.Version != buildinfo.Version, Blocking: true},
	}
	passed := true
	for _, c := range checks {
		if c.Blocking && !c.Passed {
			passed = false
		}
	}

	return checks, passed, release
}
func (r *Router) precheckPlatformUpgrade(w http.ResponseWriter, req *http.Request) {
	checks, passed, _ := r.platformPrecheck(req.URL.Query().Get("releaseId"))
	writeJSON(w, 200, platformPrecheckResponse{Passed: passed, Checks: checks, CurrentVersion: buildinfo.Version})
}
func (r *Router) listPlatformUpgrades(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformUpgradeJobs()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_platform_upgrades_failed"})
		return
	}
	writeJSON(w, 200, listResponse[store.PlatformUpgradeJob]{Items: nonNilSlice(items)})
}
func (r *Router) createPlatformUpgrade(w http.ResponseWriter, req *http.Request) {
	var body platformUpgradeRequest
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.ReleaseID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "release_id_required"})
		return
	}
	checks, passed, release := r.platformPrecheck(body.ReleaseID)
	if release.ID == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "release_not_found", "checks": checks})
		return
	}
	if !passed {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "upgrade_precheck_failed", "checks": checks})
		return
	}
	jobs, err := r.store.ListPlatformUpgradeJobs()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_platform_upgrades_failed"})
		return
	}
	for _, job := range jobs {
		if !isTerminalPlatformUpgradeStatus(job.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "platform_upgrade_in_progress", "job": job})
			return
		}
	}
	job, err := r.store.CreatePlatformUpgradeJob(store.PlatformUpgradeJobInput{
		Release: release, FromVersion: buildinfo.Version, RequestedBy: "user",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_platform_upgrade_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (r *Router) updatePlatformUpgradeStatus(w http.ResponseWriter, req *http.Request) {
	var body platformUpgradeStatusRequest
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Status) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "status_required"})
		return
	}
	job, ok, err := r.store.UpdatePlatformUpgradeJob(store.PlatformUpgradeJobUpdate{ID: req.PathValue("id"), Status: body.Status, Step: body.Step, Progress: body.Progress, ErrorCode: body.ErrorCode, ErrorMessage: body.ErrorMessage, ExecutorID: body.ExecutorID, MarkStarted: body.MarkStarted, MarkDone: body.MarkDone})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_platform_upgrade_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "upgrade_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (r *Router) frontend(w http.ResponseWriter, req *http.Request) {
	frontendDir := strings.TrimSpace(r.cfg.FrontendDir)
	if frontendDir == "" {
		http.NotFound(w, req)
		return
	}

	cleanPath := filepath.Clean(strings.TrimPrefix(req.URL.Path, "/"))
	if cleanPath == "." {
		cleanPath = "index.html"
	}
	fullPath := filepath.Join(frontendDir, cleanPath)
	if rel, err := filepath.Rel(frontendDir, fullPath); err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, req)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		// Asset URLs are content-addressed by the frontend build. Returning the
		// SPA shell for a missing JavaScript file produces a misleading 200 with
		// text/html and leaves an already-open browser on stale code.
		if strings.HasPrefix(cleanPath, "assets/") {
			http.NotFound(w, req)
			return
		}
		fullPath = filepath.Join(frontendDir, "index.html")
		cleanPath = "index.html"
	}
	if cleanPath == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, req, fullPath)
}
