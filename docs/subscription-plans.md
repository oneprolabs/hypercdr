# Worker-node subscriptions

| Plan | Monthly | Annual | Node quota |
| --- | --- | --- | --- |
| Free | $0 | $0 | 2 |
| Flex | $49 × subscribed nodes | $490 × subscribed nodes | Purchased quantity (minimum 1) |
| Business | $4,900 | $49,000 | Unlimited |

Annual billing pays for a full year at ten monthly payments. Flex charges every
subscribed node, including the first two. Business and Flex cost the same at
100 nodes; Business is cheaper above 100 nodes.

The Flex configurator displays detected current worker nodes (read only) and a
single editable subscribed-node quantity, initially set to current usage (at
least 1). The quantity must be an integer not below current usage, up to
1,000,000. A note recommends allowing expansion headroom; there is no separate
reserved-node input. Every subscribed node is billed, including the first two:
2 subscribed nodes costs $98/month or $980/year. Small environments may choose
Free instead of paid Flex.

All three plans allow unlimited cluster registrations, even when worker-node
usage is over quota or grace has expired. Registration permissions and tenant
status still apply. Node overage prompts a plan/quota change; it never blocks
cluster registration.

The subscription overview uses confirmed persisted entitlement, not detected
usage or a quote, to decide whether an organization is Free, Flex or Business.
Previous subscriptions retain their original price and are identified as legacy
subscriptions; they are not silently migrated.

`POST /api/v1/saas/subscription/quote` accepts `planId` and integer `quantity`.
Plan IDs are `flex-monthly`, `flex-annual`, `business-monthly`, and
`business-annual`. Business quantity must be 1. The server computes the amount;
client-supplied prices are rejected. A quote never changes subscription access.

## Payment integration boundary

New-plan payment activation is pending. The UI offers quote review only and
does not invoke Stripe Checkout. No Stripe products or prices are created by
this change. Existing products, subscriptions and reconciliation remain intact.

Later configuration needs two Stripe products with four recurring prices:
Flex monthly/annual ($49/$490 per node), Business monthly/annual
($4,900/$49,000). Flex Checkout passes the purchased node quantity; Business
passes 1. Paid invoice verification matches both quantity and total amount
before assigning capacity. Pending checkout reuse must match plan and quantity.

Over-quota protection tasks continue during a 30-day grace period. After it
ends, only new Sync (`backup`) and Drill (`drill`) tasks pause. All other
operations remain unrestricted by node quota, including cluster registration,
restore, takeover, failback, storage/schedule configuration, cache settings,
cleanup, cancellation and upgrades. Already running Sync/Drill tasks are not
stopped. Unlimited Business subscriptions do not trigger node overage.
