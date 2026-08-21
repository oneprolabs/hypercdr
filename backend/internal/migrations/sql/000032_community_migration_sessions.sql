alter table platform_settings add column if not exists platform_instance_id text;
update platform_settings
set platform_instance_id = md5(random()::text || clock_timestamp()::text)
where coalesce(platform_instance_id, '') = '';
alter table platform_settings alter column platform_instance_id set not null;
alter table platform_settings alter column platform_instance_id set default md5(random()::text || clock_timestamp()::text);

create table if not exists community_migration_authorizations (
  id uuid primary key,
  token_hash text not null unique,
  created_by uuid references users(id),
  expires_at timestamptz not null,
  used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null default now()
);

create table if not exists community_migration_sessions (
  id uuid primary key,
  authorization_id uuid not null references community_migration_authorizations(id),
  session_token_hash text not null unique,
  source_instance_id text not null,
  target_instance_id text not null,
  protocol_version text not null,
  state text not null default 'prechecking',
  frozen boolean not null default false,
  expires_at timestamptz not null,
  last_error_code text,
  last_error_message text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  completed_at timestamptz
);

create unique index if not exists community_one_active_migration
  on community_migration_sessions ((true))
  where state not in ('committed','rolled-back','failed','revoked');

create table if not exists community_migration_events (
  id bigserial primary key,
  migration_id uuid not null references community_migration_sessions(id) on delete cascade,
  state text not null,
  reason text not null,
  message text not null,
  created_at timestamptz not null default now()
);
