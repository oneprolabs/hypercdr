create table if not exists smtp_configurations (
  id uuid primary key,
  name text not null,
  is_default boolean not null default false,
  enabled boolean not null default true,
  host text not null,
  port integer not null default 587 check (port between 1 and 65535),
  security text not null default 'starttls' check (security in ('none','starttls','tls')),
  username text not null default '',
  password_ciphertext text not null default '',
  sender_name text not null default 'HyperCDR',
  sender_email text not null,
  last_test_status text not null default 'not_tested' check (last_test_status in ('not_tested','succeeded','failed')),
  last_tested_at timestamptz,
  last_test_error text not null default '',
  updated_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create unique index if not exists smtp_configurations_one_default
  on smtp_configurations (is_default) where is_default;
create unique index if not exists smtp_configurations_name_unique
  on smtp_configurations (lower(name));

insert into smtp_configurations(
  id,name,is_default,enabled,host,port,security,username,password_ciphertext,
  sender_name,sender_email,updated_by,created_at,updated_at
)
select
  '00000000-0000-4000-8000-000000000030'::uuid,
  'Default SMTP',true,enabled,host,port,security,username,password_ciphertext,
  sender_name,sender_email,updated_by,created_at,updated_at
from email_settings
where id=true and host<>''
on conflict do nothing;
