package saas

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/pkg/billing"
)

func TestWorkerCheckoutQuantityAndReplacement(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "worker-checkout@example.test", "Worker checkout")
	provider := &sandboxProvider{}
	service.stripe = provider
	service.cfg.SubscriptionPlans = billing.WorkerPlans()
	service.cfg.Stripe = billing.StripeConfig{Prices: map[string]string{"flex-monthly": "price_flex"}}
	ctx := context.WithValue(context.Background(), sandboxTenantKey{}, owner.TenantID)
	plan := billing.WorkerPlans()[0]
	for _, quantity := range []int{5, 5, 6} {
		if _, err := service.createCheckout(ctx, owner.TenantID, plan, quantity); err != nil {
			t.Fatal(err)
		}
	}
	if provider.checkouts != 2 || provider.lastQuantity != "6" {
		t.Fatalf("quantity or reuse: %d %s", provider.checkouts, provider.lastQuantity)
	}
	var quantity int
	if err := service.db.QueryRow(`select quantity from tenant_stripe_checkouts where tenant_id=$1 and state='open'`, owner.TenantID).Scan(&quantity); err != nil || quantity != 6 {
		t.Fatalf("persisted quantity %d %v", quantity, err)
	}
}

func TestWorkerInvoiceQuantityAndTotal(t *testing.T) {
	var subscription stripeSubscription
	if err := json.Unmarshal([]byte(`{"id":"sub_test","customer":"cus_test","items":{"data":[{"quantity":5,"current_period_start":1800000000,"current_period_end":1802592000}]}}`), &subscription); err != nil {
		t.Fatal(err)
	}
	const body = `{"id":"in_test","customer":"cus_test","status":"paid","currency":"usd","amount_paid":24500,"amount_due":24500,"parent":{"subscription_details":{"subscription":"sub_test"}},"lines":{"data":[{"quantity":5,"amount":24500,"period":{"start":1800000000,"end":1802592000},"pricing":{"price_details":{"price":"price_flex"}}}]}}`
	plan, err := billing.WorkerPlans()[0].WithQuantity(5)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		body  string
		valid bool
	}{
		{body, true},
		{strings.ReplaceAll(body, "24500", "4900"), false},
		{strings.ReplaceAll(body, `"quantity":5`, `"quantity":1`), false},
	} {
		var invoice stripeInvoice
		if err := json.Unmarshal([]byte(test.body), &invoice); err != nil {
			t.Fatal(err)
		}
		paid, err := paidInvoicePeriod(invoice, subscription, plan, "price_flex")
		if err != nil || (paid != nil) != test.valid {
			t.Fatalf("invoice proof %v %v", paid, err)
		}
	}
}
