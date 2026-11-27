alter table protection_plans add constraint protection_plan_risk_acceptance_shape check (
    not (data_protection ? 'riskAcceptance') or coalesce(
        data_protection->>'mode' = 'live-filesystem'
        and data_protection->>'riskAcknowledgement' = 'live-filesystem-no-application-consistency-v1'
        and jsonb_typeof(data_protection->'riskAcceptance') = 'object'
        and jsonb_typeof(data_protection->'riskAcceptance'->'actorId') = 'string'
        and length(data_protection->'riskAcceptance'->>'actorId') > 0
        and jsonb_typeof(data_protection->'riskAcceptance'->'acceptedAt') = 'string'
        and length(data_protection->'riskAcceptance'->>'acceptedAt') > 0,
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
        or new.payload->'dataProtectionRiskAcceptance' is distinct from old.payload->'dataProtectionRiskAcceptance'
    ) then
        raise exception 'task data protection snapshot is immutable'
            using errcode = '23514';
    end if;
    return new;
end $$;
