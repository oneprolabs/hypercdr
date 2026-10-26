alter table policies add column retention_rules jsonb not null default '[]'::jsonb;
alter table policies add column retention_timezone text not null default 'UTC';

create table restore_point_reservations (
    task_id uuid primary key references tasks(id) on delete cascade,
    protection_plan_id uuid not null references protection_plans(id) on delete cascade,
    replacement_point_id uuid references restore_points(id) on delete set null,
    state text not null default 'reserved' check (state in ('reserved','data_ready','deleting','published','released')),
    pending_point jsonb,
    cleanup_task_id uuid references tasks(id) on delete set null,
    cleanup_attempts integer not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create unique index restore_point_reservation_replacement on restore_point_reservations(replacement_point_id)
    where state in ('reserved','data_ready','deleting');
create index restore_point_reservation_plan on restore_point_reservations(protection_plan_id,state);

update tasks t set payload=coalesce(t.payload,'{}'::jsonb) || jsonb_build_object('restorePointTaskCreatedAt',coalesce(r.task_created_at,r.created_at))
from restore_points r where coalesce(t.restore_point_id::text,t.payload->>'restorePointId')=r.id::text
and t.type in ('drill','restore','takeover');

update restore_points r set metadata=r.metadata || '{"drillRetained":true}'::jsonb
where r.status='available' and r.id in (
    select distinct on (protection_plan_id) coalesce(restore_point_id,nullif(payload->>'restorePointId','')::uuid)
    from tasks where type='drill' and status='succeeded' and protection_plan_id is not null
    order by protection_plan_id,completed_at desc nulls last,created_at desc,id desc
);
