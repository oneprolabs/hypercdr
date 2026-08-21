import { useEffect, useState } from 'react';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatDateTime } from '../../lib/date-time';

type ApiTask={id:string;clusterId:string;type:string;status:string;progress:number;errorCode?:string;errorMessage?:string;payload?:Record<string,any>;createdAt?:string;completedAt?:string};
type ApiTaskEvent={id:string;taskId:string;level:string;reason:string;message:string;payload?:Record<string,any>;createdAt?:string};
type ClusterTaskLog={task:ApiTask;events:ApiTaskEvent[];loading:boolean};
type Cluster={id:string;name:string};
type VolumeProgressInfo={bytesDone:number;totalBytes:number;knownTotal:boolean;allTotalsKnown?:boolean;percent:number;speedBytesPerSecond:number;etaSeconds:number};
const formatBytes=(bytes:number)=>{if(!bytes)return'0 B';const units=['B','KB','MB','GB','TB'];let value=bytes,index=0;while(value>=1024&&index<units.length-1){value/=1024;index++}return`${value>=100||index===0?value.toFixed(0):value>=10?value.toFixed(1):value.toFixed(2)} ${units[index]}`};
const formatBytesPerSecond=(bytes:number)=>bytes>0?`${formatBytes(bytes)}/s`:'';
const formatEta=(seconds:number)=>!seconds||seconds<1?'':seconds<60?`${Math.round(seconds)}s left`:seconds<3600?`${Math.floor(seconds/60)}m ${Math.round(seconds%60)}s left`:`${Math.floor(seconds/3600)}h ${Math.floor(seconds/60)%60}m left`;
const formatPercent=(value:number)=>Number.isFinite(value)?Math.max(0,Math.min(100,value)).toFixed(2):'0.00';
const taskStatusLabel=(status?:string)=>status==='succeeded'?'Succeeded':status==='failed'?'Failed':['running','accepted','dispatched','queued'].includes(status||'')?'Running':status||'Unknown';
const taskStatusClass=(status?:string)=>status==='succeeded'?'text-emerald-600':status==='failed'?'text-rose-600':['running','accepted','dispatched','queued'].includes(status||'')?'text-blue-600':'text-slate-500';
const latestVolumeProgress=(events?:ApiTaskEvent[]):VolumeProgressInfo|null=>{for(let index=(events?.length||0)-1;index>=0;index--){const progress=events?.[index]?.payload?.velero?.volumeProgress;if(progress&&typeof progress==='object')return{bytesDone:Number(progress.bytesDone||0),totalBytes:Number(progress.totalBytes||0),knownTotal:Boolean(progress.knownTotal),allTotalsKnown:Boolean(progress.allTotalsKnown),percent:Number(progress.percent||0),speedBytesPerSecond:Number(progress.speedBytesPerSecond||0),etaSeconds:Number(progress.etaSeconds||0)}}return null};
const taskProgressInfo=(task:ApiTask,events?:ApiTaskEvent[])=>{const metrics=task.payload?.progressMetrics&&typeof task.payload.progressMetrics==='object'?task.payload.progressMetrics:task.payload||{};const totalBytes=Number(metrics.totalBytes||0),syncedBytes=Number(metrics.syncedBytes||0);if(totalBytes>0)return{bytesDone:Math.max(0,syncedBytes),totalBytes,knownTotal:true,allTotalsKnown:true,percent:Number(metrics.percent||(syncedBytes>0?syncedBytes*100/totalBytes:0)),speedBytesPerSecond:Number(metrics.speedBytesPerSecond||0),etaSeconds:Number(metrics.etaSeconds||0)};if(['succeeded','failed','canceled','cancelled'].includes(task.status))return null;const volume=latestVolumeProgress(events);return volume?.knownTotal&&volume.allTotalsKnown&&volume.totalBytes>0?volume:null};

