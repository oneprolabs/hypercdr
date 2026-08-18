import { useCallback, useEffect, useMemo, useState } from 'react';
import { Boxes, Search, Server, Terminal, Upload, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiGet, apiHeaders, apiPost, ensureApiResponse } from '../../api/client';
import { readStoredAuthSession } from '../../auth/session';
import type { ApiLoginResponse } from '../../auth/types';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatLocalDateTime, getUserTimeZone } from '../../lib/date-time';

type ApiList<T> = { items: T[] };
type ApiCluster = { id: string; tenantId: string; name: string; connectionStatus: string };
type ApiTenant = { id: string; name: string };
type ApiDiagnosticLog = {
  id: string; tenantId?: string; scope: 'tenant' | 'system'; level: 'debug' | 'info' | 'warning' | 'error';
  component: string; operation?: string; message: string; clusterId?: string; taskId?: string;
  commandId?: string; requestId?: string; errorCode?: string; status?: string; durationMs?: number;
  details?: Record<string, unknown>; eventAt: string; createdAt: string;
};

const listItems = <T,>(response: ApiList<T>) => response.items || [];
const initialFrom = () => {
  const value = new Date(Date.now() - 60 * 60 * 1000);
  return new Date(value.getTime() - value.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
};

export default function DiagnosticLogsPage({ currentUser, toast, advancedTenancy = false, initialTaskId = '' }: {
  currentUser: ApiLoginResponse['user']; toast: (message: string) => void; advancedTenancy?: boolean; initialTaskId?: string;
}) {
  const [logs, setLogs] = useState<ApiDiagnosticLog[]>([]);
  const [clusters, setClusters] = useState<ApiCluster[]>([]);
  const [tenants, setTenants] = useState<ApiTenant[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [selected, setSelected] = useState<ApiDiagnosticLog | null>(null);
  const [collectionStatus, setCollectionStatus] = useState<{ state: 'idle' | 'collecting' | 'completed' | 'partial' | 'failed'; message: string }>({ state: 'idle', message: '' });
  const [source, setSource] = useState<'platform' | 'cluster'>('platform');
  const [scope, setScope] = useState(currentUser.systemAdmin && advancedTenancy ? '' : 'tenant');
  const [tenantId, setTenantId] = useState('');
  const [clusterId, setClusterId] = useState('');
  const [level, setLevel] = useState('');
  const [platformComponent, setPlatformComponent] = useState('');
  const [clusterComponent, setClusterComponent] = useState('');
  const [query, setQuery] = useState(initialTaskId);
  const [from, setFrom] = useState(initialFrom);
  const [to, setTo] = useState('');
  const component = source === 'platform' ? platformComponent : clusterComponent;
  const selectedCluster = clusters.find(item => item.id === clusterId) || null;
  const tenantName = (id?: string) => tenants.find(item => item.id === id)?.name || id?.slice(0, 8) || 'System';
  const visibleClusters = clusters.filter(cluster => !advancedTenancy || !currentUser.systemAdmin || !tenantId || cluster.tenantId === tenantId);

  const params = useCallback((exporting = false) => {
    const values = new URLSearchParams({ source });
    if (source === 'platform' && scope) values.set('scope', scope);
    if (advancedTenancy && tenantId) values.set('tenantId', tenantId);
    if (source === 'cluster' && clusterId) values.set('clusterId', clusterId);
    if (level) values.set('level', level);
    if (component) values.set('component', component);
    if (query.trim()) values.set('q', query.trim());
    if (from) values.set('from', new Date(from).toISOString());
    if (to) values.set('to', new Date(to).toISOString());
    values.set('limit', exporting ? '5000' : '500');
    if (exporting) values.set('timezone', getUserTimeZone());
    return values;
  }, [advancedTenancy, clusterId, component, from, level, query, scope, source, tenantId, to]);

  const load = useCallback(async () => {
    setLoading(true); setLoadError('');
    try {
      const response = await apiGet<ApiList<ApiDiagnosticLog>>(`/api/v1/diagnostic-logs?${params()}`);
      setLogs(listItems(response));
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Diagnostic logs could not be loaded';
      setLoadError(message); toast(message);
    } finally { setLoading(false); }
  }, [params, toast]);

  useEffect(() => {
    void apiGet<ApiList<ApiCluster>>('/api/v1/diagnostic-log-sources').then(response => setClusters(listItems(response))).catch(() => setClusters([]));
    if (advancedTenancy && currentUser.systemAdmin) {
      void apiGet<ApiList<ApiTenant>>('/api/v1/tenants').then(response => setTenants(listItems(response))).catch(() => setTenants([]));
    }
  }, [advancedTenancy, currentUser.systemAdmin]);
  useEffect(() => { void load(); }, [load]);

  const searchLogs = async () => {
    if (source === 'platform') { await load(); return; }
    if (!clusterId || !['comm-agent', 'velero', 'node-agent'].includes(component)) {
      toast('Select a cluster and component before searching'); return;
    }
    setLoading(true);
    setCollectionStatus({ state: 'collecting', message: 'Checking stored coverage and collecting only the missing interval.' });
    try {
      const result = await apiPost<{ collected: boolean; count?: number; coverageComplete: boolean; message?: string; coverage?: { coveredFrom: string; coveredTo: string } }>(`/api/v1/clusters/${clusterId}/logs/search`, {
        component, from: from ? new Date(from).toISOString() : new Date(Date.now() - 60 * 60 * 1000).toISOString(), to: to ? new Date(to).toISOString() : new Date().toISOString(),
      });
      const coverage = result.coverage ? `${formatLocalDateTime(result.coverage.coveredFrom)} – ${formatLocalDateTime(result.coverage.coveredTo)}` : '';
      setCollectionStatus(result.coverageComplete
        ? { state: 'completed', message: result.collected ? `Missing logs collected${typeof result.count === 'number' ? ` · ${result.count} received` : ''}. Coverage: ${coverage}` : `Using retained logs. Coverage: ${coverage}` }
        : { state: 'partial', message: `${result.message || 'Only part of the requested interval is available.'}${coverage ? ` Coverage: ${coverage}` : ''}` });
      await load();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Logs could not be prepared';
      setCollectionStatus({ state: 'failed', message }); setLoading(false); toast(message);
    }
  };

  const exportLogs = async () => {
    try {
      const token = readStoredAuthSession()?.session.token || '';
      const path = `/api/v1/diagnostic-logs/export?${params(true)}`;
      const response = await ensureApiResponse(await fetch(path, { headers: apiHeaders(false, token) }), path, token);
      const blob = await response.blob(); const url = URL.createObjectURL(blob); const anchor = document.createElement('a');
      anchor.href = url; anchor.download = `hypercdr-${source}-logs-${new Date().toISOString().slice(0, 10)}.log`; anchor.click(); URL.revokeObjectURL(url);
      toast(`${source === 'platform' ? 'Platform' : 'Cluster'} log export is ready`);
    } catch (error) { toast(error instanceof Error ? error.message : 'Log export failed'); }
  };

  const columns = useMemo<HyperTableColumn<ApiDiagnosticLog>[]>(() => [
    { id: 'eventAt', header: 'Time', accessorFn: row => row.eventAt, size: 180, minSize: 170, cell: info => <span>{formatLocalDateTime(info.row.original.eventAt)}</span>, meta: { kind: 'secondary', title: row => formatLocalDateTime(row.eventAt) } },
    { id: 'level', header: 'Level', accessorFn: row => row.level, size: 90, minSize: 80, cell: info => <span className={`rounded px-2 py-1 text-[10px] font-medium ${info.row.original.level === 'error' ? 'bg-rose-50 text-rose-700' : info.row.original.level === 'warning' ? 'bg-amber-50 text-amber-700' : 'bg-slate-100 text-slate-600'}`}>{info.row.original.level.toUpperCase()}</span>, meta: { kind: 'status' } },
    { id: 'component', header: 'Component', accessorFn: row => row.component, size: 140, minSize: 120, cell: info => info.row.original.component, meta: { kind: 'secondary' } },
    ...(advancedTenancy && currentUser.systemAdmin ? [{ id: 'tenant', header: 'Tenant', accessorFn: (row: ApiDiagnosticLog) => tenantName(row.tenantId), size: 150, minSize: 120, cell: (info: any) => tenantName(info.row.original.tenantId), meta: { kind: 'secondary' } } as HyperTableColumn<ApiDiagnosticLog>] : []),
    { id: 'message', header: 'Message', accessorFn: row => row.message, size: 460, minSize: 260, cell: info => <div className="min-w-0"><p className="truncate">{info.row.original.message}</p><p className="mt-1 truncate text-[10px] font-normal text-slate-400">{info.row.original.operation || info.row.original.errorCode || ''}</p></div>, meta: { kind: 'primary', title: row => row.message } },
    { id: 'correlation', header: 'Task / Request', accessorFn: row => row.taskId || row.requestId || '', size: 170, minSize: 140, cell: info => (info.row.original.taskId || info.row.original.requestId || '-').slice(0, 13), meta: { kind: 'code' } },
  ], [advancedTenancy, currentUser.systemAdmin, tenants]);

  return <motion.div key="diagnostic-logs" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600"><Terminal size={18} /></div><div><h3 className="text-sm font-black text-slate-900">Diagnostic Logs</h3><p className="mt-1 text-[11px] text-slate-400">Trace platform requests, task stages, and managed-cluster component failures.</p></div></div></div>
    <section className="hbdr-dr-table-card overflow-hidden">
      <div className="flex border-b border-slate-200 bg-white px-4 pt-3" role="tablist" aria-label="Log source">
        <button type="button" role="tab" aria-selected={source === 'platform'} onClick={() => { setSource('platform'); setSelected(null); setCollectionStatus({ state: 'idle', message: '' }); }} className={`border-b-2 px-5 py-3 text-xs font-bold ${source === 'platform' ? 'border-blue-600 text-blue-700' : 'border-transparent text-slate-500'}`}><Server size={15} className="mr-2 inline" />Platform Logs</button>
        <button type="button" role="tab" aria-selected={source === 'cluster'} onClick={() => { setSource('cluster'); setSelected(null); }} className={`border-b-2 px-5 py-3 text-xs font-bold ${source === 'cluster' ? 'border-blue-600 text-blue-700' : 'border-transparent text-slate-500'}`}><Boxes size={15} className="mr-2 inline" />Cluster Logs</button>
      </div>
      <div className="border-b border-slate-100 bg-slate-50/60 p-4"><div className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
        {source === 'platform' && advancedTenancy && currentUser.systemAdmin && <label className="text-[10px] font-bold text-slate-500">Log Scope<select value={scope} onChange={event => { setScope(event.target.value); if (event.target.value === 'system') setTenantId(''); }} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs"><option value="">Tenant and system</option><option value="tenant">Tenant logs</option><option value="system">System logs</option></select></label>}
        {advancedTenancy && currentUser.systemAdmin && (source === 'cluster' || scope !== 'system') && <label className="text-[10px] font-bold text-slate-500">Tenant<select value={tenantId} onChange={event => { setTenantId(event.target.value); setClusterId(''); }} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs"><option value="">All tenants</option>{tenants.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}
        {source === 'cluster' && <label className="text-[10px] font-bold text-slate-500">Cluster<select value={clusterId} onChange={event => { setClusterId(event.target.value); setCollectionStatus({ state: 'idle', message: '' }); }} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs"><option value="">Select a cluster</option>{visibleClusters.map(item => <option key={item.id} value={item.id}>{item.name} · {item.connectionStatus === 'online' ? 'Online' : 'Offline'}</option>)}</select></label>}
        <label className="text-[10px] font-bold text-slate-500">Level<select value={level} onChange={event => setLevel(event.target.value)} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs"><option value="">All levels</option><option value="info">Info</option><option value="warning">Warning</option><option value="error">Error</option><option value="debug">Debug</option></select></label>
        <label className="text-[10px] font-bold text-slate-500">Component<select value={component} onChange={event => source === 'platform' ? setPlatformComponent(event.target.value) : setClusterComponent(event.target.value)} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs"><option value="">{source === 'platform' ? 'All components' : 'Select a component'}</option>{source === 'platform' ? <><option value="platform-api">Platform API</option><option value="platform-upgrader">Platform Upgrader</option><option value="task">Task Lifecycle</option></> : <><option value="comm-agent">comm-agent</option><option value="velero">Velero</option><option value="node-agent">node-agent</option></>}</select></label>
        <label className="text-[10px] font-bold text-slate-500">From<input type="datetime-local" value={from} onChange={event => setFrom(event.target.value)} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs" /></label>
        <label className="text-[10px] font-bold text-slate-500">To<input type="datetime-local" value={to} onChange={event => setTo(event.target.value)} className="mt-1 w-full rounded border border-slate-200 bg-white px-3 py-2 text-xs" /></label>
      </div></div>
      {collectionStatus.state !== 'idle' && <div role="status" className={`border-b px-4 py-3 text-xs font-semibold ${collectionStatus.state === 'failed' ? 'border-rose-100 bg-rose-50 text-rose-700' : collectionStatus.state === 'partial' ? 'border-amber-100 bg-amber-50 text-amber-700' : collectionStatus.state === 'completed' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>{collectionStatus.message}</div>}
      {loadError && <div role="alert" className="border-b border-rose-100 bg-rose-50 px-4 py-3 text-xs text-rose-700">{loadError}</div>}
      <div className="hbdr-dr-table-head"><div className="hbdr-dr-toolbar"><div className="hbdr-dr-query-group"><label className="hbdr-dr-search"><Search size={15} /><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') void searchLogs(); }} placeholder="Search message, Task ID, Request ID..." /></label></div><div className="hbdr-dr-action-group"><button type="button" disabled={loading || (source === 'cluster' && (!clusterId || !component))} onClick={() => void searchLogs()} className="hbdr-dr-action-primary">{loading ? 'Searching...' : 'Search'}</button><button type="button" disabled={loading || logs.length === 0} onClick={() => void exportLogs()} className="hbdr-dr-action-secondary"><Upload size={13} />Export</button></div></div></div>
      <HyperTable variant="page" density="comfortable" columns={columns} data={logs} getRowId={row => row.id} onRowClick={row => setSelected(row)} emptyMessage={loading ? 'Loading diagnostic logs...' : source === 'platform' ? 'No platform logs match the selected filters.' : 'No collected cluster logs match the selected filters.'} resetPageOnDataChange />
    </section>
    <AnimatePresence>{selected && <><motion.div className="hbdr-filter-drawer-backdrop" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setSelected(null)} /><motion.aside className="hbdr-filter-drawer" initial={{ x: 34, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: 34, opacity: 0 }} transition={{ duration: .18, ease: 'easeOut' }} role="dialog" aria-modal="true" aria-label="Log details"><div className="hbdr-filter-drawer-head"><div><h3>Log Details</h3><p>{selected.component} · {formatLocalDateTime(selected.eventAt)}</p></div><button type="button" onClick={() => setSelected(null)} aria-label="Close log details"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><div className="hbdr-resource-detail-section"><h4>Correlation</h4><dl>{[['Level', selected.level], ['Operation', selected.operation || '-'], ['Status', selected.status || '-'], ['Task ID', selected.taskId || '-'], ['Command ID', selected.commandId || '-'], ['Request ID', selected.requestId || '-'], ['Error Code', selected.errorCode || '-']].map(([label, value]) => <div key={label}><dt>{label}</dt><dd className="break-all">{value}</dd></div>)}</dl></div><div className="hbdr-resource-detail-section"><h4>Message</h4><p className="whitespace-pre-wrap break-words text-xs leading-5">{selected.message}</p></div>{selected.details && <div className="hbdr-resource-detail-section"><h4>Diagnostic Context</h4><pre className="max-h-72 overflow-auto rounded bg-slate-950 p-4 text-[11px] leading-5 text-slate-200">{JSON.stringify(selected.details, null, 2)}</pre></div>}</div></motion.aside></>}</AnimatePresence>
  </motion.div>;
}
