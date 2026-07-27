-- Initial HyperCDR schema.

create table if not exists tenants (
  id uuid primary key,
  name text not null,
  status text not null default 'pending_activation',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists users (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  email text not null,
  password_hash text not null,
  role text not null,
  status text not null default 'active',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, email)
);

create table if not exists clusters (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  name text not null,
  region text,
  kube_version text,
  status text not null default 'pending',
  connection_status text not null default 'offline',
  node_count integer not null default 0,
  namespace_count integer not null default 0,
  application_count integer not null default 0,
  protection_score integer not null default 0,
  role text not null default 'both',
  is_default boolean not null default false,
  agent_version text,
  latest_agent_version text,
  cluster_fingerprint text,
  velero_version text,
  velero_status text,
  last_seen_at timestamptz,
  registered_at timestamptz,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, cluster_fingerprint)
);

create table if not exists agent_tokens (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  token_hash text not null unique,
  description text,
  expires_at timestamptz not null,
  used_at timestamptz,
  cluster_id uuid references clusters(id),
  revoked_at timestamptz,
  created_by uuid references users(id),
  created_at timestamptz not null default now()
);

create table if not exists agent_credentials (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid not null references clusters(id) on delete cascade,
  credential_hash text not null unique,
  status text not null default 'active',
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null default now()
);

create table if not exists agent_sessions (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid references clusters(id),
  agent_id text not null,
  pod_name text,
  remote_addr text,
  status text not null,
  connected_at timestamptz not null default now(),
  disconnected_at timestamptz,
  last_heartbeat_at timestamptz,
  metadata jsonb not null default '{}'::jsonb
);

create table if not exists cluster_nodes (
  id uuid primary key,
  cluster_id uuid not null references clusters(id) on delete cascade,
  name text not null,
  role text,
  status text not null,
  kubelet_version text,
  cpu_capacity text,
  memory_capacity text,
  metadata jsonb not null default '{}'::jsonb,
  last_collected_at timestamptz not null,
  unique (cluster_id, name)
);

create table if not exists applications (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid not null references clusters(id) on delete cascade,
  namespace text not null,
  name text not null,
  status text not null,
  workload_count integer not null default 0,
  service_count integer not null default 0,
  ingress_count integer not null default 0,
  configmap_count integer not null default 0,
  secret_count integer not null default 0,
  pvc_count integer not null default 0,
  pv_capacity_bytes bigint not null default 0,
  protection_status text not null default 'unprotected',
  protection_score integer not null default 0,
  labels jsonb not null default '{}'::jsonb,
  resource_summary jsonb not null default '{}'::jsonb,
  last_collected_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (cluster_id, namespace)
);

create table if not exists storage_repositories (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  name text not null,
  type text not null,
  endpoint text,
  bucket text,
  region text,
  tls_enabled boolean not null default true,
  status text not null default 'unknown',
  config jsonb not null default '{}'::jsonb,
  secret_ref text,
  secret_payload jsonb not null default '{}'::jsonb,
  last_validated_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, name)
);

create table if not exists policies (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  name text not null,
  composition text not null,
  schedule_type text not null,
  interval_value integer,
  interval_unit text,
  hour integer,
  minute integer,
  week_day integer,
  month_day integer,
  retention_count integer,
  retention_days integer,
  status text not null default 'active',
  bound_count integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, name)
);

