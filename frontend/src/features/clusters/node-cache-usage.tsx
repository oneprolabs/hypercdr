import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Info, RefreshCw } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import { formatDateTime } from '../../lib/date-time';

export type NodeCacheMeasurement = { enabled?: boolean; path?: string; maxBytes?: number; usedBytes?: number; observedAt?: string; error?: string };
export type NodeCacheReport = { phase?: string; error?: string; observedAt?: string; nodes?: Array<{ name: string; index: NodeCacheMeasurement; drill: NodeCacheMeasurement }> };
type CacheCluster = { id: string; nodeCacheUsageSupported?: boolean; connectionStatus?: string; cacheUsage?: NodeCacheReport };
const size = (bytes?: number) => {
  if (bytes === undefined) return '—';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)) - 1, units.length - 1);
  return `${(bytes / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`;
};

export function NodeCacheCell({ cache, stale, label = 'Cache' }: { cache?: NodeCacheMeasurement; stale?: boolean; label?: string }) {
  const tooltipId = useId();
  const anchor = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<{ left: number; top: number; above: boolean }>();
  const showDetails = () => {
    const bounds = anchor.current?.getBoundingClientRect();
    if (!bounds) return;
    const above = window.innerHeight - bounds.bottom < 190;
    setPosition({ left: Math.max(8, Math.min(bounds.left, window.innerWidth - 368)), top: above ? bounds.top - 8 : bounds.bottom + 8, above });
  };
  useEffect(() => {
    if (!position) return;
    const close = () => setPosition(undefined);
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    return () => {
      window.removeEventListener('scroll', close, true);
      window.removeEventListener('resize', close);
    };
  }, [position]);
  if (!cache || (cache.enabled === undefined && cache.usedBytes === undefined && !cache.path)) return <span title={cache?.error || 'No node cache observation is available'}>—</span>;
  if (cache.enabled === false) return <span className="hbdr-node-cache-disabled">Disabled</span>;
  const status = cache.enabled === undefined ? 'Unknown' : cache.enabled ? 'Enabled' : 'Disabled';
  return <div ref={anchor} className="hbdr-node-cache-cell" onMouseEnter={showDetails} onMouseLeave={() => setPosition(undefined)}>
    <span className={cache.enabled ? 'hbdr-node-cache-enabled' : 'hbdr-node-cache-disabled'}>{status}</span>
    <span aria-hidden="true">·</span>
    <span>{size(cache.usedBytes)} / {size(cache.maxBytes)}</span>
    <button type="button" className="hbdr-node-cache-info" aria-label={`${label} details`} aria-describedby={position ? tooltipId : undefined} onFocus={showDetails} onBlur={() => setPosition(undefined)} onClick={showDetails} onKeyDown={event => { if (event.key === 'Escape') setPosition(undefined); }}><Info size={13} /></button>
    {position && createPortal(<div id={tooltipId} role="tooltip" className="hbdr-node-cache-tooltip" style={{ left: position.left, top: position.top, transform: position.above ? 'translateY(-100%)' : undefined }}>
      <strong>{label}</strong>
      <dl><dt>Status</dt><dd>{status}</dd><dt>Used</dt><dd>{size(cache.usedBytes)}</dd><dt>Cache size per node</dt><dd>{size(cache.maxBytes)}</dd><dt>Directory</dt><dd className="hbdr-node-cache-tooltip-path">{cache.path || '—'}</dd></dl>
      <p title={cache.error || (stale ? 'Showing the last successful observation' : undefined)}>Last updated: {cache.observedAt ? formatDateTime(cache.observedAt) : '—'}</p>
    </div>, document.body)}
  </div>;
}

export function NodeCacheRefresh({ clusterId, onReport }: { clusterId: string; onReport: (report: NodeCacheReport | undefined, offline: boolean) => void }) {
  const [cluster, setCluster] = useState<CacheCluster>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const running = useRef(false);
  const generation = useRef(0);
  const callback = useRef(onReport);
  callback.current = onReport;
  useEffect(() => {
    const current = ++generation.current;
    setCluster(undefined);
    setBusy(false);
    setError('');
    running.current = false;
    callback.current(undefined, false);
    void apiGet<{ items: CacheCluster[] }>('/api/v1/clusters').then(response => {
      if (generation.current !== current) return;
      const value = response.items.find(item => item.id === clusterId);
      setCluster(value);
      callback.current(value?.cacheUsage, value?.connectionStatus !== 'online');
    }).catch(() => { if (generation.current === current) setError('Unable to load cache measurements.'); });
    return () => { generation.current++; };
  }, [clusterId]);
  const refresh = async () => {
    if (running.current) return;
    running.current = true;
    const current = generation.current;
    setBusy(true);
    setError('');
    try {
      const request = await apiPost<{ requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, { scope: 'cache-usage' });
      const deadline = Date.now() + 7 * 60_000;
      while (generation.current === current && Date.now() < deadline) {
        const status = await apiGet<{ status: string; message?: string }>(`/api/v1/clusters/${clusterId}/inventory/requests/${request.requestId}`);
        if (status.status !== 'pending') {
          const response = await apiGet<{ items: CacheCluster[] }>('/api/v1/clusters');
          if (generation.current !== current) return;
          const value = response.items.find(item => item.id === clusterId);
          setCluster(value);
          callback.current(value?.cacheUsage, value?.connectionStatus !== 'online');
          if (status.status !== 'succeeded') throw new Error(status.message || 'Cache measurement failed. Previous values are retained.');
          return;
        }
        await new Promise(resolve => setTimeout(resolve, 2000));
      }
      if (generation.current === current) throw new Error('Cache measurement timed out. Previous values are retained.');
    } catch (failure) {
      if (generation.current === current) setError(failure instanceof Error ? failure.message : 'Unable to refresh cache usage.');
    } finally {
      if (generation.current === current) { running.current = false; setBusy(false); }
    }
  };
  return <div className="hbdr-node-cache-refresh">
    {error && <span role="alert">{error}</span>}
    <button type="button" aria-label="Refresh node cache usage" title={!cluster?.nodeCacheUsageSupported ? 'Upgrade the communication agent to collect cache usage' : 'Refresh cache usage'} disabled={busy || !cluster?.nodeCacheUsageSupported || cluster.connectionStatus !== 'online'} onClick={() => void refresh()}><RefreshCw size={14} className={busy ? 'animate-spin' : ''} /></button>
  </div>;
}
