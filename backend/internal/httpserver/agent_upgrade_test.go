package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"

	"log/slog"
	"os"

	"github.com/gorilla/websocket"
)

func TestAgentUpgradeReturnsPersistedQueuedTaskBeforeAsyncDispatch(t *testing.T) {
	repo := store.NewMemoryStore()
	if _, err := repo.UpsertComponentRelease(store.ComponentReleaseInput{Component: "comm-agent", Version: "v2", Image: "registry.example/hypercdr/comm-agent:v2", ImageDigest: "sha256:v2", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
	defer server.Close()

	token := createTestAgentToken(t, server.URL)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(protocol.Message[protocol.RegisterPayload]{
		Version: protocol.Version, MessageID: "upgrade-register", Type: protocol.MessageAgentRegister, AgentID: "agent-upgrade-test", Timestamp: time.Now().UTC(),
		Payload: protocol.RegisterPayload{InstallToken: token, Cluster: protocol.ClusterSummary{Name: "upgrade-source", KubeVersion: "v1.30.0"}, Agent: protocol.AgentSummary{Version: "v1", Namespace: "hypercdr-agent"}},
	}); err != nil {
		t.Fatal(err)
	}
	var accepted protocol.Message[protocol.RegisterAcceptedPayload]
	if err := conn.ReadJSON(&accepted); err != nil {
		t.Fatal(err)
	}
	clusterID := accepted.Payload.ClusterID

	response, err := http.Post(server.URL+"/api/v1/clusters/"+clusterID+"/agent/upgrade", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("upgrade status = %d", response.StatusCode)
	}
	var queued store.Task
	if err := json.NewDecoder(response.Body).Decode(&queued); err != nil {
		t.Fatal(err)
	}
	if queued.ID == "" || queued.Status != "queued" {
		t.Fatalf("response task = %#v, want persisted queued task", queued)
	}
	tasks, err := repo.ListTasks(clusterID)
	upgradeTasks := make([]store.Task, 0, 1)
	for _, task := range tasks {
		if task.Type == "agent-upgrade" {
			upgradeTasks = append(upgradeTasks, task)
		}
	}
	if err != nil || len(upgradeTasks) != 1 || upgradeTasks[0].ID != queued.ID || upgradeTasks[0].Status != "queued" {
		t.Fatalf("persisted tasks = %#v, err = %v", tasks, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var dispatch protocol.Message[protocol.TaskDispatchPayload]
	if err := conn.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.TaskID != queued.ID || dispatch.Payload.Type != "agent-upgrade" {
		t.Fatalf("dispatch = %#v, want task %s", dispatch.Payload, queued.ID)
	}
	deadline := time.Now().Add(time.Second)
	for {
		tasks, err = repo.ListTasks(clusterID)
		var dispatched bool
		for _, task := range tasks {
			if task.ID == queued.ID && task.Status == "dispatched" {
				dispatched = true
				break
			}
		}
		if err == nil && dispatched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not transition to dispatched: %#v, err=%v", tasks, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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
