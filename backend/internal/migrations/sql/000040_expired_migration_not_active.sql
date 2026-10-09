-- Expired sessions must not prevent a new Community migration.
-- Align the uniqueness constraint with expiry reconciliation and freeze checks.
drop index if exists community_one_active_migration;
create unique index community_one_active_migration
  on community_migration_sessions ((true))
  where state not in ('committed','rolled-back','failed','revoked','expired');
