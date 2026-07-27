create table if not exists component_releases (
  id uuid primary key,
  tenant_id uuid not null references tenants(id),
  component text not null check (component in ('comm-agent', 'velero')),
  version text not null,
  image text not null,
  image_digest text not null,
  status text not null default 'candidate' check (status in ('candidate', 'active', 'retired')),
  release_notes text,
  published_by text,
  published_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (tenant_id, component, image_digest)
);

create unique index if not exists idx_component_releases_one_active
  on component_releases (tenant_id, component)
  where status = 'active';

create index if not exists idx_component_releases_component_created
  on component_releases (tenant_id, component, created_at desc);
