-- Restore-point labels are rendered dynamically from task_created_at in the
-- timezone selected by the current user. Keep the legacy column only for
-- schema compatibility and deliberately leave it empty.
update restore_points
set display_name = ''
where display_name <> '';
