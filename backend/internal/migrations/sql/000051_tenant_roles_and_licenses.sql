create table organization_roles (
 id uuid primary key,
 tenant_id uuid not null references resource_scopes(id) on delete cascade,
 name text not null check (length(btrim(name)) between 1 and 64),
 description text not null default '' check (length(description) <= 500),
 permissions jsonb not null default '[]'::jsonb check (jsonb_typeof(permissions) = 'array'),
 created_by uuid references users(id) on delete set null,
 created_at timestamptz not null default now(),
 updated_at timestamptz not null default now(),
 unique (tenant_id,id),
 check (lower(btrim(name)) not in ('administrator','operator','admin'))
);
create unique index organization_roles_name on organization_roles(tenant_id,lower(btrim(name)));

alter table users add column custom_role_id uuid;
alter table users add constraint users_custom_role_scope foreign key (tenant_id,custom_role_id) references organization_roles(tenant_id,id);
alter table users add constraint users_custom_role_member check (custom_role_id is null or (role='operator' and not is_system_admin));
create index users_custom_role on users(tenant_id,custom_role_id) where custom_role_id is not null;

create function revoke_deleted_member_install_tokens() returns trigger language plpgsql as $$
begin
 update agent_tokens set revoked_at=coalesce(revoked_at,now()) where created_by=old.id and used_at is null;
 return old;
end;
$$;
create trigger users_revoke_install_tokens before delete on users for each row execute function revoke_deleted_member_install_tokens();

alter table saas_onboarding_tokens add column custom_role_id uuid;
alter table saas_onboarding_tokens add constraint onboarding_custom_role_scope foreign key (tenant_id,custom_role_id) references organization_roles(tenant_id,id);
alter table saas_onboarding_tokens add constraint onboarding_custom_role_member check (custom_role_id is null or (kind='invitation' and role_id='operator'));

create table tenant_licenses (
 id uuid primary key,
 tenant_id uuid not null references resource_scopes(id) on delete cascade,
 revision bigint not null check (revision > 0),
 max_clusters integer not null check (max_clusters between 2 and 1000000),
 issued_at timestamptz not null,
 valid_from timestamptz not null,
 valid_until timestamptz not null check (valid_until > valid_from),
 key_id text not null,
 signed_license text not null,
 activated_by uuid references users(id) on delete set null,
 activated_at timestamptz not null default now(),
 unique (tenant_id,revision),
 unique (tenant_id,id)
);

create table tenant_entitlements (
 tenant_id uuid primary key references resource_scopes(id) on delete cascade,
 license_id uuid,
 updated_at timestamptz not null default now(),
 foreign key (tenant_id,license_id) references tenant_licenses(tenant_id,id)
);
insert into tenant_entitlements(tenant_id) select id from resource_scopes;

create function initialize_tenant_entitlement() returns trigger language plpgsql as $$
begin
 insert into tenant_entitlements(tenant_id) values(new.id);
 return new;
end;
$$;
create trigger resource_scope_entitlement after insert on resource_scopes for each row execute function initialize_tenant_entitlement();
