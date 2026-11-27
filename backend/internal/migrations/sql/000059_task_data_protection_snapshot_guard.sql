create function enforce_task_data_protection_snapshot()
returns trigger language plpgsql as $$
begin
    if old.payload->'dataProtectionSnapshotVersion' = '1'::jsonb and (
        new.payload->'dataProtectionSnapshotVersion' is distinct from old.payload->'dataProtectionSnapshotVersion'
        or new.payload->'dataProtectionMode' is distinct from old.payload->'dataProtectionMode'
        or new.payload->'consistencyGroup' is distinct from old.payload->'consistencyGroup'
    ) then
        raise exception 'task data protection snapshot is immutable'
            using errcode = '23514';
    end if;
    return new;
end $$;

create trigger tasks_data_protection_snapshot_guard
before update of payload on tasks
for each row execute function enforce_task_data_protection_snapshot();
