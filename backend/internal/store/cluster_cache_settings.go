package store

import "database/sql"

func (s *PostgresStore) GetClusterCacheSettings(clusterID string) (ClusterCacheSettings, error) {
	settings := ClusterCacheSettings{RootPath: "/var/lib/hypercdr/cache", IndexEnabled: true, IndexMaxGiB: 5, DrillMaxGiB: 50}
	err := s.db.QueryRow(`select root_path,index_path,drill_path,index_enabled,index_max_gib,drill_enabled,drill_max_gib from cluster_cache_settings where cluster_id=$1`, clusterID).Scan(&settings.RootPath, &settings.IndexPath, &settings.DrillPath, &settings.IndexEnabled, &settings.IndexMaxGiB, &settings.DrillEnabled, &settings.DrillMaxGiB)
	if err == sql.ErrNoRows {
		settings.IndexPath = settings.RootPath + "/index"
		settings.DrillPath = settings.RootPath + "/drill-data"
		return settings, nil
	}
	if settings.IndexPath == "" {
		settings.IndexPath = settings.RootPath + "/index"
	}
	if settings.DrillPath == "" {
		settings.DrillPath = settings.RootPath + "/drill-data"
	}
	return settings, err
}

func (s *PostgresStore) SaveClusterCacheSettings(clusterID string, settings ClusterCacheSettings) error {
	_, err := s.db.Exec(`insert into cluster_cache_settings(cluster_id,root_path,index_path,drill_path,index_enabled,index_max_gib,drill_enabled,drill_max_gib) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict(cluster_id) do update set root_path=excluded.root_path,index_path=excluded.index_path,drill_path=excluded.drill_path,index_enabled=excluded.index_enabled,index_max_gib=excluded.index_max_gib,drill_enabled=excluded.drill_enabled,drill_max_gib=excluded.drill_max_gib,updated_at=now()`, clusterID, settings.RootPath, settings.IndexPath, settings.DrillPath, settings.IndexEnabled, settings.IndexMaxGiB, settings.DrillEnabled, settings.DrillMaxGiB)
	return err
}
