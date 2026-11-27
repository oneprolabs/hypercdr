alter table protection_plans
    add column data_protection jsonb not null default '{"mode":"live-filesystem"}'::jsonb;

alter table protection_plans add constraint protection_plan_data_protection_shape check (
    jsonb_typeof(data_protection) = 'object'
    and data_protection ? 'mode'
    and jsonb_typeof(data_protection->'mode') = 'string'
    and data_protection->>'mode' in ('live-filesystem', 'snapshot-crash-consistent', 'application-consistent')
    and case when data_protection->>'mode' = 'application-consistent'
        then coalesce(jsonb_typeof(data_protection->'group') = 'object', false)
        else not (data_protection ? 'group') end
);
