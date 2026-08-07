alter table protection_plans
  add column if not exists resource_selection jsonb not null
  default '{"mode":"all"}'::jsonb;

update protection_plans
set resource_selection = jsonb_build_object(
  'mode', case when jsonb_array_length(coalesce(included_resources, '[]'::jsonb)) > 0 then 'custom' else 'all' end,
  'namespaceScoped', coalesce(included_resources, '[]'::jsonb),
  'clusterScoped', '[]'::jsonb
)
where resource_selection = '{"mode":"all"}'::jsonb
  and scope_type = 'filtered';
