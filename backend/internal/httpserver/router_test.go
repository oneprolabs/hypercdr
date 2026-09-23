package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"

	"github.com/gorilla/websocket"
	"github.com/minio/minio-go/v7"
)

func TestTaskProgressPayloadPatchClearsTransferProgressDuringReadiness(t *testing.T) {
	patch := taskProgressPayloadPatch(protocol.TaskProgressPayload{
		Velero: map[string]any{
			"readinessStage": "started",
			"volumeProgress": map[string]any{"bytesDone": int64(100), "totalBytes": int64(100)},
		},
		SizeProgressV2: &protocol.SizeProgressV2{Operation: "restore", ProcessedBytes: 100, TotalBytes: 100},
	})
	value, exists := patch["sizeProgressV2"]
	if !exists || value != nil {
		t.Fatalf("sizeProgressV2 = %#v (exists=%v), want explicit nil", value, exists)
	}
}

func TestAgentCredentialReconnect(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	token := createTestAgentToken(t, server.URL)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"

	accepted := registerTestAgent(t, wsURL, protocol.RegisterPayload{
		InstallToken: token,
		Cluster: protocol.ClusterSummary{
			Name:        "test-cluster",
			KubeVersion: "v1.30.0",
		},
		Agent: protocol.AgentSummary{
			Version:   "test",
			Namespace: "hypercdr-agent",
			PodName:   "agent-0",
		},
		Velero: protocol.VeleroSummary{Status: "ready"},
	}, "")

	if accepted.Payload.AgentCredential == "" {
		t.Fatal("expected issued agent credential")
	}
	if accepted.Payload.ClusterID == "" {
		t.Fatal("expected cluster id")
	}

	reconnected := registerTestAgent(t, wsURL, protocol.RegisterPayload{
		AgentCredential: accepted.Payload.AgentCredential,
		Cluster: protocol.ClusterSummary{
			Name:        "test-cluster",
			KubeVersion: "v1.30.0",
		},
		Agent: protocol.AgentSummary{
			Version:   "test",
			Namespace: "hypercdr-agent",
			PodName:   "agent-0",
		},
		Velero: protocol.VeleroSummary{Status: "ready"},
	}, accepted.Payload.ClusterID)

	if reconnected.Payload.ClusterID != accepted.Payload.ClusterID {
		t.Fatalf("expected reconnect cluster id %q, got %q", accepted.Payload.ClusterID, reconnected.Payload.ClusterID)
	}
	if reconnected.Payload.AgentCredential != accepted.Payload.AgentCredential {
		t.Fatal("expected platform to echo current agent credential on reconnect")
	}
}