export default function ClusterActivityPanel({ logs, clusters, highlightedTaskId, onHighlightComplete }: { logs: Record<string, ClusterTaskLog[]>; clusters: Cluster[]; highlightedTaskId?: string | null; onHighlightComplete?: () => void }) {
  const entries = Object.entries(logs)
    .flatMap(([clusterId, clusterLogs]) => {
      const cluster = clusters.find(item => item.id === clusterId) || null;
      return clusterLogs.map(log => ({ cluster, clusterId, log }));
    })
    .sort((a, b) => (b.log.task.createdAt || '').localeCompare(a.log.task.createdAt || ''));
  const clusterNamesById = new Map(clusters.map(cluster => [cluster.id, cluster.name]));
  for (const entry of entries) {
    const payloadClusterId = String(entry.log.task.payload?.clusterId || entry.log.task.payload?.archivedClusterId || entry.clusterId || '');
    const payloadClusterName = String(entry.log.task.payload?.archivedClusterName || entry.log.task.payload?.clusterName || '');
    if (payloadClusterId && payloadClusterName) clusterNamesById.set(payloadClusterId, payloadClusterName);
  }
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [dockExpanded, setDockExpanded] = useState(() => {
    const saved = window.localStorage.getItem('hypercdr:clusters:recent-tasks-expanded');
    if (saved !== null) return saved === 'true';
    return !window.matchMedia('(max-width: 900px)').matches;
  });
  const setDockState = (expanded: boolean) => {
    setDockExpanded(expanded);
    window.localStorage.setItem('hypercdr:clusters:recent-tasks-expanded', String(expanded));
  };
  const runningCount = entries.filter(entry => ['running', 'accepted', 'dispatched', 'queued'].includes(entry.log.task.status || '')).length;
  const failedCount = entries.filter(entry => entry.log.task.status === 'failed').length;
  useEffect(() => {
    if (runningCount > 0) setDockState(true);
  }, [runningCount]);
  useEffect(() => {
    if (!highlightedTaskId) return;
    setDockState(true);
    const timer = window.setTimeout(() => onHighlightComplete?.(), 5000);
    return () => window.clearTimeout(timer);
  }, [highlightedTaskId]);
  const taskTypeLabel = (type: string) => type === 'register' ? 'Register cluster' : type === 'unregister' ? 'Unregister cluster' : type === 'agent-upgrade' ? 'Upgrade Comm Agent' : type === 'velero-upgrade' ? 'Upgrade Velero' : type;
  const taskInitials = (entry: { log: ClusterTaskLog }) => entry.log.task.type === 'register' ? 'R' : entry.log.task.type === 'unregister' ? 'U' : entry.log.task.type === 'agent-upgrade' ? 'CA' : entry.log.task.type === 'velero-upgrade' ? 'V' : (entry.log.task.type || '?').charAt(0).toUpperCase();
  const taskAccent = (type: string) => type === 'register' ? 'bg-blue-500' : type === 'unregister' ? 'bg-rose-500' : type === 'agent-upgrade' ? 'bg-indigo-500' : type === 'velero-upgrade' ? 'bg-cyan-600' : 'bg-slate-500';
  const componentLabel = (task: ApiTask) => task.type === 'agent-upgrade' ? 'Comm Agent' : task.type === 'velero-upgrade' ? 'Velero' : 'Cluster lifecycle';
  const componentDetail = (task: ApiTask) => {
    if (task.type === 'agent-upgrade' || task.type === 'velero-upgrade') return String(task.payload?.version || task.payload?.image || '-');
    return task.type === 'register' ? String(task.payload?.agentVersion || 'New cluster') : task.type === 'unregister' ? 'Remove managed cluster' : '-';
  };
  const clusterDisplay = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    if (entry.cluster) return entry.cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : entry.cluster.name;
    const archived = entry.log.task.payload?.archivedClusterId as string | undefined;
    if (archived) return String(entry.log.task.payload?.archivedClusterName || entry.log.task.payload?.clusterName || clusterNamesById.get(archived) || 'Unregistered cluster');
    return (entry.log.task.payload?.clusterName as string) || 'Unnamed cluster';
  };
  const clusterInitials = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    const name = clusterDisplay(entry);
    const tokens = name.split(/[^a-zA-Z0-9]+/).filter(Boolean);
    if (tokens.length === 0) return 'CL';
    if (tokens.length === 1) return tokens[0].slice(0, 2).toUpperCase();
    return (tokens[0][0] + tokens[1][0]).toUpperCase();
  };
  const targetLabel = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    if (entry.cluster) return entry.cluster.id;
    const archived = entry.log.task.payload?.archivedClusterId as string | undefined;
    return archived || entry.clusterId;
  };
  const isActive = (task: ApiTask) => !['succeeded', 'failed', 'canceled'].includes(task.status || '');
  const activityColumns: HyperTableColumn<typeof entries[number]>[] = [
    {
      id: 'task',
      header: 'Task',
      accessorFn: entry => taskTypeLabel(entry.log.task.type),
      size: 180,
      minSize: 150,
      maxSize: 280,
      cell: info => {
        const entry = info.row.original;
        return (
          <div className="flex min-w-0 items-center gap-2">
            <span className={`flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full text-[9px] font-black text-white ${taskAccent(entry.log.task.type)}`}>{taskInitials(entry)}</span>
            <div className="min-w-0">
              <p className="truncate font-bold text-slate-900">{taskTypeLabel(entry.log.task.type)}</p>
              <p className="truncate font-mono text-[10px] text-slate-400">{entry.log.task.id.slice(0, 8)}</p>
            </div>
          </div>
        );
      },
      meta: { title: entry => `${taskTypeLabel(entry.log.task.type)} / ${entry.log.task.id}` },
    },
    {
      id: 'cluster',
      header: 'Cluster',
      accessorFn: entry => clusterDisplay(entry),
      size: 260,
      minSize: 180,
      maxSize: 420,
      cell: info => {
        const entry = info.row.original;
        return (
          <div className="flex min-w-0 items-center gap-2">
            <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-slate-100 text-[9px] font-black text-slate-500">{clusterInitials(entry)}</span>
            <div className="min-w-0">
              <p className="truncate font-semibold text-slate-700">{clusterDisplay(entry)}</p>
              <p className="truncate font-mono text-[10px] text-slate-400">{targetLabel(entry)}</p>
            </div>
          </div>
        );
      },
      meta: { title: entry => `${clusterDisplay(entry)} / ${targetLabel(entry)}` },
    },
    {
      id: 'component',
      header: 'Component / Version',
      accessorFn: entry => componentLabel(entry.log.task),
      size: 190,
      minSize: 150,
      maxSize: 320,
      cell: info => {
        const task = info.row.original.log.task;
        return (
          <div className="min-w-0">
            <p className="truncate font-semibold text-slate-700">{componentLabel(task)}</p>
            <p className="truncate font-mono text-[10px] text-slate-400">{componentDetail(task)}</p>
          </div>
        );
      },
      meta: { title: entry => `${componentLabel(entry.log.task)} / ${componentDetail(entry.log.task)}` },
    },
    {
      id: 'status',
      header: 'Status',
      accessorFn: entry => entry.log.task.status || '',
      size: 130,
      minSize: 110,
      cell: info => (
        <div>
          <p className={`font-bold ${taskStatusClass(info.row.original.log.task.status)}`}>{taskStatusLabel(info.row.original.log.task.status)}</p>
          <p className="text-[9px] text-slate-400">{formatPercent(info.row.original.log.task.progress || 0)}%</p>
        </div>
      ),
      meta: { title: entry => `${taskStatusLabel(entry.log.task.status)} ${formatPercent(entry.log.task.progress || 0)}%` },
    },
    {
      id: 'progress',
      header: 'Progress / Latest Event',
      accessorFn: entry => entry.log.events.at(-1)?.message || entry.log.task.errorMessage || '',
      size: 300,
      minSize: 220,
      maxSize: 520,
      cell: info => {
        const entry = info.row.original;
        const active = isActive(entry.log.task);
        const lastEvent = entry.log.events.length > 0 ? entry.log.events[entry.log.events.length - 1] : null;
        const volume = taskProgressInfo(entry.log.task, entry.log.events);
        const latestMessage = volume
          ? [
              `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
              formatBytesPerSecond(volume.speedBytesPerSecond),
              formatEta(volume.etaSeconds),
            ].filter(Boolean).join(' · ')
          : lastEvent?.message || (active ? 'Awaiting task events from agent...' : (entry.log.task.errorMessage || 'No events recorded yet.'));
        const progress = volume ? volume.percent : (entry.log.task.progress || 0);
        return (
          <div className="flex min-w-0 items-center gap-2">
            <div className="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-slate-100">
              <div className={`h-full rounded-full transition-all ${entry.log.task.status === 'failed' ? 'bg-rose-500' : entry.log.task.status === 'succeeded' ? 'bg-emerald-500' : 'bg-blue-500'}`} style={{ width: `${Math.max(4, progress)}%` }} />
            </div>
            <p className={`truncate ${lastEvent?.level === 'error' ? 'text-rose-600' : 'text-slate-500'}`}>{latestMessage}</p>
          </div>
        );
      },
      meta: { title: entry => entry.log.events.at(-1)?.message || entry.log.task.errorMessage || 'No events recorded yet.' },
    },
    {
      id: 'time',
      header: 'Requested / Completed',
      accessorFn: entry => entry.log.task.createdAt || '',
      size: 180,
      minSize: 150,
      maxSize: 260,
      cell: info => {
        const entry = info.row.original;
        const active = isActive(entry.log.task);
        return (
          <div className="text-right text-slate-500">
            <p className="font-semibold">{formatDateTime(entry.log.task.createdAt)}</p>
            <p className="text-[10px] text-slate-400">{active ? `Updated ${formatDateTime(entry.log.task.createdAt)}` : (entry.log.task.completedAt ? `Done ${formatDateTime(entry.log.task.completedAt)}` : '-')}</p>
          </div>
        );
      },
      meta: { align: 'right', title: entry => isActive(entry.log.task) ? formatDateTime(entry.log.task.createdAt) : (entry.log.task.completedAt ? formatDateTime(entry.log.task.completedAt) : '-') },
    },
  ];
  const renderActivityExpandedRow = (entry: typeof entries[number]) => {
    const expanded = expandedId === entry.log.task.id;
    if (!expanded) return null;
    return (
      <div className="hbdr-hyper-table-expanded-row">
        {entry.log.task.status === 'failed' && (
          <div className="mb-2 rounded-lg border border-rose-200 bg-rose-50 p-2 text-xs text-rose-700">
            <p className="font-bold text-rose-900">{entry.log.task.errorCode || 'TASK_FAILED'}</p>
            <p className="mt-1">{entry.log.task.errorMessage || 'Cluster task failed. Check agent logs and RBAC permissions.'}</p>
          </div>
        )}
        <div className="mb-1.5 flex items-center justify-between">
          <p className="text-[10px] font-bold uppercase tracking-widest text-slate-500">Event Log</p>
          <p className="text-[10px] text-slate-400">{entry.log.events.length} event{entry.log.events.length === 1 ? '' : 's'}</p>
        </div>
        <div className="max-h-40 overflow-y-auto rounded-lg border border-slate-100 bg-white">
          {entry.log.events.length > 0 ? entry.log.events.map(event => (
            <div key={event.id} className="flex items-start gap-2 border-b border-slate-100 px-2.5 py-1.5 last:border-b-0">
              <span className="w-20 shrink-0 text-[9px] text-slate-400">{formatDateTime(event.createdAt)}</span>
              <span className={`shrink-0 text-[10px] font-bold uppercase ${event.level === 'error' ? 'text-rose-600' : 'text-slate-700'}`}>{(event.reason || event.level || 'info').replace(/_/g, ' ')}</span>
              <p className="min-w-0 flex-1 text-[11px] text-slate-600">{event.message}</p>
            </div>
          )) : (
            <p className="px-3 py-3 text-center text-xs font-medium text-slate-400">{entry.log.loading ? 'Awaiting task events from agent...' : 'No events recorded yet.'}</p>
          )}
        </div>
      </div>
    );
  };

  return (
      <div className={`hbdr-cluster-recent-tasks hbdr-cluster-task-dock ${dockExpanded ? 'is-expanded' : 'is-collapsed'} rounded-xl border border-slate-200 bg-white shadow-sm`}>
        <button type="button" className="hbdr-cluster-task-dock-toggle" onClick={() => setDockState(!dockExpanded)} aria-expanded={dockExpanded} aria-controls="cluster-recent-task-list">
          <span className="hbdr-cluster-task-dock-title">Recent Tasks <em>{entries.length}</em></span>
          <span className="hbdr-cluster-task-dock-summary">
            {runningCount > 0 && <span className="is-running">Running {runningCount}</span>}
            {failedCount > 0 && <span className="is-failed">Failed {failedCount}</span>}
            {dockExpanded ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
          </span>
        </button>
        {dockExpanded && <div id="cluster-recent-task-list" className="hbdr-cluster-task-dock-body">
          {entries.length === 0 ? (
            <div className="px-4 py-4 text-left text-xs font-medium text-slate-400">No recent cluster lifecycle or component tasks yet.</div>
          ) : (
            <HyperTable
              variant="page"
              density="compact"
              columns={activityColumns}
              data={entries}
              getRowId={row => row.log.task.id}
              onRowClick={row => setExpandedId(expandedId === row.log.task.id ? null : row.log.task.id)}
              getRowClassName={row => [
                expandedId === row.log.task.id ? 'hbdr-dr-row-selected' : '',
                highlightedTaskId === row.log.task.id ? 'hbdr-cluster-task-new' : '',
              ].filter(Boolean).join(' ')}
              renderExpandedRow={renderActivityExpandedRow}
              initialPageSize={8}
              pageSizeOptions={[8, 20, 50]}
              emptyMessage="No recent tasks yet."
            />
          )}
        </div>
        }
      </div>
  );
}
