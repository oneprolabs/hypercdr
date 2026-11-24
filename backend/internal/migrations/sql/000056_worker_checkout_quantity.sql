alter table tenant_stripe_checkouts add column quantity integer not null default 1 check (quantity between 1 and 1000000);
