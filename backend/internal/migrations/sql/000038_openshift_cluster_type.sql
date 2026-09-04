alter table agent_tokens drop constraint if exists agent_tokens_cluster_type_check;
alter table agent_tokens add constraint agent_tokens_cluster_type_check
  check (cluster_type in ('native-kubernetes', 'huaweicloud-cce', 'openshift'));

alter table clusters drop constraint if exists clusters_cluster_type_check;
alter table clusters add constraint clusters_cluster_type_check
  check (cluster_type in ('native-kubernetes', 'huaweicloud-cce', 'openshift'));
