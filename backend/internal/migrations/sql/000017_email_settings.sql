create table if not exists email_settings (
  id boolean primary key default true check (id),
  enabled boolean not null default false,
  host text not null default '',
  port integer not null default 587 check (port between 1 and 65535),
  security text not null default 'starttls' check (security in ('none','starttls','tls')),
  username text not null default '',
  password_ciphertext text not null default '',
  sender_name text not null default 'HyperCDR',
  sender_email text not null default '',
  updated_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
