package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestActivateLocalManifestAtomically(t *testing.T) {
	dir := t.TempDir()
	release := store.PlatformRelease{Version: "1.0.87.20260923", DatabaseSchemaVersion: "000038", RollbackSupported: true, ComponentManifest: map[string]store.ReleaseComponent{"comm-agent": {Version: "1.0.87.20260923", Image: "registry.example/agent:latest", ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	if err := activateLocalManifest(dir, release); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "current-release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != release.Version {
		t.Fatalf("version = %q, want %q", got.Version, release.Version)
	}
	if _, err := os.Stat(filepath.Join(dir, "releases", release.Version, "release-manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDeploymentLayout(t *testing.T) {
	t.Setenv("HCDR_PLATFORM_SERVICE", "enterprise-platform")
	combined := resolveDeploymentLayout("combined")
	if combined.apiKey != "PLATFORM_IMAGE" || combined.frontendKey != "PLATFORM_IMAGE" || combined.platformService != "enterprise-platform" || combined.frontendService != "" {
		t.Fatalf("unexpected combined layout: %#v", combined)
	}
	split := resolveDeploymentLayout("split")
	if split.apiKey != "PLATFORM_API_IMAGE" || split.frontendKey != "PLATFORM_FRONTEND_IMAGE" || split.platformService != "hypercdr-platform-api" || split.frontendService != "hypercdr-platform-frontend" {
		t.Fatalf("unexpected split layout: %#v", split)
	}
}

func TestComposeUsesConfiguredProjectAndFile(t *testing.T) {
	t.Setenv("HCDR_COMPOSE_PROJECT_NAME", "hypercdr-enterprise")
	t.Setenv("HCDR_COMPOSE_FILE", "compose.yaml")
	cmd := compose("/deploy", "up", "-d", "platform")
	want := []string{"docker", "compose", "--project-name", "hypercdr-enterprise", "--project-directory", "/deploy", "--env-file", "/deploy/.env", "-f", "/deploy/compose.yaml", "up", "-d", "platform"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args: %#v", cmd.Args)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("arg %d = %q, want %q; all args: %#v", i, cmd.Args[i], want[i], cmd.Args)
		}
	}
}
