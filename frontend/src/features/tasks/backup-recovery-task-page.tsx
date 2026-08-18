import React, { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { AlertCircle, Archive, ArrowDown, CheckCircle2, ChevronDown, Clock, DatabaseBackup, Eye, Filter, HardDrive, History, MoreVertical, Play, RefreshCw, Search, Server, X, Zap } from 'lucide-react';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { ModalFrame } from '../../components/modal-frame';
import { SearchBar } from '../../components/search-bar';
import ListToolbarControls from '../../components/list-toolbar-controls';
import { apiGet } from '../../api/client';
import { formatLocalDateTime } from '../../lib/date-time';
import type { Cluster } from '../clusters/types';
import type { ApiCluster, ApiStorageRepo } from '../recovery/platform-types';
import { listItems, type ApiApplication, type ApiList, type ApiRestorePoint, type ApiTask, type ApiTaskEvent } from '../recovery/types';
import { isActiveTaskStatus, isFailedStatus, isSucceededStatus, taskHasWarning } from '../recovery/task-status';
import {
  TaskErrorDetailBlock, TaskErrorStatus, TaskFinalResult, TaskProcessTimeline,
  eventRestoreResultErrors, formatBytes, formatPercent, taskDetailLabel,
  taskFailureDetails, taskFailureSummary,
} from '../recovery/task-ui';
import {
  ErrorDetailModalFrame, listToolbarQueryFields, matchesColumnFilterToken,
  namespacesFromPayload, parseColumnFilterToken, restorePointDisplayLabel,
} from '../applications/application-support';
import { restorePointListStatus, restorePointNamespaces, taskStatusLabel } from '../restore-points/restore-point-support';

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded bg-slate-50 p-3">
      <p className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</p>
      <p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p>
    </div>
  );
}

