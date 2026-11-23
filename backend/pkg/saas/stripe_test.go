package saas

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/billing"
)

type sandboxProvider struct {
	mu                   sync.Mutex
	customers, checkouts int
	paid                 bool
	canceled             bool
	scheduled            bool
	expired              bool
	refunded             bool
	unavailable          bool
	start, end           int64
}

func (provider *sandboxProvider) ValidatePrices(context.Context, []billing.Plan) error { return nil }
func (provider *sandboxProvider) Request(ctx context.Context, method, path, key string, values url.Values, result any) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	var response any
	switch {
	case path == "/customers":
		provider.customers++
		response = map[string]any{"id": "cus_sandbox", "livemode": false}
	case path == "/subscriptions":
		if provider.unavailable {
			return fmt.Errorf("sandbox provider temporarily unavailable")
		}
		data := []any{}
		if provider.paid {
			status := "active"
			if provider.canceled {
				status = "canceled"
			}
			cancelAt := int64(0)
			if provider.scheduled {
				cancelAt = provider.end
			}
			data = append(data, map[string]any{"id": "sub_sandbox", "customer": "cus_sandbox", "status": status, "livemode": false, "cancel_at_period_end": false, "cancel_at": cancelAt, "items": map[string]any{"data": []any{map[string]any{"quantity": 1, "current_period_start": provider.start, "current_period_end": provider.end, "price": map[string]any{"id": "price_standard"}}}}})
		}
		response = map[string]any{"data": data, "has_more": false}
	case path == "/checkout/sessions" || strings.HasPrefix(path, "/checkout/sessions/"):
		if method == "POST" && path == "/checkout/sessions" {
			provider.checkouts++
		}
		sessionID := fmt.Sprintf("cs_test_sandbox_%d", provider.checkouts)
		if path != "/checkout/sessions" {
			sessionID = strings.TrimSuffix(strings.TrimPrefix(path, "/checkout/sessions/"), "/expire")
		}
		status := "open"
		if provider.paid {
			status = "complete"
		}
		if provider.expired || strings.HasSuffix(path, "/expire") {
			status = "expired"
		}
		reference := values.Get("client_reference_id")
		if reference == "" {
			reference = sandboxTenantFromContext(ctx)
		}
		response = map[string]any{"id": sessionID, "url": "https://checkout.stripe.com/c/pay/" + sessionID, "customer": "cus_sandbox", "status": status, "client_reference_id": reference, "livemode": false}
	case path == "/invoices":
		response = map[string]any{"data": []any{map[string]any{"id": "in_sandbox", "customer": "cus_sandbox", "status": "paid", "amount_paid": 10000, "amount_due": 10000, "currency": "usd", "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_sandbox"}}, "lines": map[string]any{"has_more": false, "data": []any{map[string]any{"amount": 10000, "quantity": 1, "period": map[string]any{"start": provider.start, "end": provider.end}, "pricing": map[string]any{"price_details": map[string]any{"price": "price_standard"}}}}}}}}
	case path == "/invoice_payments":
		response = map[string]any{"data": []any{map[string]any{"amount_paid": 10000, "status": "paid", "payment": map[string]any{"type": "payment_intent", "payment_intent": "pi_sandbox"}}}, "has_more": false}
	case path == "/payment_intents/pi_sandbox":
		refunded := 0
		if provider.refunded {
			refunded = 10000
		}
		response = map[string]any{"customer": "cus_sandbox", "status": "succeeded", "livemode": false, "currency": "usd", "latest_charge": map[string]any{"customer": "cus_sandbox", "paid": true, "livemode": false, "disputed": false, "amount": 10000, "amount_refunded": refunded, "currency": "usd"}}
	default:
		return fmt.Errorf("unexpected request %s %s", method, path)
	}
	payload, _ := json.Marshal(response)
	return json.Unmarshal(payload, result)
}

type sandboxTenantKey struct{}

func sandboxTenantFromContext(ctx context.Context) string {
	tenant, _ := ctx.Value(sandboxTenantKey{}).(string)
	return tenant
}

