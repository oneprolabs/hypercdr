create table tenant_stripe_customers (
 tenant_id uuid primary key references resource_scopes(id) on delete cascade,
 customer_id text unique,
 created_at timestamptz not null default now(),
 reconciled_at timestamptz
);

create table tenant_stripe_checkouts (
 id uuid primary key,
 tenant_id uuid not null references tenant_stripe_customers(tenant_id) on delete cascade,
 plan_id text not null,
 price_id text not null,
 customer_id text not null,
 return_origin text not null,
 session_id text unique,
 session_url text,
 state text not null default 'creating' check (state in ('creating','open','complete','expired')),
 created_at timestamptz not null default now(),
 expires_at timestamptz not null
);
create unique index tenant_stripe_checkout_pending on tenant_stripe_checkouts(tenant_id) where state in ('creating','open');

create table stripe_webhook_inbox (
 event_id text primary key check (length(event_id) between 1 and 255),
 customer_id text not null check (length(customer_id) between 1 and 255),
 event_type text not null check (length(event_type) between 1 and 255),
 received_at timestamptz not null default now(),
 processed_at timestamptz,
 attempts integer not null default 0,
 next_attempt_at timestamptz not null default now()
);
create index stripe_webhook_pending on stripe_webhook_inbox(next_attempt_at) where processed_at is null;

create table stripe_billing_settings (
 singleton boolean primary key default true check (singleton),
 portal_configuration_id text not null
);
