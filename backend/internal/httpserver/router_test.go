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
)

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

func TestRegistryTagsTreatsMissingRepositoryAsEmpty(t *testing.T) {
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v2/hypercdr/platform-api/tags/list" {
			t.Fatalf("unexpected registry path %q", req.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"code":"NAME_UNKNOWN"}]}`))
	}))
	defer registry.Close()

	host := strings.TrimPrefix(registry.URL, "https://")
	gotRegistry, repository, tags, err := (&Router{}).registryTags(context.Background(), host+"/hypercdr/platform-api:latest")
	if err != nil {
		t.Fatalf("missing repository returned an error: %v", err)
	}
	if gotRegistry != host || repository != "hypercdr/platform-api" || len(tags) != 0 {
		t.Fatalf("registry=%q repository=%q tags=%v", gotRegistry, repository, tags)
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
		BSLName: storageDomainBSLName(storage, clusterID), ObjectPrefix: storageDomainPrefix(clusterID), Status: "active",
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
}

func TestUnregisterClusterOfflineIsBlockedBeforeTaskCreation(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
	defer server.Close()

	clusterID := registerClusterViaWS(t, server.URL, "temporary-cluster")
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
	if _, err := repo.UpsertComponentRelease(store.ComponentReleaseInput{Component: "comm-agent", Version: "active", Image: "registry.local:5000/hypercdr/comm-agent:active", ImageDigest: "sha256:agent", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertComponentRelease(store.ComponentReleaseInput{Component: "velero", Version: "v1.17.2", Image: "registry.local:5000/hypercdr/velero:v1.17.2", ImageDigest: "sha256:velero", Status: "active"}); err != nil {
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
		"/assets/velero/v1.18.2/crds.yaml",
		"--concurrent-backups=2",
		"--node-agent-configmap=node-agent-config",
		"--backup-repository-configmap=backup-repository-config",
		"name: backup-repository-config",
		`"cacheLimitMB": 5120`,
		`"prepareQueueLength": 4`,
		`"cachePVC"`,
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
		`kubectl_retry kubectl -n "$NAMESPACE" rollout restart deployment/hypercdr-comm-agent`,
		"Existing comm-agent deployment restarted with the new bootstrap token",
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
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected install script to contain %q", expected)
		}
	}
	if strings.Contains(text, "kubectl_retry kubectl apply -f \"$VELERO_CRDS_URL\"") {
		t.Fatal("expected install script to download Velero CRDs before kubectl apply so self-signed HTTPS works")
	}
	command := exec.Command("bash", "-n")
	command.Stdin = strings.NewReader(text)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated install script has invalid shell syntax: %v\n%s", err, output)
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
		expectedPrefix := storageDomainPrefix(clusterID) + "/"
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
		expectedPrefix := storageDomainPrefix(clusterID) + "/"
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

func TestForceCleanupBlocksTargetReferencedCluster(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()
	clusterID := registerClusterViaWS(t, server.URL, "target-cluster")
	if _, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "source-cluster", TargetClusterID: clusterID, AppID: "app-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, server.URL+"/api/v1/clusters/"+clusterID+"/force-cleanup", map[string]any{"reason": "test"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected force cleanup status 409, got %d", resp.StatusCode)
	}
	if !clusterExistsInStore(t, repo, clusterID) {
		t.Fatal("target-referenced cluster must remain registered")
	}
}

func TestCleanupClusterObjectStorageUsesOnlyAssociatedRepositories(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
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
	results, err := router.cleanupClusterObjectStorageRepositories(context.Background(), "cluster-a", []string{associated.ID})
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
		BSLName: storageDomainBSLName(storage, clusterID), ObjectPrefix: storageDomainPrefix(clusterID), Status: "active",
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
	planID := store.NewPublicID()
	for _, input := range []store.TaskInput{
		{ProtectionPlanID: planID, Type: "drill", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "hcdr-restore-drill-a"}},
		{ProtectionPlanID: planID, Type: "restore", Status: "failed", Payload: map[string]any{"veleroBackupName": "hcdr-restore-manual-b"}},
		{ProtectionPlanID: planID, Type: "backup", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "hcdr-backup-c"}},
		{ProtectionPlanID: store.NewPublicID(), Type: "drill", Status: "succeeded", Payload: map[string]any{"veleroBackupName": "other-plan"}},
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
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: clusterID, StorageRepoID: storage.ID, VeleroBackupName: "backup-1", Status: "available"}); err != nil {
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
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: clusterID, StorageRepoID: storage.ID, VeleroBackupName: "backup-1", Status: "available"}); err != nil {
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
