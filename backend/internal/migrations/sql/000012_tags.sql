create table if not exists tags (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, name)
);

create table if not exists application_tags (
  application_id uuid not null references applications(id) on delete cascade,
  tag_id uuid not null references tags(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (application_id, tag_id)
);
