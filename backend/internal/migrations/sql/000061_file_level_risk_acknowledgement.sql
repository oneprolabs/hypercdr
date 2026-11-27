alter table protection_plans add constraint protection_plan_file_level_risk_shape check (
    not (data_protection ? 'riskAcknowledgement')
    or coalesce(
        data_protection->>'mode' = 'live-filesystem'
        and jsonb_typeof(data_protection->'riskAcknowledgement') = 'string'
        and data_protection->>'riskAcknowledgement' = 'live-filesystem-no-application-consistency-v1',
        false
    )
);

create or replace function enforce_task_data_protection_snapshot()
returns trigger language plpgsql as $$
begin
    if old.payload->'dataProtectionSnapshotVersion' = '1'::jsonb and (
        new.payload->'dataProtectionSnapshotVersion' is distinct from old.payload->'dataProtectionSnapshotVersion'
        or new.payload->'dataProtectionMode' is distinct from old.payload->'dataProtectionMode'
        or new.payload->'consistencyGroup' is distinct from old.payload->'consistencyGroup'
        or new.payload->'dataProtectionRiskAcknowledgement' is distinct from old.payload->'dataProtectionRiskAcknowledgement'
    ) then
        raise exception 'task data protection snapshot is immutable'
            using errcode = '23514';
    end if;
    return new;
end $$;
