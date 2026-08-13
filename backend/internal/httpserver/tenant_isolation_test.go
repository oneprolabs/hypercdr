package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func tenantRequest(req *http.Request, user store.User) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
}

func TestTenantListsAndMutationsAreIsolated(t *testing.T) {
	repo := store.NewMemoryStore()
	tenantA, _ := repo.CreateTenant(store.TenantInput{Name: "Tenant A", Status: "active"})
	tenantB, _ := repo.CreateTenant(store.TenantInput{Name: "Tenant B", Status: "active"})
	storageA, _ := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: tenantA.ID, Name: "A", Type: "S3"})
	storageB, _ := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: tenantB.ID, Name: "B", Type: "S3"})
	r := &Router{cfg: config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), store: repo}
	userB := store.User{ID: "user-b", TenantID: tenantB.ID, Role: "admin", Status: "active"}

	listReq := tenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/storage-repositories", nil), userB)
	listRes := httptest.NewRecorder()
	r.listStorageRepositories(listRes, listReq)
	var result struct {
		Items []store.StorageRepository `json:"items"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].TenantID != tenantB.ID {
		t.Fatalf("tenant B saw unexpected storage: %#v", result.Items)
	}

	body := bytes.NewBufferString(`{"name":"changed","type":"S3"}`)
	updateReq := tenantRequest(httptest.NewRequest(http.MethodPatch, "/api/v1/storage-repositories/"+storageA.ID, body), userB)
	updateReq.SetPathValue("id", storageA.ID)
	updateRes := httptest.NewRecorder()
	r.tenantGuard("storage", r.updateStorageRepository)(updateRes, updateReq)
	if updateRes.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update returned %d, want 404", updateRes.Code)
	}

	systemAdmin := store.User{ID: "system-admin", TenantID: tenantA.ID, Email: store.DefaultAdminEmail, Role: "admin", Status: "active", SystemAdmin: true}
	adminListReq := tenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/storage-repositories", nil), systemAdmin)
	adminListRes := httptest.NewRecorder()
	r.listStorageRepositories(adminListRes, adminListReq)
	result.Items = nil
	if err := json.Unmarshal(adminListRes.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].TenantID != tenantA.ID {
		t.Fatalf("system admin saw resources outside its tenant: %#v", result.Items)
	}

	adminUpdateReq := tenantRequest(httptest.NewRequest(http.MethodPatch, "/api/v1/storage-repositories/"+storageB.ID, bytes.NewBufferString(`{"name":"changed","type":"S3"}`)), systemAdmin)
	adminUpdateReq.SetPathValue("id", storageB.ID)
	adminUpdateRes := httptest.NewRecorder()
	r.tenantGuard("storage", r.updateStorageRepository)(adminUpdateRes, adminUpdateReq)
	if adminUpdateRes.Code != http.StatusNotFound {
		t.Fatalf("system admin cross-tenant update returned %d, want 404", adminUpdateRes.Code)
	}
}

func TestProtectionPlanListIsTenantScoped(t *testing.T) {
	repo := store.NewMemoryStore()
	tenantA, _ := repo.CreateTenant(store.TenantInput{Name: "Topology Tenant A", Status: "active"})
	tenantB, _ := repo.CreateTenant(store.TenantInput{Name: "Topology Tenant B", Status: "active"})
	planA, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		TenantID: tenantA.ID, SourceClusterID: "a-source", TargetClusterID: "a-target", AppID: "a-app", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	planB, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		TenantID: tenantB.ID, SourceClusterID: "b-source", TargetClusterID: "b-target", AppID: "b-app", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}

	r := &Router{cfg: config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), store: repo}
	userB := store.User{ID: "user-b", TenantID: tenantB.ID, Role: "admin", Status: "active"}
	req := tenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/protection-plans", nil), userB)
	res := httptest.NewRecorder()
	r.listProtectionPlans(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list protection plans returned %d: %s", res.Code, res.Body.String())
	}
	var result struct {
		Items []store.ProtectionPlan `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != planB.ID || result.Items[0].TenantID != tenantB.ID {
		t.Fatalf("tenant B saw unexpected plans: %#v", result.Items)
	}
	if result.Items[0].ID == planA.ID {
		t.Fatalf("tenant B saw tenant A plan %q", planA.ID)
	}
}

func TestAgentRegistrationUsesTokenTenant(t *testing.T) {
	repo := store.NewMemoryStore()
	tenant, _ := repo.CreateTenant(store.TenantInput{Name: "Agent Tenant", Status: "active"})
	token, err := repo.CreateAgentToken(tenant.ID, "creator", "registration", 60_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: "tenant-cluster"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.TenantID != tenant.ID {
		t.Fatalf("cluster tenant=%s want %s", cluster.TenantID, tenant.ID)
	}
}

func TestRecoveryRejectsCrossTenantProtectionPlan(t *testing.T) {
	repo := store.NewMemoryStore()
	tenantA, _ := repo.CreateTenant(store.TenantInput{Name: "Recovery Tenant A", Status: "active"})
	tenantB, _ := repo.CreateTenant(store.TenantInput{Name: "Recovery Tenant B", Status: "active"})
	tokenB, _ := repo.CreateAgentToken(tenantB.ID, "user-b", "registration", 60_000_000_000)
	clusterB, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: tokenB.Token, ClusterName: "tenant-b-cluster"})
	if err != nil {
		t.Fatal(err)
	}
	planA, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		TenantID: tenantA.ID, SourceClusterID: "tenant-a-cluster", AppID: "tenant-a-app", StorageRepoID: "tenant-a-storage",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{cfg: config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), store: repo}
	userB := store.User{ID: "user-b", TenantID: tenantB.ID, Role: "operator", Status: "active"}
	body := bytes.NewBufferString(`{"clusterId":"` + clusterB.ID + `","protectionPlanId":"` + planA.ID + `","veleroBackupName":"foreign-backup","sourceNamespace":"demo","targetNamespace":"demo-copy"}`)
	req := tenantRequest(httptest.NewRequest(http.MethodPost, "/api/v1/tasks/drill", body), userB)
	res := httptest.NewRecorder()
	r.createRecoveryTask(res, req, "drill")
	if res.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant recovery returned %d, want 404: %s", res.Code, res.Body.String())
	}
}
