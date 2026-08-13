import { AlertTriangle, ArrowLeft, ArrowRight, Boxes, CheckCircle2, GitBranch, LoaderCircle, XCircle } from 'lucide-react';
import type { Cluster } from './types';
import { orderClustersForTopology, type DRRelationship, type DRTopologyModel } from './dr-topology';

const statusMeta = {
  healthy: { label: 'Healthy', icon: CheckCircle2 },
  warning: { label: 'Warning', icon: AlertTriangle },
  configuring: { label: 'Configuring', icon: LoaderCircle },
  failed: { label: 'Failed', icon: XCircle },
};

export default function DRTopologyView({ clusters, model, selectedRelationshipId, selectedClusterId, onSelectRelationship, onSelectCluster }: {
  clusters: Cluster[];
  model: DRTopologyModel;
  selectedRelationshipId: string | null;
  selectedClusterId?: string | null;
  onSelectRelationship: (relationship: DRRelationship) => void;
  onSelectCluster: (clusterId: string) => void;
}) {
  const clusterById = new Map(clusters.map(cluster => [cluster.id, cluster]));
  const orderedClusters = orderClustersForTopology(clusters, model);
  const columns = Math.max(1, Math.min(clusters.length, 4));
  const positions = new Map(orderedClusters.map((cluster, index) => {
    const col = index % columns;
    const row = Math.floor(index / columns);
    const x = columns === 1 ? 50 : 22 + (56 * col / (columns - 1));
    const y = clusters.length <= columns ? 40 : 22 + row * 42;
    return [cluster.id, { x, y }];
  }));

  return (
    <section className="hbdr-dr-topology" aria-label="DR Topology">
      <div className="hbdr-dr-topology-head">
        <div><h3>DR Topology</h3><p>Namespace protection relationships between registered clusters</p></div>
        <div className="hbdr-dr-topology-legend"><span className="is-healthy" />Healthy<span className="is-warning" />Attention<span className="is-configuring" />Configuring</div>
      </div>
      <div className="hbdr-dr-topology-canvas" style={{ minHeight: clusters.length > 4 ? 310 : 220 }}>
        {model.relationships.length === 0 && <div className="hbdr-dr-topology-empty"><GitBranch size={25} /><strong>No DR relationships yet</strong><span>Configure namespace DR to connect clusters.</span></div>}
        <svg className="hbdr-dr-topology-lines" viewBox="0 0 1000 500" preserveAspectRatio="none" aria-hidden="true">
          <defs><marker id="dr-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth"><path className="hbdr-dr-arrow-head" d="M1,1 L7,4 L1,7" /></marker></defs>
          {model.relationships.map(relationship => {
            const source = positions.get(relationship.sourceClusterId);
            const target = positions.get(relationship.targetClusterId);
            if (!source || !target) return null;
            const reverse = model.relationships.some(item => item.sourceClusterId === relationship.targetClusterId && item.targetClusterId === relationship.sourceClusterId);
            const offset = reverse ? (relationship.sourceClusterId.localeCompare(relationship.targetClusterId) < 0 ? -18 : 18) : 0;
            const sourceX = source.x * 10, sourceY = source.y * 5, targetX = target.x * 10, targetY = target.y * 5;
            const dx = targetX - sourceX, dy = targetY - sourceY;
            const distance = Math.max(1, Math.hypot(dx, dy));
            const endpointInset = Math.min(96, distance * 0.22);
            const x1 = sourceX + dx / distance * endpointInset;
            const y1 = sourceY + dy / distance * endpointInset;
            const x2 = targetX - dx / distance * endpointInset;
            const y2 = targetY - dy / distance * endpointInset;
            const mx = (x1 + x2) / 2, my = (y1 + y2) / 2 + offset;
            return <path key={relationship.id} className={`is-${relationship.status} ${selectedRelationshipId === relationship.id ? 'is-selected' : ''}`} d={`M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`} markerEnd="url(#dr-arrow)" />;
          })}
        </svg>
        {model.relationships.map(relationship => {
          const source = positions.get(relationship.sourceClusterId);
          const target = positions.get(relationship.targetClusterId);
          if (!source || !target) return null;
          const reverse = model.relationships.some(item => item.sourceClusterId === relationship.targetClusterId && item.targetClusterId === relationship.sourceClusterId);
          const offset = reverse ? (relationship.sourceClusterId.localeCompare(relationship.targetClusterId) < 0 ? -5 : 5) : 0;
          const meta = statusMeta[relationship.status];
          const Icon = meta.icon;
          const DirectionArrow = target.x < source.x ? ArrowLeft : ArrowRight;
          return <button key={relationship.id} type="button" className={`hbdr-dr-edge-label is-${relationship.status} ${selectedRelationshipId === relationship.id ? 'is-selected' : ''}`} style={{ left: `${(source.x + target.x) / 2}%`, top: `${(source.y + target.y) / 2 + offset}%` }} onClick={() => onSelectRelationship(relationship)} title={`${clusterById.get(relationship.sourceClusterId)?.name} to ${clusterById.get(relationship.targetClusterId)?.name}: ${relationship.appIds.length} namespaces`} aria-label={`${clusterById.get(relationship.sourceClusterId)?.name} to ${clusterById.get(relationship.targetClusterId)?.name}, ${relationship.appIds.length} ${relationship.appIds.length === 1 ? 'namespace' : 'namespaces'}`}><Icon size={12} /><DirectionArrow size={12} />{relationship.appIds.length} {relationship.appIds.length === 1 ? 'namespace' : 'namespaces'}</button>;
        })}
        {clusters.map(cluster => {
          const position = positions.get(cluster.id)!;
          const summary = model.summaries[cluster.id];
          return <button key={cluster.id} type="button" className={`hbdr-dr-node ${cluster.connectionStatus === 'online' ? 'is-online' : 'is-offline'} ${selectedClusterId === cluster.id ? 'is-selected' : ''}`} style={{ left: `${position.x}%`, top: `${position.y}%` }} onClick={() => onSelectCluster(cluster.id)}>
            <span className="hbdr-dr-node-title"><i />{cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : cluster.name}</span>
            <span>{cluster.applications} namespaces · {summary?.protectedApps || 0} protected</span>
          </button>;
        })}
      </div>
      {selectedRelationshipId && (() => {
        const relationship = model.relationships.find(item => item.id === selectedRelationshipId);
        if (!relationship) return null;
        const meta = statusMeta[relationship.status];
        return <div className="hbdr-dr-relationship-detail">
          <div className="hbdr-dr-relationship-summary"><strong>{clusterById.get(relationship.sourceClusterId)?.name}<ArrowRight size={14} />{clusterById.get(relationship.targetClusterId)?.name}</strong><span>{meta.label} · {relationship.appIds.length} protected {relationship.appIds.length === 1 ? 'namespace' : 'namespaces'}</span></div>
          <div className="hbdr-dr-relationship-namespace-list"><strong>Protected namespaces</strong><div className="hbdr-dr-relationship-namespaces">{relationship.appNames.map(name => <div key={name} title={name}><Boxes size={13} /><span>{name}</span></div>)}</div></div>
        </div>;
      })()}
    </section>
  );
}
