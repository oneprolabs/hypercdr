package billing

import "testing"

func TestWorkerPlanQuotes(t *testing.T) {
	plans := WorkerPlans()
	for _, test := range []struct {
		index, quantity int
		amount          int64
	}{
		{0, 1, 4900}, {0, 2, 9800}, {1, 2, 98000}, {0, 3, 14700}, {0, 5, 24500}, {1, 5, 245000},
		{0, 100, 490000}, {0, 101, 494900}, {2, 1, 490000}, {3, 1, 4900000},
	} {
		quote, err := plans[test.index].WithQuantity(test.quantity)
		if err != nil || quote.AmountMinor != test.amount {
			t.Fatalf("quote %+v: %+v %v", test, quote, err)
		}
		if plans[test.index].PerNode() && quote.MaxClusters != test.quantity {
			t.Fatal("node quota mismatch")
		}
	}
	for _, quantity := range []int{-1, 0, 1000001} {
		if _, err := plans[0].WithQuantity(quantity); err == nil {
			t.Fatalf("accepted invalid quantity %d", quantity)
		}
	}
	if _, err := plans[2].WithQuantity(2); err == nil {
		t.Fatal("accepted multiple unlimited subscriptions")
	}
}
