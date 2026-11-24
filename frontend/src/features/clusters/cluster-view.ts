export type ClusterView = 'cards' | 'list';
export const clusterViewKey = 'hypercdr.clusters.view';

export function readClusterView(storage: Pick<Storage, 'getItem'>): ClusterView {
  try { return storage.getItem(clusterViewKey) === 'list' ? 'list' : 'cards'; }
  catch { return 'cards'; }
}

export function saveClusterView(storage: Pick<Storage, 'setItem'>, view: ClusterView): void {
  try { storage.setItem(clusterViewKey, view); } catch { return; }
}

export function filterClusters<ClusterRow extends { name: string; id: string; connectionStatus?: string }>(clusters: ClusterRow[], query: string, connection: string): ClusterRow[] {
  const search = query.trim().toLowerCase();
  return clusters.filter(cluster => (!search || cluster.name.toLowerCase().includes(search) || cluster.id.toLowerCase().includes(search))
    && (connection === 'all' || (cluster.connectionStatus === 'online' ? 'online' : 'offline') === connection));
}
