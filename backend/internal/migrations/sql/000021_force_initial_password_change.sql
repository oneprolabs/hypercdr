alter table users
  add column if not exists must_change_password boolean not null default false;

update users
set must_change_password = true,
    updated_at = now()
where lower(email) = 'admin'
  and is_system_admin = true;
