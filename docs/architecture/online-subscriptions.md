# Tenant online subscriptions

## Scope and delivery boundary

HyperCDR follows Devify's free-plan / paid subscription / local entitlement model,
but subscriptions belong to the organization, never to an individual member.
Changing administrators must not change purchased capacity.

The subscription domain and admission checks are extended with a test-only
Stripe adapter. Production keys and live-mode events are rejected. With no
private provider configuration, or without a webhook secret, checkout remains
disabled and existing access is unchanged. A test API connection does not by
itself constitute successful payment or end-to-end acceptance.

Sandbox integration acceptance evidence and any outstanding external setup
requirements are recorded outside the source tree under the runtime validation
directory. Do not enable production payments using this sandbox adapter.

## Plans

Every new organization receives Free: one total managed cluster, no payment
method, no expiry. The approved default commercial offers are Standard
(5 total clusters, USD 100/month) and Business (unlimited clusters,
USD 500/month), billed per organization. Offers can be overridden using
`HCDR_SUBSCRIPTION_PLANS_JSON`, a JSON array with these fields:

| Field | Meaning |
| --- | --- |
| `id` | Stable lowercase commercial offer ID, not `free` |
| `name`, `description` | User-facing plan copy |
| `maxClusters` | Total organization capacity, 2–1,000,000; exactly 0 for unlimited |
| `unlimited` | Explicit boolean; true requires `maxClusters=0` |
| `amountMinor` | Positive price in the currency's minor units |
| `currency` | Lowercase three-letter currency code |
| `interval` | `month` or `year` |

An unset or blank configuration uses the approved default catalog. An explicit
`[]` hides commercial offers. A malformed catalog prevents startup rather than
silently selling a different plan. Configuring a catalog does not activate paid access.
Existing subscription capacity and price are snapshots, not derived from the
latest offer configuration. Removing or changing an offer does not rewrite an
existing paid subscription.

Unlimited is an explicit API capability, not an arbitrarily large quota. The
subscription tables store unlimited capacity as 0; plan validation requires the
explicit boolean at the trusted write boundary and reads restore that boolean.
Only an effective paid subscription can grant unlimited admission. Expiry or
inactivation clears unlimited capacity and returns to the free one-cluster limit.
Migration 000053 widens the subscription constraint without modifying existing
licenses, subscriptions, clusters, or paid-through dates.

## Persistence and effective capacity

Migration 000052 adds a source selector to `tenant_entitlements`, a current
`tenant_subscriptions` row per organization, and append-only application-written
`tenant_subscription_history` revisions. Organization deletion cascades these
records. Customer and subscription IDs are globally unique and hidden from the
public overview response. A provider customer cannot move between organizations.

`store.ApplyTenantSubscription` is a trusted backend-only transaction boundary,
not an API for a browser or tenant administrator to grant paid access. It:

1. Validates the plan snapshot, status, billing period and paid-through date.
2. Takes the same tenant advisory lock as cluster registration.
3. Rejects older/conflicting revisions; exact replay is a no-op.
4. Prevents replacing a non-terminal subscription or changing its customer.
5. Saves subscription, history and entitlement source atomically.

There is deliberately no HTTP endpoint to insert subscription snapshots.
Tests insert explicitly synthetic snapshots only into isolated test databases.

Only `active`, `past_due`, or `canceled` snapshots with a confirmed paid-through
date in the future and an already-started period provide commercial capacity.
Status `active` alone does not grant capacity. Trials do not grant commercial
capacity in this first version. An overdue renewal cannot extend the paid-through
date. Unpaid, paused, incomplete and expired access use the Free limit.
Scheduled cancellation preserves paid capacity through that date.

Expiration never deletes clusters or cancels Sync, Drill, backup or cleanup.
Existing offline/unregistering clusters still occupy capacity. If usage exceeds
the effective limit, only new cluster admission is blocked. Agent reconnection
continues to work. The admission check reads local PostgreSQL data, not Stripe.
Usage is read in one SQL statement; the overview uses a repeatable-read
transaction to keep its subscription and usage consistent.

## Online versus offline

Online (`HCDR_DEPLOYMENT_MODE=saas`) uses Subscription UI and rejects offline
License activation. Private deployments retain the signed License activation
page. Migration preserves existing signed licenses by setting their source to
`license`. An online subscription atomically switches the source; capacities
never stack, and an expired subscription cannot resurrect an old license.

## APIs and authorization

- `GET /api/v1/saas/subscription`: authenticated tenant-local overview, commercial
  catalog, effective capacity and explicit payment availability. Readable by
  members; returns `canManage=false` for non-administrators.
- `POST /api/v1/saas/subscription/checkout`: administrator only, origin checked,
  strict JSON `{"planId":"..."}`. Unknown plans return 400. With sandbox setup
  incomplete, offers return 503 `payment_not_configured`; otherwise a tenant-bound
  checkout URL is returned. Creation does not grant paid capacity.
- `POST /api/v1/saas/subscription/portal`: administrator only, origin checked;
  returns 503 `payment_not_configured` when sandbox setup is incomplete.
- `POST /api/v1/saas/subscription/webhook`: exact public server-to-server endpoint;
  requires raw-body signature verification, not a browser session.
- Private deployment payment requests return 409.

Member roles remain independent: permission to register clusters never bypasses
the organization's commercial capacity. The existing license API path remains
for compatibility, but SaaS activation returns 409 rather than switching sources.

## Stripe integration and acceptance

See `stripe-sandbox.md` for the adapter, configuration, current portal boundary
and mandatory sandbox verification. The following acceptance requirements remain
the release gate; implementing an adapter or passing stub tests is not evidence
that all real sandbox checks have passed.

Obtain an approved test account, securely supplied API key, approved prices and
cluster capacities, dedicated Price IDs and a dedicated Webhook signing secret.
Verify test/live mode and product ownership. Do not reuse Devify's price IDs,
customer IDs or Webhook secret. Confirm currency minor-unit conventions with the
provider before opening a commercial offer.

Implement tenant-customer provisioning, durable Checkout idempotency, one active
subscription per tenant, Checkout and Portal sessions, fixed return destinations,
and a persisted purchase-intent state machine. A return-page query parameter is
not proof of payment. The confirmation UI must poll the local subscription,
handle cancellation/pending/timeout, and never grant access from browser input.

Implement signature verification on raw Webhook bodies, durable event inbox,
duplicate protection, retries, reconciliation and audits. Stripe events are not
an ordered revision stream. Serialize provider reconciliation per tenant using
a durable synchronization lease, retrieve authoritative provider state outside
database transactions, and derive a monotonically increasing **local** revision
when applying it under the tenant lock; do not use event
creation time as a reliable ordering rule. Only confirmed paid invoices may
advance paid access. Verify invoice line/price/customer/subscription ownership,
refund and dispute handling, scheduled downgrades, period boundary behavior and
the incomplete first-payment path. Follow a consistent lock order and do not
hold database transactions across slow provider requests.

Required live-sandbox checks: successful/failed/abandoned purchase; duplicate
clicks and concurrent windows; delayed, duplicate and out-of-order events;
renewal failure; scheduled cancellation; downgrade at period boundary; recovery
after outage; cross-tenant denial; and quota enforcement after refresh/relogin.
Only after these pass may payment be marked configured and buttons enabled.

Do not downgrade the runtime binary to a pre-RBAC version while custom-role
members exist. Back up the database and binaries before deploying migrations;
rollback must preserve compatible authorization checks and subscription data.
