package saas

import (
	"context"
	"errors"
	"net/url"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/billing"
)

type stripeSubscription struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Status   string `json:"status"`
	Live     bool   `json:"livemode"`
	Cancel   bool   `json:"cancel_at_period_end"`
	CancelAt int64  `json:"cancel_at"`
	Items    struct {
		Data []struct {
			Quantity int64 `json:"quantity"`
			Start    int64 `json:"current_period_start"`
			End      int64 `json:"current_period_end"`
			Price    struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

type stripeInvoice struct {
	ID         string `json:"id"`
	Customer   string `json:"customer"`
	Status     string `json:"status"`
	Live       bool   `json:"livemode"`
	AmountPaid int64  `json:"amount_paid"`
	AmountDue  int64  `json:"amount_due"`
	Currency   string `json:"currency"`
	Parent     struct {
		Subscription struct {
			ID string `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
	Lines struct {
		More bool `json:"has_more"`
		Data []struct {
			Amount   int64 `json:"amount"`
			Quantity int64 `json:"quantity"`
			Period   struct {
				Start int64 `json:"start"`
				End   int64 `json:"end"`
			} `json:"period"`
			Pricing struct {
				Details struct {
					Price string `json:"price"`
				} `json:"price_details"`
			} `json:"pricing"`
		} `json:"data"`
	} `json:"lines"`
}

func paidInvoicePeriod(invoice stripeInvoice, subscription stripeSubscription, plan billing.Plan, price string) (*time.Time, error) {
	if len(subscription.Items.Data) != 1 {
		return nil, errors.New("unexpected Stripe subscription items")
	}
	if invoice.Live || invoice.Customer != subscription.Customer || invoice.Parent.Subscription.ID != subscription.ID || invoice.Status != "paid" || invoice.Currency != plan.Currency || invoice.AmountPaid < plan.AmountMinor || invoice.AmountDue < plan.AmountMinor || invoice.Lines.More || len(invoice.Lines.Data) != 1 {
		return nil, nil
	}
	line := invoice.Lines.Data[0]
	item := subscription.Items.Data[0]
	if line.Quantity != 1 || line.Amount < plan.AmountMinor || line.Pricing.Details.Price != price || line.Period.Start != item.Start || line.Period.End != item.End {
		return nil, nil
	}
	end := time.Unix(item.End, 0).UTC()
	return &end, nil
}

func (s *Service) reconcileStripe(ctx context.Context, tenant string) error {
	unlock, err := s.billingLock(ctx, tenant)
	if err != nil {
		return err
	}
	defer unlock()
	var customer string
	if err = s.db.QueryRowContext(ctx, `select customer_id from tenant_stripe_customers where tenant_id=$1 and customer_id is not null`, tenant).Scan(&customer); err != nil {
		return err
	}
	var list struct {
		More bool                 `json:"has_more"`
		Data []stripeSubscription `json:"data"`
	}
	if err = s.stripe.Request(ctx, "GET", "/subscriptions", "", url.Values{"customer": {customer}, "status": {"all"}, "limit": {"100"}}, &list); err != nil {
		return err
	}
	if list.More {
		return errors.New("Stripe subscription history exceeds supported size")
	}
	current, err := store.ReadTenantSubscription(ctx, s.db, tenant)
	if err != nil {
		return err
	}
	var selected *stripeSubscription
	for index := range list.Data {
		subscription := &list.Data[index]
		if subscription.Live || subscription.Customer != customer {
			return errors.New("Stripe subscription ownership mismatch")
		}
		if subscription.Status != "canceled" && subscription.Status != "incomplete_expired" {
			if selected != nil {
				return errors.New("multiple active subscriptions require review")
			}
			selected = subscription
		}
	}
	if selected == nil && current != nil {
		for index := range list.Data {
			if list.Data[index].ID == current.SubscriptionID {
				selected = &list.Data[index]
				break
			}
		}
	}
	if selected == nil && current == nil && len(list.Data) > 0 {
		selected = &list.Data[0]
	}
	if selected != nil {
		if current != nil && current.SubscriptionID != selected.ID && current.Status != "canceled" && current.Status != "incomplete_expired" {
			var terminal string
			for _, previous := range list.Data {
				if previous.ID == current.SubscriptionID && (previous.Status == "canceled" || previous.Status == "incomplete_expired") {
					terminal = previous.Status
					break
				}
			}
			if terminal == "" {
				return errors.New("previous Stripe subscription must be finalized first")
			}
			previous := *current
			previous.Status = terminal
			previous.CancelAtPeriodEnd = false
			previous.Revision++
			if _, err = store.ApplyTenantSubscription(ctx, s.db, previous); err != nil {
				return err
			}
			current = &previous
		}
		if len(selected.Items.Data) != 1 || selected.Items.Data[0].Quantity != 1 {
			return errors.New("unexpected Stripe subscription items")
		}
		item := selected.Items.Data[0]
		var plan billing.Plan
		for _, candidate := range s.cfg.SubscriptionPlans {
			if s.cfg.Stripe.Prices[candidate.ID] == item.Price.ID {
				plan = candidate
				break
			}
		}
		if plan.ID == "" || item.Start <= 0 || item.End <= item.Start {
			return errors.New("unknown Stripe subscription plan or period")
		}
		if err = s.stripe.ValidatePrices(ctx, []billing.Plan{plan}); err != nil {
			return err
		}
		var invoices struct {
			More bool            `json:"has_more"`
			Data []stripeInvoice `json:"data"`
		}
		if err = s.stripe.Request(ctx, "GET", "/invoices", "", url.Values{"customer": {customer}, "subscription": {selected.ID}, "status": {"paid"}, "limit": {"20"}}, &invoices); err != nil {
			return err
		}
		var paidUntil *time.Time
		for _, invoice := range invoices.Data {
			paid, proofErr := paidInvoicePeriod(invoice, *selected, plan, item.Price.ID)
			if proofErr != nil {
				return proofErr
			}
			if paid != nil {
				verified, proofErr := s.invoicePaidByCard(ctx, invoice.ID, customer, plan.AmountMinor, plan.Currency)
				if proofErr != nil {
					return proofErr
				}
				if verified {
					paidUntil = paid
				}
				break
			}
		}
		revision := int64(1)
		if current != nil {
			if current.SubscriptionID != selected.ID && current.Status != "canceled" && current.Status != "incomplete_expired" {
				return errors.New("previous Stripe subscription must be finalized first")
			}
			revision = current.Revision + 1
		}
		cancelAtPeriodEnd := selected.Status != "canceled" && selected.Status != "incomplete_expired" && (selected.Cancel || selected.CancelAt == item.End)
		snapshot := store.TenantSubscription{TenantID: tenant, Provider: "stripe", CustomerID: customer, SubscriptionID: selected.ID, Plan: plan, Status: selected.Status, PeriodStart: time.Unix(item.Start, 0).UTC(), PeriodEnd: time.Unix(item.End, 0).UTC(), PaidUntil: paidUntil, CancelAtPeriodEnd: cancelAtPeriodEnd, Revision: revision}
		preservePaidPrevious := current != nil && current.SubscriptionID != selected.ID && paidUntil == nil && current.PaidUntil != nil && current.PaidUntil.After(time.Now())
		unchanged := current != nil && current.SubscriptionID == selected.ID && current.Plan.ID == plan.ID && current.Plan.MaxClusters == plan.MaxClusters && current.Plan.AmountMinor == plan.AmountMinor && current.Plan.Currency == plan.Currency && current.Status == snapshot.Status && current.PeriodStart.Equal(snapshot.PeriodStart) && current.PeriodEnd.Equal(snapshot.PeriodEnd) && current.CancelAtPeriodEnd == snapshot.CancelAtPeriodEnd && samePaidUntil(current.PaidUntil, paidUntil)
		if !preservePaidPrevious && !unchanged {
			if _, err = store.ApplyTenantSubscription(ctx, s.db, snapshot); err != nil {
				return err
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `select id,session_id from tenant_stripe_checkouts where tenant_id=$1 and state='open'`, tenant)
	if err != nil {
		return err
	}
	type pending struct{ id, session string }
	pendingSessions := []pending{}
	for rows.Next() {
		var checkout pending
		if err = rows.Scan(&checkout.id, &checkout.session); err != nil {
			rows.Close()
			return err
		}
		pendingSessions = append(pendingSessions, checkout)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, checkout := range pendingSessions {
		var session stripeSession
		if err = s.stripe.Request(ctx, "GET", "/checkout/sessions/"+url.PathEscape(checkout.session), "", nil, &session); err != nil {
			return err
		}
		if session.Live || session.Customer != customer || session.Reference != tenant {
			return errors.New("Stripe checkout ownership mismatch")
		}
		if session.Status == "complete" || session.Status == "expired" {
			if _, err = s.db.ExecContext(ctx, `update tenant_stripe_checkouts set state=$2 where id=$1`, checkout.id, session.Status); err != nil {
				return err
			}
		}
	}
	_, err = s.db.ExecContext(ctx, `update tenant_stripe_customers set reconciled_at=now(),last_attempt_at=now(),verification_delayed=false where tenant_id=$1`, tenant)
	return err
}

func (s *Service) invoicePaidByCard(ctx context.Context, invoice, customer string, minimum int64, currency string) (bool, error) {
	var payments struct {
		More bool `json:"has_more"`
		Data []struct {
			Amount  int64  `json:"amount_paid"`
			Status  string `json:"status"`
			Payment struct {
				Type   string `json:"type"`
				Intent string `json:"payment_intent"`
			} `json:"payment"`
		} `json:"data"`
	}
	if err := s.stripe.Request(ctx, "GET", "/invoice_payments", "", url.Values{"invoice": {invoice}, "status": {"paid"}, "limit": {"100"}}, &payments); err != nil {
		return false, err
	}
	if payments.More {
		return false, errors.New("invoice payments require review")
	}
	var netPaid int64
	seen := map[string]bool{}
	for _, payment := range payments.Data {
		if payment.Status != "paid" || payment.Payment.Type != "payment_intent" || payment.Payment.Intent == "" || seen[payment.Payment.Intent] {
			continue
		}
		seen[payment.Payment.Intent] = true
		var intent struct {
			Customer string `json:"customer"`
			Status   string `json:"status"`
			Live     bool   `json:"livemode"`
			Currency string `json:"currency"`
			Charge   struct {
				Customer string `json:"customer"`
				Paid     bool   `json:"paid"`
				Live     bool   `json:"livemode"`
				Disputed bool   `json:"disputed"`
				Amount   int64  `json:"amount"`
				Refunded int64  `json:"amount_refunded"`
				Currency string `json:"currency"`
			} `json:"latest_charge"`
		}
		if err := s.stripe.Request(ctx, "GET", "/payment_intents/"+url.PathEscape(payment.Payment.Intent), "", url.Values{"expand[]": {"latest_charge"}}, &intent); err != nil {
			return false, err
		}
		charge := intent.Charge
		if intent.Live || charge.Live || intent.Customer != customer || charge.Customer != customer || intent.Status != "succeeded" || intent.Currency != currency || charge.Currency != currency || !charge.Paid || charge.Disputed || charge.Refunded != 0 {
			continue
		}
		if charge.Amount < payment.Amount {
			return false, errors.New("invoice payment exceeds verified charge")
		}
		netPaid += payment.Amount
	}
	return netPaid >= minimum, nil
}

func samePaidUntil(left, right *time.Time) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && left.Equal(*right))
}

func (s *Service) MaintainBilling(ctx context.Context) {
	if !s.billingReady() {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := s.db.QueryContext(ctx, `select tenant_id from tenant_stripe_customers where customer_id is not null and (last_attempt_at is null or last_attempt_at<now()-interval '5 minutes' or exists(select 1 from stripe_webhook_inbox where customer_id=tenant_stripe_customers.customer_id and processed_at is null and next_attempt_at<=now())) order by last_attempt_at nulls first limit 20`)
		if err != nil {
			continue
		}
		tenants := []string{}
		for rows.Next() {
			var tenant string
			if rows.Scan(&tenant) == nil {
				tenants = append(tenants, tenant)
			}
		}
		rows.Close()
		for _, tenant := range tenants {
			if ctx.Err() != nil {
				return
			}
			work, cancel := context.WithTimeout(ctx, 45*time.Second)
			var watermark time.Time
			err = s.db.QueryRowContext(work, `select now()`).Scan(&watermark)
			if err == nil {
				err = s.reconcileStripe(work, tenant)
			}
			if err == nil {
				_, _ = s.db.ExecContext(work, `update stripe_webhook_inbox set processed_at=now() where customer_id=(select customer_id from tenant_stripe_customers where tenant_id=$1) and processed_at is null and received_at<=$2`, tenant, watermark)
			} else {
				_, _ = s.db.ExecContext(work, `update stripe_webhook_inbox set attempts=attempts+1,next_attempt_at=now()+least(3600,30*power(2,least(attempts,6))) * interval '1 second' where customer_id=(select customer_id from tenant_stripe_customers where tenant_id=$1) and processed_at is null and next_attempt_at<=now()`, tenant)
				_, _ = s.db.ExecContext(work, `update tenant_stripe_customers set last_attempt_at=now(),verification_delayed=true where tenant_id=$1`, tenant)
			}
			cancel()
		}
	}
}
