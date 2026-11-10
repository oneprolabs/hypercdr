create table if not exists cluster_cache_settings (
    cluster_id uuid primary key references clusters(id) on delete cascade,
    enabled boolean not null default true,
    max_gib integer not null default 5 check (max_gib between 1 and 1024),
    root_path text not null default '/var/lib/hypercdr/cache',
    index_enabled boolean not null default true,
    index_max_gib integer not null default 5 check (index_max_gib between 1 and 1024),
    drill_enabled boolean not null default false,
    drill_max_gib integer not null default 50 check (drill_max_gib between 1 and 4096),
    updated_at timestamptz not null default now()
);
alter table cluster_cache_settings add column if not exists root_path text not null default '/var/lib/hypercdr/cache';
alter table cluster_cache_settings add column if not exists index_enabled boolean not null default true;
alter table cluster_cache_settings add column if not exists index_max_gib integer not null default 5;
alter table cluster_cache_settings add column if not exists drill_enabled boolean not null default false;
alter table cluster_cache_settings add column if not exists drill_max_gib integer not null default 50;
