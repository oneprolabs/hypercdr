create table if not exists cluster_cache_settings (
    cluster_id uuid primary key references clusters(id) on delete cascade,
    enabled boolean not null default true,
    max_gib integer not null default 5 check (max_gib between 1 and 1024),
    updated_at timestamptz not null default now()
);
