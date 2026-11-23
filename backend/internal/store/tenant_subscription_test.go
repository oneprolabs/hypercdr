package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/billing"
	"hypercdr-platform/platform/backend/pkg/licensing"
)

func subscriptionFixture() TenantSubscription {
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(time.Hour)
	return TenantSubscription{TenantID: DefaultTenantID, Provider: "stripe", CustomerID: "cus_test", SubscriptionID: "sub_test", Plan: billing.Plan{ID: "business", Name: "Business", MaxClusters: 5, AmountMinor: 1200, Currency: "usd", Interval: "month"}, Status: "active", PeriodStart: now.Add(-time.Hour), PeriodEnd: end, PaidUntil: &end, Revision: 1}
}

func TestUnlimitedSubscriptionCapacityAndExpiry(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	owner := testAdmin(t, repo)
	ctx := context.Background()
	snapshot := subscriptionFixture()
	snapshot.TenantID = owner.TenantID
	snapshot.Plan = billing.DefaultPlans()[1]
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, owner.TenantID)
	if err != nil || !usage.Unlimited || !usage.CanRegister || usage.OverLimit || usage.MaxClusters != 0 {
		t.Fatalf("unlimited admission: %+v %v", usage, err)
	}
	stored, err := ReadTenantSubscription(ctx, repo.db, owner.TenantID)
	if err != nil || stored == nil || !stored.Plan.Unlimited {
		t.Fatalf("unlimited readback: %+v %v", stored, err)
	}
	if changed, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil || changed {
		t.Fatalf("unlimited replay: %t %v", changed, err)
	}
	for index := 0; index < 6; index++ {
		token, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "unlimited admission", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "unlimited-test"}); err != nil {
			t.Fatalf("unlimited registration %d: %v", index, err)
		}
	}
	snapshot.Revision++
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	snapshot.PaidUntil = &past
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	usage, err = ReadTenantLicenseUsage(ctx, repo.db, owner.TenantID)
	if err != nil || usage.Unlimited || usage.MaxClusters != 1 || usage.Plan != "free" || usage.UsedClusters != 6 || !usage.OverLimit || usage.CanRegister {
		t.Fatalf("expired unlimited: %+v %v", usage, err)
	}
	if _, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "expired unlimited", time.Hour); !errors.Is(err, ErrClusterCapacity) {
		t.Fatalf("expired unlimited admitted registration: %v", err)
	}
}

func TestSubscriptionPaidAccessLifecycle(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	ctx := context.Background()
	snapshot := subscriptionFixture()
	for index, scenario := range []struct {
		status string
		paid   bool
		cancel bool
		want   int
	}{
		{"incomplete", false, false, 1}, {"active", false, false, 1}, {"active", true, false, 5},
		{"active", true, true, 5}, {"past_due", true, false, 5}, {"canceled", true, false, 5},
		{"unpaid", true, false, 1}, {"paused", true, false, 1}, {"trialing", true, false, 1},
	} {
		snapshot.Revision = int64(index + 1)
		snapshot.Status, snapshot.CancelAtPeriodEnd = scenario.status, scenario.cancel
		snapshot.PaidUntil = nil
		if scenario.paid {
			snapshot.PaidUntil = &snapshot.PeriodEnd
		}
		if changed, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil || !changed {
			t.Fatalf("apply %s: %t %v", scenario.status, changed, err)
		}
		usage, err := ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
		if err != nil || usage.MaxClusters != scenario.want || usage.Source != "subscription" {
			t.Fatalf("%s paid=%t: %+v %v", scenario.status, scenario.paid, usage, err)
		}
	}
	snapshot.Revision++
	snapshot.Status = "active"
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	snapshot.PaidUntil = &past
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.MaxClusters != 1 || usage.Status != "expired" {
		t.Fatalf("expired paid access: %+v %v", usage, err)
	}
}

