alter table tenant_subscriptions drop constraint tenant_subscriptions_max_clusters_check;
alter table tenant_subscriptions add constraint tenant_subscriptions_max_clusters_check check (max_clusters = 0 or max_clusters between 2 and 1000000);
