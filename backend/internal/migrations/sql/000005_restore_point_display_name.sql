alter table restore_points
  add column if not exists display_name text;

update restore_points
set display_name = 'RP-' || to_char(
  coalesce(completed_at, started_at, created_at),
  'YYYY-MM-DD HH24:MI:SS'
)
where display_name is null or btrim(display_name) = '';

alter table restore_points
  alter column display_name set not null;
