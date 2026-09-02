alter table platform_releases
  add column if not exists component_manifest jsonb not null default '{}'::jsonb;

comment on column platform_releases.component_manifest is
  'Immutable image and digest manifest for all platform and cluster components shipped by this HyperCDR release.';

drop table if exists component_releases;
