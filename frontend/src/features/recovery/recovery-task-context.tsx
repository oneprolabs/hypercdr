import { useEffect, useState } from 'react';
import { formatLocalDateTime } from '../../lib/date-time';
import { recoveryDuration, recoveryNamespaces } from './recovery-detail-model';
import { isActiveTaskStatus } from './task-status';
import type { ApiTask, ApiTaskEvent } from './types';
import { taskCacheHit } from './cache-hit-model';
import './recovery-task-context.css';

type Point = { id: string; sourceClusterId?: string; sourceNamespace?: string; taskCreatedAt?: string; createdAt?: string; veleroBackupName?: string };

export function RecoveryTaskContext({ task, point, clusters, events = [] }: { task: ApiTask; point?: Point; clusters: { id: string; name: string }[]; events?: ApiTaskEvent[] }) {
  const [now, setNow] = useState(Date.now);
  const active = isActiveTaskStatus(task.status);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active, task.id]);
  if (!['backup', 'drill', 'restore', 'takeover', 'failover', 'failback'].includes(task.type)) return null;
  const backup = task.type === 'backup';
  const payload = task.payload || {};
  const pointID = task.restorePointId || payload.restorePointId;
  const pointTime = formatLocalDateTime(point?.taskCreatedAt || point?.createdAt || payload.restorePointTaskCreatedAt);
  const clusterName = (id?: string) => clusters.find(cluster => cluster.id === id)?.name || id || '—';
  const sources = recoveryNamespaces(task, 'source');
  const included = Array.isArray(payload.includedNamespaces) ? payload.includedNamespaces.filter((value: unknown): value is string => typeof value === 'string' && Boolean(value.trim())) : [];
  const sourceNamespace = sources.join(', ') || included.join(', ') || payload.namespace || point?.sourceNamespace || '—';
  const targets = recoveryNamespaces(task, 'target');
  const cache = taskCacheHit(task, events);
  const fields = [
    ['Restore point', pointTime ? `RP-${pointTime}` : pointID || (backup && active ? 'Creating…' : '—')],
    [active ? 'Elapsed time' : 'Total duration', recoveryDuration(task, now)],
    ['Source cluster', clusterName(backup ? task.clusterId : point?.sourceClusterId || payload.sourceClusterId)],
    ['Source namespace', sourceNamespace],
    ...(!backup ? [
      ['Target cluster', clusterName(task.clusterId)],
      [task.type === 'drill' ? 'Drill namespace' : 'Target namespace', targets.join(', ') || (payload.namespaceMode === 'original' ? sourceNamespace : '') || '—'],
    ] : []),
    [cache.label, cache.value],
  ];
  return <section className="hbdr-recovery-context" aria-label={backup ? 'Backup information' : 'Recovery information'}><dl>{fields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd title={label === cache.label ? cache.title : value}>{value}</dd></div>)}</dl></section>;
}
