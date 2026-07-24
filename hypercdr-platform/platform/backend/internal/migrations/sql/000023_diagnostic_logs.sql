create table if not exists diagnostic_logs (
  id uuid primary key,
  tenant_id uuid references tenants(id) on delete cascade,
  scope text not null default 'tenant' check (scope in ('tenant','system')),
  level text not null check (level in ('debug','info','warning','error')),
  component text not null,
  operation text not null default '',
  message text not null,
  cluster_id uuid references clusters(id) on delete cascade,
  task_id uuid references tasks(id) on delete cascade,
  command_id uuid,
  request_id uuid,
  error_code text,
  status text,
  duration_ms bigint,
  details jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  check ((scope = 'system' and tenant_id is null) or (scope = 'tenant' and tenant_id is not null))
);

create index if not exists idx_diagnostic_logs_tenant_time on diagnostic_logs(tenant_id, created_at desc);
create index if not exists idx_diagnostic_logs_scope_time on diagnostic_logs(scope, created_at desc);
create index if not exists idx_diagnostic_logs_task_time on diagnostic_logs(task_id, created_at) where task_id is not null;
create index if not exists idx_diagnostic_logs_cluster_time on diagnostic_logs(cluster_id, created_at desc) where cluster_id is not null;
create index if not exists idx_diagnostic_logs_request on diagnostic_logs(request_id) where request_id is not null;
create index if not exists idx_diagnostic_logs_created_at on diagnostic_logs(created_at);
