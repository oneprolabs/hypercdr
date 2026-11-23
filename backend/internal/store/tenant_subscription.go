package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/pkg/billing"
)

var ErrSubscriptionConflict = errors.New("subscription revision or ownership conflict")

type TenantSubscription struct {
	TenantID          string       `json:"-"`
	Provider          string       `json:"provider"`
	CustomerID        string       `json:"-"`
	SubscriptionID    string       `json:"-"`
	Plan              billing.Plan `json:"plan"`
	Status            string       `json:"status"`
	PeriodStart       time.Time    `json:"periodStart"`
	PeriodEnd         time.Time    `json:"periodEnd"`
	PaidUntil         *time.Time   `json:"paidUntil,omitempty"`
	CancelAtPeriodEnd bool         `json:"cancelAtPeriodEnd"`
	Revision          int64        `json:"-"`
}

func ReadTenantSubscription(ctx context.Context, reader TenantLicenseReader, tenantID string) (*TenantSubscription, error) {
	var snapshot TenantSubscription
	err := reader.QueryRowContext(ctx, `select tenant_id,provider,customer_id,subscription_id,plan_id,plan_name,status,max_clusters,amount_minor,currency,billing_interval,period_start,period_end,paid_until,cancel_at_period_end,revision from tenant_subscriptions where tenant_id=$1`, tenantID).Scan(&snapshot.TenantID, &snapshot.Provider, &snapshot.CustomerID, &snapshot.SubscriptionID, &snapshot.Plan.ID, &snapshot.Plan.Name, &snapshot.Status, &snapshot.Plan.MaxClusters, &snapshot.Plan.AmountMinor, &snapshot.Plan.Currency, &snapshot.Plan.Interval, &snapshot.PeriodStart, &snapshot.PeriodEnd, &snapshot.PaidUntil, &snapshot.CancelAtPeriodEnd, &snapshot.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	snapshot.Plan.Unlimited = snapshot.Plan.MaxClusters == 0
	return &snapshot, err
}

func ApplyTenantSubscription(ctx context.Context, db *sql.DB, snapshot TenantSubscription) (bool, error) {
	snapshot = subscriptionComparable(snapshot)
	if snapshot.TenantID == "" || snapshot.Provider != "stripe" || strings.TrimSpace(snapshot.CustomerID) == "" || strings.TrimSpace(snapshot.CustomerID) != snapshot.CustomerID || len(snapshot.CustomerID) > 255 || strings.TrimSpace(snapshot.SubscriptionID) == "" || strings.TrimSpace(snapshot.SubscriptionID) != snapshot.SubscriptionID || len(snapshot.SubscriptionID) > 255 || snapshot.Revision < 1 || snapshot.Plan.Validate() != nil || snapshot.PeriodStart.IsZero() || snapshot.PeriodEnd.IsZero() || !snapshot.PeriodEnd.After(snapshot.PeriodStart) || (snapshot.PaidUntil != nil && (snapshot.PaidUntil.After(snapshot.PeriodEnd) || !snapshot.PaidUntil.After(snapshot.PeriodStart))) {
		return false, errors.New("invalid subscription snapshot")
	}
	switch snapshot.Status {
	case "incomplete", "incomplete_expired", "trialing", "active", "past_due", "canceled", "unpaid", "paused":
	default:
		return false, errors.New("invalid subscription status")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext($1))`, snapshot.TenantID); err != nil {
		return false, err
	}
	current, err := ReadTenantSubscription(ctx, tx, snapshot.TenantID)
	if err != nil {
		return false, err
	}
	if current != nil {
		if snapshot.Revision <= current.Revision {
			if reflect.DeepEqual(subscriptionComparable(snapshot), subscriptionComparable(*current)) {
				return false, tx.Commit()
			}
			return false, ErrSubscriptionConflict
		}
		if current.CustomerID != snapshot.CustomerID || (current.SubscriptionID != snapshot.SubscriptionID && current.Status != "canceled" && current.Status != "incomplete_expired") {
			return false, ErrSubscriptionConflict
		}
	}
	_, err = tx.ExecContext(ctx, `insert into tenant_subscriptions(tenant_id,provider,customer_id,subscription_id,plan_id,plan_name,status,max_clusters,amount_minor,currency,billing_interval,period_start,period_end,paid_until,cancel_at_period_end,revision) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) on conflict(tenant_id) do update set subscription_id=excluded.subscription_id,plan_id=excluded.plan_id,plan_name=excluded.plan_name,status=excluded.status,max_clusters=excluded.max_clusters,amount_minor=excluded.amount_minor,currency=excluded.currency,billing_interval=excluded.billing_interval,period_start=excluded.period_start,period_end=excluded.period_end,paid_until=excluded.paid_until,cancel_at_period_end=excluded.cancel_at_period_end,revision=excluded.revision,updated_at=now()`, snapshot.TenantID, snapshot.Provider, snapshot.CustomerID, snapshot.SubscriptionID, snapshot.Plan.ID, snapshot.Plan.Name, snapshot.Status, snapshot.Plan.MaxClusters, snapshot.Plan.AmountMinor, snapshot.Plan.Currency, snapshot.Plan.Interval, snapshot.PeriodStart, snapshot.PeriodEnd, snapshot.PaidUntil, snapshot.CancelAtPeriodEnd, snapshot.Revision)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `insert into tenant_subscription_history(tenant_id,revision,plan_id,status,max_clusters,paid_until,cancel_at_period_end) values($1,$2,$3,$4,$5,$6,$7)`, snapshot.TenantID, snapshot.Revision, snapshot.Plan.ID, snapshot.Status, snapshot.Plan.MaxClusters, snapshot.PaidUntil, snapshot.CancelAtPeriodEnd)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `update tenant_entitlements set source='subscription',updated_at=now() where tenant_id=$1`, snapshot.TenantID)
	if err != nil {
		return false, err
	}
	if affected, countErr := result.RowsAffected(); countErr != nil || affected != 1 {
		return false, errors.New("organization entitlement not found")
	}
	return true, tx.Commit()
}

func subscriptionComparable(snapshot TenantSubscription) TenantSubscription {
	snapshot.Plan.Description = ""
	snapshot.PeriodStart = snapshot.PeriodStart.UTC().Truncate(time.Microsecond)
	snapshot.PeriodEnd = snapshot.PeriodEnd.UTC().Truncate(time.Microsecond)
	if snapshot.PaidUntil != nil {
		paidUntil := snapshot.PaidUntil.UTC().Truncate(time.Microsecond)
		snapshot.PaidUntil = &paidUntil
	}
	return snapshot
}
