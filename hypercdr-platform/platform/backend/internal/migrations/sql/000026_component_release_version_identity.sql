with ranked as (
  select id,
         row_number() over (
           partition by tenant_id, component, version
           order by case status when 'active' then 0 else 1 end, updated_at desc, created_at desc
         ) as position
  from component_releases
)
delete from component_releases
where id in (select id from ranked where position > 1);

create unique index if not exists idx_component_releases_version
  on component_releases (tenant_id, component, version);
