package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/pkg/billing"
)

func TestWorkerEntitlementGraceAndUnlimited(t *testing.T) {
	repo := newTestStore(t)
	repo.EnableCommunityEntitlements()
	repo.EnableWorkerNodeEntitlements()
	owner := testAdmin(t, repo)
	ctx := context.Background()
	for index := 0; index < 2; index++ {
		token, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "node entitlement test", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "worker-test", NodeCount: 2}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	admit := func(taskType string, at time.Time) error {
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := CheckWorkerTaskAdmission(ctx, tx, owner.TenantID, taskType, at); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := admit("backup", now); err != nil {
		t.Fatalf("first overage must start grace: %v", err)
	}
	if err := admit("backup", now.Add(29*24*time.Hour)); err != nil {
		t.Fatalf("grace denied: %v", err)
	}
	if err := admit("backup", now.Add(31*24*time.Hour)); !errors.Is(err, ErrWorkerNodeCapacity) {
		t.Fatalf("expired grace allowed: %v", err)
	}
	if err := admit("drill", now.Add(31*24*time.Hour)); !errors.Is(err, ErrWorkerNodeCapacity) {
		t.Fatalf("expired grace allowed Drill: %v", err)
	}
	for _, taskType := range []string{"restore", "takeover", "failback", "protection-cleanup", "storage-sync", "schedule-sync", "cache-settings", "task-abort", "cluster-registration", "agent-upgrade", "velero-upgrade"} {
		if err := admit(taskType, now.Add(31*24*time.Hour)); err != nil {
			t.Fatalf("unrestricted operation blocked: %s %v", taskType, err)
		}
	}
	token, err := repo.CreateAgentToken(owner.TenantID, owner.ID, "over-quota registration", time.Hour)
	if err != nil {
		t.Fatalf("over-quota registration token rejected: %v", err)
	}
	if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "over-quota-extra"}); err != nil {
		t.Fatalf("over-quota registration rejected: %v", err)
	}
	snapshot := subscriptionFixture()
	snapshot.TenantID = owner.TenantID
	snapshot.Plan = billing.WorkerPlans()[2]
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := admit("backup", now); err != nil {
		t.Fatalf("Business must clear overage: %v", err)
	}
	usage, err := ReadTenantLicenseUsage(ctx, repo.db, owner.TenantID)
	if err != nil || !usage.Unlimited || usage.WorkerNodesOverLimit || usage.WorkerTasksBlocked || usage.WorkerOverageStartedAt != nil {
		t.Fatalf("unlimited usage: %+v %v", usage, err)
	}
	snapshot.Plan, err = billing.WorkerPlans()[0].WithQuantity(5)
	snapshot.Revision++
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyTenantSubscription(ctx, repo.db, snapshot); err != nil {
		t.Fatal(err)
	}
	usage, err = ReadTenantLicenseUsage(ctx, repo.db, owner.TenantID)
	if err != nil || usage.MaxWorkerNodes != 5 || usage.WorkerNodes != 4 || usage.WorkerNodesOverLimit {
		t.Fatalf("Flex quota: %+v %v", usage, err)
	}
	if _, err := repo.db.ExecContext(ctx, `update tenant_subscriptions set period_start=$2,period_end=$3,paid_until=$3 where tenant_id=$1`, owner.TenantID, now.Add(24*time.Hour), now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `update tenant_worker_entitlement_state set overage_started_at=$2 where tenant_id=$1`, owner.TenantID, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := admit("backup", now); !errors.Is(err, ErrWorkerNodeCapacity) {
		t.Fatalf("future subscription must not grant capacity: %v", err)
	}
}
