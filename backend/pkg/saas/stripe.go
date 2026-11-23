package saas

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/billing"
)

var errBillingBusy = errors.New("billing operation already in progress")

func (s *Service) billingReady() bool {
	return s.cfg.SaaS && s.stripe != nil && s.cfg.Stripe.WebhookSecret != ""
}

func (s *Service) billingLock(ctx context.Context, tenant string) (func(), error) {
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	err = connection.QueryRowContext(ctx, `select pg_try_advisory_lock(hashtext('stripe-sandbox'),hashtext($1))`, tenant).Scan(&locked)
	if err != nil || !locked {
		connection.Close()
		if err != nil {
			return nil, err
		}
		return nil, errBillingBusy
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if connection.QueryRowContext(cleanup, `select pg_advisory_unlock(hashtext('stripe-sandbox'),hashtext($1))`, tenant).Scan(&unlocked) != nil || !unlocked {
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		}
		connection.Close()
	}, nil
}

type stripeSession struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	Customer  string `json:"customer"`
	Live      bool   `json:"livemode"`
	Reference string `json:"client_reference_id"`
}

func (s *Service) createCheckout(ctx context.Context, tenant string, plan billing.Plan) (string, error) {
	unlock, err := s.billingLock(ctx, tenant)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err = s.stripe.ValidatePrices(ctx, s.cfg.SubscriptionPlans); err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `insert into tenant_stripe_customers(tenant_id) values($1) on conflict do nothing`, tenant)
	if err != nil {
		return "", err
	}
	var customer sql.NullString
	var customerCreated time.Time
	if err = s.db.QueryRowContext(ctx, `select customer_id,created_at from tenant_stripe_customers where tenant_id=$1`, tenant).Scan(&customer, &customerCreated); err != nil {
		return "", err
	}
	if !customer.Valid {
		if time.Since(customerCreated) > 23*time.Hour {
			return "", errors.New("Stripe customer recovery requires administrator review")
		}
		var created struct {
			ID   string `json:"id"`
			Live bool   `json:"livemode"`
		}
		err = s.stripe.Request(ctx, "POST", "/customers", "hcdr-sandbox-customer-"+tenant, url.Values{"metadata[hypercdr_tenant_id]": {tenant}}, &created)
		if err != nil {
			return "", err
		}
		if created.ID == "" || created.Live {
			return "", errors.New("invalid Stripe sandbox customer")
		}
		_, err = s.db.ExecContext(ctx, `update tenant_stripe_customers set customer_id=$2 where tenant_id=$1`, tenant, created.ID)
		if err != nil {
			return "", err
		}
		customer.String = created.ID
	}
	var subscriptions struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
		More bool `json:"has_more"`
	}
	err = s.stripe.Request(ctx, "GET", "/subscriptions", "", url.Values{"customer": {customer.String}, "status": {"all"}, "limit": {"100"}}, &subscriptions)
	if err != nil {
		return "", err
	}
	if subscriptions.More {
		return "", errors.New("subscription history requires review")
	}
	for _, subscription := range subscriptions.Data {
		if subscription.Status != "canceled" && subscription.Status != "incomplete_expired" {
			return "", errors.New("an existing subscription must be managed in the billing portal")
		}
	}
	var intent, existingPlan, price, origin string
	var sessionID sql.NullString
	var created time.Time
	err = s.db.QueryRowContext(ctx, `select id,plan_id,price_id,session_id,created_at,return_origin from tenant_stripe_checkouts where tenant_id=$1 and state in ('creating','open')`, tenant).Scan(&intent, &existingPlan, &price, &sessionID, &created, &origin)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil && sessionID.Valid {
		var session stripeSession
		if err = s.stripe.Request(ctx, "GET", "/checkout/sessions/"+url.PathEscape(sessionID.String), "", nil, &session); err != nil {
			return "", err
		}
		if session.Customer != customer.String || session.Live || session.Reference != tenant {
			return "", errors.New("Stripe checkout ownership mismatch")
		}
		if session.Status == "open" {
			if existingPlan != plan.ID {
				var expired stripeSession
				if err = s.stripe.Request(ctx, "POST", "/checkout/sessions/"+url.PathEscape(sessionID.String)+"/expire", "hcdr-expire-"+intent, url.Values{}, &expired); err != nil {
					return "", err
				}
				if expired.Status != "expired" || expired.ID != sessionID.String {
					return "", errors.New("previous checkout could not be expired")
				}
				session.Status = "expired"
			} else {
				return checkedStripeURL(session.URL, "checkout.stripe.com")
			}
		}
		_, err = s.db.ExecContext(ctx, `update tenant_stripe_checkouts set state=$2 where id=$1`, intent, session.Status)
		if err != nil {
			return "", err
		}
		if session.Status == "complete" {
			return "", errors.New("payment confirmation is in progress; refresh shortly")
		}
		intent = ""
	}
	if intent != "" && time.Since(created) > 23*time.Hour {
		return "", errors.New("checkout recovery requires administrator review")
	}
	if intent != "" && existingPlan != plan.ID {
		return "", errors.New("another checkout is being prepared")
	}
	if intent == "" {
		intent = uuid.NewString()
		price = s.cfg.Stripe.Prices[plan.ID]
		origin = s.cfg.Origin
		_, err = s.db.ExecContext(ctx, `insert into tenant_stripe_checkouts(id,tenant_id,plan_id,price_id,customer_id,return_origin,expires_at) values($1,$2,$3,$4,$5,$6,now()+interval '24 hours')`, intent, tenant, plan.ID, price, customer.String, origin)
		if err != nil {
			return "", err
		}
	}
	var session stripeSession
	err = s.stripe.Request(ctx, "POST", "/checkout/sessions", "hcdr-sandbox-checkout-"+intent, url.Values{
		"mode": {"subscription"}, "customer": {customer.String}, "client_reference_id": {tenant},
		"adaptive_pricing[enabled]": {"false"},
		"line_items[0][price]":      {price}, "line_items[0][quantity]": {"1"},
		"metadata[hypercdr_tenant_id]": {tenant}, "subscription_data[metadata][hypercdr_tenant_id]": {tenant},
		"success_url": {origin + "/?billing=success"}, "cancel_url": {origin + "/?billing=canceled"},
	}, &session)
	if err != nil {
		return "", err
	}
	if session.ID == "" || session.Live || session.Customer != customer.String || session.Reference != tenant {
		return "", errors.New("Stripe checkout ownership mismatch")
	}
	redirect, err := checkedStripeURL(session.URL, "checkout.stripe.com")
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `update tenant_stripe_checkouts set session_id=$2,session_url=$3,state='open' where id=$1`, intent, session.ID, redirect)
	return redirect, err
}

