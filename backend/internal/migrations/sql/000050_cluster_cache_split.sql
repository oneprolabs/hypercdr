alter table cluster_cache_settings add column if not exists root_path text not null default '/var/lib/hypercdr/cache';
alter table cluster_cache_settings add column if not exists index_path text not null default '';
alter table cluster_cache_settings add column if not exists drill_path text not null default '';
alter table cluster_cache_settings add column if not exists index_enabled boolean not null default true;
alter table cluster_cache_settings add column if not exists index_max_gib integer not null default 5;
alter table cluster_cache_settings add column if not exists drill_enabled boolean not null default false;
alter table cluster_cache_settings add column if not exists drill_max_gib integer not null default 50;
