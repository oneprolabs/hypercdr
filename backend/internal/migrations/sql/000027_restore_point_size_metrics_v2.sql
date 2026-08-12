alter table restore_points
    add column if not exists size_metrics_v2 jsonb not null default '{}'::jsonb;

create index if not exists restore_points_tenant_plan_created_idx
    on restore_points (tenant_id, protection_plan_id, created_at desc);
