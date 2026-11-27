create or replace function enforce_restore_point_consistency_evidence()
returns trigger language plpgsql as $$
begin
    if (
        (old.metadata->'velero'->'consistency' is not null and old.metadata->'velero'->'consistency' <> 'null'::jsonb)
        or (old.metadata->'velero'->'snapshotEvidence' is not null and old.metadata->'velero'->'snapshotEvidence' <> 'null'::jsonb)
    ) and (
        new.metadata->'velero'->'consistency' is distinct from old.metadata->'velero'->'consistency'
        or new.metadata->'velero'->'snapshotEvidence' is distinct from old.metadata->'velero'->'snapshotEvidence'
        or new.metadata->'velero'->'requestedConsistency' is distinct from old.metadata->'velero'->'requestedConsistency'
        or new.metadata->'velero'->'achievedConsistency' is distinct from old.metadata->'velero'->'achievedConsistency'
        or new.backup_task_id is distinct from old.backup_task_id
    ) then
        raise exception 'restore point consistency evidence is immutable' using errcode = '23514';
    end if;
    return new;
end $$;
