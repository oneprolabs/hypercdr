update restore_points r
set size_metrics_v2 = t.payload->'sizeMetricsV2',
    metadata = coalesce(r.metadata, '{}'::jsonb) || jsonb_build_object('sizeMetricsV2', t.payload->'sizeMetricsV2')
from tasks t
where t.id = r.backup_task_id
  and (r.size_metrics_v2 is null or r.size_metrics_v2 in ('null'::jsonb, '{}'::jsonb))
  and jsonb_typeof(t.payload->'sizeMetricsV2') = 'object'
  and t.payload->'sizeMetricsV2' <> '{}'::jsonb;
