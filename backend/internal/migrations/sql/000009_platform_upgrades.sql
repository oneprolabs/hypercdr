create table if not exists platform_releases (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  version text not null,
  api_image text not null,
  api_image_digest text not null,
  frontend_image text not null,
  frontend_image_digest text not null,
  database_schema_version text not null,
  minimum_agent_version text,
  rollback_supported boolean not null default false,
  release_notes text,
  status text not null default 'candidate' check (status in ('candidate', 'active', 'retired')),
  published_by text,
  published_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, version)
);

create unique index if not exists idx_platform_releases_one_active
  on platform_releases (tenant_id) where status = 'active';

create table if not exists platform_upgrade_jobs (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  release_id uuid not null references platform_releases(id),
  from_version text not null,
  target_version text not null,
  status text not null default 'queued',
  step text not null default 'queued',
  progress integer not null default 0 check (progress between 0 and 100),
  api_image text not null,
  api_image_digest text not null,
  frontend_image text not null,
  frontend_image_digest text not null,
  database_schema_version text not null,
  rollback_supported boolean not null default false,
  backup_path text,
  previous_api_image text,
  previous_frontend_image text,
  error_code text,
  error_message text,
  requested_by text,
  executor_id text,
  executor_heartbeat_at timestamptz,
  created_at timestamptz not null default now(),
  started_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz not null default now()
);

create unique index if not exists idx_platform_upgrade_one_active
  on platform_upgrade_jobs (tenant_id)
  where status in ('queued','prechecking','waiting','backing_up','pulling','migrating','switching_api','verifying_api','switching_frontend','verifying','rolling_back');

create index if not exists idx_platform_upgrade_jobs_created
  on platform_upgrade_jobs (tenant_id, created_at desc);
