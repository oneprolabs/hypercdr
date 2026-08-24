create index if not exists tasks_tenant_status_type_created_idx
    on tasks(tenant_id, status, type, created_at desc);

create index if not exists restore_points_tenant_source_created_idx
    on restore_points(tenant_id, source_cluster_id, created_at desc);

create index if not exists applications_cluster_namespace_idx
    on applications(cluster_id, namespace);
