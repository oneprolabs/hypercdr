import { useEffect, useRef, useState } from 'react';
import './node-resources.css';
import { apiGet, apiPost } from '../../api/client';
import { formatDateTime } from '../../lib/date-time';
import { nodeResource, type NodeResourceKind } from './node-resources';
import type { Cluster, ClusterNode, ClusterNodeRow } from './types';

export function NodeResourceCell({ kind, node, offline }: { kind: NodeResourceKind; node: ClusterNodeRow; offline: boolean }) {
  const value = nodeResource(kind, node.capacity, node.resourceUsage);
  const timestamp = Date.parse(value.observedAt || '');
  const stale = offline || !Number.isFinite(timestamp) || Date.now() - timestamp > 180_000;
  const explanation = kind === 'storage' ? 'Node root filesystem; excludes PVC and distributed storage capacity.' : kind === 'memory' ? 'Working set / physical memory capacity.' : 'CPU usage / physical CPU capacity.';
  const title = `${explanation}\n${value.observedAt ? `Collected: ${formatDateTime(value.observedAt)}` : 'No usage observation available.'}${stale ? '\nUsage may be stale or unavailable.' : ''}${node.resourceUsage?.error ? `\n${node.resourceUsage.error}` : ''}`;
  return <div className="hbdr-node-resource-cell" title={title}>
    <span>{value.text}</span>
    <span className={stale ? 'is-stale' : value.percent !== undefined && value.percent >= 90 ? 'is-critical' : value.percent !== undefined && value.percent >= 80 ? 'is-warning' : ''}>{value.percent === undefined ? 'Usage unavailable' : `${value.percent.toFixed(0)}% used${stale ? ' · stale' : ''}`}</span>
  </div>;
}

export function NodeResourceRefresh({ cluster, onRefresh }: { cluster: Cluster; onRefresh: (nodes: ClusterNode[]) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const running = useRef(false);
  const mounted = useRef(true);
  const callback = useRef(onRefresh);
  callback.current = onRefresh;
  useEffect(() => {
    let active = true;
    mounted.current = true;
    const timer = setInterval(() => {
      if (document.hidden || running.current) return;
      void apiGet<{ items: Array<{ id: string; nodes?: ClusterNode[] }> }>('/api/v1/clusters').then(response => {
        const updated = response.items.find(item => item.id === cluster.id);
        if (active && updated?.nodes) callback.current(updated.nodes);
      }).catch(() => {});
    }, 60_000);
    return () => { active = false; mounted.current = false; clearInterval(timer); };
  }, [cluster.id]);
  async function refresh() {
    if (running.current) return;
    running.current = true; setBusy(true); setError('');
    try {
      const request = await apiPost<{ requestId: string }>(`/api/v1/clusters/${cluster.id}/inventory/request`, { scope: 'summary' });
      const deadline = Date.now() + 60_000;
      while (mounted.current && Date.now() < deadline) {
        const status = await apiGet<{ status: string; message?: string }>(`/api/v1/clusters/${cluster.id}/inventory/requests/${request.requestId}`);
        if (status.status !== 'pending') {
          if (status.status !== 'succeeded') throw new Error(status.message || 'Unable to refresh node resources.');
          const response = await apiGet<{ items: Array<{ id: string; nodes?: ClusterNode[] }> }>('/api/v1/clusters');
          const updated = response.items.find(item => item.id === cluster.id);
          if (mounted.current && updated?.nodes) callback.current(updated.nodes);
          return;
        }
        await new Promise(resolve => setTimeout(resolve, 1000));
      }
      if (mounted.current) throw new Error('Node resource refresh timed out. Previous values are retained.');
    } catch (failure) { if (mounted.current) setError(failure instanceof Error ? failure.message : 'Unable to refresh node resources.'); }
    finally { running.current = false; if (mounted.current) setBusy(false); }
  }
  return <div className="hbdr-node-resource-refresh">{error && <span role="alert">{error}</span>}<button type="button" disabled={busy || cluster.connectionStatus !== 'online'} onClick={() => void refresh()}>{busy ? 'Refreshing…' : 'Refresh resources'}</button></div>;
}
