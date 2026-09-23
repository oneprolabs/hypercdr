package httpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"hypercdr-platform/platform/backend/internal/store"
)

// A configured local manifest is authoritative. Never fall back to a database
// release when it is unreadable: that could silently install another version.
func localManifestComponent(path, name string) (store.ComponentRelease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return store.ComponentRelease{}, fmt.Errorf("read local release manifest: %w", err)
	}
	var manifest struct {
		Version    string                            `json:"version"`
		Components map[string]store.ReleaseComponent `json:"componentManifest"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return store.ComponentRelease{}, fmt.Errorf("decode local release manifest: %w", err)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return store.ComponentRelease{}, fmt.Errorf("local release manifest has no version")
	}
	component, ok := manifest.Components[name]
	if !ok || strings.TrimSpace(component.Version) == "" || strings.TrimSpace(component.Image) == "" || !validImageDigest(component.ImageDigest) {
		return store.ComponentRelease{}, fmt.Errorf("local release %s has no valid %s component", manifest.Version, name)
	}
	return store.ComponentRelease{Component: name, Version: component.Version, Image: component.Image, ImageDigest: component.ImageDigest, Status: "active"}, nil
}
