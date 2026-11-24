package saas

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"log"
	"net/http"
	"net/url"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/billing"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func (s *Service) subscription(w http.ResponseWriter, r *http.Request, principal platform.Principal) {
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	usage, err := store.ReadTenantLicenseUsage(r.Context(), tx, principal.TenantID)
	if err != nil {
		problem(w, 503, "subscription_unavailable", "Subscription information could not be loaded. Retry shortly.")
		return
	}
	if s.cfg.SaaS {
		usage.CanRegister = true
		usage.OverLimit = false
	}
	if usage.WorkerNodesOverLimit && usage.WorkerOverageStartedAt == nil {
		now := time.Now().UTC()
		if _, err := tx.ExecContext(r.Context(), `insert into tenant_worker_entitlement_state(tenant_id,overage_started_at,updated_at) values($1,$2,$2) on conflict(tenant_id) do update set overage_started_at=coalesce(tenant_worker_entitlement_state.overage_started_at,excluded.overage_started_at),updated_at=excluded.updated_at`, principal.TenantID, now); err != nil {
			internal(w)
			return
		}
		ends := now.Add(30 * 24 * time.Hour)
		usage.WorkerOverageStartedAt = &now
		usage.WorkerGracePeriodEndsAt = &ends
	}
	if !usage.WorkerNodesOverLimit {
		if _, err := tx.ExecContext(r.Context(), `update tenant_worker_entitlement_state set overage_started_at=null,updated_at=now() where tenant_id=$1 and overage_started_at is not null`, principal.TenantID); err != nil {
			internal(w)
			return
		}
	}
	snapshot, err := store.ReadTenantSubscription(r.Context(), tx, principal.TenantID)
	if err != nil {
		internal(w)
		return
	}
	var verificationDelayed bool
	if err = tx.QueryRowContext(r.Context(), `select coalesce((select verification_delayed from tenant_stripe_customers where tenant_id=$1),false)`, principal.TenantID).Scan(&verificationDelayed); err != nil {
		internal(w)
		return
	}
	if err := tx.Commit(); err != nil {
		internal(w)
		return
	}
	plans := s.cfg.SubscriptionPlans
	if plans == nil {
		plans = []billing.Plan{}
	}
	mode := "online"
	if !s.cfg.SaaS {
		mode = "offline"
	}
	reason := "Online payments are not connected yet. Your current access is unchanged."
	if s.stripe != nil && s.cfg.Stripe.WebhookSecret == "" {
		reason = "Stripe sandbox is awaiting webhook setup. Purchases are unavailable; your current access is unchanged."
	}
	if s.billingReady() {
		reason = "Sandbox billing only. No real payments are taken."
	}
	respond(w, 200, map[string]any{"mode": mode, "usage": usage, "subscription": snapshot, "plans": plans, "canManage": principal.Role == "admin" || principal.SystemAdmin, "payment": map[string]any{"configured": s.billingReady(), "reason": reason, "testMode": s.billingReady(), "verificationDelayed": verificationDelayed}})
}

func (s *Service) subscriptionCheckout(w http.ResponseWriter, r *http.Request, principal platform.Principal) {
	if !admin(w, principal) {
		return
	}
	if !s.cfg.SaaS {
		problem(w, 409, "subscription_mode_unavailable", "Online subscriptions are not available in this deployment.")
		return
	}
	var input struct {
		PlanID   string `json:"planId"`
		Quantity int    `json:"quantity"`
	}
	if !decode(r, &input) {
		problem(w, 400, "invalid_plan", "Select a commercial plan.")
		return
	}
	for _, plan := range s.cfg.SubscriptionPlans {
		if plan.ID == input.PlanID {
			quantity := input.Quantity
			if quantity == 0 && !plan.PerNode() {
				quantity = 1
			}
			if _, err := plan.WithQuantity(quantity); err != nil {
				problem(w, 400, "invalid_quantity", err.Error())
				return
			}
			if s.billingReady() {
				ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
				defer cancel()
				redirect, err := s.createCheckout(ctx, principal.TenantID, plan, quantity)
				if err != nil {
					billingProblem(w, err)
					return
				}
				respond(w, 200, map[string]string{"url": redirect})
				return
			}
			problem(w, 503, "payment_not_configured", "Online payments are not connected yet. No subscription was created and no payment was taken.")
			return
		}
	}
	problem(w, 400, "invalid_plan", "This plan is not available. Refresh the subscription page.")
}

