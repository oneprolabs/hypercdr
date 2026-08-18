import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, CheckCircle2, RefreshCw, Server, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiGet } from '../../api/client';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatLocalDateTime } from '../../lib/date-time';

type ApiList<T> = { items: T[] };
type ApiCluster = { id: string; name: string; status: string; connectionStatus: string; agentVersion?: string; lastSeenAt?: string };
type ApiStorage = { id: string; name: string; status: string; lastValidatedAt?: string };
type ApiTask = { id: string; clusterId?: string; appId?: string; type: string; status: string; progress: number; errorCode?: string; errorMessage?: string; payload?: Record<string, unknown>; createdAt?: string; startedAt?: string; completedAt?: string };
type Issue = { id: string; severity: 'Critical' | 'Warning'; issue: string; resource: string; resourceType: 'cluster' | 'storage' | 'task'; resourceId: string; impact: string; detectedAt: string; taskId?: string; details: string };
type Operation = { id: string; operation: string; resource: string; route: string; stage: string; progress: number; status: string; duration: string; createdAt: string; errorMessage?: string; clusterId?: string };

const listItems = <T,>(response: ApiList<T>) => response.items || [];
const activeStatuses = new Set(['queued', 'dispatched', 'accepted', 'running', 'syncing', 'finalizing', 'canceling']);
const taskLabel = (type: string) => ({ backup: 'Data Sync', restore: 'Restore', drill: 'DR Drill', takeover: 'Takeover', failback: 'Failback', 'agent-upgrade': 'Agent Upgrade', 'velero-upgrade': 'Velero Upgrade' }[type] || type.replaceAll('-', ' '));
const statusLabel = (status: string) => status ? status.charAt(0).toUpperCase() + status.slice(1) : 'Unknown';
const durationLabel = (task: ApiTask) => {
  const startDate = new Date(task.startedAt || task.createdAt || '');
  const endDate = task.completedAt ? new Date(task.completedAt) : new Date();
  // Zero-value timestamps from older tasks must never become multi-million-hour durations.
  if (!Number.isFinite(startDate.getTime()) || !Number.isFinite(endDate.getTime())
    || startDate.getUTCFullYear() < 2020 || endDate.getUTCFullYear() < 2020
    || endDate.getTime() < startDate.getTime()) return '-';
  const seconds = Math.floor((endDate.getTime() - startDate.getTime()) / 1000);
  if (seconds > 366 * 24 * 60 * 60) return '-';
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
};

