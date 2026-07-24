alter table users
  add column if not exists time_zone text;

alter table restore_points
  add column if not exists task_created_at timestamptz;

update restore_points rp
set task_created_at = coalesce(t.created_at, rp.created_at)
from tasks t
where rp.task_created_at is null
  and (rp.metadata->>'backupTaskId') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  and nullif(rp.metadata->>'backupTaskId', '')::uuid = t.id;

update restore_points
set task_created_at = created_at
where task_created_at is null;

update restore_points
set display_name = ''
where display_name ~ '^RP-[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$';
