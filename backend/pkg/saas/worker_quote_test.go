package saas

import (
	"encoding/json"
	"testing"
)

func TestWorkerQuotesDoNotGrantAccess(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "worker-quote@example.test", "Quote test")
	for _, test := range []struct {
		plan     string
		quantity int
		amount   int64
	}{
		{"flex-monthly", 2, 9800}, {"flex-annual", 2, 98000},
		{"flex-monthly", 5, 24500}, {"flex-annual", 5, 245000},
		{"business-monthly", 1, 490000}, {"business-annual", 1, 4900000},
	} {
		response := call(t, service, "POST", "/api/v1/saas/subscription/quote", map[string]any{"planId": test.plan, "quantity": test.quantity}, principal(owner))
		var result struct {
			AmountMinor       int64
			CheckoutAvailable bool
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || result.AmountMinor != test.amount || result.CheckoutAvailable {
			t.Fatalf("quote: %d %s", response.Code, response.Body.String())
		}
	}
	for _, body := range []map[string]any{
		{"planId": "flex-monthly", "quantity": 0}, {"planId": "flex-monthly", "quantity": 3.5},
		{"planId": "business-monthly", "quantity": 2}, {"planId": "standard", "quantity": 1},
		{"planId": "flex-monthly", "quantity": 5, "amountMinor": 1},
	} {
		if response := call(t, service, "POST", "/api/v1/saas/subscription/quote", body, principal(owner)); response.Code != 400 {
			t.Fatalf("invalid quote accepted: %s", response.Body.String())
		}
	}
	var count int
	if err := service.db.QueryRow(`select count(*) from tenant_subscriptions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("quote granted subscription: %d %v", count, err)
	}
}
