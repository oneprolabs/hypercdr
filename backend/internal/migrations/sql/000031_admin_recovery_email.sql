create table if not exists admin_recovery_email (
  user_id uuid primary key references users(id) on delete cascade,
  email text not null,
  verified_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create unique index if not exists admin_recovery_email_unique_idx on admin_recovery_email(lower(email));
