package saas

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/billing"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func TestApprovedSubscriptionCatalogConfiguration(t *testing.T) {
	t.Setenv("HCDR_DEPLOYMENT_MODE", "private")
	t.Setenv("HCDR_LICENSE_PUBLIC_KEYS", "")
	t.Setenv("HCDR_GOOGLE_CLIENT_ID", "")
	t.Setenv("HCDR_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("HCDR_SUBSCRIPTION_PLANS_JSON", "")
	cfg, err := ConfigFromEnvironment()
	if err != nil || len(cfg.SubscriptionPlans) != 4 || cfg.SubscriptionPlans[0].MaxClusters != 3 || cfg.SubscriptionPlans[0].AmountMinor != 4900 || !cfg.SubscriptionPlans[2].Unlimited || cfg.SubscriptionPlans[2].AmountMinor != 490000 {
		t.Fatalf("approved default catalog: %+v %v", cfg.SubscriptionPlans, err)
	}
	t.Setenv("HCDR_SUBSCRIPTION_PLANS_JSON", "[]")
	cfg, err = ConfigFromEnvironment()
	if err != nil || len(cfg.SubscriptionPlans) != 0 {
		t.Fatalf("explicit catalog disable: %+v %v", cfg.SubscriptionPlans, err)
	}
	t.Setenv("HCDR_SUBSCRIPTION_PLANS_JSON", `[{"id":"business","name":"Business","maxClusters":0,"unlimited":false,"amountMinor":50000,"currency":"usd","interval":"month"}]`)
	if _, err := ConfigFromEnvironment(); err == nil {
		t.Fatal("contradictory unlimited catalog must fail startup")
	}
}

func TestSubscriptionAPIWithoutPaymentProvider(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "subscription@example.test", "Subscription organization")
	other := signup(t, service, mail, "other-subscription@example.test", "Other subscription organization")
	admin := principal(owner)
	member := platform.Principal{ID: owner.ID, TenantID: owner.TenantID, Role: "operator"}
	plan := billing.Plan{ID: "business", Name: "Business", MaxClusters: 5, AmountMinor: 1200, Currency: "usd", Interval: "month"}
	service.cfg.SubscriptionPlans = []billing.Plan{plan}
	path := "/api/v1/saas/subscription"
	for _, actor := range []platform.Principal{admin, member} {
		response := call(t, service, "GET", path, nil, actor)
		var result struct {
			Usage     store.TenantLicenseUsage
			Payment   struct{ Configured bool }
			CanManage bool
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); response.Code != 200 || err != nil || result.Usage.Plan != "free" || result.Usage.MaxClusters != 1 || result.Payment.Configured || result.CanManage != (actor.Role == "admin") {
			t.Fatalf("overview: %d %s %v", response.Code, response.Body.String(), err)
		}
	}
	for _, action := range []string{"checkout", "portal"} {
		if response := call(t, service, "POST", path+"/"+action, map[string]any{"planId": "business", "maxClusters": 1000, "tenantId": other.TenantID}, member); response.Code != 403 {
			t.Fatalf("member %s: %d", action, response.Code)
		}
		if response := call(t, service, "POST", path+"/"+action, map[string]string{"planId": "business"}, admin); response.Code != 503 || !strings.Contains(response.Body.String(), "payment_not_configured") {
			t.Fatalf("unconfigured %s: %d %s", action, response.Code, response.Body.String())
		}
	}
	if response := call(t, service, "POST", path+"/checkout", map[string]string{"planId": "unknown"}, admin); response.Code != 400 {
		t.Fatalf("unknown plan: %d", response.Code)
	}
	if response := call(t, service, "POST", path+"/checkout", map[string]any{"planId": "business", "tenantId": other.TenantID, "paid": true}, admin); response.Code != 400 {
		t.Fatalf("untrusted checkout fields: %d", response.Code)
	}
	if response := call(t, service, "POST", "/api/v1/saas/license", map[string]string{"license": "invalid"}, admin); response.Code != 409 {
		t.Fatalf("online license activation: %d", response.Code)
	}
	var count int
	if err := service.db.QueryRow(`select count(*) from tenant_subscriptions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unconfigured payments must not create subscriptions: %d %v", count, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(time.Hour)
	snapshot := store.TenantSubscription{TenantID: owner.TenantID, Provider: "stripe", CustomerID: "cus_test", SubscriptionID: "sub_test", Plan: plan, Status: "active", PeriodStart: now.Add(-time.Hour), PeriodEnd: end, PaidUntil: &end, Revision: 1}
	if _, err := store.ApplyTenantSubscription(context.Background(), service.db, snapshot); err != nil {
		t.Fatal(err)
	}
	response := call(t, service, "GET", path+"?tenantId="+owner.TenantID, nil, principal(other))
	var otherOverview struct {
		Usage        store.TenantLicenseUsage
		Subscription *store.TenantSubscription
	}
	decodeErr := json.Unmarshal(response.Body.Bytes(), &otherOverview)
	if response.Code != 200 || decodeErr != nil || otherOverview.Usage.TenantID != other.TenantID || otherOverview.Usage.MaxClusters != 1 || otherOverview.Subscription != nil {
		t.Fatalf("cross tenant overview: %d %s", response.Code, response.Body.String())
	}
	response = call(t, service, "GET", path, nil, admin)
	if response.Code != 200 || strings.Contains(response.Body.String(), "cus_test") || strings.Contains(response.Body.String(), "sub_test") || !strings.Contains(response.Body.String(), `"plan":"commercial"`) {
		t.Fatalf("overview exposure: %d %s", response.Code, response.Body.String())
	}
	service.cfg.SaaS = false
	if response := call(t, service, "POST", path+"/checkout", map[string]string{"planId": "business"}, admin); response.Code != 409 {
		t.Fatalf("private checkout: %d", response.Code)
	}
}