create table if not exists protection_plans (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  source_cluster_id uuid not null references clusters(id),
  app_id uuid not null references applications(id),
  scope_type text not null,
  label_selector text,
  include_cluster_scoped boolean not null default false,
  storage_repo_id uuid references storage_repositories(id),
  policy_id uuid references policies(id),
  target_cluster_id uuid references clusters(id),
  exclude_rules jsonb not null default '[]'::jsonb,
  pre_hooks jsonb not null default '[]'::jsonb,
  post_hooks jsonb not null default '[]'::jsonb,
  plan_storage_size jsonb not null default '{}'::jsonb,
  status text not null default 'active',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists protection_plan_apps (
  plan_id uuid not null references protection_plans(id) on delete cascade,
  app_id uuid not null references applications(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (plan_id, app_id)
);

create table if not exists protection_plan_schedules (
  protection_plan_id uuid primary key references protection_plans(id) on delete cascade,
  last_fired_at timestamptz,
  next_fire_at timestamptz,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists restore_points (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  protection_plan_id uuid references protection_plans(id),
  source_cluster_id uuid not null references clusters(id),
  app_id uuid references applications(id),
  storage_repo_id uuid references storage_repositories(id),
  velero_backup_name text not null,
  point_type text not null,
  status text not null,
  size_bytes bigint,
  started_at timestamptz,
  completed_at timestamptz,
  expires_at timestamptz,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  unique (source_cluster_id, velero_backup_name)
);

create table if not exists tasks (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid references clusters(id),
  app_id uuid references applications(id),
  protection_plan_id uuid references protection_plans(id),
  restore_point_id uuid references restore_points(id),
  type text not null,
  status text not null default 'queued',
  progress integer not null default 0,
  command_id uuid,
  requested_by uuid references users(id),
  error_code text,
  error_message text,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  dispatched_at timestamptz,
  accepted_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz
);

create table if not exists task_events (
  id uuid primary key,
  task_id uuid not null references tasks(id) on delete cascade,
  level text not null,
  reason text,
  message text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create table if not exists audit_logs (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  actor_id uuid references users(id),
  action text not null,
  resource_type text not null,
  resource_id uuid,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create table if not exists platform_settings (
  tenant_id uuid primary key references tenants(id),
  image_registry text not null default 'registry.local:5000/hypercdr',
  agent_namespace text not null default 'hypercdr-agent',
  velero_version text not null default 'v1.17.1',
  public_endpoint text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists cluster_storage_bindings (
  id uuid primary key,
  tenant_id uuid not null references tenants(id) on delete cascade,
  cluster_id uuid not null references clusters(id) on delete cascade,
  storage_repo_id uuid not null references storage_repositories(id) on delete cascade,
  source_cluster_id uuid not null references clusters(id) on delete cascade,
  bsl_name text not null,
  object_prefix text,
  status text not null default 'pending',
  retry_count integer not null default 0,
  last_synced_at timestamptz,
  last_success_at timestamptz,
  last_error_code text,
  last_error_message text,
  repo_updated_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (cluster_id, storage_repo_id, source_cluster_id)
);

create index if not exists idx_clusters_tenant_status on clusters(tenant_id, status);
create unique index if not exists idx_clusters_single_default
  on clusters(tenant_id)
  where is_default;
create index if not exists idx_agent_credentials_cluster on agent_credentials(cluster_id, status);
create index if not exists idx_applications_cluster on applications(cluster_id);
create index if not exists idx_applications_protection on applications(tenant_id, protection_status);
create index if not exists idx_tasks_cluster_status on tasks(cluster_id, status);
create index if not exists idx_tasks_tenant_created on tasks(tenant_id, created_at desc);
create index if not exists idx_restore_points_app on restore_points(app_id, created_at desc);
create index if not exists idx_agent_sessions_cluster on agent_sessions(cluster_id, connected_at desc);
create index if not exists idx_protection_plan_apps_app on protection_plan_apps(app_id);
create index if not exists protection_plan_schedules_next_fire_at_idx
  on protection_plan_schedules (next_fire_at)
  where enabled = true;
create index if not exists cluster_storage_bindings_cluster_idx
  on cluster_storage_bindings (cluster_id);
create index if not exists cluster_storage_bindings_storage_repo_idx
  on cluster_storage_bindings (storage_repo_id);
create index if not exists cluster_storage_bindings_source_cluster_idx
  on cluster_storage_bindings (source_cluster_id);
