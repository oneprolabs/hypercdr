-- Separate the core DR isolation key from Enterprise tenant product metadata.
-- PostgreSQL keeps existing foreign keys attached when the referenced table is
-- renamed, so all core resources retain their current UUID scope without data
-- movement or relationship loss.
alter table tenants rename to resource_scopes;

comment on table resource_scopes is
  'Opaque core DR resource isolation scopes. Community uses only the built-in default scope.';
