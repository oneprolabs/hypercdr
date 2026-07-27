update restore_points rp
set display_name = 'RP-' || to_char(t.created_at, 'YYYY-MM-DD HH24:MI:SS'),
    metadata = jsonb_set(
      coalesce(rp.metadata, '{}'::jsonb),
      '{displayNameAt}',
      to_jsonb(t.created_at),
      true
    )
from tasks t
where t.id::text = rp.metadata->>'backupTaskId';
