alter table users add column if not exists display_name text;
alter table users add column if not exists auth_provider text not null default 'password';

update users set role = 'operator' where role = 'member';

create table if not exists platform_sessions (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  token_hash text not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  last_seen_at timestamptz not null default now()
);

create index if not exists idx_platform_sessions_user on platform_sessions(user_id, expires_at desc);