func TestStripeCheckoutAndReconcile(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "stripe-owner@example.test", "Stripe test")
	other := signup(t, service, mail, "stripe-other@example.test", "Other test")
	provider := &sandboxProvider{start: time.Now().Add(-time.Minute).Unix(), end: time.Now().Add(time.Hour).Unix()}
	service.stripe = provider
	service.cfg.Stripe = billing.StripeConfig{SecretKey: "rk_test_mock", WebhookSecret: "whsec_test", Prices: map[string]string{"standard": "price_standard", "business": "price_business"}}
	service.cfg.SubscriptionPlans = billing.DefaultPlans()
	ctx := context.WithValue(context.Background(), sandboxTenantKey{}, owner.TenantID)
	first, err := service.createCheckout(ctx, owner.TenantID, billing.DefaultPlans()[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.createCheckout(ctx, owner.TenantID, billing.DefaultPlans()[0])
	if err != nil || first != second {
		t.Fatalf("retry: %s %v", second, err)
	}
	if provider.customers != 1 || provider.checkouts != 1 {
		t.Fatal("duplicate provider writes")
	}
	if _, err = service.createCheckout(ctx, owner.TenantID, billing.DefaultPlans()[1]); err != nil {
		t.Fatalf("replace unpaid checkout with another plan: %v", err)
	}
	if _, err = service.createCheckout(ctx, owner.TenantID, billing.DefaultPlans()[0]); err != nil {
		t.Fatalf("return to original plan: %v", err)
	}
	var expiredCount, openCount int
	if err = service.db.QueryRowContext(ctx, `select count(*) filter(where state='expired'),count(*) filter(where state='open') from tenant_stripe_checkouts where tenant_id=$1`, owner.TenantID).Scan(&expiredCount, &openCount); err != nil || expiredCount != 2 || openCount != 1 || provider.checkouts != 3 || provider.customers != 1 {
		t.Fatalf("checkout replacement must expire old sessions and reuse customer: expired=%d open=%d writes=%d customers=%d error=%v", expiredCount, openCount, provider.checkouts, provider.customers, err)
	}
	usage, err := store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
	if err != nil || usage.MaxClusters != 1 {
		t.Fatalf("checkout granted unpaid capacity: %+v %v", usage, err)
	}
	unlock, err := service.billingLock(ctx, owner.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.createCheckout(ctx, owner.TenantID, billing.DefaultPlans()[0]); err != errBillingBusy {
		t.Fatalf("concurrent checkout: %v", err)
	}
	unlock()
	provider.paid = true
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	usage, err = store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
	if err != nil || usage.MaxClusters != 5 {
		t.Fatalf("paid capacity: %+v %v", usage, err)
	}
	snapshot, err := store.ReadTenantSubscription(ctx, service.db, owner.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.ReadTenantSubscription(ctx, service.db, owner.TenantID)
	if err != nil || replayed.Revision != snapshot.Revision {
		t.Fatal("reconcile was not idempotent")
	}
	otherUsage, err := store.ReadTenantLicenseUsage(ctx, service.db, other.TenantID)
	if err != nil || otherUsage.MaxClusters != 1 {
		t.Fatal("cross tenant entitlement leak")
	}
	provider.scheduled = true
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	scheduled, err := store.ReadTenantSubscription(ctx, service.db, owner.TenantID)
	if err != nil || !scheduled.CancelAtPeriodEnd || !samePaidUntil(scheduled.PaidUntil, snapshot.PaidUntil) {
		t.Fatalf("Stripe cancel_at at period end was not reflected without losing paid access: %+v %v", scheduled, err)
	}
	provider.scheduled = false
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.ReadTenantSubscription(ctx, service.db, owner.TenantID)
	if err != nil || resumed.CancelAtPeriodEnd || !samePaidUntil(resumed.PaidUntil, snapshot.PaidUntil) {
		t.Fatalf("resuming subscription changed paid access or retained cancellation: %+v %v", resumed, err)
	}
	provider.scheduled = true
	provider.canceled = true
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	usage, err = store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
	if err != nil || usage.MaxClusters != 5 {
		t.Fatal("cancellation removed already paid access")
	}
	canceled, err := store.ReadTenantSubscription(ctx, service.db, owner.TenantID)
	if err != nil || canceled.CancelAtPeriodEnd {
		t.Fatalf("completed cancellation still presented as scheduled: %+v %v", canceled, err)
	}
	provider.refunded = true
	if err = service.reconcileStripe(ctx, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	usage, err = store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
	if err != nil || usage.MaxClusters != 1 {
		t.Fatal("refunded payment retained commercial access")
	}
}

func TestStripeWebhookDurableDeduplication(t *testing.T) {
	service, _ := newTestService(t)
	service.stripe = &sandboxProvider{}
	service.cfg.Stripe.WebhookSecret = "whsec_test"
	body := `{"id":"evt_sandbox","type":"invoice.paid","livemode":false,"data":{"object":{"customer":"cus_sandbox"}}}`
	now := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(service.cfg.Stripe.WebhookSecret))
	fmt.Fprintf(mac, "%d.%s", now, body)
	signature := fmt.Sprintf("t=%d,v1=%s", now, hex.EncodeToString(mac.Sum(nil)))
	for index := 0; index < 2; index++ {
		r := httptest.NewRequest("POST", "/api/v1/saas/subscription/webhook", strings.NewReader(body))
		r.Header.Set("Stripe-Signature", signature)
		w := httptest.NewRecorder()
		service.stripeWebhook(w, r)
		if w.Code != 200 {
			t.Fatalf("webhook %d %s", w.Code, w.Body.String())
		}
	}
	var count int
	if err := service.db.QueryRow(`select count(*) from stripe_webhook_inbox where event_id='evt_sandbox'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("webhook not deduplicated")
	}
	r := httptest.NewRequest("POST", "/api/v1/saas/subscription/webhook", strings.NewReader(body+" "))
	r.Header.Set("Stripe-Signature", signature)
	w := httptest.NewRecorder()
	service.stripeWebhook(w, r)
	if w.Code != 400 {
		t.Fatal("tampered raw body accepted")
	}
}

func TestStripeWorkerRetriesAndRecoversMissedEvents(t *testing.T) {
	for _, failureFirst := range []bool{true, false} {
		name := "periodic_without_webhook"
		if failureFirst {
			name = "durable_retry_after_provider_failure"
		}
		t.Run(name, func(t *testing.T) {
			service, mail := newTestService(t)
			owner := signup(t, service, mail, "stripe-worker@example.test", "Worker test")
			provider := &sandboxProvider{paid: true, unavailable: failureFirst, start: time.Now().Add(-time.Minute).Unix(), end: time.Now().Add(time.Hour).Unix()}
			service.stripe = provider
			service.cfg.SaaS = true
			service.cfg.Stripe = billing.StripeConfig{SecretKey: "rk_test_mock", WebhookSecret: "whsec_test", Prices: map[string]string{"standard": "price_standard", "business": "price_business"}}
			service.cfg.SubscriptionPlans = billing.DefaultPlans()
			if _, err := service.db.Exec(`insert into tenant_stripe_customers(tenant_id,customer_id) values($1,'cus_sandbox')`, owner.TenantID); err != nil {
				t.Fatal(err)
			}
			if failureFirst {
				if _, err := service.db.Exec(`insert into stripe_webhook_inbox(event_id,customer_id,event_type) values('evt_worker','cus_sandbox','invoice.paid')`); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				service.MaintainBilling(ctx)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-stopped:
				case <-time.After(3 * time.Second):
					t.Error("billing worker did not stop after cancellation")
				}
			})
			if failureFirst {
				waitStripeCondition(t, func() bool {
					var delayed bool
					err := service.db.QueryRow(`select verification_delayed and exists(select 1 from stripe_webhook_inbox where event_id='evt_worker' and attempts=1 and processed_at is null and next_attempt_at>now()+interval '15 seconds') from tenant_stripe_customers where tenant_id=$1`, owner.TenantID).Scan(&delayed)
					return err == nil && delayed
				})
				usage, err := store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
				if err != nil || usage.Plan != "free" {
					t.Fatalf("provider failure granted capacity: %+v %v", usage, err)
				}
				provider.mu.Lock()
				provider.unavailable = false
				provider.mu.Unlock()
				if _, err = service.db.Exec(`update stripe_webhook_inbox set next_attempt_at=now() where event_id='evt_worker'`); err != nil {
					t.Fatal(err)
				}
			}
			waitStripeCondition(t, func() bool {
				usage, err := store.ReadTenantLicenseUsage(ctx, service.db, owner.TenantID)
				return err == nil && usage.Plan == "commercial" && usage.MaxClusters == 5
			})
			waitStripeCondition(t, func() bool {
				var delayed bool
				if err := service.db.QueryRow(`select verification_delayed from tenant_stripe_customers where tenant_id=$1`, owner.TenantID).Scan(&delayed); err != nil || delayed {
					return false
				}
				if !failureFirst {
					return true
				}
				var processed bool
				return service.db.QueryRow(`select processed_at is not null from stripe_webhook_inbox where event_id='evt_worker'`).Scan(&processed) == nil && processed
			})
		})
	}
}

func waitStripeCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Stripe worker condition did not become true within the bounded timeout")
}

func TestStripePaidInvoiceMustMatchFullPeriod(t *testing.T) {
	const subscriptionJSON = `{"id":"sub_verified","customer":"cus_verified","status":"active","items":{"data":[{"quantity":1,"current_period_start":1800000000,"current_period_end":1802592000,"price":{"id":"price_standard"}}]}}`
	const invoiceJSON = `{"id":"in_verified","customer":"cus_verified","status":"paid","amount_paid":10000,"amount_due":10000,"currency":"usd","parent":{"subscription_details":{"subscription":"sub_verified"}},"lines":{"has_more":false,"data":[{"amount":10000,"quantity":1,"period":{"start":1800000000,"end":1802592000},"pricing":{"price_details":{"price":"price_standard"}}}]}}`
	var subscription stripeSubscription
	if err := json.Unmarshal([]byte(subscriptionJSON), &subscription); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body string
		paid       bool
	}{
		{"verified", invoiceJSON, true},
		{"other customer", strings.ReplaceAll(invoiceJSON, "cus_verified", "cus_other"), false},
		{"other subscription", strings.ReplaceAll(invoiceJSON, "sub_verified", "sub_other"), false},
		{"other price", strings.ReplaceAll(invoiceJSON, "price_standard", "price_other"), false},
		{"partial payment", strings.ReplaceAll(invoiceJSON, `"amount_paid":10000`, `"amount_paid":9999`), false},
		{"wrong currency", strings.ReplaceAll(invoiceJSON, "usd", "eur"), false},
		{"unpaid", strings.ReplaceAll(invoiceJSON, `"status":"paid"`, `"status":"open"`), false},
		{"unread lines", strings.ReplaceAll(invoiceJSON, `"has_more":false`, `"has_more":true`), false},
		{"partial period", strings.ReplaceAll(invoiceJSON, "1802592000", "1801296000"), false},
		{"wrong quantity", strings.ReplaceAll(invoiceJSON, `"quantity":1`, `"quantity":2`), false},
		{"live", strings.ReplaceAll(invoiceJSON, `"id":"in_verified"`, `"id":"in_verified","livemode":true`), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var invoice stripeInvoice
			if err := json.Unmarshal([]byte(test.body), &invoice); err != nil {
				t.Fatal(err)
			}
			paid, err := paidInvoicePeriod(invoice, subscription, billing.DefaultPlans()[0], "price_standard")
			if err != nil || (paid != nil) != test.paid {
				t.Fatalf("paid=%v error=%v", paid, err)
			}
		})
	}
}
