alter table tenant_entitlements add column source text not null default 'subscription' check (source in ('subscription','license'));
update tenant_entitlements set source='license' where license_id is not null;

create table tenant_subscriptions (
 tenant_id uuid primary key references resource_scopes(id) on delete cascade,
 provider text not null check (provider='stripe'),
 customer_id text not null unique check (length(customer_id) between 1 and 255),
 subscription_id text not null unique check (length(subscription_id) between 1 and 255),
 plan_id text not null check (length(plan_id) between 1 and 80),
 plan_name text not null check (length(plan_name) between 1 and 80),
 status text not null check (status in ('incomplete','incomplete_expired','trialing','active','past_due','canceled','unpaid','paused')),
 max_clusters integer not null check (max_clusters between 2 and 1000000),
 amount_minor bigint not null check (amount_minor > 0),
 currency text not null check (currency ~ '^[a-z]{3}$'),
 billing_interval text not null check (billing_interval in ('month','year')),
 period_start timestamptz not null,
 period_end timestamptz not null check (period_end > period_start),
 paid_until timestamptz,
 cancel_at_period_end boolean not null default false,
 revision bigint not null check (revision > 0),
 updated_at timestamptz not null default now(),
 check (paid_until is null or paid_until <= period_end)
);

create table tenant_subscription_history (
 tenant_id uuid not null references tenant_subscriptions(tenant_id) on delete cascade,
 revision bigint not null,
 plan_id text not null,
 status text not null,
 max_clusters integer not null,
 paid_until timestamptz,
 cancel_at_period_end boolean not null,
 recorded_at timestamptz not null default now(),
 primary key (tenant_id,revision)
);
