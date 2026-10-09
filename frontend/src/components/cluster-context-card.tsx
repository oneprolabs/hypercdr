import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { ChevronDown } from 'lucide-react';
import type { Cluster } from '../features/clusters/types';

function agentReadiness(cluster: Cluster) {
  if (cluster.connectionStatus !== 'online') {
    return { label: 'Offline', className: 'text-slate-500' };
  }
  if (cluster.status === 'healthy') {
    return { label: 'Ready', className: 'text-emerald-600' };
  }
  if (cluster.status === 'syncing') {
    return { label: 'Syncing', className: 'text-blue-600' };
  }
  return { label: 'Degraded', className: 'text-amber-600' };
}

function DashboardClusterIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="2" y="2" width="20" height="8" rx="2" />
      <rect x="2" y="14" width="20" height="8" rx="2" />
      <path d="M6 6h.01" />
      <path d="M6 18h.01" />
    </svg>
  );
}

export default function ClusterContextCard(props: {
  cluster: Cluster | null;
  clusters: Cluster[];
  defaultClusterId: string | null;
  pickerOpen: boolean;
  compact?: boolean;
  setPickerOpen: (open: boolean) => void;
  setSelectedCluster: (cluster: Cluster) => void;
  setDefaultCluster: (cluster: Cluster, event?: React.MouseEvent) => void;
  openClusters: () => void;
}) {
  const { cluster, clusters, defaultClusterId, pickerOpen, compact, setPickerOpen, setSelectedCluster, setDefaultCluster, openClusters } = props;
  const isClusterOffline = Boolean(cluster && cluster.connectionStatus !== 'online');
  const clusterMeta = cluster
    ? isClusterOffline
      ? `Agent offline. Reconnecting... · ${cluster.version}`
      : `${agentReadiness(cluster).label} · ${cluster.version} · ${cluster.applications} namespaces`
    : 'Select a cluster to activate the DR workspace';
  return (
    <div className={`relative hbdr-cluster-context-wrap ${pickerOpen ? 'z-[110]' : ''}`}>
      <button
        type="button"
        onClick={() => setPickerOpen(!pickerOpen)}
        className={`hbdr-cluster-context ${compact ? 'hbdr-cluster-context-compact' : ''} ${cluster ? 'hbdr-cluster-context-active' : 'hbdr-cluster-context-empty'} ${isClusterOffline ? 'hbdr-cluster-context-offline' : ''}`}
      >
        <div className="hbdr-cluster-context-icon"><DashboardClusterIcon /></div>
        <div className="min-w-0 flex-1 text-left">
          <p className="hbdr-cluster-context-kicker">DR Workspace</p>
          <h4 className="hbdr-cluster-context-title">{cluster ? cluster.name : 'No default cluster selected'}</h4>
          <p className="hbdr-cluster-context-meta">
            {clusterMeta}
          </p>
        </div>
        {isClusterOffline && <span className="hbdr-cluster-offline-pill">Offline</span>}
        {cluster?.id === defaultClusterId && <span className="hbdr-cluster-default-pill">Default</span>}
        <ChevronDown size={14} className={`hbdr-cluster-context-chevron ${pickerOpen ? 'rotate-180' : ''}`} />
      </button>
      <AnimatePresence>
        {pickerOpen && (
          <>
            <div className="fixed inset-0 z-[90]" onClick={() => setPickerOpen(false)} />
            <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: 8 }} className="hbdr-cluster-picker">
              <div className="hbdr-cluster-picker-list">
                {clusters.map(item => {
                  const itemOffline = item.connectionStatus !== 'online';
                  return (
                  <div key={item.id} className={`hbdr-cluster-picker-row ${cluster?.id === item.id ? 'hbdr-cluster-picker-row-active' : ''} ${itemOffline ? 'hbdr-cluster-picker-row-offline' : ''}`}>
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedCluster(item);
                        setPickerOpen(false);
                      }}
                      className="min-w-0 flex-1 text-left"
                    >
                      <p className="hbdr-cluster-picker-name"><span className="hbdr-cluster-picker-status-dot" />{item.name}</p>
                      <p className="hbdr-cluster-picker-meta">{itemOffline ? 'Agent offline. Reconnecting...' : `${agentReadiness(item).label} · ${item.version} · ${item.applications} namespaces`}</p>
                    </button>
                    {itemOffline && <span className="hbdr-cluster-picker-offline-pill">Offline</span>}
                    <button type="button" onClick={(event) => setDefaultCluster(item, event)} className={`hbdr-cluster-picker-default ${item.id === defaultClusterId ? 'hbdr-cluster-picker-default-active' : ''}`}>
                      {item.id === defaultClusterId ? 'Default' : 'Set Default'}
                    </button>
                  </div>
                  );
                })}
              </div>
              <div className="hbdr-cluster-picker-footer">
                <button
                  type="button"
                  onClick={() => {
                    setPickerOpen(false);
                    openClusters();
                  }}
                  className="hbdr-cluster-picker-open"
                >
                  Go to cluster
                </button>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

