# Stripe sandbox subscription adapter

## Configuration and safety boundary

Set `HCDR_STRIPE_CONFIG_FILE` to an external private regular file (mode 0600,
at most 16 KiB). Its literal `NAME=value` entries are:

```
HCDR_STRIPE_MODE=test
HCDR_STRIPE_SECRET_KEY=rk_test_REPLACE_OUTSIDE_GIT
HCDR_STRIPE_STANDARD_PRICE_ID=price_REPLACE_STANDARD
HCDR_STRIPE_BUSINESS_PRICE_ID=price_REPLACE_BUSINESS
HCDR_STRIPE_WEBHOOK_SECRET=whsec_REPLACE_OUTSIDE_GIT
```

Do not place real credentials in documentation, command arguments, screenshots,
the source tree, or shell history. The file is parsed, not executed. Unknown or
duplicate entries, live keys, and public file permissions fail startup. Private
deployments cannot enable this adapter. An empty webhook secret leaves purchases
disabled, allowing read-only and write-permission preflight before activation.

The API version is pinned to `2026-11-18.endive`; requests have a bounded timeout,
bounded response size, no redirect following, and sanitized errors. Write calls
require an idempotency key. The restricted sandbox key needs Customer, Checkout,
Subscription and Customer Portal operations plus reads for Products/Prices,
Invoices/Invoice Payments, Payment Intents and Charges. Do not grant unrelated
live-mode or account-administration access.

For local sandbox verification, Stripe CLI can forward genuine events to the
local webhook endpoint. Its signing secret must match the API configuration.
This development listener is not a production webhook deployment: keep it
running while sandbox checkout is enabled, or clear the webhook secret and
restart the API to disable new purchases. A stopped listener must not be
reported as a working callback setup. Test Clock write access is needed only
for accelerated lifecycle acceptance, not for ordinary checkout operation.

## Checkout and ownership

The server chooses the price and quantity (always 1), tenant and customer from
the authenticated principal and configured catalog. Client-supplied capacity,
price, customer and redirect URL fields are rejected. Only tenant administrators
can purchase or open the portal; business-role permissions do not grant billing
administration.

Migration 000054 stores a unique tenant/customer binding, pending checkout intent,
Stripe session, exact return origin, webhook inbox and shared portal configuration.
A tenant-scoped PostgreSQL session advisory lock serializes checkout and
reconciliation across processes without keeping a database transaction open
during provider HTTP requests. A durable intent supplies a stable provider
idempotency key. Ambiguous uncompleted operations older than 23 hours require
operator review rather than blindly reusing an expired provider idempotency key.
An existing nonterminal subscription prevents a second subscription. Selecting
a different plan expires a previously open checkout before creating another.

Return destinations use the configured platform origin, never a client URL or
request Host. Redirect destinations must be exact HTTPS Stripe Checkout or
Billing Portal hosts. Returning from checkout does not grant capacity: the UI
loads persisted entitlements and polls for up to two minutes. Cancellation is
shown separately. The compact subscription layout and tenant membership model
are retained; sandbox billing is explicitly labeled when enabled.

## Webhooks and reconciliation

The sole public billing endpoint is exact `POST
/api/v1/saas/subscription/webhook`; all other subscription endpoints still require
authentication. Raw payloads are limited to 1 MiB and verified using HMAC-SHA256,
constant-time comparison and a five-minute past/future timestamp tolerance.
Browser Origin/session checks are not applied to this server-to-server endpoint.
Signed live events are rejected. A successful response means the event has been
durably deduplicated by event ID, not that the payment has been applied. Only
customer/event identifiers and type are stored, not full billing payloads.

A lifecycle-bound worker checks the inbox every five seconds, retries failures
with bounded exponential backoff, and reconciles customers every five minutes
to recover missed webhooks. It retrieves current Stripe state rather than
ordering grants by event timestamps. Events arriving during a reconciliation
remain pending for another pass. Local subscription history uses monotonically
increasing revisions; unchanged state is a no-op.

Commercial entitlement requires a matching paid invoice for the same customer,
subscription, price, quantity, currency and current full billing period. The
paid amount must cover the approved offer, and verified invoice payments must
link to successful test-mode PaymentIntents and their paid charges without
refunds or disputes. Payment method availability is managed in Stripe Dashboard;
Checkout disables Adaptive Pricing to retain the approved USD currency. Trials,
out-of-band paid markers, credits alone, unpaid invoices and success URL visits
do not grant capacity. Full or partial refunds and disputed charges conservatively
remove the commercial entitlement once reconciled. Failure to read payment proof
does not create new paid access. Existing unexpired confirmed access is retained
until a subsequent successful reconciliation or its previously stored expiry.

Scheduled cancellation preserves confirmed paid access until expiry. Expiry
returns to Free (one cluster) without deleting clusters or interrupting tasks.
For an active subscription, a positive `cancel_at` equal to its item period end
also represents period-end cancellation, even when `cancel_at_period_end` is
false. Completed cancellations are not displayed as merely scheduled.
The portal allows invoices, customer email, payment methods and period-end
cancellation; in-place tier changes/proration are intentionally not enabled in
this first sandbox adapter. They require separate paid-proration acceptance,
not an unverified expansion of entitlement based on a new subscription price.

## Validation

Run isolated database tests using `scripts/test-backend.sh` for `pkg/billing`,
`pkg/saas`, `internal/store`, and `internal/httpserver`; run frontend lint and
the API/model tests, then the fixed development deployment script. Never run
these isolated tests against the platform database. Back up the 149 database
and runtime binary before applying migrations.

Real acceptance additionally requires Stripe-hosted test checkout, successful
and declined test cards, cancellation, genuine signed webhook forwarding to149,
refresh, permission/tenant boundaries and quota checks. Locally signed test
fixtures prove handler behavior only, not real payment or webhook delivery.
Use disposable tenants and customers, and clean up unpaid sessions, test
subscriptions, sessions and validation tenants. Never substitute live payments.
