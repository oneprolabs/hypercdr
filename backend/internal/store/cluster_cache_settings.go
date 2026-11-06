package store

import "database/sql"

func (s *PostgresStore) GetClusterCacheSettings(clusterID string) (ClusterCacheSettings, error) {
	settings := ClusterCacheSettings{Enabled: true, MaxGiB: 5}
	err := s.db.QueryRow(`select enabled,max_gib from cluster_cache_settings where cluster_id=$1`, clusterID).Scan(&settings.Enabled, &settings.MaxGiB)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	return settings, err
}

func (s *PostgresStore) SaveClusterCacheSettings(clusterID string, settings ClusterCacheSettings) error {
	_, err := s.db.Exec(`insert into cluster_cache_settings(cluster_id,enabled,max_gib) values($1,$2,$3) on conflict(cluster_id) do update set enabled=excluded.enabled,max_gib=excluded.max_gib,updated_at=now()`, clusterID, settings.Enabled, settings.MaxGiB)
	return err
}
