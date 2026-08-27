alter table restore_points
  add column if not exists backup_task_id uuid;

do $$
begin
  if not exists (
    select 1 from pg_constraint where conname = 'restore_points_backup_task_id_fkey'
  ) then
    alter table restore_points
      add constraint restore_points_backup_task_id_fkey
      foreign key (backup_task_id) references tasks(id) on delete set null;
  end if;
end $$;

create or replace function enforce_restore_point_backup_task()
returns trigger language plpgsql as $$
declare
  linked_plan_id uuid;
  linked_type text;
  linked_created_at timestamptz;
begin
  if new.backup_task_id is null then
    raise exception 'restore point backup_task_id is required';
  end if;
  select protection_plan_id, type, created_at
    into linked_plan_id, linked_type, linked_created_at
    from tasks where id = new.backup_task_id;
  if not found or linked_type <> 'backup' or linked_plan_id is distinct from new.protection_plan_id then
    raise exception 'restore point backup task must be a backup task owned by the same protection plan';
  end if;
  new.task_created_at = linked_created_at;
  return new;
end $$;

drop trigger if exists restore_points_backup_task_guard on restore_points;
create trigger restore_points_backup_task_guard
before insert or update of backup_task_id, protection_plan_id, task_created_at on restore_points
for each row execute function enforce_restore_point_backup_task();

create index if not exists restore_points_backup_task_id_idx
  on restore_points (backup_task_id)
  where backup_task_id is not null;