func TestSubscriptionRevisionIsolationAndLicenseExclusion(t *testing.T) {
	repo := newTestStore(t)
	ctx := context.Background()
	snapshot := subscriptionFixture()
	now := time.Now().UTC()
	license := licensing.Verified{Claims: licensing.Claims{ID: uuid.NewString(), TenantID: DefaultTenantID, Revision: 1, MaxClusters: 20, IssuedAt: now.Add(-time.Hour), ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}, KeyID: "test"}
	if _, err := ActivateTenantLicense(ctx, repo.db, license, "test-only", ""); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil || !changed {
		t.Fatalf("apply: %t %v", changed, err)
	}
	if changed, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil || changed {
		t.Fatalf("replay: %t %v", changed, err)
	}
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.MaxClusters != 5 || usage.LicenseID != "" {
		t.Fatalf("license and subscription must not stack: %+v %v", usage, err)
	}
	var wait sync.WaitGroup
	for _, revision := range []int64{2, 3, 4} {
		wait.Add(1)
		go func(revision int64) {
			defer wait.Done()
			next := snapshot
			next.Revision = revision
			next.Plan.MaxClusters = int(revision + 5)
			if _, err := ApplyTenantSubscription(ctx, repo.db, next); err != nil && !errors.Is(err, ErrSubscriptionConflict) {
				t.Errorf("concurrent snapshot: %v", err)
			}
		}(revision)
	}
	wait.Wait()
	current, err := ReadTenantSubscription(ctx, repo.db, DefaultTenantID)
	if err != nil || current.Revision != 4 || current.Plan.MaxClusters != 9 {
		t.Fatalf("latest revision: %+v %v", current, err)
	}
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	otherID := uuid.NewString()
	if _, err := repo.db.Exec(`insert into resource_scopes(id,name,status) values($1,'Other subscription organization','active')`, otherID); err != nil {
		t.Fatal(err)
	}
	other, err := ReadTenantLicenseUsage(ctx, repo.db, otherID)
	if err != nil || other.MaxClusters != 1 || other.Plan != "free" {
		t.Fatalf("tenant isolation: %+v %v", other, err)
	}
	wrong := *current
	wrong.TenantID = otherID
	if _, err := ApplyTenantSubscription(ctx, repo.db, wrong); err == nil {
		t.Fatal("customer ownership reassigned to another tenant")
	}
	next := *current
	next.Revision++
	next.Status = "unpaid"
	if _, err := ApplyTenantSubscription(ctx, repo.db, next); err != nil {
		t.Fatal(err)
	}
	usage, err = ReadTenantLicenseUsage(ctx, repo.db, DefaultTenantID)
	if err != nil || usage.MaxClusters != 1 || usage.LicenseID != "" {
		t.Fatalf("expired subscription must not resurrect license: %+v %v", usage, err)
	}
	if _, err := repo.db.Exec(`delete from resource_scopes where id=$1`, otherID); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionSnapshotValidation(t *testing.T) {
	repo := newTestStore(t)
	for _, mutate := range []func(*TenantSubscription){
		func(snapshot *TenantSubscription) { snapshot.Status = "made_up" },
		func(snapshot *TenantSubscription) { snapshot.Plan.MaxClusters = 0 },
		func(snapshot *TenantSubscription) { snapshot.PeriodEnd = snapshot.PeriodStart },
		func(snapshot *TenantSubscription) {
			beyond := snapshot.PeriodEnd.Add(time.Hour)
			snapshot.PaidUntil = &beyond
		},
		func(snapshot *TenantSubscription) { snapshot.Revision = 0 },
		func(snapshot *TenantSubscription) { snapshot.Provider = "browser" },
	} {
		snapshot := subscriptionFixture()
		mutate(&snapshot)
		if _, err := ApplyTenantSubscription(context.Background(), repo.db, snapshot); err == nil {
			t.Fatal("accepted invalid snapshot")
		}
	}
}

func TestSubscriptionReplayNormalizesDatabasePrecision(t *testing.T) {
	repo := newTestStore(t)
	snapshot := subscriptionFixture()
	snapshot.PeriodStart = snapshot.PeriodStart.Add(123456789 * time.Nanosecond)
	snapshot.PeriodEnd = snapshot.PeriodEnd.Add(123456789 * time.Nanosecond)
	snapshot.PaidUntil = &snapshot.PeriodEnd
	if changed, err := ApplyTenantSubscription(context.Background(), repo.db, snapshot); err != nil || !changed {
		t.Fatalf("initial snapshot: %t %v", changed, err)
	}
	if changed, err := ApplyTenantSubscription(context.Background(), repo.db, snapshot); err != nil || changed {
		t.Fatalf("replay at PostgreSQL timestamp precision: %t %v", changed, err)
	}
}

func TestSubscriptionClusterAdmissionAndExpiry(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	owner := testAdmin(t, repo)
	ctx := context.Background()
	snapshot := subscriptionFixture()
	snapshot.TenantID = owner.TenantID
	snapshot.Plan.MaxClusters = 2
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	tokens := make([]AgentToken, 3)
	for index := range tokens {
		var err error
		tokens[index], err = repo.CreateAgentToken(owner.TenantID, owner.ID, "subscription admission", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		cluster    Cluster
		credential string
		err        error
	}
	outcomes := make(chan result, len(tokens))
	for _, token := range tokens {
		go func(token AgentToken) {
			cluster, credential, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "subscription-test"})
			outcomes <- result{cluster, credential, err}
		}(token)
	}
	accepted, rejected := 0, 0
	var connected result
	for range tokens {
		outcome := <-outcomes
		if outcome.err == nil {
			accepted++
			connected = outcome
		} else if errors.Is(outcome.err, ErrClusterCapacity) {
			rejected++
		} else {
			t.Fatal(outcome.err)
		}
	}
	if accepted != 2 || rejected != 1 {
		t.Fatalf("commercial concurrent admission: %d accepted %d rejected", accepted, rejected)
	}
	snapshot.Revision++
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	snapshot.PaidUntil = &past
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, owner.TenantID)
	if err != nil || !usage.OverLimit || usage.CanRegister || usage.UsedClusters != 2 || usage.MaxClusters != 1 {
		t.Fatalf("expiry must preserve clusters and deny new registration: %+v %v", usage, err)
	}
	if _, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "expired capacity", time.Hour); !errors.Is(err, ErrClusterCapacity) {
		t.Fatalf("expired capacity admitted another cluster: %v", err)
	}
	if _, ok, err := repo.AuthenticateAgentCredential(AgentCredentialInput{ClusterID: connected.cluster.ID, Credential: connected.credential}); err != nil || !ok {
		t.Fatalf("expiry broke existing agent reconnect: %t %v", ok, err)
	}
	var history int
	if err := repo.db.QueryRow(`select count(*) from tenant_subscription_history where tenant_id=$1`, owner.TenantID).Scan(&history); err != nil || history != 2 {
		t.Fatalf("subscription history: %d %v", history, err)
	}
}
