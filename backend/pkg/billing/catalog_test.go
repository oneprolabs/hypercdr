package billing

import "testing"

func TestDefaultPlans(t *testing.T) {
	plans := DefaultPlans()
	if len(plans) != 2 || plans[0].MaxClusters != 5 || plans[0].Unlimited || plans[0].AmountMinor != 10000 || !plans[1].Unlimited || plans[1].MaxClusters != 0 || plans[1].AmountMinor != 50000 {
		t.Fatalf("unexpected approved catalog: %+v", plans)
	}
	for _, plan := range plans {
		if err := plan.Validate(); err != nil || plan.Currency != "usd" || plan.Interval != "month" {
			t.Fatalf("invalid default: %+v %v", plan, err)
		}
	}
	for _, capacity := range []Plan{
		{MaxClusters: 0}, {MaxClusters: 5, Unlimited: true}, {MaxClusters: -1, Unlimited: true},
	} {
		plan := plans[1]
		plan.MaxClusters, plan.Unlimited = capacity.MaxClusters, capacity.Unlimited
		if plan.Validate() == nil {
			t.Fatalf("accepted contradictory capacity: %+v", plan)
		}
	}
}

func TestParsePlans(t *testing.T) {
	valid := `[{"id":"business-monthly","name":"Business","description":"Shared capacity","maxClusters":5,"amountMinor":1200,"currency":"usd","interval":"month"}]`
	plans, err := ParsePlans(valid)
	if err != nil || len(plans) != 1 || plans[0].MaxClusters != 5 {
		t.Fatalf("valid catalog: %+v %v", plans, err)
	}
	for _, raw := range []string{`null`, `{}`, valid + `{}`, `[{"id":"free"}]`, `[{"unknown":true}]`, `[{"id":"business","name":" ","maxClusters":5,"amountMinor":100,"currency":"usd","interval":"month"}]`, `[{"id":"business","name":"Business","maxClusters":1,"amountMinor":100,"currency":"usd","interval":"month"}]`, `[{"id":"business","name":"Business","maxClusters":5,"amountMinor":0,"currency":"usd","interval":"month"}]`, `[{"id":"business","name":"Business","maxClusters":5,"amountMinor":100,"currency":"usd","interval":"daily"}]`, valid[:len(valid)-1] + "," + valid[1:]} {
		if _, err := ParsePlans(raw); err == nil {
			t.Fatalf("accepted invalid catalog %s", raw)
		}
	}
	if plans, err := ParsePlans(""); err != nil || plans == nil || len(plans) != 0 {
		t.Fatalf("unconfigured catalog: %+v %v", plans, err)
	}
}
