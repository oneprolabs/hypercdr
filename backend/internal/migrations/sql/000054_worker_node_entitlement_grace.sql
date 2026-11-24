create table tenant_worker_entitlement_state (
 tenant_id uuid primary key references resource_scopes(id) on delete cascade,
 overage_started_at timestamptz,
 updated_at timestamptz not null default now()
);
insert into tenant_worker_entitlement_state(tenant_id)
select id from resource_scopes
on conflict (tenant_id) do nothing;

create function initialize_tenant_worker_entitlement_state() returns trigger language plpgsql as $$
begin
 insert into tenant_worker_entitlement_state(tenant_id) values(new.id) on conflict do nothing;
 return new;
end;
$$;
create trigger resource_scope_worker_entitlement_state after insert on resource_scopes for each row execute function initialize_tenant_worker_entitlement_state();