export default function OperationsCenterPage({ toast, openLogs, openClusters }: {
  toast: (message: string) => void; openLogs: (taskId?: string) => void; openClusters: () => void;
}) {
  const [clusters, setClusters] = useState<ApiCluster[]>([]);
  const [storage, setStorage] = useState<ApiStorage[]>([]);
  const [tasks, setTasks] = useState<ApiTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [selectedIssue, setSelectedIssue] = useState<Issue | null>(null);
  const [selectedOperation, setSelectedOperation] = useState<Operation | null>(null);
  const [issueFilter, setIssueFilter] = useState<'all' | 'critical' | 'failed' | 'offline'>('all');

  const load = useCallback(async () => {
    setLoading(true); setLoadError('');
    try {
      const [clusterResponse, storageResponse, taskResponse] = await Promise.all([
        apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
        apiGet<ApiList<ApiStorage>>('/api/v1/storage-repositories'),
        apiGet<ApiList<ApiTask>>('/api/v1/tasks?types=backup,restore,drill,takeover,failback,agent-upgrade,velero-upgrade&limit=100'),
      ]);
      setClusters(listItems(clusterResponse)); setStorage(listItems(storageResponse)); setTasks(listItems(taskResponse));
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Operations data could not be loaded';
      setLoadError(message); toast(message);
    } finally { setLoading(false); }
  }, [toast]);
  useEffect(() => { void load(); }, [load]);

  const clusterName = (id?: string) => clusters.find(item => item.id === id)?.name || id?.slice(0, 8) || 'Platform';
  const issues = useMemo<Issue[]>(() => {
    const rows: Issue[] = [];
    for (const cluster of clusters) {
      if (cluster.connectionStatus !== 'online') rows.push({ id: `cluster-${cluster.id}`, severity: 'Critical', issue: 'Cluster agent is offline', resource: cluster.name, resourceType: 'cluster', resourceId: cluster.id, impact: 'DR operations cannot be dispatched to this cluster.', detectedAt: cluster.lastSeenAt || '', details: `The platform has no active agent connection for ${cluster.name}. Verify comm-agent connectivity and credentials.` });
      else if (cluster.status && !['healthy', 'ready', 'online'].includes(cluster.status.toLowerCase())) rows.push({ id: `cluster-health-${cluster.id}`, severity: 'Warning', issue: `Cluster health is ${cluster.status}`, resource: cluster.name, resourceType: 'cluster', resourceId: cluster.id, impact: 'Some protection or recovery operations may be degraded.', detectedAt: cluster.lastSeenAt || '', details: 'Review cluster component health and correlated diagnostic logs.' });
    }
    for (const repo of storage) if (!['connected', 'ready', 'available', 'healthy'].includes((repo.status || '').toLowerCase())) rows.push({ id: `storage-${repo.id}`, severity: 'Critical', issue: 'Storage repository is unavailable', resource: repo.name, resourceType: 'storage', resourceId: repo.id, impact: 'Backup and restore-point access may fail.', detectedAt: repo.lastValidatedAt || '', details: `Repository status is ${repo.status || 'unknown'}. Test connectivity and review platform diagnostic logs.` });
    for (const task of tasks.filter(item => item.status === 'failed').slice(0, 20)) rows.push({ id: `task-${task.id}`, severity: 'Critical', issue: `${taskLabel(task.type)} failed`, resource: clusterName(task.clusterId), resourceType: 'task', resourceId: task.id, taskId: task.id, impact: task.errorMessage || 'The requested DR operation did not complete.', detectedAt: task.completedAt || task.createdAt || '', details: [task.errorCode, task.errorMessage].filter(Boolean).join(' · ') || 'Open correlated diagnostic logs to identify the failed stage.' });
    return rows.sort((a, b) => (b.detectedAt || '').localeCompare(a.detectedAt || ''));
  }, [clusters, storage, tasks]);

  const operations = useMemo<Operation[]>(() => tasks.filter(task => activeStatuses.has(task.status) || task.status === 'failed').slice(0, 30).map(task => ({
    id: task.id, operation: taskLabel(task.type), resource: String(task.payload?.namespace || task.payload?.applicationName || task.appId || '-'), route: clusterName(task.clusterId), stage: String(task.payload?.stage || statusLabel(task.status)), progress: Number(task.progress || 0), status: statusLabel(task.status), duration: durationLabel(task), createdAt: task.createdAt || '', errorMessage: task.errorMessage, clusterId: task.clusterId,
  })), [clusters, tasks]);
  const filteredIssues = issues.filter(issue => issueFilter === 'all' || (issueFilter === 'critical' && issue.severity === 'Critical') || (issueFilter === 'failed' && issue.resourceType === 'task') || (issueFilter === 'offline' && issue.id.startsWith('cluster-')));
  const runningCount = tasks.filter(task => activeStatuses.has(task.status)).length;
  const failedCount = tasks.filter(task => task.status === 'failed').length;
  const offlineCount = clusters.filter(cluster => cluster.connectionStatus !== 'online').length;

  const issueColumns: HyperTableColumn<Issue>[] = [
    { id: 'severity', header: 'Severity', accessorFn: row => row.severity, size: 110, cell: info => <span className={`rounded-full border px-2 py-1 text-[10px] font-semibold ${info.row.original.severity === 'Critical' ? 'border-rose-100 bg-rose-50 text-rose-700' : 'border-amber-100 bg-amber-50 text-amber-700'}`}>{info.row.original.severity}</span>, meta: { kind: 'status' } },
    { id: 'issue', header: 'Issue', accessorFn: row => row.issue, size: 230, minSize: 180, cell: info => info.row.original.issue, meta: { kind: 'primary', title: row => row.issue } },
    { id: 'resource', header: 'Affected Resource', accessorFn: row => row.resource, size: 180, cell: info => info.row.original.resource, meta: { kind: 'secondary', title: row => row.resource } },
    { id: 'impact', header: 'Impact', accessorFn: row => row.impact, size: 360, minSize: 220, cell: info => info.row.original.impact, meta: { kind: 'secondary', title: row => row.impact } },
    { id: 'detected', header: 'Detected', accessorFn: row => row.detectedAt, size: 180, cell: info => info.row.original.detectedAt ? formatLocalDateTime(info.row.original.detectedAt) : 'Current', meta: { kind: 'secondary' } },
  ];
  const operationColumns: HyperTableColumn<Operation>[] = [
    { id: 'operation', header: 'Operation', accessorFn: row => row.operation, size: 170, cell: info => info.row.original.operation, meta: { kind: 'primary' } },
    { id: 'resource', header: 'Application / Resource', accessorFn: row => row.resource, size: 220, cell: info => info.row.original.resource, meta: { kind: 'secondary', title: row => row.resource } },
    { id: 'route', header: 'Cluster', accessorFn: row => row.route, size: 150, cell: info => info.row.original.route, meta: { kind: 'secondary' } },
    { id: 'stage', header: 'Stage', accessorFn: row => row.stage, size: 160, cell: info => info.row.original.stage, meta: { kind: 'secondary' } },
    { id: 'progress', header: 'Progress', accessorFn: row => row.progress, size: 130, cell: info => <span>{Math.max(0, Math.min(100, info.row.original.progress)).toFixed(0)}%</span>, meta: { kind: 'status' } },
    { id: 'status', header: 'Status', accessorFn: row => row.status, size: 120, cell: info => <span className={info.row.original.status === 'Failed' ? 'text-rose-700' : 'text-blue-700'}>{info.row.original.status}</span>, meta: { kind: 'status' } },
    { id: 'duration', header: 'Duration', accessorFn: row => row.duration, size: 110, cell: info => info.row.original.duration, meta: { kind: 'secondary' } },
  ];

  return <motion.div key="operations-center" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex items-center justify-between gap-4"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600"><Activity size={18} /></div><div><h3 className="text-sm font-black text-slate-900">Operations Center</h3><p className="mt-1 text-[11px] text-slate-400">Monitor platform health, active DR operations, and issues requiring attention.</p></div></div><button type="button" disabled={loading} onClick={() => void load()} className="hbdr-dr-action-secondary"><RefreshCw size={13} />{loading ? 'Refreshing...' : 'Refresh'}</button></div></div>
    {loadError && <div role="alert" className="rounded border border-rose-100 bg-rose-50 px-4 py-3 text-xs text-rose-700">{loadError}</div>}
    <div className="grid gap-3 md:grid-cols-4">
      {[{ key: 'critical', label: 'Critical Issues', value: issues.filter(issue => issue.severity === 'Critical').length, tone: 'rose' }, { key: 'failed', label: 'Failed Operations', value: failedCount, tone: 'rose' }, { key: 'all', label: 'Running Operations', value: runningCount, tone: 'blue' }, { key: 'offline', label: 'Offline Clusters', value: offlineCount, tone: 'amber' }].map(item => <button type="button" key={item.label} onClick={() => setIssueFilter(item.key as typeof issueFilter)} className={`rounded-xl border bg-white p-4 text-left shadow-sm transition hover:border-blue-200 ${issueFilter === item.key ? 'border-blue-300 ring-1 ring-blue-100' : 'border-slate-200'}`}><span className="text-[11px] font-semibold text-slate-500">{item.label}</span><strong className={`mt-2 block text-2xl ${item.tone === 'rose' ? 'text-rose-700' : item.tone === 'amber' ? 'text-amber-700' : 'text-blue-700'}`}>{loading ? '—' : item.value}</strong></button>)}
    </div>
    <section className="hbdr-dr-table-card"><div className="hbdr-dr-table-head"><div><h3>Current Issues</h3><p>Open an issue to review impact and continue to the relevant resource or logs.</p></div></div><HyperTable variant="page" density="comfortable" columns={issueColumns} data={filteredIssues} getRowId={row => row.id} onRowClick={row => setSelectedIssue(row)} emptyMessage={loading ? 'Loading current issues...' : 'No current issues match this view.'} resetPageOnDataChange /></section>
    <section className="hbdr-dr-table-card"><div className="hbdr-dr-table-head"><div><h3>Active & Failed Operations</h3><p>Recent operations that are still running or require investigation.</p></div></div><HyperTable variant="page" density="comfortable" columns={operationColumns} data={operations} getRowId={row => row.id} onRowClick={row => setSelectedOperation(row)} emptyMessage={loading ? 'Loading operations...' : 'No active or failed operations.'} resetPageOnDataChange /></section>
    <section className="hbdr-dr-table-card"><div className="hbdr-dr-table-head"><div><h3>Component Health</h3><p>Live health derived from registered clusters and storage repositories.</p></div></div><div className="grid gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">{clusters.map(cluster => <button type="button" key={cluster.id} onClick={openClusters} className="flex items-center justify-between rounded-lg border border-slate-100 bg-slate-50/60 p-3 text-left"><span className="flex items-center gap-2 text-xs font-semibold text-slate-700"><Server size={15} />{cluster.name} Agent</span><span className={`text-[10px] font-semibold ${cluster.connectionStatus === 'online' ? 'text-emerald-700' : 'text-rose-700'}`}>{cluster.connectionStatus === 'online' ? 'Online' : 'Offline'}</span></button>)}{storage.map(repo => <div key={repo.id} className="flex items-center justify-between rounded-lg border border-slate-100 bg-slate-50/60 p-3"><span className="text-xs font-semibold text-slate-700">{repo.name} Storage</span><span className={`text-[10px] font-semibold ${['connected', 'ready', 'available', 'healthy'].includes((repo.status || '').toLowerCase()) ? 'text-emerald-700' : 'text-rose-700'}`}>{statusLabel(repo.status)}</span></div>)}{!loading && clusters.length === 0 && storage.length === 0 && <div className="col-span-full flex items-center gap-2 py-6 text-xs text-slate-400"><CheckCircle2 size={16} />No registered components yet.</div>}</div></section>
    <AnimatePresence>{selectedIssue && <><motion.div className="hbdr-filter-drawer-backdrop" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setSelectedIssue(null)} /><motion.aside className="hbdr-filter-drawer" initial={{ x: 34, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: 34, opacity: 0 }} role="dialog" aria-modal="true" aria-label="Issue details"><div className="hbdr-filter-drawer-head"><div><h3>Issue Details</h3><p>{selectedIssue.resource}</p></div><button type="button" onClick={() => setSelectedIssue(null)} aria-label="Close issue details"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><div className="hbdr-resource-detail-section"><h4>Impact</h4><dl><div><dt>Severity</dt><dd>{selectedIssue.severity}</dd></div><div><dt>Issue</dt><dd>{selectedIssue.issue}</dd></div><div><dt>Resource</dt><dd>{selectedIssue.resource}</dd></div><div><dt>Impact</dt><dd>{selectedIssue.impact}</dd></div></dl></div><div className="hbdr-resource-detail-section"><h4>Recommended Investigation</h4><p className="text-xs leading-5 text-slate-600">{selectedIssue.details}</p></div></div><div className="hbdr-filter-drawer-actions"><button type="button" onClick={() => { setSelectedIssue(null); if (selectedIssue.resourceType === 'cluster') openClusters(); else openLogs(selectedIssue.taskId); }}>{selectedIssue.resourceType === 'cluster' ? 'View Cluster' : 'View Diagnostic Logs'}</button><button type="button" className="hbdr-dr-action-primary" onClick={() => { setSelectedIssue(null); openLogs(selectedIssue.taskId); }}>Open Logs</button></div></motion.aside></>}</AnimatePresence>
    <AnimatePresence>{selectedOperation && <><motion.div className="hbdr-filter-drawer-backdrop" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setSelectedOperation(null)} /><motion.aside className="hbdr-filter-drawer" initial={{ x: 34, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: 34, opacity: 0 }} role="dialog" aria-modal="true" aria-label="Operation details"><div className="hbdr-filter-drawer-head"><div><h3>{selectedOperation.operation}</h3><p>{selectedOperation.id}</p></div><button type="button" onClick={() => setSelectedOperation(null)} aria-label="Close operation details"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><div className="hbdr-resource-detail-section"><h4>Operation</h4><dl><div><dt>Status</dt><dd>{selectedOperation.status}</dd></div><div><dt>Stage</dt><dd>{selectedOperation.stage}</dd></div><div><dt>Progress</dt><dd>{selectedOperation.progress.toFixed(0)}%</dd></div><div><dt>Cluster</dt><dd>{selectedOperation.route}</dd></div><div><dt>Duration</dt><dd>{selectedOperation.duration}</dd></div></dl></div>{selectedOperation.errorMessage && <div className="hbdr-resource-detail-section text-rose-700"><h4>Failure</h4><p className="text-xs leading-5">{selectedOperation.errorMessage}</p></div>}</div><div className="hbdr-filter-drawer-actions"><button type="button" onClick={() => setSelectedOperation(null)}>Close</button><button type="button" className="hbdr-dr-action-primary" onClick={() => { const id = selectedOperation.id; setSelectedOperation(null); openLogs(id); }}>View Diagnostic Logs</button></div></motion.aside></>}</AnimatePresence>
  </motion.div>;
}
