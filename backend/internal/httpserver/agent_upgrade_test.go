package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestResolveRegistryManifestDigestKeepsIndexAndManifestNegotiationSeparate(t *testing.T) {
	var accepts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		accepts = append(accepts, req.Header.Get("Accept"))
		if len(accepts) == 1 {
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			w.Header().Set("Docker-Content-Digest", "sha256:index")
			return
		}
		http.Error(w, "unexpected fallback", http.StatusInternalServerError)
	}))
	defer server.Close()

	digest, err := resolveRegistryManifestDigest(context.Background(), server.Client(), server.URL, "hypercdr/comm-agent")
	if err != nil || digest != "sha256:index" {
		t.Fatalf("resolve index digest = %q, %v", digest, err)
	}
	if len(accepts) != 1 {
		t.Fatalf("requests = %d, want 1", len(accepts))
	}
	if accepts[0] != "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json" {
		t.Fatalf("unexpected index Accept header: %q", accepts[0])
	}
}

func TestResolveRegistryManifestDigestFallsBackToSingleManifest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", "sha256:manifest")
	}))
	defer server.Close()

	digest, err := resolveRegistryManifestDigest(context.Background(), server.Client(), server.URL, "hypercdr/comm-agent")
	if err != nil || digest != "sha256:manifest" {
		t.Fatalf("resolve manifest digest = %q, %v", digest, err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestAgentUpgradeTargetMatchesRegistryManifestAndRuntimeImageIDDifference(t *testing.T) {
	cluster := store.Cluster{
		AgentImage:       "registry.example/hypercdr/comm-agent:v2",
		AgentVersion:     "v2",
		AgentImageDigest: "sha256:runtime-image-id",
	}

	if !agentUpgradeTargetMatches(cluster, cluster.AgentImage, "v2", "sha256:registry-manifest") {
		t.Fatal("expected matching image and version to complete the upgrade when digest kinds differ")
	}
}

func TestAgentUpgradeTargetRejectsWrongVersionAndDigest(t *testing.T) {
	cluster := store.Cluster{
		AgentImage:       "registry.example/hypercdr/comm-agent:v2",
		AgentVersion:     "v1",
		AgentImageDigest: "sha256:old-runtime-image-id",
	}

	if agentUpgradeTargetMatches(cluster, "registry.example/hypercdr/comm-agent:v2", "v2", "sha256:registry-manifest") {
		t.Fatal("expected an old agent version and non-matching digest to remain incomplete")
	}
}

func TestAgentUpgradeTargetRejectsWrongReportedIdentityEvenWhenLegacyDigestMatches(t *testing.T) {
	cluster := store.Cluster{
		AgentImage:       "registry.example/hypercdr/comm-agent:v2",
		AgentVersion:     "v1",
		AgentImageDigest: "sha256:expected",
	}

	if agentUpgradeTargetMatches(cluster, cluster.AgentImage, "v2", "sha256:expected") {
		t.Fatal("complete runtime identity must take precedence over the legacy digest fallback")
	}
}

func TestAgentUpgradeTargetSupportsDigestOnlyLegacyReports(t *testing.T) {
	cluster := store.Cluster{AgentImageDigest: "sha256:expected"}

	if !agentUpgradeTargetMatches(cluster, "", "", "sha256:expected") {
		t.Fatal("expected matching digest to remain supported for legacy reports")
	}
}

func TestAgentUpgradeIsNotAvailableForMatchingImageAndVersionWithDifferentDigestKinds(t *testing.T) {
	cluster := store.Cluster{
		AgentVersion:     "v20260729.9",
		AgentImage:       "registry.example/hypercdr/comm-agent:v20260729.9",
		AgentImageDigest: "sha256:runtime-image-id",
	}
	target := store.ComponentRelease{
		Version:     "v20260729.9",
		Image:       cluster.AgentImage,
		ImageDigest: "sha256:registry-index",
	}

	if agentUpgradeIsAvailable(cluster, target) {
		t.Fatal("matching image and version must not display Update when digest kinds differ")
	}
}

func TestAgentUpgradeIsAvailableForNewerVersion(t *testing.T) {
	cluster := store.Cluster{AgentVersion: "v20260729.8", AgentImage: "registry.example/hypercdr/comm-agent:v20260729.8"}
	target := store.ComponentRelease{Version: "v20260729.9", Image: "registry.example/hypercdr/comm-agent:v20260729.9"}

	if !agentUpgradeIsAvailable(cluster, target) {
		t.Fatal("newer target version must display Update")
	}
}

func TestAgentUpgradeIsAvailableForDifferentImageAtSameVersion(t *testing.T) {
	cluster := store.Cluster{AgentVersion: "v20260729.9", AgentImage: "registry.example/old/comm-agent:v20260729.9"}
	target := store.ComponentRelease{Version: "v20260729.9", Image: "registry.example/new/comm-agent:v20260729.9"}

	if !agentUpgradeIsAvailable(cluster, target) {
		t.Fatal("different target image at the same version must display Update")
	}
}

func TestVeleroUpgradeIsNotAvailableForMatchingImageAndVersionWithDifferentDigestKinds(t *testing.T) {
	cluster := store.Cluster{
		VeleroVersion:          "v1.17.1",
		VeleroImage:            "registry.example/hypercdr/velero:v1.17.1",
		VeleroImageDigest:      "sha256:runtime-image-id",
		VeleroServerReady:      true,
		VeleroNodeAgentDesired: 2,
		VeleroNodeAgentReady:   2,
	}
	target := store.ComponentRelease{
		Version:     "v1.17.1",
		Image:       cluster.VeleroImage,
		ImageDigest: "sha256:registry-index",
	}

	if veleroUpgradeIsAvailable(cluster, target) {
		t.Fatal("matching Velero image and version must not display Update when digest kinds differ")
	}
}

func TestVeleroUpgradeIsNotAvailableWhenPublishedDigestIsAlreadyRunning(t *testing.T) {
	target := store.ComponentRelease{
		Version:     "v1.18.2-hcdr.1",
		Image:       "registry.example/hypercdr/velero:v1.18.2-hcdr.1",
		ImageDigest: "sha256:published",
	}
	cluster := store.Cluster{
		VeleroVersion:              "v1.18.2",
		VeleroImage:                target.Image,
		VeleroImageDigest:          target.ImageDigest,
		VeleroNodeAgentImageDigest: target.ImageDigest,
		VeleroServerReady:          true,
		VeleroNodeAgentDesired:     2,
		VeleroNodeAgentReady:       2,
	}

	if veleroUpgradeIsAvailable(cluster, target) {
		t.Fatal("published build suffix must not display Update when the target digest is already fully ready")
	}
}

func TestVeleroUpgradeRemainsAvailableWhenTargetDigestIsNotFullyReady(t *testing.T) {
	target := store.ComponentRelease{Version: "v1.18.2-hcdr.1", Image: "registry.example/hypercdr/velero:v1.18.2-hcdr.1", ImageDigest: "sha256:published"}
	cluster := store.Cluster{
		VeleroVersion:              "v1.18.2",
		VeleroImage:                target.Image,
		VeleroImageDigest:          target.ImageDigest,
		VeleroNodeAgentImageDigest: target.ImageDigest,
		VeleroServerReady:          true,
		VeleroNodeAgentDesired:     2,
		VeleroNodeAgentReady:       1,
	}

	if !veleroUpgradeIsAvailable(cluster, target) {
		t.Fatal("an incomplete target rollout must still offer Update so the platform can reconcile it")
	}
}

func TestVeleroUpgradeDigestVerificationAllowsPackagingVersionSuffix(t *testing.T) {
	cluster := store.Cluster{
		VeleroVersion:              "v1.18.2",
		VeleroImage:                "registry.example/hypercdr/velero:v1.18.2-hcdr.1",
		VeleroImageDigest:          "sha256:published",
		VeleroNodeAgentImageDigest: "sha256:published",
	}
	expectedVersion := "v1.18.2-hcdr.1"
	identityMatches := cluster.VeleroImage == "registry.example/hypercdr/velero:v1.18.2-hcdr.1" && cluster.VeleroVersion == expectedVersion
	digestMatches := cluster.VeleroImageDigest == "sha256:published" && cluster.VeleroNodeAgentImageDigest == "sha256:published"

	if identityMatches || !digestMatches {
		t.Fatal("test fixture must exercise digest verification with a packaging-only version mismatch")
	}
}

func TestComponentUpgradeTimedOut(t *testing.T) {
	now := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		task store.Task
		want bool
	}{
		{name: "agent waiting too long", task: store.Task{Type: "agent-upgrade", Status: "running", StartedAt: now.Add(-11 * time.Minute)}, want: true},
		{name: "velero still within window", task: store.Task{Type: "velero-upgrade", Status: "running", StartedAt: now.Add(-9 * time.Minute)}, want: false},
		{name: "completed task", task: store.Task{Type: "agent-upgrade", Status: "succeeded", StartedAt: now.Add(-20 * time.Minute)}, want: false},
		{name: "unrelated task", task: store.Task{Type: "backup", Status: "running", StartedAt: now.Add(-20 * time.Minute)}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := componentUpgradeTimedOut(test.task, now); got != test.want {
				t.Fatalf("componentUpgradeTimedOut() = %v, want %v", got, test.want)
			}
		})
	}
}
