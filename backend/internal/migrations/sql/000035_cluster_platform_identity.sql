alter table agent_tokens add column if not exists cluster_type text not null default 'native-kubernetes';
alter table clusters add column if not exists cluster_type text not null default 'native-kubernetes';
alter table clusters add column if not exists cloud_provider text;
alter table clusters add column if not exists cloud_region text;
alter table clusters add column if not exists cloud_cluster_id text;

alter table agent_tokens drop constraint if exists agent_tokens_cluster_type_check;
alter table agent_tokens add constraint agent_tokens_cluster_type_check check (cluster_type in ('native-kubernetes', 'huaweicloud-cce'));
alter table clusters drop constraint if exists clusters_cluster_type_check;
alter table clusters add constraint clusters_cluster_type_check check (cluster_type in ('native-kubernetes', 'huaweicloud-cce'));
