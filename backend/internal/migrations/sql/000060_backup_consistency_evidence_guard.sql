create function enforce_backup_result_snapshot()
returns trigger language plpgsql as $$
begin
    if old.payload->'backupResult' is not null
       and old.payload->'backupResult' <> 'null'::jsonb
       and new.payload->'backupResult' is distinct from old.payload->'backupResult' then
        raise exception 'accepted backup result is immutable' using errcode = '23514';
    end if;
    return new;
end $$;

create trigger tasks_backup_result_snapshot_guard
before update of payload on tasks
for each row execute function enforce_backup_result_snapshot();

create function enforce_restore_point_consistency_evidence()
returns trigger language plpgsql as $$
begin
    if old.metadata->'velero'->'consistency' is not null
       and old.metadata->'velero'->'consistency' <> 'null'::jsonb and (
        new.metadata->'velero'->'consistency' is distinct from old.metadata->'velero'->'consistency'
        or new.metadata->'velero'->'requestedConsistency' is distinct from old.metadata->'velero'->'requestedConsistency'
        or new.metadata->'velero'->'achievedConsistency' is distinct from old.metadata->'velero'->'achievedConsistency'
        or new.backup_task_id is distinct from old.backup_task_id
    ) then
        raise exception 'restore point consistency evidence is immutable' using errcode = '23514';
    end if;
    return new;
end $$;

create trigger restore_points_consistency_evidence_guard
before update of metadata, backup_task_id on restore_points
for each row execute function enforce_restore_point_consistency_evidence();
