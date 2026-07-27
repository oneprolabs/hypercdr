update restore_points rp
set display_name = 'RP-' || to_char(t.created_at at time zone 'Asia/Shanghai', 'YYYY-MM-DD HH24:MI:SS'),
    metadata = coalesce(rp.metadata, '{}'::jsonb) - 'displayNameAt'
from tasks t
where t.id::text = rp.metadata->>'backupTaskId';