func checkedStripeURL(raw, host string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != host || parsed.User != nil {
		return "", errors.New("Stripe returned an invalid redirect URL")
	}
	return raw, nil
}

func (s *Service) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.billingReady() {
		problem(w, 503, "billing_unavailable", "Stripe sandbox is not configured.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		problem(w, 400, "invalid_webhook", "Invalid webhook payload.")
		return
	}
	if billing.VerifyStripeSignature(body, r.Header.Get("Stripe-Signature"), s.cfg.Stripe.WebhookSecret, time.Now()) != nil {
		problem(w, 400, "invalid_signature", "Invalid webhook signature.")
		return
	}
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Live bool   `json:"livemode"`
		Data struct {
			Object struct {
				Customer string `json:"customer"`
				Charge   string `json:"charge"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || event.ID == "" || event.Live {
		problem(w, 400, "invalid_event", "Invalid sandbox event.")
		return
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.expired", "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted", "invoice.paid", "invoice.payment_failed", "invoice.voided", "charge.refunded", "charge.dispute.created", "charge.dispute.closed":
	default:
		respond(w, 200, map[string]bool{"received": true})
		return
	}
	if event.Data.Object.Customer == "" {
		if event.Data.Object.Charge != "" && (event.Type == "charge.dispute.created" || event.Type == "charge.dispute.closed") {
			var charge struct {
				Customer string `json:"customer"`
				Live     bool   `json:"livemode"`
			}
			if err = s.stripe.Request(r.Context(), "GET", "/charges/"+url.PathEscape(event.Data.Object.Charge), "", nil, &charge); err != nil || charge.Live || charge.Customer == "" {
				problem(w, 503, "webhook_unavailable", "Dispute ownership could not be verified. Retry later.")
				return
			}
			event.Data.Object.Customer = charge.Customer
		}
	}
	if event.Data.Object.Customer == "" {
		problem(w, 400, "invalid_event", "Event customer is missing.")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `insert into stripe_webhook_inbox(event_id,customer_id,event_type) values($1,$2,$3) on conflict do nothing`, event.ID, event.Data.Object.Customer, event.Type)
	if err != nil {
		problem(w, 503, "webhook_unavailable", "Webhook could not be stored. Retry later.")
		return
	}
	respond(w, 200, map[string]bool{"received": true})
}
