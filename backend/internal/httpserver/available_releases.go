package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

type githubRelease struct {
	ID          int64  `json:"id"`
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
	AssetsURL string `json:"assets_url"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// listAvailableReleases reads immutable release metadata from GitHub. A network
// failure is reported to the UI and never affects the current local manifest.
func (r *Router) listAvailableReleases(w http.ResponseWriter, req *http.Request) {
	if strings.TrimSpace(r.cfg.ReleaseRepository) == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "release_repository_not_configured"})
		return
	}
	client := &http.Client{Timeout: 15 * time.Second}
	url := "https://api.github.com/repos/" + strings.TrimSpace(r.cfg.ReleaseRepository) + "/releases?per_page=30"
	request, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, url, nil)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "hypercdr-platform")
	response, err := client.Do(request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "release_catalog_unavailable", "message": err.Error()})
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "release_catalog_unavailable"})
		return
	}
	var releases []githubRelease
	if err := json.NewDecoder(response.Body).Decode(&releases); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "release_catalog_invalid"})
		return
	}
	items := make([]store.PlatformRelease, 0, len(releases))
	for _, item := range releases {
		if item.Draft || item.Prerelease || strings.TrimSpace(item.TagName) == "" {
			continue
		}
		version := strings.TrimPrefix(item.TagName, "v")
		publishedAt, _ := time.Parse(time.RFC3339, item.PublishedAt)
		entry := store.PlatformRelease{ID: item.TagName, Version: version, ReleaseNotes: item.Body, Status: "published", PublishedAt: publishedAt}
		assets := make([]githubAsset, 0, len(item.Assets))
		for _, asset := range item.Assets {
			assets = append(assets, githubAsset{Name: asset.Name, URL: asset.URL})
		}
		if item.AssetsURL != "" {
			assetsReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, item.AssetsURL, nil)
			assetsReq.Header.Set("Accept", "application/vnd.github+json")
			assetsReq.Header.Set("User-Agent", "hypercdr-platform")
			if assetsResp, assetsErr := client.Do(assetsReq); assetsErr == nil && assetsResp.StatusCode >= 200 && assetsResp.StatusCode < 300 {
				var remoteAssets []githubAsset
				if json.NewDecoder(assetsResp.Body).Decode(&remoteAssets) == nil {
					assets = remoteAssets
				}
				assetsResp.Body.Close()
			}
		}
		for _, asset := range assets {
			if asset.Name != "release-manifest.json" {
				continue
			}
			manifestReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, asset.URL, nil)
			manifestReq.Header.Set("Accept", "application/octet-stream")
			manifestReq.Header.Set("User-Agent", "hypercdr-platform")
			manifestResp, manifestErr := client.Do(manifestReq)
			if manifestErr != nil || manifestResp.StatusCode < 200 || manifestResp.StatusCode >= 300 {
				if manifestResp != nil {
					manifestResp.Body.Close()
				}
				continue
			}
			var manifest struct {
				Version               string                            `json:"version"`
				DatabaseSchemaVersion string                            `json:"databaseSchemaVersion"`
				RollbackSupported     bool                              `json:"rollbackSupported"`
				ComponentManifest     map[string]store.ReleaseComponent `json:"componentManifest"`
			}
			manifestErr = json.NewDecoder(manifestResp.Body).Decode(&manifest)
			manifestResp.Body.Close()
			if manifestErr == nil && manifest.Version == version {
				entry.ComponentManifest = manifest.ComponentManifest
				entry.DatabaseSchemaVersion = manifest.DatabaseSchemaVersion
				entry.RollbackSupported = manifest.RollbackSupported
				if c := entry.ComponentManifest["platform-api"]; c.Image != "" {
					entry.APIImage, entry.APIImageDigest = c.Image, c.ImageDigest
				}
				if c := entry.ComponentManifest["platform-frontend"]; c.Image != "" {
					entry.FrontendImage, entry.FrontendImageDigest = c.Image, c.ImageDigest
				}
			}
		}
		items = append(items, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