export default function BackupRecoveryTaskPage({
  toast,
  workspaceCluster,
  clusterContext,
  initialTasks = [],
  initialRestorePoints = [],
  initialClusters = [],
  initialApps = [],
  initialStorageRepos = [],
}: {
  toast: (msg: string) => void;
  workspaceCluster?: Cluster | ApiCluster | null;
  clusterContext?: React.ReactNode;
  initialTasks?: ApiTask[];
  initialRestorePoints?: ApiRestorePoint[];
  initialClusters?: ApiCluster[];
  initialApps?: ApiApplication[];
  initialStorageRepos?: ApiStorageRepo[];
}) {
  type Row = {
    id: string;
    operation: string;
    taskType: string;
    namespace: string;
    cluster: string;
    repository: string;
    status: string;
    progress: number;
    restorePointState: 'available' | 'cleared' | 'not_created' | 'failed' | 'pending';
    restorePointLabel: string;
    restorePointName: string;
    createdAt: string;
    completedAt: string;
    task: ApiTask;
    point?: ApiRestorePoint;
  };

  const [tasks, setTasks] = useState<ApiTask[]>(initialTasks);
  const [restorePoints, setRestorePoints] = useState<ApiRestorePoint[]>(initialRestorePoints);
  const [clusters, setClusters] = useState<ApiCluster[]>(initialClusters);
  const [apps, setApps] = useState<ApiApplication[]>(initialApps);
  const [storageRepos, setStorageRepos] = useState<ApiStorageRepo[]>(initialStorageRepos);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('namespace');
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState<string[]>(['namespace', 'repository', 'restorePoint', 'createdAt', 'completedAt']);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [selectedTaskEvents, setSelectedTaskEvents] = useState<ApiTaskEvent[]>([]);
  const [timeWindow, setTimeWindow] = useState<'24h' | '7d' | '30d' | 'all'>('7d');

  const load = async () => {
    const [taskRes, pointRes, clusterRes, appRes, storageRes] = await Promise.all([
      apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
      apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
      apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
      apiGet<ApiList<ApiApplication>>('/api/v1/applications'),
      apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
    ]);
    setTasks(listItems(taskRes));
    setRestorePoints(listItems(pointRes));
    setClusters(listItems(clusterRes));
    setApps(listItems(appRes));
    setStorageRepos(listItems(storageRes));
  };

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        if (!cancelled) await load();
      } catch {
        if (!cancelled) toast('Failed to refresh backup and recovery tasks');
      }
    };
    refresh();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  const pointsById = new Map(restorePoints.map(point => [point.id, point]));
  const pointsByBackupTaskId = new Map<string, ApiRestorePoint>();
  restorePoints.forEach(point => {
    const taskId = String(point.metadata?.backupTaskId || (point as any).backupTaskId || '');
    if (taskId) pointsByBackupTaskId.set(taskId, point);
  });
  const clustersById = new Map(clusters.map(cluster => [cluster.id, cluster]));
  const appsById = new Map(apps.map(app => [app.id, app]));
  const storageById = new Map(storageRepos.map(repo => [repo.id, repo]));
  const taskTypes = new Set(['backup', 'restore', 'drill', 'takeover', 'failover', 'failback']);

  const taskPoint = (task: ApiTask): ApiRestorePoint | undefined => {
    const pointByTask = pointsByBackupTaskId.get(task.id);
    if (pointByTask) return pointByTask;
    const ids = [
      task.restorePointId,
      String(task.payload?.restorePointId || ''),
      String(task.payload?.archivedRestorePointId || ''),
      String(task.payload?.pointId || ''),
    ].filter(Boolean);
    for (const id of ids) {
      const point = pointsById.get(id);
      if (point) return point;
    }
    const backupName = String(task.payload?.veleroBackupName || task.payload?.backupName || '');
    if (backupName) {
      const point = restorePoints.find(item => item.veleroBackupName === backupName);
      if (point) return point;
    }
    return undefined;
  };

  const namespaceForTask = (task: ApiTask, point?: ApiRestorePoint) => {
    const app = task.appId ? appsById.get(task.appId) : undefined;
    return String(task.payload?.sourceNamespace || point?.sourceNamespace || app?.namespace || '-');
  };

  const repositoryForTask = (task: ApiTask, point?: ApiRestorePoint) => {
    const repoId = point?.storageRepoId || String(task.payload?.storageRepoId || task.payload?.repositoryId || '');
    const veleroStorage = point?.metadata?.velero && typeof point.metadata.velero === 'object' ? String((point.metadata.velero as Record<string, any>).storageLocation || '') : '';
    return (repoId && storageById.get(repoId)?.name)
      || point?.backupStorageName
      || String(point?.metadata?.backupStorageName || '')
      || String(task.payload?.storageRepo || task.payload?.backupStorageName || task.payload?.storageLocation || task.payload?.repository || task.payload?.name || '')
      || veleroStorage
      || 'Unknown';
  };

  const restorePointStateForTask = (task: ApiTask, point?: ApiRestorePoint): Row['restorePointState'] => {
    if (point) return restorePointListStatus(point) === 'available' ? 'available' : 'cleared';
    if (isFailedStatus(task.status)) return 'failed';
    if (isActiveTaskStatus(task.status)) return 'pending';
    if (isSucceededStatus(task.status) && task.type === 'backup') return 'cleared';
    if (isSucceededStatus(task.status) && ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(task.type)) return task.restorePointId || task.payload?.restorePointId ? 'cleared' : 'not_created';
    return 'not_created';
  };

  const operationLabel = (type: string) => {
    if (type === 'backup') return 'Backup';
    if (type === 'drill') return 'DR Drill';
    if (type === 'takeover' || type === 'failover') return 'Takeover';
    if (type === 'failback') return 'Failback';
    return 'Restore';
  };

  const rows: Row[] = tasks
    .filter(task => taskTypes.has(task.type))
    .map(task => {
      const point = taskPoint(task);
      const pointState = restorePointStateForTask(task, point);
      const cluster = clustersById.get(task.clusterId);
      return {
        id: task.id,
        operation: operationLabel(task.type),
        taskType: task.type,
        namespace: namespaceForTask(task, point),
        cluster: cluster?.name || task.clusterId || '-',
        repository: repositoryForTask(task, point),
        status: task.status || 'unknown',
        progress: Number(task.progress || 0),
        restorePointState: pointState,
        restorePointLabel: point
          ? restorePointDisplayLabel(point)
          : pointState === 'cleared' ? 'Cleared' : pointState === 'failed' ? 'Not created' : pointState === 'pending' ? 'Pending' : '-',
        restorePointName: point?.veleroBackupName || String(task.payload?.veleroBackupName || task.payload?.backupName || '') || '-',
        createdAt: formatLocalDateTime(task.createdAt),
        completedAt: formatLocalDateTime(task.completedAt),
        task,
        point,
      };
    })
    .sort((a, b) => (b.task.createdAt || '').localeCompare(a.task.createdAt || ''));
  const activeLedgerTaskIds = rows.filter(row => isActiveTaskStatus(row.status)).map(row => row.id);
  const activeLedgerTaskKey = activeLedgerTaskIds.sort().join('|');

  useEffect(() => {
    const ids = activeLedgerTaskKey ? activeLedgerTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const idSet = new Set(ids);
    const pollActiveTasks = async () => {
      try {
        const [taskRes, pointRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
          apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
        ]);
        if (cancelled) return;
        const latestTasks = new Map(listItems(taskRes).map(task => [task.id, task]));
        setTasks(prev => prev.map(task => {
          if (!idSet.has(task.id)) return task;
          const next = latestTasks.get(task.id);
          return next ? { ...task, ...next } : task;
        }));
        setRestorePoints(listItems(pointRes));
      } catch {
        // Keep the current list stable if a status poll fails.
      }
    };
    pollActiveTasks();
    const timer = window.setInterval(pollActiveTasks, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeLedgerTaskKey]);

  const timeWindowOptions = [
    { value: '24h', label: 'Last 24 hours' },
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
    { value: 'all', label: 'All history' },
  ] as const;

  const timeWindowLabel = timeWindowOptions.find(option => option.value === timeWindow)?.label || 'Last 7 days';
  const rowsInTimeWindow = rows.filter(row => {
    if (timeWindow === 'all') return true;
    if (isActiveTaskStatus(row.status) && !row.task.createdAt) return true;
    const created = row.task.createdAt ? new Date(row.task.createdAt).getTime() : 0;
    if (!created || Number.isNaN(created)) return false;
    const spanMs = timeWindow === '24h' ? 24 * 60 * 60 * 1000 : timeWindow === '7d' ? 7 * 24 * 60 * 60 * 1000 : 30 * 24 * 60 * 60 * 1000;
    return created >= Date.now() - spanMs;
  });

  const filteredRows = rowsInTimeWindow.filter(row => {
    const q = query.trim().toLowerCase();
    const searchableValues: Record<string, string> = {
      operation: row.operation,
      namespace: row.namespace,
      cluster: row.cluster,
      repository: row.repository,
      restorePoint: row.restorePointLabel,
      restorePointName: row.restorePointName,
      createdAt: row.createdAt,
      completedAt: row.completedAt,
      status: row.status,
    };
    const matchesQuery = !q || (searchableValues[queryField] || Object.values(searchableValues).join(' ')).toLowerCase().includes(q);
    const matchesFilters = activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => searchableValues[field] || '');
      if (filter === 'backup') return row.taskType === 'backup';
      if (filter === 'recovery') return ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType);
      if (filter === 'running') return isActiveTaskStatus(row.status);
      if (filter === 'succeeded') return isSucceededStatus(row.status);
      if (filter === 'failed') return isFailedStatus(row.status);
      if (filter === 'cleared') return row.restorePointState === 'cleared';
      return true;
    });
    return matchesQuery && matchesFilters;
  });

  const selectedRow = selectedTaskId ? rows.find(row => row.id === selectedTaskId) || null : null;
  useEffect(() => {
    if (!selectedTaskId) {
      setSelectedTaskEvents([]);
      return;
    }
    let cancelled = false;
    apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${selectedTaskId}/events`)
      .then(result => {
        if (!cancelled) setSelectedTaskEvents(listItems(result));
      })
      .catch(() => {
        if (!cancelled) setSelectedTaskEvents([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedTaskId]);

  const summary = {
    total: rowsInTimeWindow.length,
    backup: rowsInTimeWindow.filter(row => row.taskType === 'backup').length,
    recovery: rowsInTimeWindow.filter(row => ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType)).length,
    running: rowsInTimeWindow.filter(row => isActiveTaskStatus(row.status)).length,
    succeeded: rowsInTimeWindow.filter(row => isSucceededStatus(row.status)).length,
    failed: rowsInTimeWindow.filter(row => isFailedStatus(row.status)).length,
  };
  const taskToolbarColumns = [
    { value: 'namespace', label: 'Namespace' },
    { value: 'cluster', label: 'Cluster' },
    { value: 'repository', label: 'Repository' },
    { value: 'restorePoint', label: 'Restore Point' },
    { value: 'createdAt', label: 'Started' },
    { value: 'completedAt', label: 'Completed' },
  ];
  const taskQueryFields = listToolbarQueryFields([
    { value: 'operation', label: 'Task Type' },
    { value: 'status', label: 'Task Status' },
  ], taskToolbarColumns, visibleColumns);

  const renderTaskStatus = (row: Row) => {
    const tone = isSucceededStatus(row.status) ? 'ok' : isFailedStatus(row.status) ? 'fail' : isActiveTaskStatus(row.status) ? 'running' : 'muted';
    const title = isFailedStatus(row.status)
      ? (row.task.errorMessage || row.task.errorCode || 'Task failed')
      : taskStatusLabel(row.status);
    return <span className={`hbdr-task-ledger-status is-${tone}`} title={title}>{taskStatusLabel(row.status)}</span>;
  };

  const renderPointState = (row: Row) => {
    const label = row.restorePointState === 'available' ? 'Available'
      : row.restorePointState === 'cleared' ? 'Cleared'
        : row.restorePointState === 'failed' ? 'Not created'
          : row.restorePointState === 'pending' ? 'Pending'
            : '-';
    const primary = row.point ? row.restorePointLabel : label;
    const secondary = row.point ? label : row.restorePointLabel;
    return <span className={`hbdr-task-ledger-point is-${row.restorePointState}`}><strong>{primary}</strong><em>{secondary}</em></span>;
  };

  const renderTaskType = (row: Row) => {
    const Icon = row.taskType === 'backup' ? Archive
      : row.taskType === 'drill' ? Play
        : row.taskType === 'takeover' || row.taskType === 'failover' ? Zap
          : row.taskType === 'failback' ? ArrowDown
            : RefreshCw;
    return (
      <span className={`hbdr-task-ledger-op is-${row.taskType}`}>
        <i aria-hidden="true"><Icon size={14} /></i>
        <strong>{row.operation}</strong>
      </span>
    );
  };

  const progressLabel = (row: Row) => isActiveTaskStatus(row.status) ? `${formatPercent(row.progress || 0)}%` : isSucceededStatus(row.status) ? '100.00%' : row.progress ? `${formatPercent(row.progress)}%` : '-';
  const taskDurationLabel = (row: Row) => {
    const start = row.task.createdAt ? new Date(row.task.createdAt).getTime() : 0;
    const end = row.task.completedAt ? new Date(row.task.completedAt).getTime() : Date.now();
    if (!start || Number.isNaN(start) || Number.isNaN(end) || end < start) return '-';
    const seconds = Math.max(1, Math.round((end - start) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    return `${hours}h ${minutes % 60}m`;
  };

  const columns = useMemo<HyperTableColumn<Row>[]>(() => [
    { id: 'operation', header: 'Task Type', accessorFn: row => row.operation, size: 145, minSize: 125, cell: info => renderTaskType(info.row.original), meta: { title: row => row.id } },
    ...(visibleColumns.includes('namespace') ? [{ id: 'namespace', header: 'Namespace', accessorFn: (row: Row) => row.namespace, size: 190, minSize: 150, maxSize: 320, cell: (info: any) => <span>{info.row.original.namespace}</span>, meta: { kind: 'primary', title: (row: Row) => row.namespace } }] : []),
    ...(visibleColumns.includes('cluster') ? [{ id: 'cluster', header: 'Cluster', accessorFn: (row: Row) => row.cluster, size: 160, minSize: 130, maxSize: 260, cell: (info: any) => <span>{info.row.original.cluster}</span>, meta: { kind: 'secondary', title: (row: Row) => row.cluster } }] : []),
    ...(visibleColumns.includes('repository') ? [{ id: 'repository', header: 'Repository', accessorFn: (row: Row) => row.repository, size: 145, minSize: 120, maxSize: 240, cell: (info: any) => <span>{info.row.original.repository}</span>, meta: { kind: 'secondary', title: (row: Row) => row.repository } }] : []),
    { id: 'status', header: 'Task Status', accessorFn: row => taskStatusLabel(row.status), size: 130, minSize: 110, cell: info => renderTaskStatus(info.row.original), meta: { title: row => isFailedStatus(row.status) ? (row.task.errorMessage || row.task.errorCode || 'Task failed') : taskStatusLabel(row.status) } },
    ...(visibleColumns.includes('restorePoint') ? [{ id: 'restorePoint', header: 'Restore Point', accessorFn: (row: Row) => row.restorePointLabel, size: 220, minSize: 170, maxSize: 360, cell: (info: any) => renderPointState(info.row.original), meta: { title: (row: Row) => row.point ? `${row.restorePointLabel} / ${row.restorePointName}` : row.restorePointLabel } }] : []),
    ...(visibleColumns.includes('createdAt') ? [{ id: 'createdAt', header: 'Started', accessorFn: (row: Row) => row.createdAt, size: 170, minSize: 140, maxSize: 230, cell: (info: any) => <span>{info.row.original.createdAt || '-'}</span>, meta: { kind: 'secondary', title: (row: Row) => row.createdAt } }] : []),
    ...(visibleColumns.includes('completedAt') ? [{ id: 'completedAt', header: 'Completed', accessorFn: (row: Row) => row.completedAt, size: 170, minSize: 140, maxSize: 230, cell: (info: any) => <span>{info.row.original.completedAt || '-'}</span>, meta: { kind: 'secondary', title: (row: Row) => row.completedAt } }] : []),
  ], [visibleColumns]);

  return (
    <motion.div key="dr-tasks" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
      <div className="hbdr-app-workspace-bar">
        <div className="min-w-0">
          <h3 className="hbdr-app-workspace-title">Backup & Recovery Tasks</h3>
          <p className="hbdr-app-workspace-desc">Audit backup, drill, takeover, and restore tasks across outcomes. Default view shows recent tasks.</p>
        </div>
        {clusterContext && <div className="hbdr-app-workspace-cluster">{clusterContext}</div>}
      </div>
      <div className="hbdr-task-ledger-range">
        <div className="hbdr-task-ledger-range-options">
          {timeWindowOptions.map(option => (
            <button key={option.value} type="button" className={timeWindow === option.value ? 'is-active' : ''} onClick={() => setTimeWindow(option.value)}>
              {option.label}
            </button>
          ))}
        </div>
      </div>
      <div className="hbdr-task-ledger-summary">
        <section><strong>{summary.total}</strong><span>Total</span></section>
        <section><strong>{summary.backup}</strong><span>Backups</span></section>
        <section><strong>{summary.recovery}</strong><span>Recovery</span></section>
        <section><strong>{summary.running}</strong><span>Running</span></section>
        <section><strong>{summary.succeeded}</strong><span>Succeeded</span></section>
        <section><strong>{summary.failed}</strong><span>Failed</span></section>
      </div>
      <div className="hbdr-dr-table-card hbdr-task-ledger-table hbdr-task-ledger-table-clean">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={taskQueryFields}
              tags={[]}
              activeTags={[]}
              setActiveTags={() => {}}
              filters={[
                { value: 'backup', label: 'Backup', count: rowsInTimeWindow.filter(row => row.taskType === 'backup').length },
                { value: 'recovery', label: 'Recovery', count: rowsInTimeWindow.filter(row => ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType)).length },
                { value: 'running', label: 'Running', count: rowsInTimeWindow.filter(row => isActiveTaskStatus(row.status)).length },
                { value: 'succeeded', label: 'Succeeded', count: rowsInTimeWindow.filter(row => isSucceededStatus(row.status)).length },
                { value: 'failed', label: 'Failed', count: rowsInTimeWindow.filter(row => isFailedStatus(row.status)).length },
                { value: 'cleared', label: 'Restore Point Cleared', count: rowsInTimeWindow.filter(row => row.restorePointState === 'cleared').length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={taskToolbarColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={async () => {
                try {
                  await load();
                  toast('Backup and recovery tasks refreshed');
                } catch {
                  toast('Failed to refresh backup and recovery tasks');
                }
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={columns}
          data={filteredRows}
          getRowId={row => row.id}
          onRowClick={row => setSelectedTaskId(row.id)}
          getRowClassName={row => selectedTaskId === row.id ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRow ? 1 : 0}
          resetPageOnDataChange={false}
          emptyMessage={query || activeFilters.length ? 'No tasks match the current search.' : `No backup or recovery tasks in ${timeWindowLabel.toLowerCase()}.`}
          className="hbdr-dr-main-table"
        />
      </div>
      <AnimatePresence>
        {selectedRow && (
          <ModalFrame title="Task Details" subtitle={`${selectedRow.operation} / ${selectedRow.namespace}`} icon={<History size={20} />} maxWidthClass="max-w-3xl" onClose={() => setSelectedTaskId(null)}>
            <div className="hbdr-task-detail">
              <div className="hbdr-task-detail-hero">
                <div>
                  <span>{selectedRow.operation}</span>
                  <strong>{selectedRow.namespace}</strong>
                  <em>{selectedRow.id}</em>
                </div>
                <div className="hbdr-task-detail-state">
                  {renderTaskStatus(selectedRow)}
                  {renderPointState(selectedRow)}
                </div>
              </div>

              <div className="hbdr-task-detail-grid">
                <Info label="Cluster" value={selectedRow.cluster} />
                <Info label="Repository" value={selectedRow.repository} />
                <Info label="Progress" value={progressLabel(selectedRow)} />
                <Info label="Duration" value={taskDurationLabel(selectedRow)} />
                <Info label="Started" value={selectedRow.createdAt || '-'} />
                <Info label="Completed" value={selectedRow.completedAt || '-'} />
              </div>

              {(selectedRow.task.errorCode || selectedRow.task.errorMessage || taskFailureDetails(selectedRow.task, selectedTaskEvents).length > 0) && (
                <TaskErrorDetailBlock
                  failure={taskFailureSummary(selectedRow.task, selectedTaskEvents)}
                  details={taskFailureDetails(selectedRow.task, selectedTaskEvents)}
                />
              )}

              <div className="hbdr-task-detail-section">
                <div className="hbdr-task-detail-section-title">
                  <strong>Restore point relationship</strong>
                  <span>{selectedRow.restorePointState === 'available' ? 'Usable recovery artifact' : selectedRow.restorePointState === 'cleared' ? 'Task record retained after artifact cleanup' : selectedRow.restorePointState === 'pending' ? 'Waiting for task completion' : 'No recovery artifact was created'}</span>
                </div>
                <div className="hbdr-task-detail-list">
                  <span>Restore point</span><strong>{selectedRow.point ? selectedRow.restorePointLabel : selectedRow.restorePointState}</strong>
                  <span>Velero backup</span><strong>{selectedRow.restorePointName}</strong>
                  <span>Point type</span><strong>{selectedRow.point?.pointType || '-'}</strong>
                  <span>Point completed</span><strong>{formatLocalDateTime(selectedRow.point?.completedAt) || '-'}</strong>
                </div>
              </div>

              <div className="hbdr-task-detail-section">
                <div className="hbdr-task-detail-section-title">
                  <strong>Task events</strong>
                  <span>{selectedTaskEvents.length} records</span>
                </div>
                <div className="hbdr-task-detail-events">
                  {selectedTaskEvents.length > 0 ? selectedTaskEvents.map(event => {
                    const errors = eventRestoreResultErrors(event);
                    return (
                      <section key={event.id}>
                        <div>
                          <strong className={event.level === 'error' ? 'is-error' : ''}>{(event.reason || event.level || 'event').replace(/_/g, ' ')}</strong>
                          <span>{formatLocalDateTime(event.createdAt) || '-'}</span>
                        </div>
                        <p>{event.message}</p>
                        {errors.length > 0 && (
                          <ul>
                            {errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}
                          </ul>
                        )}
                      </section>
                    );
                  }) : (
                    <p className="hbdr-task-detail-empty">No task events recorded.</p>
                  )}
                </div>
              </div>

              <details className="hbdr-task-detail-raw">
                <summary>Technical payload</summary>
                <pre>{JSON.stringify(selectedRow.task.payload || {}, null, 2)}</pre>
              </details>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
