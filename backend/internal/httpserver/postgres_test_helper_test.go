package httpserver

import (
	"context"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/internal/testdb"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	repo, err := store.NewPostgresStore(context.Background(), testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := repo.ConfigureSecretKey(context.Background(), "postgres-test-secret"); err != nil {
		t.Fatal(err)
	}
	users, err := repo.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("test admin: %v %v", users, err)
	}
	admin, found, err := repo.SetUserPassword(users[0].ID, store.DefaultAdminPassword, false)
	if err != nil || !found {
		t.Fatalf("test admin password: %v", err)
	}
	session, err := repo.CreatePlatformSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	testSessions.Store(t.Name(), session.Token)
	t.Cleanup(func() { testSessions.Delete(t.Name()) })
	return repo
}

var testSessions sync.Map

// authenticatedTestClient sends a real persisted session through production middleware.
// Unauthenticated and role tests explicitly construct requests without this client.
func authenticatedTestClient(t *testing.T) *http.Client {
	t.Helper()
	token, _ := testSessions.Load(t.Name())
	return &http.Client{Transport: testSessionTransport{token: token}, Timeout: 15 * time.Second}
}

type testSessionTransport struct{ token any }

func (tr testSessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	if token, ok := tr.token.(string); ok && copy.Header.Get("Authorization") == "" && strings.HasPrefix(copy.URL.Path, "/api/v1/") && !strings.HasPrefix(copy.URL.Path, "/api/v1/auth/") {
		copy.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultTransport.RoundTrip(copy)
}
func testAdmin(t *testing.T, repo *store.PostgresStore) store.User {
	t.Helper()
	users, err := repo.ListUsers()
	if err != nil || len(users) == 0 {
		t.Fatalf("test admin: %v", err)
	}
	return users[0]
}

func testApplicationID(t *testing.T, repo *store.PostgresStore, clusterID string) string {
	t.Helper()
	apps, err := repo.ListApplications(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) > 0 {
		return apps[0].ID
	}
	return seedSchedulerApplication(t, repo, clusterID, "test-namespace").ID
}
func testClusterID(t *testing.T, repo *store.PostgresStore, name string) string {
	t.Helper()
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range clusters {
		if c.Name == name {
			return c.ID
		}
	}
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: name})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func testTenantCluster(t *testing.T, repo *store.PostgresStore, tenantID, name string) store.Cluster {
	t.Helper()
	token, err := repo.CreateAgentToken(tenantID, "", name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: name})
	if err != nil {
		t.Fatal(err)
	}
	return cluster
}
func testTenantPlan(t *testing.T, repo *store.PostgresStore, tenantID string) store.ProtectionPlan {
	t.Helper()
	source := testTenantCluster(t, repo, tenantID, "source")
	target := testTenantCluster(t, repo, tenantID, "target")
	app := seedSchedulerApplication(t, repo, source.ID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: tenantID, SourceClusterID: source.ID, TargetClusterID: target.ID, AppID: app.ID, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
