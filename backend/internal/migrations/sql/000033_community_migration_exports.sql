create table if not exists community_migration_backups (
  migration_id uuid primary key references community_migration_sessions(id) on delete cascade,
  format_version text not null default 'hypercdr-community-logical-backup/v1',
  manifest jsonb not null,
  snapshot jsonb not null,
  snapshot_sha256 text not null,
  created_at timestamptz not null default now()
);

alter table community_migration_sessions
  add column if not exists target_public_key text not null default '';

alter table community_migration_sessions
  add column if not exists observation_ends_at timestamptz;
