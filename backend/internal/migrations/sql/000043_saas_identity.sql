-- One global account belongs to exactly one resource scope (organization).
-- users.tenant_id is NOT NULL and the email index is globally unique.
alter table resource_scopes add column if not exists description text not null default '';
comment on table resource_scopes is 'Organization isolation scopes shared by both editions.';
create table saas_onboarding_tokens (
 token_hash text primary key, kind text not null check(kind in ('signup','invitation','google','session')),
 email text not null, tenant_id uuid references resource_scopes(id), role_id text check(role_id in ('admin','operator')),
 user_id uuid references users(id) on delete cascade, browser_hash text, provider_subject text,
 created_by uuid references users(id), created_at timestamptz not null default now(),
 expires_at timestamptz not null, consumed_at timestamptz,
 check(kind <> 'invitation' or (tenant_id is not null and role_id is not null))
);
create index on saas_onboarding_tokens(expires_at);
create table saas_external_identities (
 provider text not null check(provider='google'), subject text not null,
 user_id uuid not null references users(id) on delete cascade,
 primary key(provider,subject), unique(provider,user_id)
);
create table saas_oauth_requests (
 state_hash text primary key, browser_hash text not null, nonce text not null, verifier text not null,
 invitation_hash text, linking_user_id uuid references users(id) on delete cascade,
 expires_at timestamptz not null
);
create table saas_auth_rate_limits(bucket text primary key,count integer not null,expires_at timestamptz not null);
