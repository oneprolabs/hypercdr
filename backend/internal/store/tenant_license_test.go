package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/licensing"
)

func TestTenantLicenseUsageAndMonotonicActivation(t *testing.T) {
	repo := newTestStore(t)
	ctx := context.Background()
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.Plan != "free" || usage.MaxClusters != 1 || usage.UsedClusters != 0 || !usage.CanRegister {
		t.Fatalf("free entitlement: %+v %v", usage, err)
	}
	now := time.Now().UTC()
	license := licensing.Verified{Claims: licensing.Claims{ID: uuid.NewString(), TenantID: DefaultTenantID, Revision: 1, MaxClusters: 5, IssuedAt: now.Add(-time.Hour), ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}, KeyID: "test-issuer"}
	changed, err := ActivateTenantLicense(ctx, repo.db, license, "verified-by-service", "")
	if err != nil || !changed {
		t.Fatalf("activate: %t %v", changed, err)
	}
	changed, err = ActivateTenantLicense(ctx, repo.db, license, "verified-by-service", "")
	if err != nil || changed {
		t.Fatalf("idempotent activation: %t %v", changed, err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 2)
	for _, revision := range []int64{2, 3} {
		wait.Add(1)
		go func(revision int64) {
			defer wait.Done()
			next := license
			next.ID, next.Revision, next.MaxClusters = uuid.NewString(), revision, 10
			_, err := ActivateTenantLicense(ctx, repo.db, next, next.ID, "")
			if err != nil && !errors.Is(err, ErrLicenseRevision) {
				errorsFound <- err
			}
		}(revision)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	usage, err = ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.Revision != 3 || usage.MaxClusters != 10 || usage.Plan != "commercial" {
		t.Fatalf("newest revision must win: %+v %v", usage, err)
	}
	if _, err := ActivateTenantLicense(ctx, repo.db, license, "verified-by-service", ""); !errors.Is(err, ErrLicenseRevision) {
		t.Fatalf("old license accepted: %v", err)
	}
	_, err = repo.db.Exec(`update tenant_licenses set valid_until=now()-interval '1 second' where id=$1`, usage.LicenseID)
	if err != nil {
		t.Fatal(err)
	}
	usage, err = ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.Plan != "free" || usage.Status != "expired" || usage.MaxClusters != 1 {
		t.Fatalf("expired license: %+v %v", usage, err)
	}
	otherID := uuid.NewString()
	if _, err := repo.db.Exec(`insert into resource_scopes(id,name,status) values($1,'Other organization','active')`, otherID); err != nil {
		t.Fatal(err)
	}
	other, err := ReadTenantLicenseUsage(ctx, repo.db, otherID)
	if err != nil || other.Plan != "free" || other.LicenseID != "" || other.MaxClusters != 1 {
		t.Fatalf("new organization entitlement or license isolation: %+v %v", other, err)
	}
	if _, err := repo.db.Exec(`update tenant_entitlements set license_id=$2 where tenant_id=$1`, otherID, usage.LicenseID); err == nil {
		t.Fatal("database allowed a cross-tenant license assignment")
	}
	if deleted, inUse, err := repo.DeleteTenant(otherID); err != nil || !deleted || inUse {
		t.Fatalf("free entitlement blocked empty organization deletion: %t %t %v", deleted, inUse, err)
	}
}

func TestCommunityConcurrentClusterAdmission(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	owner := testAdmin(t, repo)
	tokens := make([]AgentToken, 2)
	for index := range tokens {
		var err error
		tokens[index], err = repo.CreateAgentToken(owner.TenantID, owner.ID, "capacity test", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
	}
	type outcome struct {
		index      int
		cluster    Cluster
		credential string
		err        error
	}
	outcomes := make(chan outcome, 2)
	for index, token := range tokens {
		go func(index int, token AgentToken) {
			cluster, credential, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "capacity-test"})
			outcomes <- outcome{index: index, cluster: cluster, credential: credential, err: err}
		}(index, token)
	}
	var successful outcome
	failedIndex := -1
	for attempt := 0; attempt < 2; attempt++ {
		result := <-outcomes
		if result.err == nil {
			if successful.cluster.ID != "" {
				t.Fatal("both registrations exceeded the free cluster limit")
			}
			successful = result
		} else if errors.Is(result.err, ErrClusterCapacity) {
			failedIndex = result.index
		} else {
			t.Fatal(result.err)
		}
	}
	if successful.cluster.ID == "" || failedIndex < 0 {
		t.Fatal("expected exactly one accepted and one capacity-rejected registration")
	}
	usage, err := ReadTenantLicenseUsage(context.Background(), repo.db, owner.TenantID)
	if err != nil || usage.UsedClusters != 1 || usage.CanRegister {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	if _, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "over capacity", time.Hour); !errors.Is(err, ErrClusterCapacity) {
		t.Fatalf("token preflight did not report capacity: %v", err)
	}
	if _, ok, err := repo.AuthenticateAgentCredential(AgentCredentialInput{ClusterID: successful.cluster.ID, Credential: successful.credential}); err != nil || !ok {
		t.Fatalf("existing Agent reconnect denied: %t %v", ok, err)
	}
	if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: tokens[successful.index].Token}); !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("consumed token registered another cluster: %v", err)
	}
	if _, err := repo.db.Exec(`update clusters set connection_status='offline' where id=$1`, successful.cluster.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ValidateAgentToken(tokens[failedIndex].Token); !errors.Is(err, ErrClusterCapacity) {
		t.Fatalf("offline cluster did not occupy capacity: %v", err)
	}
	if deleted, err := repo.DeleteCluster(successful.cluster.ID); err != nil || !deleted {
		t.Fatalf("cluster deletion: %t %v", deleted, err)
	}
	if err := repo.ValidateAgentToken(tokens[failedIndex].Token); err != nil {
		t.Fatalf("rejected registration consumed its token: %v", err)
	}
	if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: tokens[failedIndex].Token, ClusterName: "replacement"}); err != nil {
		t.Fatalf("released capacity was not reusable: %v", err)
	}
}

func TestRegistrationRechecksMemberPermission(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	member, err := repo.CreateUser(DefaultTenantID, "registration-member@example.test", "Test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.CreateAgentToken(DefaultTenantID, member.ID, "member permission", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	roleID := uuid.NewString()
	if _, err := repo.db.Exec(`insert into organization_roles(id,tenant_id,name) values($1,$2,'No registration')`, roleID, DefaultTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`update users set custom_role_id=$2 where id=$1`, member.ID, roleID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ValidateAgentToken(token.Token); !errors.Is(err, ErrRegistrationPermission) {
		t.Fatalf("preflight ignored revoked permission: %v", err)
	}
	if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token}); !errors.Is(err, ErrRegistrationPermission) {
		t.Fatalf("registration ignored revoked permission: %v", err)
	}
	if _, err := repo.db.Exec(`update users set custom_role_id=null where id=$1`, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteUser(member.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ValidateAgentToken(token.Token); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("deleted member left usable installation credentials: %v", err)
	}
}
