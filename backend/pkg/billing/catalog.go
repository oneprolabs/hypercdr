package billing

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MaxClusters int    `json:"maxClusters"`
	Unlimited   bool   `json:"unlimited"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
}

var planID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)
var currency = regexp.MustCompile(`^[a-z]{3}$`)

func (plan Plan) Validate() error {
	minimumCapacity := 2
	if plan.PerNode() {
		minimumCapacity = 1
	}
	validCapacity := (plan.Unlimited && plan.MaxClusters == 0) || (!plan.Unlimited && plan.MaxClusters >= minimumCapacity && plan.MaxClusters <= 1000000)
	if !planID.MatchString(plan.ID) || plan.ID == "free" || strings.TrimSpace(plan.Name) != plan.Name || plan.Name == "" || len(plan.Name) > 80 || len(plan.Description) > 500 || !validCapacity || plan.AmountMinor <= 0 || plan.AmountMinor > 100000000000 || !currency.MatchString(plan.Currency) || (plan.Interval != "month" && plan.Interval != "year") {
		return errors.New("invalid commercial subscription plan")
	}
	return nil
}

func DefaultPlans() []Plan {
	return []Plan{
		{ID: "standard", Name: "Standard", Description: "Protect across multiple clusters.", MaxClusters: 5, AmountMinor: 10000, Currency: "usd", Interval: "month"},
		{ID: "business", Name: "Business", Description: "Scale protection without a cluster limit.", Unlimited: true, AmountMinor: 50000, Currency: "usd", Interval: "month"},
	}
}

func WorkerPlans() []Plan {
	return []Plan{
		{ID: "flex-monthly", Name: "Flex", Description: "Per subscribed worker node", MaxClusters: 3, AmountMinor: 4900, Currency: "usd", Interval: "month"},
		{ID: "flex-annual", Name: "Flex", Description: "Per subscribed worker node; pay for 10 months", MaxClusters: 3, AmountMinor: 49000, Currency: "usd", Interval: "year"},
		{ID: "business-monthly", Name: "Business", Description: "Unlimited worker nodes", Unlimited: true, AmountMinor: 490000, Currency: "usd", Interval: "month"},
		{ID: "business-annual", Name: "Business", Description: "Unlimited worker nodes; pay for 10 months", Unlimited: true, AmountMinor: 4900000, Currency: "usd", Interval: "year"},
	}
}

func (plan Plan) PerNode() bool {
	return plan.ID == "flex-monthly" || plan.ID == "flex-annual"
}

func (plan Plan) WithQuantity(quantity int) (Plan, error) {
	if plan.PerNode() {
		if quantity < 1 || quantity > 1000000 {
			return Plan{}, errors.New("Flex requires 1 to 1000000 subscribed worker nodes")
		}
		plan.MaxClusters = quantity
		plan.AmountMinor *= int64(quantity)
	} else if quantity != 1 {
		return Plan{}, errors.New("fixed subscriptions require quantity 1")
	}
	return plan, plan.Validate()
}

func ParsePlans(raw string) ([]Plan, error) {
	plans := []Plan{}
	if strings.TrimSpace(raw) == "" {
		return plans, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plans); err != nil {
		return nil, errors.New("HCDR_SUBSCRIPTION_PLANS_JSON must be a valid plan array")
	}
	if decoder.Decode(new(any)) != io.EOF || plans == nil || len(plans) > 20 {
		return nil, errors.New("invalid subscription plan catalog")
	}
	seen := map[string]bool{}
	for _, plan := range plans {
		if err := plan.Validate(); err != nil || seen[plan.ID] {
			return nil, errors.New("invalid or duplicate subscription plan")
		}
		seen[plan.ID] = true
	}
	return plans, nil
}
