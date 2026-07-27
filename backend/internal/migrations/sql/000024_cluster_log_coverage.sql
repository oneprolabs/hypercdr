alter table diagnostic_logs
  add column if not exists event_at timestamptz,
  add column if not exists fingerprint text;

update diagnostic_logs
set event_at = case
  when component in ('comm-agent', 'velero', 'node-agent')
    and details ? 'sourceTimestamp'
    and coalesce(details->>'sourceTimestamp', '') <> ''
  then (details->>'sourceTimestamp')::timestamptz
  else created_at
end
where event_at is null;

alter table diagnostic_logs alter column event_at set default now();
alter table diagnostic_logs alter column event_at set not null;

create unique index if not exists idx_diagnostic_logs_fingerprint
  on diagnostic_logs(fingerprint) where fingerprint is not null;
create index if not exists idx_diagnostic_logs_cluster_event_time
  on diagnostic_logs(cluster_id, component, event_at desc) where cluster_id is not null;

create table if not exists cluster_log_coverage (
  cluster_id uuid not null references clusters(id) on delete cascade,
  tenant_id uuid not null references tenants(id) on delete cascade,
  component text not null check (component in ('comm-agent', 'velero', 'node-agent')),
  covered_from timestamptz not null,
  covered_to timestamptz not null,
  last_collected_at timestamptz not null,
  last_request_id uuid,
  last_entry_count integer not null default 0,
  truncated boolean not null default false,
  updated_at timestamptz not null default now(),
  primary key (cluster_id, component)
);

create index if not exists idx_cluster_log_coverage_tenant
  on cluster_log_coverage(tenant_id, updated_at desc);
