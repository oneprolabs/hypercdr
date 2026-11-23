alter table tenant_stripe_customers add column last_attempt_at timestamptz;
alter table tenant_stripe_customers add column verification_delayed boolean not null default false;
update tenant_stripe_customers set last_attempt_at=reconciled_at;
