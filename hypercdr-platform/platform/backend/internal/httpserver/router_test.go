package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
	token, err := repo.CreateAgentToken("test", time.Hour)
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

func TestUnregisterClusterOfflineCreatesQueuedTask(t *testing.T) {
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
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected unregister status 202, got %d", resp.StatusCode)
	}
	var body struct {
		Task    store.Task `json:"task"`
		Warning string     `json:"warning"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Task.ID == "" {
		t.Fatal("expected queued unregister task")
	}
	if body.Task.Type != "unregister" || body.Task.Status != "queued" {
		t.Fatalf("unexpected task state: type=%q status=%q", body.Task.Type, body.Task.Status)
	}
	if body.Task.Payload["namespace"] != "hypercdr-agent" || body.Task.Payload["deleteNamespace"] != true {
		t.Fatalf("unexpected unregister payload: %#v", body.Task.Payload)
	}
	if body.Warning == "" {
		t.Fatal("expected offline warning")
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

func TestVeleroCRDsEndpoint(t *testing.T) {
	repo := store.NewMemoryStore()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{}, logger, repo))
	defer server.Close()

	resp, err := http.Get(server.URL + "/assets/velero/v1.17.1/crds.yaml")
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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	server := httptest.NewServer(NewRouter(config.Config{
		AgentImage:      "registry.local:5000/hypercdr/comm-agent:test",
		AgentNamespace:  "hypercdr-agent",
		VeleroImage:     "registry.local:5000/hypercdr/velero:v1.17.1",
		VeleroAWSPlugin: "registry.local:5000/hypercdr/velero-plugin-for-aws:v1.13.0",
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
		"/assets/velero/v1.17.1/crds.yaml",
		"kind: Deployment",
		"type: Recreate",
		"name: velero",
		"kind: DaemonSet",
		"name: node-agent",
		"name: NODE_NAME",
		"fieldPath: spec.nodeName",
		"name: VELERO_NAMESPACE",
		"fieldPath: metadata.namespace",
		"registry.local:5000/hypercdr/velero:v1.17.1",
		"registry.local:5000/hypercdr/velero-plugin-for-aws:v1.13.0",
		"name: velero-plugin-for-aws",
		"mountPath: /target",
		"mountPath: /plugins",
		"Velero AWS ObjectStore plugin is installed",
		`if ! kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state`,
		"Keeping existing comm-agent state PVC and StorageClass",
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
	server := httptest.NewServer(NewRouter(config.Config{
		ImageRegistry: "192.168.8.149/hypercdr",
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
	server := httptest.NewServer(NewRouter(config.Config{AgentNamespace: "hypercdr-agent"}, logger, repo))
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

func TestFinishUnregisterCleansObjectStorageBeforeDeletingCluster(t *testing.T) {
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

	if !called {
		t.Fatal("expected object storage cleanup to run")
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

func TestFinishUnregisterKeepsClusterWhenObjectStorageCleanupFails(t *testing.T) {
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
	found := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cluster to remain when object storage cleanup fails")
	}
	tasks, err := repo.ListTasks(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	var unregisterTask store.Task
	for _, item := range tasks {
		if item.ID == task.ID {
			unregisterTask = item
			break
		}
	}
	if unregisterTask.ID == "" || unregisterTask.Status != "failed" || unregisterTask.ErrorCode != "OBJECT_STORAGE_CLEANUP_FAILED" {
		t.Fatalf("expected unregister task to be marked failed, got %#v", tasks)
	}
}

func TestForceCleanupCleansObjectStorageBeforeDeletingCluster(t *testing.T) {
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
	if !called {
		t.Fatal("expected object storage cleanup to run")
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
	token, err := repo.CreateAgentToken("test", time.Hour)
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