func (s *Service) subscriptionQuote(w http.ResponseWriter, r *http.Request, principal platform.Principal) {
	var input struct {
		PlanID   string `json:"planId"`
		Quantity int    `json:"quantity"`
	}
	if !decode(r, &input) {
		problem(w, 400, "invalid_quote", "Select a plan and a whole number of nodes.")
		return
	}
	for _, plan := range billing.WorkerPlans() {
		if plan.ID != input.PlanID {
			continue
		}
		quote, err := plan.WithQuantity(input.Quantity)
		if err != nil {
			problem(w, 400, "invalid_quantity", err.Error())
			return
		}
		respond(w, 200, map[string]any{"planId": plan.ID, "quantity": input.Quantity, "unitAmountMinor": plan.AmountMinor, "amountMinor": quote.AmountMinor, "interval": plan.Interval, "currency": plan.Currency, "unlimited": plan.Unlimited, "paymentRequired": true, "checkoutAvailable": false})
		return
	}
	problem(w, 400, "invalid_plan", "Select Flex or Business.")
}

func (s *Service) subscriptionPortal(w http.ResponseWriter, r *http.Request, principal platform.Principal) {
	if !admin(w, principal) {
		return
	}
	if !s.cfg.SaaS {
		problem(w, 409, "subscription_mode_unavailable", "Online subscriptions are not available in this deployment.")
		return
	}
	if s.billingReady() {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		unlock, err := s.billingLock(ctx, principal.TenantID)
		if err != nil {
			billingProblem(w, err)
			return
		}
		defer unlock()
		var customer string
		if err = s.db.QueryRowContext(ctx, `select customer_id from tenant_stripe_customers where tenant_id=$1 and customer_id is not null`, principal.TenantID).Scan(&customer); err != nil {
			problem(w, 409, "billing_customer_unavailable", "No billing account exists for this organization.")
			return
		}
		var config struct {
			ID string `json:"id"`
		}
		err = s.db.QueryRowContext(ctx, `select portal_configuration_id from stripe_billing_settings where singleton`).Scan(&config.ID)
		if errors.Is(err, sql.ErrNoRows) {
			err = s.stripe.Request(ctx, "POST", "/billing_portal/configurations", "hcdr-sandbox-portal-config-v1", url.Values{"features[customer_update][enabled]": {"true"}, "features[customer_update][allowed_updates][0]": {"email"}, "features[invoice_history][enabled]": {"true"}, "features[payment_method_update][enabled]": {"true"}, "features[subscription_cancel][enabled]": {"true"}, "features[subscription_cancel][mode]": {"at_period_end"}, "features[subscription_update][enabled]": {"false"}}, &config)
			if err == nil && config.ID != "" {
				_, err = s.db.ExecContext(ctx, `insert into stripe_billing_settings(singleton,portal_configuration_id) values(true,$1) on conflict do nothing`, config.ID)
				if err == nil {
					err = s.db.QueryRowContext(ctx, `select portal_configuration_id from stripe_billing_settings where singleton`).Scan(&config.ID)
				}
			}
		}
		if err != nil || config.ID == "" {
			billingProblem(w, err)
			return
		}
		var session struct {
			URL string `json:"url"`
		}
		err = s.stripe.Request(ctx, "POST", "/billing_portal/sessions", "hcdr-sandbox-portal-"+uuid.NewString(), url.Values{"customer": {customer}, "configuration": {config.ID}, "return_url": {s.cfg.Origin + "/?billing=return"}}, &session)
		if err != nil {
			billingProblem(w, err)
			return
		}
		redirect, err := checkedStripeURL(session.URL, "billing.stripe.com")
		if err != nil {
			billingProblem(w, err)
			return
		}
		respond(w, 200, map[string]string{"url": redirect})
		return
	}
	problem(w, 503, "payment_not_configured", "Subscription management will be available after online payments are connected. Your subscription has not changed.")
}

func billingProblem(w http.ResponseWriter, err error) {
	var providerError *billing.StripeError
	if errors.As(err, &providerError) {
		log.Printf("Stripe sandbox request failed: %s", providerError)
	}
	if errors.Is(err, errBillingBusy) {
		problem(w, 409, "billing_busy", "Another billing operation is in progress. Wait a moment and retry.")
		return
	}
	problem(w, 503, "billing_request_failed", "Billing could not be completed. Refresh and retry. Your access has not been changed by this request.")
}
