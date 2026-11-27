create table cluster_sync_settings (
    cluster_id uuid primary key references clusters(id) on delete cascade,
    max_sync_tasks integer not null default 4 check (max_sync_tasks between 1 and 20),
    node_transfer_concurrency integer not null default 4 check (node_transfer_concurrency between 1 and 32),
    applied_max_sync_tasks integer,
    applied_node_transfer_concurrency integer,
    apply_task_id uuid references tasks(id) on delete set null,
    updated_at timestamptz not null default now()
);
alter table tasks add column sync_admitted_at timestamptz;
create index tasks_sync_admission_idx on tasks(cluster_id,created_at,id)
    where type='backup' and completed_at is null;
