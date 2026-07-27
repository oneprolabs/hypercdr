-- Establish explicit tenant administration and globally unambiguous login identities.

alter table users
  add column if not exists is_system_admin boolean not null default false;

update users
set is_system_admin = true,
    role = 'admin',
    status = 'active'
where lower(email) = 'admin';

update tenants
set name = 'Default Tenant',
    status = case when status = 'pending_activation' then 'active' else status end,
    updated_at = now()
where id = '00000000-0000-0000-0000-000000000001';

create unique index if not exists idx_users_email_global_unique
  on users (lower(email));

create index if not exists idx_users_tenant_status
  on users (tenant_id, status);

create index if not exists idx_tenants_status_name
  on tenants (status, lower(name));
