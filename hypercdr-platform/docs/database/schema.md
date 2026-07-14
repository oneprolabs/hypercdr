# Database Schema Design

PostgreSQL is the first-phase source of truth. The schema should be migration-managed and should avoid storing raw Kubernetes Secret values.

## Naming and IDs

- Use UUID primary keys for platform records.
- Use `created_at`, `updated_at`, and soft-delete timestamps where user-facing records can be removed.
- Use JSONB for Kubernetes resource summaries and provider-specific config, but keep query-critical fields as columns.

## Core Tables

### tenants

Stores tenant boundary.

```sql
create table tenants (
  id uuid primary key,
  name text not null,
  status text not null default 'active',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
```

### users

```sql
create table users (
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
```

### clusters

Represents a registered Kubernetes cluster.

```sql
create table clusters (
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
```

### agent_tokens

One-time bootstrap tokens used by install scripts.

```sql
create table agent_tokens (
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
```

### agent_sessions

Tracks WebSocket connections.

```sql
create table agent_sessions (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid not null references clusters(id),
  agent_id text not null,
  pod_name text,
  remote_addr text,
  status text not null,
  connected_at timestamptz not null default now(),
  disconnected_at timestamptz,
  last_heartbeat_at timestamptz,
  metadata jsonb not null default '{}'::jsonb
);
```

### cluster_nodes

```sql
create table cluster_nodes (
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
```

### applications

First phase application equals Kubernetes namespace.

```sql
create table applications (
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
```

### storage_repositories

```sql
create table storage_repositories (
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
  last_validated_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, name)
);
```

### policies

```sql
create table policies (
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
```

### protection_plans

```sql
create table protection_plans (
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
  status text not null default 'active',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (source_cluster_id, app_id)
);
```

### tasks

```sql
create table tasks (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  cluster_id uuid not null references clusters(id),
  app_id uuid references applications(id),
  protection_plan_id uuid references protection_plans(id),
  restore_point_id uuid,
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
```

### task_events

```sql
create table task_events (
  id uuid primary key,
  task_id uuid not null references tasks(id) on delete cascade,
  level text not null,
  reason text,
  message text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
```

### restore_points

```sql
create table restore_points (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  protection_plan_id uuid not null references protection_plans(id),
  source_cluster_id uuid not null references clusters(id),
  app_id uuid not null references applications(id),
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
```

### audit_logs

```sql
create table audit_logs (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  actor_id uuid references users(id),
  action text not null,
  resource_type text not null,
  resource_id uuid,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
```

## Status Values

Recommended first-phase enums:

- `clusters.status`: `pending`, `healthy`, `warning`, `error`, `syncing`, `offline`.
- `clusters.connection_status`: `online`, `offline`, `reconnecting`.
- `applications.status`: `running`, `pending`, `failed`, `unknown`.
- `applications.protection_status`: `unprotected`, `pending`, `protected`, `failed`.
- `tasks.type`: `inventory_scan`, `backup`, `restore_drill`, `takeover`, `validate_storage`, `agent_upgrade`, `unregister`.
- `tasks.status`: `queued`, `dispatching`, `dispatched`, `accepted`, `running`, `succeeded`, `failed`, `canceled`, `timeout`.
- `restore_points.status`: `creating`, `available`, `expired`, `failed`, `deleting`.

## Indexes

```sql
create index idx_clusters_tenant_status on clusters(tenant_id, status);
create index idx_applications_cluster on applications(cluster_id);
create index idx_applications_protection on applications(tenant_id, protection_status);
create index idx_tasks_cluster_status on tasks(cluster_id, status);
create index idx_tasks_tenant_created on tasks(tenant_id, created_at desc);
create index idx_restore_points_app on restore_points(app_id, created_at desc);
create index idx_agent_sessions_cluster on agent_sessions(cluster_id, connected_at desc);
```

## Migration Phases

1. Base identity and tenant tables.
2. Cluster registration and agent session tables.
3. Inventory tables.
4. Storage, policy, and protection plan tables.
5. Task and event tables.
6. Restore point and audit tables.