func TestFrontendCacheAndMissingAssetBehavior(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>shell</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-hash.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	router := &Router{cfg: config.Config{FrontendDir: dir}}

	for _, tc := range []struct {
		path, cache string
		status      int
	}{
		{path: "/", status: http.StatusOK, cache: "no-store"},
		{path: "/applications/detail", status: http.StatusOK, cache: "no-store"},
		{path: "/assets/app-hash.js", status: http.StatusOK, cache: "public, max-age=31536000, immutable"},
		{path: "/assets/old-hash.js", status: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		router.frontend(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != tc.status {
			t.Fatalf("%s: expected status %d, got %d", tc.path, tc.status, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != tc.cache {
			t.Fatalf("%s: expected Cache-Control %q, got %q", tc.path, tc.cache, got)
		}
	}
}

func TestValidReleaseToken(t *testing.T) {
	for _, test := range []struct {
		name, expected, provided string
		want                     bool
	}{
		{name: "match", expected: "release-secret", provided: "release-secret", want: true},
		{name: "wrong", expected: "release-secret", provided: "another-secret", want: false},
		{name: "missing configured token", provided: "release-secret", want: false},
		{name: "missing provided token", expected: "release-secret", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validReleaseToken(test.expected, test.provided); got != test.want {
				t.Fatalf("validReleaseToken() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDisasterHandoverRejectsOrdinaryAgentTokenAndPublishesRollbackScript(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	ordinary, err := repo.CreateAgentToken(store.DefaultTenantID, "", "ordinary registration", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, server.URL+"/api/v1/disaster-handovers/validate", map[string]string{"token": ordinary.Token})
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ordinary Agent token accepted for disaster handover: %d", response.StatusCode)
	}
	disaster, err := repo.CreateAgentToken(store.DefaultTenantID, "", "disaster-handover:migration-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	response = postJSON(t, server.URL+"/api/v1/disaster-handovers/validate", map[string]string{"token": disaster.Token})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("disaster token rejected: %d", response.StatusCode)
	}
	scriptResponse, err := http.Get(server.URL + "/disaster-handover.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer scriptResponse.Body.Close()
	script, _ := io.ReadAll(scriptResponse.Body)
	for _, required := range []string{`NAMESPACE="hypercdr-agent"`, "hypercdr-agent-handover", "PreviousEndpoint", "RollbackDeadline"} {
		if !bytes.Contains(script, []byte(required)) {
			t.Fatalf("disaster script is missing %q", required)
		}
	}
}

func TestPasswordResetFlow(t *testing.T) {
	repo := store.NewMemoryStore()
	if _, err := repo.CreateUser(store.DefaultTenantID, "reset-user@example.com", "old-password"); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewRouter(config.Config{PasswordResetRevealToken: true}, logger, repo))
	defer server.Close()

	response, err := http.Post(server.URL+"/api/v1/auth/forgot-password", "application/json", strings.NewReader(`{"email":"reset-user@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("forgot password status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var forgot struct {
		ResetToken string `json:"resetToken"`
	}
	if err = json.NewDecoder(response.Body).Decode(&forgot); err != nil {
		t.Fatal(err)
	}
	if forgot.ResetToken == "" {
		t.Fatal("forgot password response did not contain the test reset token")
	}

	resetBody, _ := json.Marshal(map[string]string{"token": forgot.ResetToken, "password": "new-password"})
	response, err = http.Post(server.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(resetBody))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reset password status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if _, ok, err := repo.AuthenticateUser(store.UserAuthInput{Email: "reset-user@example.com", Password: "new-password"}); err != nil || !ok {
		t.Fatalf("new password authentication failed: ok=%v err=%v", ok, err)
	}

	response, err = http.Post(server.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(resetBody))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("reused reset token status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestClusterRoleAndDefault(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	first := registerClusterViaWS(t, server.URL, "source-cluster")
	second := registerClusterViaWS(t, server.URL, "target-cluster")

	patchBody := bytes.NewReader([]byte(`{"role":"source"}`))
	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/clusters/"+first+"/", patchBody)
	if err != nil {
		t.Fatal(err)
	}
	req.URL.Path = "/api/v1/clusters/" + first
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected update status 200, got %d", resp.StatusCode)
	}

	resp, err = http.Post(server.URL+"/api/v1/clusters/"+first+"/default", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first default status 200, got %d", resp.StatusCode)
	}
	resp, err = http.Post(server.URL+"/api/v1/clusters/"+second+"/default", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected second default status 200, got %d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/api/v1/clusters")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Items []store.Cluster `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	defaultCount := 0
	for _, cluster := range body.Items {
		if cluster.ID == first && cluster.Role != "source" {
			t.Fatalf("expected first cluster role source, got %q", cluster.Role)
		}
		if cluster.IsDefault {
			defaultCount++
			if cluster.ID != second {
				t.Fatalf("expected second cluster as default, got %s", cluster.ID)
			}
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default cluster, got %d", defaultCount)
	}
}

func TestSchedulerCreatesSingleBackupTaskPerPlan(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := &Router{store: repo, logger: logger, hub: newSessionHub()}

	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name: "minio", Type: "s3", Endpoint: "http://minio:9000", Bucket: "bucket",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID: clusterID, StorageRepoID: storage.ID, SourceClusterID: clusterID,
		BSLName: storageDomainBSLName(storage, clusterID), ObjectPrefix: storageDomainPrefix(storage.TenantID, clusterID), Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(store.PolicyInput{
		Name: "hourly", ScheduleType: "interval", IntervalValue: 1, IntervalUnit: "hour", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		SourceClusterID: clusterID, AppID: app.ID, AppIDs: []string{app.ID}, StorageRepoID: storage.ID,
		PolicyID: policy.ID, ScopeType: "all", Status: "active",
		ResourceSelection: store.ResourceSelection{
			Mode:            "custom",
			NamespaceScoped: []string{"deployments.apps", "persistentvolumeclaims"},
			ClusterScoped:   []string{"storageclasses.storage.k8s.io"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 6, 3, 0, 0, 0, time.UTC)
	if _, err := repo.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{
		ProtectionPlanID: plan.ID, NextFireAt: now.Add(-time.Minute), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	router.runSchedulerTick(now)
	tasks, err := repo.ListTasks(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	backupCount := 0
	for _, task := range tasks {
		if task.Type != "backup" {
			continue
		}
		backupCount++
		if task.ProtectionPlanID != plan.ID {
			t.Fatalf("scheduled backup plan id = %q, want %q", task.ProtectionPlanID, plan.ID)
		}
		if got := taskPayloadString(task.Payload, "trigger"); got != "scheduled" {
			t.Fatalf("scheduled backup trigger = %q, want scheduled", got)
		}
		if got := resourceSelectionPayload(task.Payload); !reflect.DeepEqual(got, plan.ResourceSelection) {
			t.Fatalf("scheduled backup resource selection = %#v, want %#v", got, plan.ResourceSelection)
		}
	}
	if backupCount != 1 {
		t.Fatalf("backup task count = %d, want 1", backupCount)
	}
	updatedPlan, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok {
		t.Fatalf("get scheduled plan: ok=%v err=%v", ok, err)
	}
	if updatedPlan.LatestSyncTaskID == "" {
		t.Fatal("scheduled backup did not update latest sync task pointer")
	}

	router.runSchedulerTick(now.Add(time.Second))
	tasks, err = repo.ListTasks(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	backupCount = 0
	for _, task := range tasks {
		if task.Type == "backup" {
			backupCount++
		}
	}
	if backupCount != 1 {
		t.Fatalf("backup task count after second tick = %d, want 1", backupCount)
	}
}

func seedSchedulerCluster(t *testing.T, repo *store.MemoryStore) string {
	t.Helper()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{
		Token:         token.Token,
		ClusterName:   "source",
		KubeVersion:   "v1.30.0",
		AgentVersion:  "test",
		VeleroVersion: "v1.17.1",
		VeleroStatus:  "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cluster.ID
}

func seedSchedulerApplication(t *testing.T, repo *store.MemoryStore, clusterID string, namespace string) store.Application {
	t.Helper()
	_, _, err := repo.ApplyInventory(store.InventoryInput{
		ClusterID:      clusterID,
		KubeVersion:    "v1.30.0",
		VeleroStatus:   "ready",
		NodeCount:      1,
		NamespaceCount: 1,
		CollectedAt:    time.Now().UTC(),
		Apps: []store.Application{{
			ClusterID:        clusterID,
			Namespace:        namespace,
			Name:             namespace,
			Status:           "healthy",
			ProtectionStatus: "unprotected",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if app.Namespace == namespace {
			return app
		}
	}
	t.Fatalf("seeded app %q not found", namespace)
	return store.Application{}
}

func TestDeleteCluster(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	clusterID := registerClusterViaWS(t, server.URL, "temporary-cluster")
	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/clusters/"+clusterID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected delete status 409 without force, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/clusters/"+clusterID+"?force=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected force delete status 200, got %d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/api/v1/clusters")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Items []store.Cluster `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, cluster := range body.Items {
		if cluster.ID == clusterID {
			t.Fatalf("expected cluster %s to be deleted", clusterID)
		}
	}

	req, err = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/clusters/"+clusterID+"?force=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected second delete status 404, got %d", resp.StatusCode)
	}
}

func TestCreateBackupTaskRequiresProtectionPlan(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	clusterID, appID := seedBackupTarget(t, repo)

	resp := postJSON(t, server.URL+"/api/v1/tasks/backup", map[string]any{
		"clusterId":       clusterID,
		"appId":           appID,
		"sourceNamespace": "demo-mysql-csi",
		"scope":           "namespace",
		"storageRepo":     "default",
		"labelSelector":   map[string]any{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected backup without protection plan to return 409, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "protection_plan_required" {
		t.Fatalf("expected protection_plan_required, got %v", body["error"])
	}
}

func TestCreateBackupTaskResolvesProtectionPlanForApp(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	clusterID, appID := seedBackupTarget(t, repo)
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		SourceClusterID: clusterID,
		AppIDs:          []string{appID},
		ScopeType:       "all",
		Status:          "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, server.URL+"/api/v1/tasks/backup", map[string]any{
		"clusterId":       clusterID,
		"appId":           appID,
		"sourceNamespace": "demo-mysql-csi",
		"scope":           "namespace",
		"storageRepo":     "default",
		"labelSelector":   map[string]any{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected backup with resolved protection plan to return 201, got %d", resp.StatusCode)
	}
	var body struct {
		Task store.Task `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Task.ProtectionPlanID != plan.ID {
		t.Fatalf("expected task protection plan %q, got %q", plan.ID, body.Task.ProtectionPlanID)
	}
	updatedPlan, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok {
		t.Fatalf("get plan after manual backup: ok=%v err=%v", ok, err)
	}
	if updatedPlan.LatestSyncTaskID != body.Task.ID {
		t.Fatalf("latest sync task id = %q, want %q", updatedPlan.LatestSyncTaskID, body.Task.ID)
	}
}

func TestUnregisterClusterOfflineIsBlockedBeforeTaskCreation(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
	defer server.Close()

	clusterID := registerClusterViaWS(t, server.URL, "temporary-cluster")
	// WebSocket close handling is asynchronous. Wait until the hub has removed
	// the short-lived test connection before asserting the offline guard.
	deadline := time.Now().Add(time.Second)
	for {
		precheck, precheckErr := http.Get(server.URL + "/api/v1/clusters/" + clusterID + "/unregister/precheck")
		if precheckErr != nil {
			t.Fatal(precheckErr)
		}
		var audit unregisterAudit
		decodeErr := json.NewDecoder(precheck.Body).Decode(&audit)
		precheck.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if !audit.AgentOnline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test agent remained online after WebSocket closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp, err := http.Post(server.URL+"/api/v1/clusters/"+clusterID+"/unregister", "application/json", strings.NewReader(`{"reason":"test cleanup"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected unregister status 409, got %d", resp.StatusCode)
	}
	var body struct {
		Error    string          `json:"error"`
		Precheck unregisterAudit `json:"precheck"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "unregister_precheck_blocked" || body.Precheck.AgentOnline || body.Precheck.Allowed {
		t.Fatalf("unexpected offline precheck response: %#v", body)
	}
	tasks, err := repo.ListTasks(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Type == "unregister" {
			t.Fatalf("offline precheck must not create an unregister task: %#v", tasks)
		}
	}
}

func TestStorageRepositorySecretsAreNotReturned(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	payload := `{
		"name":"minio-primary",
		"type":"S3",
		"endpoint":"http://minio.example:9000",
		"bucket":"hypercdr",
		"region":"us-east-1",
		"tlsEnabled":false,
		"accessKey":"minio-access",
		"secretKey":"minio-secret"
	}`
	resp, err := http.Post(server.URL+"/api/v1/storage-repositories", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d", resp.StatusCode)
	}
	var createdRaw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&createdRaw); err != nil {
		t.Fatal(err)
	}
	if _, ok := createdRaw["secretKey"]; ok {
		t.Fatal("create response must not include secretKey")
	}
	if _, ok := createdRaw["accessKey"]; ok {
		t.Fatal("create response must not include accessKey")
	}

	resp, err = http.Get(server.URL + "/api/v1/storage-repositories")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listRaw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listRaw); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(listRaw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "minio-secret") || strings.Contains(string(encoded), "minio-access") {
		t.Fatalf("list response leaked credentials: %s", string(encoded))
	}
}

func TestUpdateStorageRepositoryRevalidatesStatus(t *testing.T) {
	objectStorage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected authenticated S3 GET probe, got %s", req.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>hypercdr</Name><IsTruncated>false</IsTruncated></ListBucketResult>`))
	}))
	defer objectStorage.Close()

	repo := store.NewMemoryStore()
	storageRepo, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name: "minio-primary", Type: "S3", Endpoint: objectStorage.URL, Bucket: "hypercdr", AccessKey: "test-access", SecretKey: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	payload := `{"name":"minio-primary","type":"S3","endpoint":"` + objectStorage.URL + `","bucket":"hypercdr","region":"","tlsEnabled":false,"config":{"urlStyle":"path"}}`
	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/storage-repositories/"+storageRepo.ID, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected update status 200, got %d", resp.StatusCode)
	}
	var updated store.StorageRepository
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != "connected" || updated.LastValidatedAt.IsZero() {
		t.Fatalf("expected connected repository with validation time, got %#v", updated)
	}
}

func TestVeleroCRDsEndpoint(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	resp, err := http.Get(server.URL + "/assets/velero/v1.18.2/crds.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	text := body.String()
	if !strings.Contains(text, "kind: CustomResourceDefinition") || !strings.Contains(text, "backups.velero.io") {
		t.Fatalf("unexpected crd payload prefix: %.200s", text)
	}
}

func TestRecoveryTaskRequiresOriginalNamespaceConfirmation(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	body := bytes.NewBufferString(`{
		"clusterId":"cluster-a",
		"veleroBackupName":"backup-a",
		"sourceNamespace":"demo-mysql-csi",
		"targetNamespace":"demo-mysql-csi",
		"conflictPolicy":"overwrite"
	}`)
	resp, err := http.Post(server.URL+"/api/v1/tasks/drill", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "original_namespace_confirmation_required" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestRecoveryTaskRejectsDataOnlyRestoreWhenExecutorDisabled(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	body := bytes.NewBufferString(`{
		"clusterId":"cluster-a",
		"veleroBackupName":"backup-a",
		"sourceNamespace":"demo-mysql-csi",
		"targetNamespace":"demo-mysql-csi",
		"restoreMode":"dataOnly",
		"conflictPolicy":"overwrite",
		"originalNamespaceConfirmed":true
	}`)
	resp, err := http.Post(server.URL+"/api/v1/tasks/drill", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "data_only_restore_not_enabled" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestDrillTaskRejectsDuplicateActiveTask(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	_, err := repo.CreateTask(store.TaskInput{
		ClusterID: "cluster-a",
		Type:      "drill",
		Status:    "running",
		Payload: map[string]any{
			"sourceNamespace": "demo-mysql-csi",
			"targetNamespace": "demo-mysql-csi-drill",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{
		"clusterId":"cluster-a",
		"veleroBackupName":"backup-a",
		"sourceNamespace":"demo-mysql-csi",
		"targetNamespace":"demo-mysql-csi-drill-2",
		"restoreMode":"full"
	}`)
	resp, err := http.Post(server.URL+"/api/v1/tasks/drill", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "active_drill_task_exists" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestInstallScriptIncludesVeleroInstaller(t *testing.T) {
	repo := store.NewMemoryStore()
	if _, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: "active", APIImage: "registry.local:5000/hypercdr/platform-api:active", APIImageDigest: "sha256:api", FrontendImage: "registry.local:5000/hypercdr/platform-frontend:active", FrontendImageDigest: "sha256:frontend", Status: "active", ComponentManifest: map[string]store.ReleaseComponent{
		"comm-agent":                        {Version: "active", Image: "registry.local:5000/hypercdr/comm-agent:active", ImageDigest: "sha256:agent"},
		"oadp-comm-agent":                   {Version: "active", Image: "registry.local:5000/hypercdr/oadp-comm-agent:active", ImageDigest: "sha256:oadp-agent"},
		"oadp-catalog":                      {Version: "stable-1.3", Image: "registry.local:5000/hypercdr/oadp-catalog:stable-1.3", ImageDigest: "sha256:oadp-catalog"},
		"velero":                            {Version: "v1.17.2", Image: "registry.local:5000/hypercdr/velero:v1.17.2", ImageDigest: "sha256:velero"},
		"velero-plugin-for-aws":             {Version: "v1.13.0", Image: "registry.local:5000/hypercdr/velero-plugin-for-aws:v1.13.0", ImageDigest: "sha256:aws"},
		"velero-plugin-for-microsoft-azure": {Version: "v1.13.0", Image: "registry.local:5000/hypercdr/velero-plugin-for-microsoft-azure:v1.13.0", ImageDigest: "sha256:azure"},
		"velero-plugin-for-gcp":             {Version: "v1.13.0", Image: "registry.local:5000/hypercdr/velero-plugin-for-gcp:v1.13.0", ImageDigest: "sha256:gcp"},
	}}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{
		AgentImage:        "registry.local:5000/hypercdr/comm-agent:test",
		AgentNamespace:    "hypercdr-agent",
		VeleroImage:       "registry.local:5000/hypercdr/velero:v1.17.1",
		VeleroAWSPlugin:   "registry.local:5000/hypercdr/velero-plugin-for-aws:v1.13.0",
		VeleroAzurePlugin: "registry.local:5000/hypercdr/velero-plugin-for-microsoft-azure:v1.13.0",
		VeleroGCPPlugin:   "registry.local:5000/hypercdr/velero-plugin-for-gcp:v1.13.0",
	}, logger, repo))
	defer server.Close()

	resp, err := http.Get(server.URL + "/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	text := body.String()
	for _, expected := range []string{
		"registry.local:5000/hypercdr/comm-agent:active",
		"registry.local:5000/hypercdr/oadp-comm-agent:active",
		"registry.local:5000/hypercdr/oadp-catalog:stable-1.3",
		`WAIT_TIMEOUT="300s"`,
		"single canonical namespace 'hypercdr-agent'",
		"Checking whether this cluster is already managed by HyperCDR",
		"Community and Enterprise use the same Agent and cannot coexist in one cluster.",
		"Use Community Migration for a normal edition upgrade",
		`serviceAccountName: default`,
		`automountServiceAccountToken: false`,
		`rollout status "${workload_kind}/${workload_name}" --timeout=2s`,
		`[[ -n "$pending_pvcs" ]]`,
		"/assets/velero/v1.18.2/crds.yaml",
		"--concurrent-backups=2",
		"--node-agent-configmap=node-agent-config",
		"--backup-repository-configmap=backup-repository-config",
		"name: backup-repository-config",
		`"cacheLimitMB": 5120`,
		`"prepareQueueLength": 4`,
		`"cachePVC"`,
		"resources: [\"services\"]\n    verbs: [\"patch\", \"update\"]",
		"resources: [\"customresourcedefinitions\"]\n    verbs: [\"get\", \"list\", \"watch\", \"create\", \"patch\", \"update\", \"delete\"]",
		"kind: Deployment",
		"type: Recreate",
		"name: velero",
		"kind: DaemonSet",
		"name: node-agent",
		"name: NODE_NAME",
		"fieldPath: spec.nodeName",
		"name: VELERO_NAMESPACE",
		"fieldPath: metadata.namespace",
		"registry.local:5000/hypercdr/velero:v1.17.2",
		"registry.local:5000/hypercdr/velero-plugin-for-aws:v1.13.0",
		"name: velero-plugin-for-aws",
		"registry.local:5000/hypercdr/velero-plugin-for-microsoft-azure:v1.13.0",
		"name: velero-plugin-for-microsoft-azure",
		"registry.local:5000/hypercdr/velero-plugin-for-gcp:v1.13.0",
		"name: velero-plugin-for-gcp",
		"mountPath: /target",
		"mountPath: /plugins",
		"Velero AWS ObjectStore plugin is installed",
		`if ! kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state`,
		"Keeping existing comm-agent state PVC and StorageClass",
		"--storage-class",
		"select_agent_storage_class",
		"Default StorageClass detected",
		"Select a StorageClass for the HyperCDR Agent state volume",
		"Enter selection [1-%d]",
		"Rerun this command with: --storage-class <name>",
		"Installation stopped: no StorageClass is available in this cluster.",
		"storageClassName: ${STORAGE_CLASS}",
		"exec 3<>/dev/tty",
		"--registry-server",
		"create secret docker-registry",
		"IMAGE_PULL_SECRETS_BLOCK",
		"--reset-agent-credential",
		"delete secret hypercdr-agent-credential",
		"name: HCDR_PLATFORM_TLS_INSECURE_SKIP_VERIFY",
		"value: \"true\"",
		"download_url \"$VELERO_CRDS_URL\" \"$crds_file\"",
		"kubectl_retry kubectl apply -f \"$crds_file\"",
		"wait_for_rollout deployment hypercdr-comm-agent",
		"Kubernetes API server is currently unavailable; waiting for recovery",
		"Reason: a persistent volume could not be attached or mounted.",
		"Reason: a required container image could not be pulled.",
		"If this cluster is already Online in HyperCDR, do not register it again.",
		"generate a new registration command in HyperCDR before retrying",
		"TENANT_LICENSE_[A-Z_]+",
		"rollback_failed_registration",
		`trap 'status=130; if [[ "$ROLLBACK_ACTIVE" == "true" ]]; then rollback_failed_registration; fi; exit $status' TERM INT`,
		"Failed first-time installation was rolled back",
		"Isolated installation preflight",
		"provider_huaweicloud_cce_dynamic_pvc_preflight",
		`"$HOME/.kube"/*kubeconfig*`,
		`IMAGE_PULL_PREFLIGHT_STRATEGY="sequential"`,
		"Dynamic PVC provisioning passed",
		`target_namespace="${PREFLIGHT_NAMESPACE:-$NAMESPACE}"`,
		"configmap hypercdr-agent-uninstaller",
		"/uninstall-agent.sh",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected install script to contain %q", expected)
		}
	}
	if strings.Contains(text, "kubectl_retry kubectl apply -f \"$VELERO_CRDS_URL\"") {
		t.Fatal("expected install script to download Velero CRDs before kubectl apply so self-signed HTTPS works")
	}
	if strings.Contains(text, "kubectl delete crd") {
		t.Fatal("registration rollback must not delete cluster-scoped Velero CRDs shared by another HyperCDR edition")
	}
	if strings.Contains(text, "{{PLATFORM_CA_URL}}") {
		t.Fatal("generated install script must contain the resolved platform CA URL")
	}
	if !strings.Contains(text, "/assets/platform/ca.crt") {
		t.Fatal("generated install script must download the platform CA from this control plane")
	}
	if strings.Contains(text, "Platform TLS certificate is unavailable; Agent TLS verification fallback is enabled") {
		t.Fatal("agent installation must never silently fall back to insecure platform TLS")
	}
	if strings.Contains(text, "supports isolated Community and Enterprise Velero instances") {
		t.Fatal("installer must not advertise dual-edition Agent coexistence")
	}
	command := exec.Command("bash", "-n")
	command.Stdin = strings.NewReader(text)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated install script has invalid shell syntax: %v\n%s", err, output)
	}
}

func TestAgentOfflineUninstallScriptIsSafeByDefault(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	resp, err := http.Get(server.URL + "/uninstall-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"Dry-run only", "--execute", "--force-finalizers", "Refusing cleanup", "Application namespaces, Longhorn"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("offline uninstaller missing %q", expected)
		}
	}
}

func TestDetailedTaskFailureMessageDoesNotDuplicateStatusMessage(t *testing.T) {
	message := `BackupStorageLocation "minio" is unavailable: unable to locate ObjectStore plugin named velero.io/aws`
	details := map[string]any{"velero": map[string]any{"status": map[string]any{"message": message}}}
	if got := detailedTaskFailureMessage(message, details); got != message {
		t.Fatalf("expected one failure message, got %q", got)
	}
}

func TestPrepareNodeScriptInstallsRegistryCA(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	caPath := filepath.Join(t.TempDir(), "registry-ca.crt")
	caData := []byte("test-private-registry-ca\n")
	if err := os.WriteFile(caPath, caData, 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(config.Config{
		ImageRegistry:  "192.168.8.149/hypercdr",
		RegistryCAPath: caPath,
	}, logger, repo))
	defer server.Close()

	resp, err := http.Get(server.URL + "/prepare-node.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	text := body.String()
	for _, expected := range []string{
		`REGISTRY_HOST="192.168.8.149"`,
		"/assets/registry/ca.crt",
		"/etc/containerd/certs.d/${REGISTRY_HOST}",
		"/etc/docker/certs.d/${REGISTRY_HOST}",
		"hosts.toml",
		"systemctl restart containerd",
		"systemctl restart docker",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected prepare node script to contain %q", expected)
		}
	}
	caResponse, err := http.Get(server.URL + "/assets/registry/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	defer caResponse.Body.Close()
	servedCA, err := io.ReadAll(caResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(servedCA, caData) {
		t.Fatalf("served registry CA differs from configured CA: got %q want %q", servedCA, caData)
	}
}

func TestPublicRegistryOmitsNodeCAPreparation(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
	defer server.Close()

	response, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"description":"public-registry"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["prepareNodeCommand"]; exists {
		t.Fatalf("public registry response must not include prepareNodeCommand: %#v", body)
	}
	prepareResponse, err := http.Get(server.URL + "/prepare-node.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer prepareResponse.Body.Close()
	if prepareResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("prepare-node status = %d, want 404 without private CA", prepareResponse.StatusCode)
	}
}

func TestAgentTokenInstallCommandUsesKubernetesMode(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"description":"test"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		InstallCommand string `json:"installCommand"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.InstallCommand, "--executor-mode kubernetes") {
		t.Fatalf("expected kubernetes executor install command, got %q", body.InstallCommand)
	}
	if !strings.Contains(body.InstallCommand, "--namespace hypercdr-agent") {
		t.Fatalf("expected namespace in install command, got %q", body.InstallCommand)
	}
	if !strings.Contains(body.InstallCommand, "--install-registry-ca false") {
		t.Fatalf("expected install command to skip registry CA after prepare-node step, got %q", body.InstallCommand)
	}
}

func TestCCEAgentTokenInstallCommandUsesDualEndpoints(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := config.Config{AgentNamespace: "hypercdr-agent", AgentPrivateWSEndpoint: "wss://10.0.0.10:3102/ws/agent", AgentPublicWSEndpoint: "wss://203.0.113.10:3102/ws/agent"}
	server := httptest.NewServer(NewRouter(cfg, logger, repo))
	defer server.Close()
	resp, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"clusterType":"huaweicloud-cce"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ ClusterType, InstallCommand string }
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--cluster-type huaweicloud-cce", "--endpoint wss://10.0.0.10:3102/ws/agent", "--endpoint-public wss://203.0.113.10:3102/ws/agent"} {
		if !strings.Contains(body.InstallCommand, expected) {
			t.Fatalf("CCE install command %q does not contain %q", body.InstallCommand, expected)
		}
	}
	if strings.Contains(body.InstallCommand, "--endpoint-private") {
		t.Fatalf("CCE install command must expose the primary address as --endpoint: %q", body.InstallCommand)
	}
	if body.ClusterType != "huaweicloud-cce" {
		t.Fatalf("cluster type = %q", body.ClusterType)
	}
}

func TestNativeAgentTokenInstallCommandUsesDualEndpoints(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := config.Config{AgentNamespace: "hypercdr-agent", AgentPrivateWSEndpoint: "wss://10.0.0.10:3002/ws/agent", AgentPublicWSEndpoint: "wss://203.0.113.10:3002/ws/agent"}
	server := httptest.NewServer(NewRouter(cfg, logger, repo))
	defer server.Close()
	resp, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"clusterType":"native-kubernetes"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ ClusterType, InstallCommand string }
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--cluster-type native-kubernetes", "--endpoint wss://10.0.0.10:3002/ws/agent", "--endpoint-public wss://203.0.113.10:3002/ws/agent"} {
		if !strings.Contains(body.InstallCommand, expected) {
			t.Fatalf("native install command %q does not contain %q", body.InstallCommand, expected)
		}
	}
	if strings.Contains(body.InstallCommand, "--endpoint-private") {
		t.Fatalf("native install command contains a legacy endpoint argument: %q", body.InstallCommand)
	}
}

func TestOpenShiftAgentTokenPreservesSelectedClusterType(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := config.Config{AgentNamespace: "hypercdr-agent", AgentPrivateWSEndpoint: "wss://10.0.0.10:3002/ws/agent"}
	server := httptest.NewServer(NewRouter(cfg, logger, repo))
	defer server.Close()
	resp, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"clusterType":"openshift"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ ClusterType, InstallCommand string }
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ClusterType != "openshift" || !strings.Contains(body.InstallCommand, "--cluster-type openshift") {
		t.Fatalf("unexpected OpenShift registration response: %#v", body)
	}
	if !strings.Contains(body.InstallCommand, "--namespace openshift-adp") {
		t.Fatalf("OpenShift registration must use openshift-adp namespace: %q", body.InstallCommand)
	}
}

func TestInstallScriptDoesNotReplacePrimaryEndpointWithPublicFallback(t *testing.T) {
	if strings.Contains(installScriptTemplate, `elif [[ -n "$ENDPOINT_PUBLIC" ]]; then ENDPOINT="$ENDPOINT_PUBLIC"`) {
		t.Fatal("install script overwrites an explicit primary endpoint with the public fallback")
	}
	want := `if [[ -z "$ENDPOINT" ]]; then`
	if !strings.Contains(installScriptTemplate, want) {
		t.Fatalf("install script is missing the guarded legacy endpoint fallback %q", want)
	}
}

func TestStorageDomainPrefixIncludesTenantAndCluster(t *testing.T) {
	prefix := storageDomainPrefix("tenant-a", "cluster-b")
	if prefix != "hypercdr/v1/tenants/tenant-a/clusters/cluster-b" {
		t.Fatalf("prefix = %q", prefix)
	}
	if !validStorageDomainPrefix(prefix + "/") {
		t.Fatal("expected complete tenant cluster prefix to be valid")
	}
	for _, unsafe := range []string{"", "hypercdr/", "hypercdr/v1/", "hypercdr/v1/tenants/tenant-a/", "hypercdr/clusters/cluster-b/"} {
		if validStorageDomainPrefix(unsafe) {
			t.Fatalf("unsafe prefix accepted: %q", unsafe)
		}
	}
}

func TestAgentTokenInstallCommandSkipsSelfSignedHTTPS(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	caPath := filepath.Join(t.TempDir(), "registry-ca.crt")
	if err := os.WriteFile(caPath, []byte("test-ca"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent", RegistryCAPath: caPath}, logger, repo))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/agent-tokens", bytes.NewReader([]byte(`{"description":"test"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "192.168.8.149:18080")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		PrepareNodeCommand string `json:"prepareNodeCommand"`
		InstallCommand     string `json:"installCommand"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.PrepareNodeCommand, "curl -k -sSL https://192.168.8.149:18080/prepare-node.sh") {
		t.Fatalf("expected HTTPS prepare-node command to skip self-signed cert verification, got %q", body.PrepareNodeCommand)
	}
	if !strings.HasPrefix(body.InstallCommand, "curl -k -sSL https://192.168.8.149:18080/install.sh") {
		t.Fatalf("expected HTTPS install command to skip self-signed cert verification, got %q", body.InstallCommand)
	}
}

func TestStorageCredentialsBuildAgentPayload(t *testing.T) {
	credentials := storageCredentials(store.StorageRepository{
		Secret: map[string]string{
			"accessKey": "minio-access",
			"secretKey": "minio-secret",
		},
	})
	if credentials == nil {
		t.Fatal("expected credentials")
	}
	if credentials.AccessKey != "minio-access" || credentials.SecretKey != "minio-secret" {
		t.Fatalf("unexpected credentials: %#v", credentials)
	}
}

func TestStorageBucketLookupHonorsRepositoryURLStyle(t *testing.T) {
	tests := []struct {
		name  string
		style string
		want  minio.BucketLookupType
	}{
		{name: "OBS virtual host", style: "virtual", want: minio.BucketLookupDNS},
		{name: "virtual host label", style: "Virtual-host", want: minio.BucketLookupDNS},
		{name: "MinIO path", style: "path", want: minio.BucketLookupPath},
		{name: "unspecified", style: "", want: minio.BucketLookupAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := store.StorageRepository{Config: map[string]any{"urlStyle": tt.style}}
			if got := storageBucketLookup(repo); got != tt.want {
				t.Fatalf("lookup = %v, want %v for style %q", got, tt.want, tt.style)
			}
		})
	}
}

func TestBuildStoredStorageSyncDispatchIncludesCredentials(t *testing.T) {
	repo := store.NewMemoryStore()
	storageRepo, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name:       "minio-primary",
		Type:       "S3",
		Endpoint:   "http://minio.example:9000",
		Bucket:     "hypercdr",
		Region:     "us-east-1",
		TLSEnabled: false,
		AccessKey:  "minio-access",
		SecretKey:  "minio-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{
		ClusterID: "cluster-a",
		Type:      "storage-sync",
		Status:    "queued",
		CommandID: store.NewPublicID(),
		Payload: map[string]any{
			"repositoryId": storageRepo.ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := &Router{cfg: config.Config{}, logger: logger, store: repo}
	dispatch, err := router.buildStoredTaskDispatch(task)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.StorageSync == nil {
		t.Fatal("expected storage sync payload")
	}
	credentials := dispatch.Payload.StorageSync.Credentials
	if credentials == nil || credentials.AccessKey != "minio-access" || credentials.SecretKey != "minio-secret" {
		t.Fatalf("unexpected credentials: %#v", credentials)
	}
}

func TestBuildStoredUnregisterDispatch(t *testing.T) {
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{
		ClusterID: "cluster-a",
		Type:      "unregister",
		Status:    "queued",
		CommandID: store.NewPublicID(),
		Payload: map[string]any{
			"clusterId":       "cluster-a",
			"namespace":       "hypercdr-agent",
			"deleteVelero":    true,
			"deleteNamespace": true,
			"reason":          "test cleanup",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := &Router{cfg: config.Config{AgentNamespace: "hypercdr-agent"}, logger: logger, store: repo}
	dispatch, err := router.buildStoredTaskDispatch(task)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.Unregister == nil {
		t.Fatal("expected unregister payload")
	}
	if dispatch.Payload.Unregister.ClusterID != "cluster-a" ||
		dispatch.Payload.Unregister.Namespace != "hypercdr-agent" ||
		!dispatch.Payload.Unregister.DeleteVelero ||
		!dispatch.Payload.Unregister.DeleteNamespace {
		t.Fatalf("unexpected unregister dispatch: %#v", dispatch.Payload.Unregister)
	}
}

func TestBuildStoredOADPUpgradeDispatch(t *testing.T) {
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{ClusterID: "openshift-a", Type: "velero-upgrade", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"namespace": "openshift-adp", "oadp": true, "catalogImage": "registry/oadp-catalog@sha256:catalog", "oadpPackage": "oadp-operator", "oadpChannel": "stable-1.3", "oadpTargetCsv": "oadp-operator.v1.3.10", "image": "registry/oadp-velero@sha256:velero", "version": "1.3.10", "expectedDigest": "sha256:velero",
	}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{cfg: config.Config{AgentNamespace: "hypercdr-agent"}, store: repo}
	dispatch, err := router.buildStoredTaskDispatch(task)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatch.Payload.VeleroUpgrade
	if command == nil || !command.OADP || command.Namespace != "openshift-adp" || command.CatalogImage != "registry/oadp-catalog@sha256:catalog" || command.OADPPackage != "oadp-operator" || command.OADPChannel != "stable-1.3" || command.OADPTargetCSV != "oadp-operator.v1.3.10" {
		t.Fatalf("unexpected OADP dispatch: %#v", command)
	}
}

func TestBuildStoredScheduleSyncDispatch(t *testing.T) {
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{ClusterID: "cluster-a", Type: "schedule-sync", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"planId": "plan-a", "scheduleName": "hcdr-plan-a", "cron": "0 * * * *", "sourceNamespaces": []string{"demo"}, "storageRepo": "repo-a",
		"includedResources": []string{"deployments.apps"}, "labelSelector": map[string]any{"matchLabels": map[string]any{"app": "demo"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{cfg: config.Config{AgentNamespace: "hypercdr-agent"}, store: repo}
	dispatch, err := router.buildStoredTaskDispatch(task)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.ScheduleSync == nil || dispatch.Payload.ScheduleSync.PlanID != "plan-a" || dispatch.Payload.ScheduleSync.SourceNamespaces[0] != "demo" {
		t.Fatalf("unexpected schedule dispatch: %#v", dispatch.Payload.ScheduleSync)
	}
	command := dispatch.Payload.ScheduleSync
	if len(command.IncludedResources) != 1 || command.IncludedResources[0] != "deployments.apps" || command.Selector.MatchLabels["app"] != "demo" {
		t.Fatalf("schedule filters were not preserved: %#v", command)
	}
}

func TestOpenShiftUsesOADPNamespace(t *testing.T) {
	repo := store.NewMemoryStore()
	token, err := repo.CreateAgentToken("tenant-a", "admin", "openshift", time.Hour, "openshift")
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterType: "openshift", ClusterName: "ocp-415"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{cfg: config.Config{AgentNamespace: "hypercdr-agent"}, store: repo}
	if got := router.agentNamespaceForCluster(cluster.ID); got != "openshift-adp" {
		t.Fatalf("OpenShift agent namespace = %q", got)
	}
	if got := router.dataProtectionNamespaceForCluster(cluster.ID); got != "openshift-adp" {
		t.Fatalf("OpenShift data protection namespace = %q", got)
	}
	if got := router.agentNamespaceForCluster("unknown"); got != "hypercdr-agent" {
		t.Fatalf("native fallback namespace = %q", got)
	}
	if got := router.dataProtectionNamespaceForCluster("unknown"); got != "hypercdr-agent" {
		t.Fatalf("native data protection namespace = %q", got)
	}
}

func TestFinishUnregisterDoesNotAccessObjectStorage(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "source-cluster")
	storageRepo, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name:      "minio-primary",
		Type:      "S3",
		Endpoint:  "http://minio.example:9000",
		Bucket:    "hypercdr",
		AccessKey: "minio-access",
		SecretKey: "minio-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "unregister", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}

	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	called := false
	cleanObjectStoragePrefix = func(_ context.Context, repo store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		called = true
		if repo.ID != storageRepo.ID {
			t.Fatalf("unexpected repository %s", repo.ID)
		}
		expectedPrefix := storageDomainPrefix(store.DefaultTenantID, clusterID) + "/"
		if prefix != expectedPrefix {
			t.Fatalf("expected cleanup prefix %q, got %q", expectedPrefix, prefix)
		}
		return objectStorageCleanupResult{RepositoryID: repo.ID, RepositoryName: repo.Name, Prefix: prefix, ObjectsDeleted: 3, BytesDeleted: 42}, nil
	}

	router := &Router{cfg: config.Config{}, logger: logger, store: repo, hub: newSessionHub()}
	router.finishUnregisterTask(clusterID, task)

	if called {
		t.Fatal("final platform cleanup must not access object storage")
	}
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			t.Fatal("expected cluster to be deleted after object storage cleanup succeeds")
		}
	}
	tasks, err := repo.ListTasks("")
	if err != nil {
		t.Fatal(err)
	}
	var archived *store.Task
	for index := range tasks {
		if tasks[index].Type == "unregister" {
			archived = &tasks[index]
			break
		}
	}
	if archived == nil || archived.ClusterID != "" || archived.Payload["archivedClusterId"] != clusterID || archived.Payload["archivedClusterName"] != "source-cluster" {
		t.Fatalf("expected archived unregister task to preserve cluster identity and name, got %#v", archived)
	}
}

func TestFinishUnregisterDeletesClusterWithoutObjectStorageDependency(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "source-cluster")
	if _, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name:      "minio-primary",
		Type:      "S3",
		Endpoint:  "http://minio.example:9000",
		Bucket:    "hypercdr",
		AccessKey: "minio-access",
		SecretKey: "minio-secret",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "unregister", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}

	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	cleanObjectStoragePrefix = func(_ context.Context, repo store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		return objectStorageCleanupResult{RepositoryID: repo.ID, RepositoryName: repo.Name, Prefix: prefix}, errors.New("minio unavailable")
	}

	router := &Router{cfg: config.Config{}, logger: logger, store: repo, hub: newSessionHub()}
	router.finishUnregisterTask(clusterID, task)

	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			t.Fatal("expected platform cluster record to be deleted")
		}
	}
}

func TestForceCleanupRemovesPlatformRecordsWithoutObjectStorage(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "source-cluster")
	storageRepo, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name:      "minio-primary",
		Type:      "S3",
		Endpoint:  "http://minio.example:9000",
		Bucket:    "hypercdr",
		AccessKey: "minio-access",
		SecretKey: "minio-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	called := false
	cleanObjectStoragePrefix = func(_ context.Context, repo store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		called = true
		if repo.ID != storageRepo.ID {
			t.Fatalf("unexpected repository %s", repo.ID)
		}
		expectedPrefix := storageDomainPrefix(store.DefaultTenantID, clusterID) + "/"
		if prefix != expectedPrefix {
			t.Fatalf("expected cleanup prefix %q, got %q", expectedPrefix, prefix)
		}
		return objectStorageCleanupResult{RepositoryID: repo.ID, RepositoryName: repo.Name, Prefix: prefix, ObjectsDeleted: 5, BytesDeleted: 1024}, nil
	}

	resp, err := http.Post(server.URL+"/api/v1/clusters/"+clusterID+"/force-cleanup", "application/json", strings.NewReader(`{"reason":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected force cleanup status 200, got %d", resp.StatusCode)
	}
	if called {
		t.Fatal("force remove must not access object storage")
	}
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			t.Fatal("expected cluster to be deleted after force cleanup succeeds")
		}
	}
}

func TestForceCleanupClearsTargetReferenceAndPreservesSourcePlan(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	sourceID := registerClusterViaWS(t, server.URL, "source-cluster")
	targetID := registerClusterViaWS(t, server.URL, "offline-target")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: sourceID, TargetClusterID: targetID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, server.URL+"/api/v1/clusters/"+targetID+"/force-cleanup", map[string]any{"reason": "test"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected force cleanup status 200, got %d", resp.StatusCode)
	}
	plans, err := repo.ListProtectionPlans("")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plans {
		if item.ID == plan.ID {
			if item.TargetClusterID != "" {
				t.Fatalf("expected target reference to be cleared, got %q", item.TargetClusterID)
			}
			return
		}
	}
	t.Fatal("source protection plan must be preserved when only its target is force removed")
}

func TestCleanupClusterObjectStorageSkipsClusterWithoutDRData(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "empty-cluster")
	if _, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "unrelated", Type: "S3", Endpoint: "http://minio", Bucket: "bucket", AccessKey: "ak", SecretKey: "sk"}); err != nil {
		t.Fatal(err)
	}
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	called := false
	cleanObjectStoragePrefix = func(context.Context, store.StorageRepository, string) (objectStorageCleanupResult, error) {
		called = true
		return objectStorageCleanupResult{}, nil
	}
	router := &Router{cfg: config.Config{}, logger: logger, store: repo, hub: newSessionHub()}
	results, err := router.cleanupClusterObjectStorage(context.Background(), clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if called || len(results) != 0 {
		t.Fatalf("expected no object storage access, called=%v results=%#v", called, results)
	}
}

func TestUnregisterPrecheckBlocksTargetClusterAndActiveTasks(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "target-cluster")
	if _, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "source-cluster", TargetClusterID: clusterID, AppID: "app-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "drill", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(server.URL + "/api/v1/clusters/" + clusterID + "/unregister/precheck")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var audit unregisterAudit
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		t.Fatal(err)
	}
	if audit.Allowed || audit.TargetPlanCount != 1 || audit.ActiveTaskCount != 1 || len(audit.Blockers) < 2 {
		t.Fatalf("unexpected unregister audit: %#v", audit)
	}
}

func TestUnregisterPrecheckBlocksExistingUnregisterTask(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "unregistering-cluster")
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "unregister", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, hub: newSessionHub()}
	router.hub.set(clusterID, nil)
	audit, err := router.auditClusterUnregister(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if !audit.UnregisterActive || audit.Allowed {
		t.Fatalf("expected active unregister task to block a second request: %#v", audit)
	}
}

func TestCleanupClusterObjectStorageUsesOnlyAssociatedRepositories(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "cleanup-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: "cleanup-cluster"})
	if err != nil {
		t.Fatal(err)
	}
	associated, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "associated", Type: "S3", Endpoint: "http://minio", Bucket: "bucket-a"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "unrelated", Type: "S3", Endpoint: "http://minio", Bucket: "bucket-b"})
	if err != nil {
		t.Fatal(err)
	}
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	called := []string{}
	cleanObjectStoragePrefix = func(_ context.Context, repository store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		called = append(called, repository.ID)
		return objectStorageCleanupResult{RepositoryID: repository.ID, Prefix: prefix}, nil
	}
	router := &Router{store: repo, logger: logger, hub: newSessionHub()}
	results, err := router.cleanupClusterObjectStorageRepositories(context.Background(), cluster.ID, []string{associated.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(called) != 1 || called[0] != associated.ID || called[0] == unrelated.ID {
		t.Fatalf("expected only associated repository cleanup, called=%v results=%#v", called, results)
	}
}

func TestUnregisterAuditUsesHistoricalStorageBinding(t *testing.T) {
	repo := store.NewMemoryStore()
	clusterID := seedSchedulerCluster(t, repo)
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "historical", Type: "S3", Endpoint: "http://minio", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID: clusterID, StorageRepoID: storage.ID, SourceClusterID: clusterID,
		BSLName: storageDomainBSLName(storage, clusterID), ObjectPrefix: storageDomainPrefix(storage.TenantID, clusterID), Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, hub: newSessionHub()}
	router.hub.set(clusterID, nil)
	audit, err := router.auditClusterUnregister(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if audit.RestorePointCount != 0 || !audit.ObjectStorageNeeded || len(audit.StorageRepositoryIDs) != 1 || audit.StorageRepositoryIDs[0] != storage.ID {
		t.Fatalf("expected historical binding to require cluster-prefix cleanup: %#v", audit)
	}
}

func TestProtectionPlanRestoreNamesComeFromDatabaseTasks(t *testing.T) {
	repo := store.NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: "cluster-a", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	otherPlan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: "cluster-b", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	planID := plan.ID
	for _, input := range []store.TaskInput{
		{ProtectionPlanID: planID, Type: "drill", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "hcdr-restore-drill-a"}},
		{ProtectionPlanID: planID, Type: "restore", Status: "failed", Payload: map[string]any{"veleroBackupName": "hcdr-restore-manual-b"}},
		{ProtectionPlanID: planID, Type: "backup", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "hcdr-backup-c"}},
		{ProtectionPlanID: otherPlan.ID, Type: "drill", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "other-plan"}},
	} {
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
	}
	router := &Router{store: repo}
	names, err := router.protectionPlanRestoreNames(planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || !slices.Contains(names, "hcdr-restore-drill-a") || !slices.Contains(names, "hcdr-restore-manual-b") {
		t.Fatalf("unexpected restore cleanup names: %#v", names)
	}
}

func TestUnregisterRequiresExplicitBackupDeletion(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := newUnregisterTestRouter(logger, repo)
	server := httptest.NewServer(router.mux)
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "protected-cluster")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "backup", Type: "S3", Endpoint: "http://minio", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, StorageRepoID: storage.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	backupTask, err := repo.CreateTask(store.TaskInput{ProtectionPlanID: plan.ID, ClusterID: clusterID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{ProtectionPlanID: plan.ID, BackupTaskID: backupTask.ID, SourceClusterID: clusterID, StorageRepoID: storage.ID, VeleroBackupName: "backup-1", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	router.hub.set(clusterID, nil)
	resp := postJSON(t, server.URL+"/api/v1/clusters/"+clusterID+"/unregister", map[string]any{"deleteBackupData": false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected backup deletion decision status 409, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "backup_data_decision_required" {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestTargetUnregisterPreservesSourcePlanRestorePointAndStorage(t *testing.T) {
	repo := store.NewMemoryStore()
	register := func(name string) store.Cluster {
		token, err := repo.CreateAgentToken(store.DefaultTenantID, "", name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: name})
		if err != nil {
			t.Fatal(err)
		}
		return cluster
	}
	source, target := register("source"), register("target")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: store.DefaultTenantID, Name: "backup", Type: "S3", Endpoint: "http://minio", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: source.ID, TargetClusterID: target.ID, StorageRepoID: storage.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: source.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	point, err := repo.CreateRestorePoint(store.RestorePointInput{ProtectionPlanID: plan.ID, BackupTaskID: backup.ID, SourceClusterID: source.ID, StorageRepoID: storage.ID, VeleroBackupName: "source-backup", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo}
	if err := router.cleanupUnregisterProtectionRelationships(target.ID, []store.ProtectionPlan{plan}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.DeleteCluster(target.ID); err != nil || !deleted {
		t.Fatalf("delete target cluster: deleted=%v err=%v", deleted, err)
	}
	gotPlan, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok || gotPlan.TargetClusterID != "" || gotPlan.SourceClusterID != source.ID {
		t.Fatalf("source plan was not preserved with an empty target: plan=%#v ok=%v err=%v", gotPlan, ok, err)
	}
	gotPoint, ok, err := repo.GetRestorePoint(point.ID)
	if err != nil || !ok || gotPoint.SourceClusterID != source.ID || gotPoint.VeleroBackupName != "source-backup" {
		t.Fatalf("source restore point was not preserved: point=%#v ok=%v err=%v", gotPoint, ok, err)
	}
	if _, ok, err := repo.GetStorageRepository(storage.ID); err != nil || !ok {
		t.Fatalf("source storage repository was not preserved: ok=%v err=%v", ok, err)
	}
}

func TestSourceUnregisterDeletesOnlyItsOwnPlans(t *testing.T) {
	repo := store.NewMemoryStore()
	register := func(name string) store.Cluster {
		token, _ := repo.CreateAgentToken(store.DefaultTenantID, "", name, time.Hour)
		cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: name})
		if err != nil {
			t.Fatal(err)
		}
		return cluster
	}
	a, b, c := register("a"), register("b"), register("c")
	aPlan, _ := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: a.ID, TargetClusterID: b.ID, Status: "active"})
	cPlan, _ := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: c.ID, TargetClusterID: a.ID, Status: "active"})
	router := &Router{store: repo}
	if err := router.cleanupUnregisterProtectionRelationships(a.ID, []store.ProtectionPlan{aPlan, cPlan}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := repo.GetProtectionPlan(aPlan.ID); ok {
		t.Fatal("plan owned by the unregistered source was retained")
	}
	got, ok, err := repo.GetProtectionPlan(cPlan.ID)
	if err != nil || !ok || got.TargetClusterID != "" || got.SourceClusterID != c.ID {
		t.Fatalf("other source plan was not preserved with target cleared: plan=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestObjectStorageCleanupFailurePreventsAgentDispatch(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := newUnregisterTestRouter(logger, repo)
	server := httptest.NewServer(router.mux)
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "protected-cluster")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "backup", Type: "S3", Endpoint: "http://minio", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, StorageRepoID: storage.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	backupTask, err := repo.CreateTask(store.TaskInput{ProtectionPlanID: plan.ID, ClusterID: clusterID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{ProtectionPlanID: plan.ID, BackupTaskID: backupTask.ID, SourceClusterID: clusterID, StorageRepoID: storage.ID, VeleroBackupName: "backup-1", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	router.hub.set(clusterID, nil)
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	cleanObjectStoragePrefix = func(context.Context, store.StorageRepository, string) (objectStorageCleanupResult, error) {
		return objectStorageCleanupResult{}, errors.New("storage unavailable")
	}
	resp := postJSON(t, server.URL+"/api/v1/clusters/"+clusterID+"/unregister", map[string]any{"deleteBackupData": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected failed task response 202, got %d", resp.StatusCode)
	}
	var unregisterTasks []store.Task
	var tasks []store.Task
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		unregisterTasks = nil
		tasks, err = repo.ListTasks(clusterID)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if task.Type == "unregister" {
				unregisterTasks = append(unregisterTasks, task)
			}
		}
		if len(unregisterTasks) == 1 && unregisterTasks[0].Status == "failed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(unregisterTasks) != 1 || unregisterTasks[0].Status != "failed" || unregisterTasks[0].ErrorCode != "OBJECT_STORAGE_CLEANUP_FAILED" {
		t.Fatalf("expected failed unregister task before dispatch: %#v", tasks)
	}
	if !clusterExistsInStore(t, repo, clusterID) {
		t.Fatal("cleanup failure must preserve platform cluster records")
	}
	if _, ok, err := repo.GetProtectionPlan(plan.ID); err != nil || !ok {
		t.Fatalf("cleanup failure must preserve the source protection plan: ok=%v err=%v", ok, err)
	}
}

func TestCleanupConsentDoesNotBypassOfflineAgent(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	router := newUnregisterTestRouter(logger, repo)
	server := httptest.NewServer(router.mux)
	defer server.Close()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "offline-target", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: "offline-target"})
	if err != nil {
		t.Fatal(err)
	}
	clusterID := cluster.ID
	if _, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "other-source", TargetClusterID: clusterID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, server.URL+"/api/v1/clusters/"+clusterID+"/unregister", map[string]any{"deleteBackupData": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cleanup consent must not bypass an offline agent: got HTTP %d", resp.StatusCode)
	}
}

func TestTargetOnlyAuditDoesNotIncludeSourceObjectStorage(t *testing.T) {
	repo := store.NewMemoryStore()
	register := func(name string) store.Cluster {
		token, _ := repo.CreateAgentToken(store.DefaultTenantID, "", name, time.Hour)
		cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: name})
		if err != nil {
			t.Fatal(err)
		}
		return cluster
	}
	source, target := register("source-owner"), register("target-only")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "shared", Type: "S3", Endpoint: "http://minio", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{ClusterID: target.ID, StorageRepoID: storage.ID, SourceClusterID: source.ID, BSLName: "source-bsl", ObjectPrefix: "tenant/source-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: source.ID, TargetClusterID: target.ID, StorageRepoID: storage.ID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, hub: newSessionHub()}
	router.hub.set(target.ID, nil)
	audit, err := router.auditClusterUnregister(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if audit.TargetPlanCount != 1 || audit.SourcePlanCount != 0 || audit.ObjectStorageNeeded || len(audit.StorageRepositoryIDs) != 0 {
		t.Fatalf("target-only audit must not include source-owned object storage: %#v", audit)
	}
}

func clusterExistsInStore(t *testing.T, repo store.Store, clusterID string) bool {
	t.Helper()
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return true
		}
	}
	return false
}

func newUnregisterTestRouter(logger *slog.Logger, repo store.Store) *Router {
	router := &Router{
		logger:       logger,
		mux:          http.NewServeMux(),
		store:        repo,
		hub:          newSessionHub(),
		captchas:     map[string]captchaChallenge{},
		oauthStates:  map[string]time.Time{},
		inventory:    map[string]inventoryRequestStatus{},
		imageDigests: map[string]imageDigestCacheEntry{},
	}
	router.routes()
	return router
}

func registerClusterViaWS(t *testing.T, baseURL string, name string) string {
	t.Helper()
	token := createTestAgentToken(t, baseURL)
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/agent"
	accepted := registerTestAgent(t, wsURL, protocol.RegisterPayload{
		InstallToken: token,
		Cluster: protocol.ClusterSummary{
			Name:        name,
			KubeVersion: "v1.30.0",
		},
		Agent: protocol.AgentSummary{
			Version:   "test",
			Namespace: "hypercdr-agent",
			PodName:   "agent-0",
		},
		Velero: protocol.VeleroSummary{Status: "ready"},
	}, "")
	return accepted.Payload.ClusterID
}

func seedBackupTarget(t *testing.T, repo store.Store) (string, string) {
	t.Helper()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{
		Token:       token.Token,
		ClusterName: "source-cluster",
	})
	if err != nil {
		t.Fatal(err)
	}
	appID := store.NewPublicID()
	_, ok, err := repo.ApplyInventory(store.InventoryInput{
		ClusterID: cluster.ID,
		Apps: []store.Application{{
			ID:        appID,
			Namespace: "demo-mysql-csi",
			Name:      "demo-mysql-csi",
			Status:    "healthy",
		}},
		CollectedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected inventory to apply")
	}
	return cluster.ID, appID
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func createTestAgentToken(t *testing.T, baseURL string) string {
	t.Helper()
	resp, err := http.Post(baseURL+"/api/v1/agent-tokens", "application/json", bytes.NewReader([]byte(`{"description":"test"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected token status 201, got %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("expected token")
	}
	return body.Token
}

func registerTestAgent(t *testing.T, wsURL string, payload protocol.RegisterPayload, clusterID string) protocol.Message[protocol.RegisterAcceptedPayload] {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	msg := protocol.Message[protocol.RegisterPayload]{
		Version:   protocol.Version,
		MessageID: "test-register",
		Type:      protocol.MessageAgentRegister,
		ClusterID: clusterID,
		AgentID:   "agent-test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}

	var accepted protocol.Message[protocol.RegisterAcceptedPayload]
	if err := conn.ReadJSON(&accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Type != protocol.MessagePlatformRegisterAccepted {
		t.Fatalf("expected register accepted, got %s", accepted.Type)
	}
	return accepted
}

func TestLicenseRejectedAgentRegistrationRollsBackPlatformResources(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	admission := EditionAdmissionController(func(_ context.Context, request EditionAdmissionRequest) EditionAuthorizationDecision {
		if request.Operation != "cluster.register" || request.WorkerNodes != 11 {
			t.Fatalf("unexpected admission request: %#v", request)
		}
		return EditionAuthorizationDecision{Allowed: false, Code: "LICENSE_NODE_CAPACITY_EXCEEDED", Message: "licensed 10 Worker Nodes, registration requires 11"}
	})
	handler := NewRouterWithProductInfo(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo, ProductInfo{}, WithEditionAdmissionController(admission))
	server := httptest.NewServer(handler)
	defer server.Close()
	token := createTestAgentToken(t, server.URL)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	message := protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, MessageID: "license-rejected", Type: protocol.MessageAgentRegister, AgentID: "agent-license-test", Timestamp: time.Now().UTC(), Payload: protocol.RegisterPayload{InstallToken: token, Cluster: protocol.ClusterSummary{Name: "over-limit", NodeCount: 11}}}
	if err = conn.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
	var rejected protocol.Message[protocol.RegisterRejectedPayload]
	if err = conn.ReadJSON(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Type != protocol.MessagePlatformRegisterRejected || rejected.Payload.Reason != "LICENSE_NODE_CAPACITY_EXCEEDED" {
		t.Fatalf("rejected response = %#v", rejected)
	}
	clusters, err := repo.ListClusters()
	if err != nil || len(clusters) != 0 {
		t.Fatalf("license-rejected registration left clusters: %#v err=%v", clusters, err)
	}
	if err = repo.ValidateAgentToken(token); !errors.Is(err, store.ErrTokenInvalid) {
		t.Fatalf("license-rejected token was not removed: %v", err)
	}
}

func TestValidUserEmail(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "user@example.com", valid: true},
		{value: " USER@example.com ", valid: true},
		{value: "admin", valid: false},
		{value: "user@localhost", valid: false},
		{value: "Display Name <user@example.com>", valid: false},
		{value: "", valid: false},
	}
	for _, test := range tests {
		if actual := validUserEmail(test.value); actual != test.valid {
			t.Errorf("validUserEmail(%q) = %v, want %v", test.value, actual, test.valid)
		}
	}
}

func TestUpgradeTargetIsNewer(t *testing.T) {
	tests := []struct {
		name                    string
		current, target         string
		digestMismatch, upgrade bool
	}{
		{name: "newer calendar build", current: "v20260723.4", target: "v20260724.1", digestMismatch: true, upgrade: true},
		{name: "older target is not upgrade", current: "v20260723.4", target: "v20260723.1", digestMismatch: true, upgrade: false},
		{name: "same immutable version and digest", current: "v1.17.1", target: "v1.17.1", upgrade: false},
		{name: "same version changed digest", current: "v1.17.1", target: "v1.17.1", digestMismatch: true, upgrade: true},
		{name: "unknown versions use digest", current: "unknown", target: "candidate", digestMismatch: true, upgrade: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := upgradeTargetIsNewer(test.current, test.target, test.digestMismatch); actual != test.upgrade {
				t.Fatalf("upgradeTargetIsNewer(%q, %q, %v) = %v, want %v", test.current, test.target, test.digestMismatch, actual, test.upgrade)
			}
		})
	}
}

func TestResourceSelectionPayloadAcceptsInMemoryAndJSONShapes(t *testing.T) {
	want := store.ResourceSelection{
		Mode:            "custom",
		NamespaceScoped: []string{"pods", "configmaps"},
		ClusterScoped:   []string{"persistentvolumes"},
	}
	for name, payload := range map[string]map[string]any{
		"in-memory task": {"resourceSelection": want},
		"database task": {"resourceSelection": map[string]any{
			"mode":            "custom",
			"namespaceScoped": []any{"pods", "configmaps"},
			"clusterScoped":   []any{"persistentvolumes"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			got := resourceSelectionPayload(payload)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resourceSelectionPayload() = %#v, want %#v", got, want)
			}
		})
	}
}
