package platform

import (
	"context"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type catalogRelease struct {
	Version, DatabaseSchemaVersion, MinimumAgentVersion, ReleaseNotes string
	RollbackSupported                                                 bool
	ComponentManifest                                                 map[string]store.ReleaseComponent `json:"componentManifest"`
}
type releaseCatalog struct {
	Items []catalogRelease `json:"items"`
}

func startReleaseCatalogSync(ctx context.Context, cfg config.Config, repo store.Store, logger *slog.Logger) {
	if strings.TrimSpace(cfg.ReleaseCenterURL) == "" {
		return
	}
	interval, err := time.ParseDuration(strings.TrimSpace(cfg.ReleaseCenterSyncInterval) + "s")
	if err != nil || interval < time.Minute {
		interval = time.Hour
	}
	sync := func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ReleaseCenterURL+"/api/v1/catalog", nil)
		if cfg.ReleaseCenterToken != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.ReleaseCenterToken)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Warn("release catalog synchronization failed", "error", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			logger.Warn("release catalog returned unexpected status", "status", resp.StatusCode)
			return
		}
		var c releaseCatalog
		if json.NewDecoder(resp.Body).Decode(&c) != nil {
			logger.Warn("release catalog response is invalid")
			return
		}
		for _, v := range c.Items {
			if _, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: v.Version, DatabaseSchemaVersion: v.DatabaseSchemaVersion, MinimumAgentVersion: v.MinimumAgentVersion, ReleaseNotes: v.ReleaseNotes, RollbackSupported: v.RollbackSupported, ComponentManifest: v.ComponentManifest, APIImage: v.ComponentManifest["platform-api"].Image, APIImageDigest: v.ComponentManifest["platform-api"].ImageDigest, FrontendImage: v.ComponentManifest["platform-frontend"].Image, FrontendImageDigest: v.ComponentManifest["platform-frontend"].ImageDigest, Status: "candidate", PublishedBy: "release-center"}); err != nil {
				logger.Warn("release catalog entry could not be stored", "version", v.Version, "error", err)
			}
		}
	}
	go func() {
		sync()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sync()
			}
		}
	}()
}
