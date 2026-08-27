alter table protection_plans
  add column if not exists latest_sync_task_id uuid,
  add column if not exists latest_recovery_task_id uuid;

do $$
begin
  if not exists (
    select 1 from pg_constraint where conname = 'protection_plans_latest_sync_task_id_fkey'
  ) then
    alter table protection_plans
      add constraint protection_plans_latest_sync_task_id_fkey
      foreign key (latest_sync_task_id) references tasks(id) on delete set null;
  end if;
  if not exists (
    select 1 from pg_constraint where conname = 'protection_plans_latest_recovery_task_id_fkey'
  ) then
    alter table protection_plans
      add constraint protection_plans_latest_recovery_task_id_fkey
      foreign key (latest_recovery_task_id) references tasks(id) on delete set null;
  end if;
end $$;

update protection_plans pp
set latest_sync_task_id = (
  select t.id from tasks t
  where t.protection_plan_id = pp.id and t.type = 'backup'
  order by t.created_at desc, t.id desc limit 1
)
where pp.latest_sync_task_id is null
  and exists (select 1 from tasks t where t.protection_plan_id = pp.id and t.type = 'backup');

update protection_plans pp
set latest_recovery_task_id = (
  select t.id from tasks t
  where t.protection_plan_id = pp.id and t.type in ('drill', 'restore', 'takeover')
  order by t.created_at desc, t.id desc limit 1
)
where pp.latest_recovery_task_id is null
  and exists (select 1 from tasks t where t.protection_plan_id = pp.id and t.type in ('drill', 'restore', 'takeover'));
