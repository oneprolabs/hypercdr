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

create or replace function enforce_protection_plan_latest_tasks()
returns trigger language plpgsql as $$
declare
  linked_plan_id uuid;
  linked_type text;
begin
  if new.latest_sync_task_id is not null then
    select protection_plan_id, type into linked_plan_id, linked_type from tasks where id = new.latest_sync_task_id;
    if not found or linked_plan_id is distinct from new.id or linked_type <> 'backup' then
      raise exception 'latest_sync_task_id must reference a backup task owned by the same protection plan';
    end if;
  end if;
  if new.latest_recovery_task_id is not null then
    select protection_plan_id, type into linked_plan_id, linked_type from tasks where id = new.latest_recovery_task_id;
    if not found or linked_plan_id is distinct from new.id or linked_type not in ('drill', 'restore', 'takeover') then
      raise exception 'latest_recovery_task_id must reference a recovery task owned by the same protection plan';
    end if;
  end if;
  return new;
end $$;

drop trigger if exists protection_plans_latest_task_guard on protection_plans;
create trigger protection_plans_latest_task_guard
before insert or update of latest_sync_task_id, latest_recovery_task_id on protection_plans
for each row execute function enforce_protection_plan_latest_tasks();
