truncate table protection_plans cascade;

alter table protection_plans
  drop column label_selector,
  drop column exclude_rules,
  add column included_resources jsonb not null default '[]'::jsonb,
  add column label_selector jsonb not null default '{}'::jsonb,
  add column excluded_resources jsonb not null default '[]'::jsonb;
