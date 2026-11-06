create unique index if not exists tasks_plan_capacity_active_cluster
on tasks(cluster_id) where type='plan-capacity' and status in ('queued','dispatched','accepted','running');

create unique index if not exists tasks_plan_capacity_daily_cluster
on tasks(cluster_id,(payload->>'scheduleDay'))
where type='plan-capacity' and payload->>'scheduleDay' is not null;
