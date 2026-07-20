update protection_plans
set scope_type = 'all'
where scope_type not in ('all', 'filtered');

alter table protection_plans
  add constraint protection_plans_scope_type_check
  check (scope_type in ('all', 'filtered'));
