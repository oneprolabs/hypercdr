import React, { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { AlertCircle, Check, CheckCircle2, ChevronDown, Clock, DatabaseBackup, Eye, Filter, HardDrive, History, Layers, MoreVertical, Play, RefreshCw, Search, Server, Trash2, X } from 'lucide-react';
import { RecoveryWizardModal, type BackupContentResource, type RecoveryWizardConfig } from '../../recovery-wizard-modal';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { SearchBar } from '../../components/search-bar';
import ListToolbarControls from '../../components/list-toolbar-controls';
import { apiGet, apiPost } from '../../api/client';
import { formatLocalDateTime } from '../../lib/date-time';
import type { Cluster } from '../clusters/types';
import type { ApiCluster, ApiStorageRepo } from '../recovery/platform-types';
import type { ApiList, ApiProtectionPlan, ApiRestorePoint, ApiTask, ApiTaskCancelResponse, ApiTaskEvent, ApiTaskResponse } from '../recovery/types';
import { listItems } from '../recovery/types';
import { isActiveTaskStatus, isCompletedTaskStatus, isFailedStatus, taskHasWarning } from '../recovery/task-status';
import {
  TaskErrorDetailBlock, TaskErrorStatus, TaskFinalResult, TaskProcessTimeline, eventRestoreResultErrors,
  formatBytes, formatPercent, restorePointOriginalSize, restorePointStorageSize, taskDetailLabel,
  taskFailureDetails, taskFailureSummary,
} from '../recovery/task-ui';
import {
  ErrorDetailModalFrame, listToolbarQueryFields, matchesColumnFilterToken, namespacesFromPayload,
  parseColumnFilterToken, recoveryCompletedTargetLabel, recoveryCompletedTargetTitle,
  restorePointDisplayLabel, taskRestorePointId,
} from '../applications/application-support';
import {
  latestTaskForRestorePoint, restorePointIsScheduled, restorePointListStatus,
  restorePointNamespaces, taskMatchesRestorePoint, taskStatusLabel,
} from './restore-point-support';

export default function RealRestorePointPage({
  openDr,
  toast,
  workspaceCluster,
  clusterContext,
  initialPoints = [],
  initialClusters = [],
  initialStorageRepos = [],
  initialPlans = [],
  initialTasks = [],
  namespaceFilter = [],
}: {
  openDr: () => void;
  toast: (msg: string) => void;
  workspaceCluster?: Cluster | ApiCluster | null;
  clusterContext?: React.ReactNode;
  initialPoints?: ApiRestorePoint[];
  initialClusters?: ApiCluster[];
  initialStorageRepos?: ApiStorageRepo[];
  initialPlans?: ApiProtectionPlan[];
  initialTasks?: ApiTask[];
  namespaceFilter?: string[];
}) {
  type Row = {
    id: string;
    name: string;
    namespace: string;
    namespaces: string[];
    namespaceLabel: string;
    uid: string;
    pointId: string;
    time: string;
    media: string;
    backupMode: string;
    storage: string;
    size: string;
    sizeTitle: string;
    sizeBytes: number;
    storageSize: string;
    storageSizeTitle: string;
    storageSizeBytes: number;
    sourceCluster: string;
    targetCluster: string;
    targetClusterId: string;
    sourceClusterId: string;
    protectionPlanId?: string;
    status: string;
    veleroBackupName: string;
  };
  const [points, setPoints] = useState<ApiRestorePoint[]>(initialPoints);
  const [clusters, setClusters] = useState<ApiCluster[]>(initialClusters);
  const [storageRepos, setStorageRepos] = useState<ApiStorageRepo[]>(initialStorageRepos);
  const [plans, setPlans] = useState<ApiProtectionPlan[]>(initialPlans);
  const [tasks, setTasks] = useState<ApiTask[]>(initialTasks);
  const [recoverySubmitting, setRecoverySubmitting] = useState(false);
  const [loading, setLoading] = useState(initialPoints.length === 0);
  const [selectedRestorePoints, setSelectedRestorePoints] = useState<string[]>([]);
  const [activeRestorePointId, setActiveRestorePointId] = useState('');
  const [restoreAction, setRestoreAction] = useState<{ mode: 'drill' | 'takeover'; row: Row; config: RecoveryWizardConfig } | null>(null);
  const [recoveryTaskDetail, setRecoveryTaskDetail] = useState<{ row: Row; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null>(null);
  const [recoveryTaskDetailEvents, setRecoveryTaskDetailEvents] = useState<ApiTaskEvent[]>([]);
  const [deleteDialog, setDeleteDialog] = useState<{
    rows: Row[];
    status: 'confirm' | 'submitting' | 'deleting' | 'succeeded' | 'failed';
    taskIds: string[];
    message: string;
  } | null>(null);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('namespace');
  const [quickNamespaceFilters, setQuickNamespaceFilters] = useState<string[]>([]);
  const [draftNamespaceFilters, setDraftNamespaceFilters] = useState<string[]>([]);
  const [namespaceFilterSearch, setNamespaceFilterSearch] = useState('');
  const [quickNamespaceMenuOpen, setQuickNamespaceMenuOpen] = useState(false);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState<string[]>(['time', 'storage', 'size', 'storageSize', 'media', 'backupMode', 'task']);
  const [restoreBulkMenuOpen, setRestoreBulkMenuOpen] = useState(false);
  const visibleRecoveryTask = (restorePointId: string) => latestTaskForRestorePoint(tasks, restorePointId);

  const load = async () => {
    const [pointRes, clusterRes, storageRes, planRes, taskRes] = await Promise.all([
      apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
      apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
      apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
      apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
      apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
    ]);
    setPoints(listItems(pointRes));
    setClusters(listItems(clusterRes));
    setStorageRepos(listItems(storageRes));
    setPlans(listItems(planRes));
    setTasks(listItems(taskRes));
    setLoading(false);
  };

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        if (!cancelled) await load();
      } catch {
        if (!cancelled) setLoading(false);
        toast('Failed to refresh restore points');
      }
    };
    refresh();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  useEffect(() => {
    const nextNamespaces = Array.from(new Set(namespaceFilter.map(item => item.trim()).filter(Boolean)));
    setSelectedRestorePoints([]);
    setActiveRestorePointId('');
    setQuickNamespaceFilters(nextNamespaces);
    setDraftNamespaceFilters(nextNamespaces);
    setNamespaceFilterSearch('');
    setQuickNamespaceMenuOpen(false);
  }, [namespaceFilter.join('\n')]);

  useEffect(() => {
    if (!recoveryTaskDetail?.task.id) {
      setRecoveryTaskDetailEvents([]);
      return;
    }
    let cancelled = false;
    const loadEvents = async () => {
      try {
        const [eventResult, taskResult] = await Promise.all([
          apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${recoveryTaskDetail.task.id}/events`),
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
        ]);
        if (!cancelled) {
          setRecoveryTaskDetailEvents(listItems(eventResult));
          const latest = listItems(taskResult).find(task => task.id === recoveryTaskDetail.task.id);
          if (latest) setRecoveryTaskDetail(prev => prev?.task.id === latest.id ? { ...prev, task: latest } : prev);
        }
      } catch {
        // Preserve the last successful live snapshot and retry on the next interval.
      }
    };
    void loadEvents();
    const timer = window.setInterval(loadEvents, 2000);
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [recoveryTaskDetail?.task.id, recoveryTaskDetail?.task.status]);

  useEffect(() => {
    setSelectedRestorePoints([]);
    setActiveRestorePointId('');
  }, [quickNamespaceFilters.join('\n')]);

  useEffect(() => {
    if (!deleteDialog || deleteDialog.status !== 'deleting' || deleteDialog.taskIds.length === 0) return;
    const deleteTasks = deleteDialog.taskIds
      .map(id => tasks.find(task => task.id === id))
      .filter(Boolean) as ApiTask[];
    if (deleteTasks.length === 0) return;
    const failed = deleteTasks.find(task => isFailedStatus(task.status));
    if (failed) {
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'failed',
        message: failed.errorMessage || failed.errorCode || 'Velero delete failed.',
      } : prev);
      return;
    }
    if (deleteTasks.length === deleteDialog.taskIds.length && deleteTasks.every(task => isCompletedTaskStatus(task.status))) {
      setSelectedRestorePoints([]);
      setActiveRestorePointId('');
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'succeeded',
        message: 'Restore points deleted from Velero and the backup repository.',
      } : prev);
      void load();
    }
  }, [tasks, deleteDialog]);

  const rows: Row[] = points.filter(point => !workspaceCluster?.id || point.sourceClusterId === workspaceCluster.id).map(point => {
    const sourceCluster = clusters.find(item => item.id === point.sourceClusterId);
    const plan = plans.find(item => item.id === point.protectionPlanId || item.appId === point.appId || Boolean(point.appId && item.appIds?.includes(point.appId)));
    const targetCluster = clusters.find(item => item.id === plan?.targetClusterId) || clusters.find(item => item.id !== point.sourceClusterId);
    const repo = storageRepos.find(item => item.id === point.storageRepoId);
    const originalSize = restorePointOriginalSize(point);
    const storageSize = restorePointStorageSize(point);
    const namespaces = restorePointNamespaces(point);
    const primaryNamespace = namespaces[0] || point.sourceNamespace || point.veleroBackupName;
    const namespaceLabel = namespaces.length > 0 ? namespaces.join(', ') : point.veleroBackupName;
    return {
      id: point.id,
      name: namespaceLabel,
      namespace: primaryNamespace,
      namespaces,
      namespaceLabel,
      uid: point.id.slice(0, 8),
      pointId: point.id,
      time: restorePointDisplayLabel(point),
      media: point.pointType === 'backup' ? 'Remote Snapshot' : point.pointType,
      backupMode: restorePointIsScheduled(point) ? 'Automatic' : 'Manual',
      storage: repo?.name || point.backupStorageName || 'default',
      size: originalSize.label,
      sizeTitle: originalSize.title,
      sizeBytes: originalSize.bytes,
      storageSize: storageSize.label,
      storageSizeTitle: storageSize.title,
      storageSizeBytes: storageSize.bytes,
      sourceCluster: sourceCluster?.name || 'Unknown cluster',
      targetCluster: targetCluster?.name || sourceCluster?.name || 'Target Cluster',
      targetClusterId: targetCluster?.id || sourceCluster?.id || point.sourceClusterId,
      sourceClusterId: point.sourceClusterId,
      protectionPlanId: point.protectionPlanId,
      status: restorePointListStatus(point),
      veleroBackupName: point.veleroBackupName,
    };
  }).sort((a, b) => (b.time || '').localeCompare(a.time || ''));
  const availableRows = rows.filter(row => row.status === 'available');
  const namespaceOptions = Array.from(availableRows.reduce((acc, row) => {
    const namespaces = row.namespaces.length ? row.namespaces : [row.namespace].filter(Boolean);
    namespaces.forEach(namespace => {
      acc.set(namespace, (acc.get(namespace) || 0) + 1);
    });
    return acc;
  }, new Map<string, number>()))
    .map(([namespace, count]) => ({ namespace, count }))
    .sort((a, b) => a.namespace.localeCompare(b.namespace));
  const quickNamespaceFilterSet = new Set(quickNamespaceFilters);
  const quickNamespaceFilterLabel = quickNamespaceFilters.length === 0
    ? 'All namespaces'
    : quickNamespaceFilters.length === 1
      ? quickNamespaceFilters[0]
      : `${quickNamespaceFilters.length} namespaces`;
  const namespaceFilterSearchTerm = namespaceFilterSearch.trim().toLowerCase();
  const visibleNamespaceOptions = namespaceOptions.filter(option => !namespaceFilterSearchTerm || option.namespace.toLowerCase().includes(namespaceFilterSearchTerm));
  const restorePointToolbarColumns = [
    { value: 'time', label: 'Restore Point' },
    { value: 'storage', label: 'Repository' },
    { value: 'size', label: 'Original Size' },
    { value: 'storageSize', label: 'Storage Size' },
    { value: 'media', label: 'Type' },
    { value: 'backupMode', label: 'Backup Mode' },
    { value: 'task', label: 'Recovery Task' },
  ];
  const restorePointQueryFields = listToolbarQueryFields([
    { value: 'namespace', label: 'Namespace' },
    { value: 'backup', label: 'Backup Name' },
    { value: 'cluster', label: 'Cluster' },
  ], restorePointToolbarColumns, visibleColumns);
  const openQuickNamespaceFilter = () => {
    setDraftNamespaceFilters(quickNamespaceFilters);
    setNamespaceFilterSearch('');
    setQuickNamespaceMenuOpen(true);
  };
  const applyQuickNamespaceFilter = () => {
    setQuickNamespaceFilters(Array.from(new Set(draftNamespaceFilters)));
    setQuickNamespaceMenuOpen(false);
  };
  const filteredRows = availableRows.filter(row => {
    const rowNamespaces = row.namespaces.length ? row.namespaces : [row.namespace].filter(Boolean);
    if (quickNamespaceFilterSet.size > 0 && !rowNamespaces.some(namespace => quickNamespaceFilterSet.has(namespace))) return false;
    const q = query.trim().toLowerCase();
    const searchableValues: Record<string, string> = {
      namespace: row.namespaceLabel,
      backup: row.veleroBackupName || row.pointId,
      cluster: row.sourceCluster,
      time: row.time,
      storage: row.storage,
      size: row.size,
      storageSize: row.storageSize,
      media: row.media,
      backupMode: row.backupMode,
      task: visibleRecoveryTask(row.id)?.status || 'No recovery task',
    };
    const matchesQuery = !q || (searchableValues[queryField] || Object.values(searchableValues).join(' ')).toLowerCase().includes(q);
    const latestTask = visibleRecoveryTask(row.id);
    const matchesFilters = activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => searchableValues[field] || '');
      if (filter === 'remote') return row.media === 'Remote Snapshot';
      if (filter === 'local') return row.media === 'Local Snapshot';
      if (filter === 'manual') return row.backupMode === 'Manual';
      if (filter === 'automatic') return row.backupMode === 'Automatic';
      if (filter === 'running') return isActiveTaskStatus(latestTask?.status);
      if (filter === 'failed') return isFailedStatus(latestTask?.status);
      return true;
    });
    return matchesQuery && matchesFilters;
  });
  const selectedRestorePointId = activeRestorePointId || selectedRestorePoints.at(-1) || '';
  const selectedRow = selectedRestorePointId
    ? rows.find(row => row.id === selectedRestorePointId)
      || rows.find(row => selectedRestorePoints.includes(row.id))
      || null
    : rows.find(row => selectedRestorePoints.includes(row.id)) || null;
  const selectedRowRecoveryTask = selectedRow ? visibleRecoveryTask(selectedRow.id) : undefined;
  const activeRecoveryTaskForSelectedRow = isActiveTaskStatus(selectedRowRecoveryTask?.status)
    ? selectedRowRecoveryTask
    : undefined;
  const activeRecoveryTaskIds = tasks
    .filter(task => ['restore', 'drill', 'takeover'].includes(task.type) && isActiveTaskStatus(task.status))
    .map(task => task.id)
    .filter(Boolean)
    .sort();
  const activeRecoveryTaskKey = activeRecoveryTaskIds.join('|');

  useEffect(() => {
    const ids = activeRecoveryTaskKey ? activeRecoveryTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const idSet = new Set(ids);
    const pollActiveRecoveryTasks = async () => {
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
        setPoints(listItems(pointRes));
      } catch {
        // Keep the current restore point list stable if a status poll fails.
      }
    };
    void pollActiveRecoveryTasks();
    const timer = window.setInterval(pollActiveRecoveryTasks, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeRecoveryTaskKey]);

  useEffect(() => {
    if (!recoveryTaskDetail?.task.id) return;
    const latest = tasks.find(task => task.id === recoveryTaskDetail.task.id);
    if (!latest) return;
    const current = recoveryTaskDetail.task;
    if (
      latest.status === current.status
      && Number(latest.progress || 0) === Number(current.progress || 0)
      && latest.completedAt === current.completedAt
      && latest.errorCode === current.errorCode
      && latest.errorMessage === current.errorMessage
    ) return;
    setRecoveryTaskDetail(prev => {
      if (!prev || prev.task.id !== latest.id) return prev;
      return { ...prev, task: { ...prev.task, ...latest } };
    });
  }, [
    tasks,
    recoveryTaskDetail?.task.id,
    recoveryTaskDetail?.task.status,
    recoveryTaskDetail?.task.progress,
    recoveryTaskDetail?.task.completedAt,
    recoveryTaskDetail?.task.errorCode,
    recoveryTaskDetail?.task.errorMessage,
  ]);
  const hasRestorePointSelection = selectedRestorePoints.some(id => rows.some(row => row.id === id));
  const selectedDisabled = !hasRestorePointSelection || Boolean(activeRecoveryTaskForSelectedRow);
  const allVisibleSelected = filteredRows.length > 0 && filteredRows.every(row => selectedRestorePoints.includes(row.id));
  const toggleVisibleRows = () => {
    if (allVisibleSelected) {
      setSelectedRestorePoints([]);
      setActiveRestorePointId('');
      return;
    }
    const ids = filteredRows.map(row => row.id);
    setSelectedRestorePoints(ids);
    setActiveRestorePointId(ids[0] || '');
  };
  const toggleSelectedRestorePoint = (id: string) => {
    setSelectedRestorePoints(prev => {
      if (prev.includes(id)) {
        const next = prev.filter(item => item !== id);
        if (activeRestorePointId === id) setActiveRestorePointId(next.at(-1) || '');
        return next;
      }
      setActiveRestorePointId(id);
      return [...prev, id];
    });
  };
  const clusterOptions = clusters.map((cluster, index) => ({
    id: cluster.id,
    name: cluster.name,
    region: cluster.connectionStatus === 'online' ? 'connected' : 'disconnected',
    version: cluster.kubeVersion || 'unknown',
    isCurrent: index === 0,
    storageClasses: cluster.storageClasses,
  }));
  const repositoryOptions = storageRepos.map(repo => ({
    id: repo.id,
    name: repo.name,
    type: repo.type,
    endpoint: repo.endpoint || '',
    bucket: repo.bucket || '',
  }));
  const pointsForRow = (row: Row) => [
    { id: row.id, title: 'Backup task', time: row.time, type: row.media, status: 'Available' },
  ];
  const buildConfig = (mode: 'drill' | 'takeover', row: Row): RecoveryWizardConfig => {
    const currentClusterName = clusters.find(cluster => cluster.id === row.sourceClusterId)?.name || 'Source Cluster';
    const targetCluster = row.targetCluster || currentClusterName;
    const targetMode = targetCluster === currentClusterName
      ? mode === 'drill' ? 'sandbox' : 'inPlace'
      : 'crossCluster';
    return {
      pointId: row.id,
      sourceType: row.media.toLowerCase().includes('local') ? 'snapshot' : 'export',
      targetMode,
      targetCluster,
      namespaceMode: mode === 'takeover' ? 'original' : 'generated',
      targetNamespace: mode === 'takeover' ? row.namespace : `${row.namespace}-drill`,
      restoreMode: 'full',
      artifactMode: 'all',
      conflictPolicy: mode === 'takeover' ? 'replace' : 'skip',
      originalNamespaceConfirmed: false,
      includeClusterScoped: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : targetMode === 'sandbox' ? 'drill' : 'none',
      storageProfileMode: 'original',
      alternateProfileId: '',
      preflightChecks: true,
      autoStartValidation: mode === 'drill',
      notes: mode === 'drill'
        ? 'Validate service startup, storage attachment, and namespace isolation after recovery.'
        : 'Confirm traffic cutover and production freeze before takeover.',
      forceProceed: false,
      includedResources: [],
      excludedResources: [],
	  resourceSelection: { mode: 'all', namespaceScoped: [], clusterScoped: [] },
      storageClassMappings: {},
      imageMappings: {},
      waitForWorkloads: true,
      runValidation: mode === 'drill',
      forceStart: false,
      contentCatalogLoaded: false,
      persistentDataExpected: false,
    };
  };
  const startRestoreAction = (mode: 'drill' | 'takeover') => {
    const row = selectedRow || rows.find(item => selectedRestorePoints.includes(item.id)) || null;
    if (!row) {
      toast('Select a restore point first');
      return;
    }
    const activeTask = tasks.find(task => ['restore', 'drill', 'takeover'].includes(task.type)
      && isActiveTaskStatus(task.status)
      && taskMatchesRestorePoint(task, row.id));
    if (activeTask) {
      const label = activeTask.type === 'drill' ? 'drill' : activeTask.type === 'takeover' ? 'takeover' : 'recovery';
      toast(`A ${label} task is already running for this restore point`);
      return;
    }
    setActiveRestorePointId(row.id);
    setRestoreAction({ mode, row, config: buildConfig(mode, row) });
  };
  const openDeleteRestorePointDialog = () => {
    const targetRows = selectedRestorePoints
      .map(id => rows.find(row => row.id === id))
      .filter(Boolean) as Row[];
    if (targetRows.length === 0) {
      toast('Select restore points to delete');
      return;
    }
    setRestoreBulkMenuOpen(false);
    setDeleteDialog({
      rows: targetRows,
      status: 'confirm',
      taskIds: [],
      message: 'This will create Velero DeleteBackupRequest objects on the source cluster.',
    });
  };
  const submitRestorePointDelete = async () => {
    if (!deleteDialog || deleteDialog.status !== 'confirm') return;
    const ids = deleteDialog.rows.map(row => row.id);
    setDeleteDialog(prev => prev ? { ...prev, status: 'submitting', message: 'Submitting delete request to the source cluster agent...' } : prev);
    try {
      const result = await apiPost<{ task?: ApiTask; tasks?: ApiTask[]; warning?: string }>('/api/v1/restore-points/delete', { restorePointIds: ids });
      const nextTasks = result.tasks?.length ? result.tasks : result.task ? [result.task] : [];
      await load();
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'deleting',
        taskIds: nextTasks.map(task => task.id).filter(Boolean),
        message: result.warning || 'Waiting for Velero to delete the selected backups...',
      } : prev);
      toast(result.warning || `Delete requested for ${ids.length} restore point${ids.length === 1 ? '' : 's'}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'unknown error';
      setDeleteDialog(prev => prev ? { ...prev, status: 'failed', message } : prev);
      toast('Failed to delete restore points: ' + message);
    }
  };
  const renderRestorePageTask = (row: Row) => {
    const task = visibleRecoveryTask(row.id);
    if (!task) return <span className="hbdr-dr-task-neutral">No recovery task</span>;
    const label = task.type === 'drill' ? 'Drill' : task.type === 'takeover' ? 'Takeover' : 'Restore';
    if (task.status === 'succeeded') {
      const targetNamespace = String(task.payload?.targetNamespace || row.namespace);
      const targetCluster = clusters.find(cluster => cluster.id === task.clusterId)?.name
        || String(task.payload?.targetCluster || row.targetCluster || '');
      const restorePointLabel = row.time || row.veleroBackupName || row.pointId || 'restore point';
      const completedTitle = recoveryCompletedTargetTitle(restorePointLabel, task.completedAt, targetCluster, targetNamespace, `${label} complete`);
      const targetLabel = recoveryCompletedTargetLabel(targetCluster, targetNamespace);
      return <span className="hbdr-recovery-task-complete"><strong title={completedTitle}>[{restorePointLabel}] {label.toLowerCase()} complete</strong><em title={completedTitle}>{targetLabel}</em></span>;
    }
    if (task.status === 'failed') {
      const failure = taskFailureSummary(task);
      return (
        <TaskErrorStatus
          code={failure.code}
          title={failure.title}
          description={failure.description}
          detail={failure.fullText}
          onClick={event => {
            event.stopPropagation();
            setActiveRestorePointId(row.id);
            setRecoveryTaskDetail({ row, task, failure });
          }}
        />
      );
    }
    const progress = Math.max(0, Math.min(100, task.progress || 0));
    return (
      <span className="hbdr-dr-progress-cell hbdr-recovery-task-progress is-syncing">
        <em className="hbdr-sync-label">{label} running {formatPercent(progress)}%</em>
        <i><b style={{ width: `${progress}%` }} /></i>
        <small>{String(task.payload?.targetNamespace || row.namespace)}</small>
      </span>
    );
  };
  const restorePointTableColumns = useMemo<HyperTableColumn<Row>[]>(() => [
    {
      id: 'select',
      header: () => (
        <input
          type="checkbox"
          checked={allVisibleSelected}
          onClick={event => event.stopPropagation()}
          onChange={toggleVisibleRows}
        />
      ),
      cell: info => (
        <input
          type="checkbox"
          checked={selectedRestorePoints.includes(info.row.original.id)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedRestorePoint(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Namespace',
      accessorFn: row => row.namespaceLabel,
      size: 270,
      minSize: 190,
      maxSize: 520,
      cell: info => {
        const row = info.row.original;
        const namespaces = row.namespaces.length ? row.namespaces : [row.namespaceLabel].filter(Boolean);
        return (
          <div className="hbdr-dr-name-cell">
            <div className="hbdr-dr-namespace-icon"><Layers size={18} /></div>
            <div>
              {namespaces.length > 1 ? (
                <div className="hbdr-merged-namespace-list">
                  {namespaces.map(namespace => <p key={namespace} className="hbdr-dr-app-name">{namespace}</p>)}
                </div>
              ) : (
                <p className="hbdr-dr-app-name">{row.namespaceLabel}</p>
              )}
            </div>
          </div>
        );
      },
      meta: { title: row => `${row.namespaceLabel} / ${row.pointId}` },
    },
    ...(visibleColumns.includes('time') ? [{ id: 'time', header: 'Restore Point', accessorFn: (row: Row) => row.time, size: 190, minSize: 160, maxSize: 260, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.time}</span>, meta: { title: (row: Row) => row.time } }] : []),
    ...(visibleColumns.includes('storage') ? [{ id: 'storage', header: 'Repository', accessorFn: (row: Row) => row.storage, size: 150, minSize: 120, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.storage}</span>, meta: { title: (row: Row) => row.storage } }] : []),
    ...(visibleColumns.includes('size') ? [{ id: 'size', header: 'Original Size', accessorFn: (row: Row) => row.sizeBytes, size: 150, minSize: 130, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.size}</span>, meta: { title: (row: Row) => row.sizeTitle } }] : []),
    ...(visibleColumns.includes('storageSize') ? [{ id: 'storageSize', header: 'Storage Size', accessorFn: (row: Row) => row.storageSizeBytes, size: 150, minSize: 130, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.storageSize}</span>, meta: { title: (row: Row) => row.storageSizeTitle } }] : []),
    ...(visibleColumns.includes('media') ? [{ id: 'media', header: 'Type', accessorFn: (row: Row) => row.media, size: 145, minSize: 120, maxSize: 220, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.media}</span>, meta: { title: (row: Row) => row.media } }] : []),
    ...(visibleColumns.includes('backupMode') ? [{ id: 'backupMode', header: 'Backup Mode', accessorFn: (row: Row) => row.backupMode, size: 140, minSize: 120, maxSize: 190, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.backupMode}</span>, meta: { title: (row: Row) => row.backupMode } }] : []),
    ...(visibleColumns.includes('task') ? [{ id: 'task', header: 'Recovery Task', accessorFn: (row: Row) => visibleRecoveryTask(row.id)?.status || 'No recovery task', size: 210, minSize: 170, maxSize: 360, cell: (info: any) => renderRestorePageTask(info.row.original) }] : []),
  ], [allVisibleSelected, selectedRestorePoints, visibleColumns, tasks]);

  return (
    <motion.div key="restore" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
      <div className="hbdr-app-workspace-bar">
        <div className="min-w-0">
          <h3 className="hbdr-app-workspace-title">Restore Points</h3>
          <p className="hbdr-app-workspace-desc">Browse recovery points and launch drill or takeover.</p>
        </div>
        {clusterContext && <div className="hbdr-app-workspace-cluster">{clusterContext}</div>}
      </div>
      <div className="hbdr-dr-table-card hbdr-restore-point-table-list">
        {namespaceOptions.length > 0 && (
          <div className="hbdr-restore-quick-filter">
            <span>Namespace</span>
            <div className="hbdr-restore-namespace-menu">
              <button type="button" onClick={() => quickNamespaceMenuOpen ? setQuickNamespaceMenuOpen(false) : openQuickNamespaceFilter()}>
                <strong>{quickNamespaceFilterLabel}</strong>
                <ChevronDown size={14} className={quickNamespaceMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
              </button>
              <AnimatePresence>
                {quickNamespaceMenuOpen && (
                  <>
                    <div className="fixed inset-0 z-30" onClick={() => setQuickNamespaceMenuOpen(false)} />
                    <motion.div
                      initial={{ opacity: 0, y: 6, scale: 0.98 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 6, scale: 0.98 }}
                      className="hbdr-restore-namespace-menu-panel"
                    >
                      <div className="hbdr-restore-namespace-menu-search">
                        <Search size={13} />
                        <input value={namespaceFilterSearch} onChange={event => setNamespaceFilterSearch(event.target.value)} placeholder="Filter namespace" autoFocus />
                      </div>
                      <div className="hbdr-restore-namespace-menu-list">
                      {visibleNamespaceOptions.map(option => {
                        const selected = draftNamespaceFilters.includes(option.namespace);
                        return (
                          <button
                            key={option.namespace}
                            type="button"
                            className={selected ? 'is-active' : ''}
                            onClick={() => setDraftNamespaceFilters(prev => selected ? prev.filter(item => item !== option.namespace) : [...prev, option.namespace])}
                          >
                            <i aria-hidden="true">{selected && <Check size={12} />}</i>
                            <span>{option.namespace}</span>
                          </button>
                        );
                      })}
                      {visibleNamespaceOptions.length === 0 && <div className="hbdr-restore-namespace-menu-empty">No namespaces found</div>}
                      </div>
                      <div className="hbdr-restore-namespace-menu-actions">
                        <button type="button" onClick={() => setDraftNamespaceFilters([])}>Clear</button>
                        <button type="button" onClick={applyQuickNamespaceFilter}>Apply</button>
                      </div>
                    </motion.div>
                  </>
                )}
              </AnimatePresence>
            </div>
          </div>
        )}
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group hbdr-dr-action-group-managed hbdr-dr-action-group-run">
              <button disabled={selectedDisabled} title={!selectedRow ? 'Select one restore point' : activeRecoveryTaskForSelectedRow ? 'A recovery task is already running' : undefined} onClick={() => startRestoreAction('drill')} className="hbdr-dr-action-primary">Drill</button>
              <button disabled={selectedDisabled} title={!selectedRow ? 'Select one restore point' : activeRecoveryTaskForSelectedRow ? 'A recovery task is already running' : undefined} onClick={() => startRestoreAction('takeover')} className="hbdr-dr-action-danger">Takeover</button>
              <div className="relative">
                <button onClick={() => setRestoreBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={restoreBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {restoreBulkMenuOpen && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setRestoreBulkMenuOpen(false)} />
                      <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.96 }}
                        className="absolute right-0 top-11 z-40 w-52 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"
                      >
                        <button
                          disabled={selectedRestorePoints.length === 0}
                          onClick={() => {
                            setSelectedRestorePoints([]);
                            setActiveRestorePointId('');
                            setRestoreBulkMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                        >
                          <X size={14} className="text-slate-400" />Clear Selection
                        </button>
                        <button
                          disabled={selectedRestorePoints.length === 0}
                          onClick={openDeleteRestorePointDialog}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                        >
                          <Trash2 size={14} className="text-rose-500" />Delete Restore Points
                        </button>
                        <button
                          onClick={async () => {
                            setRestoreBulkMenuOpen(false);
                            try {
                              await load();
                              toast('Restore point list refreshed');
                            } catch {
                              toast('Failed to refresh restore points');
                            }
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50"
                        >
                          <RefreshCw size={14} className="text-blue-500" />Refresh
                        </button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={restorePointQueryFields}
              tags={[]}
              activeTags={[]}
              setActiveTags={() => {}}
              filters={[
                { value: 'remote', label: 'Remote Snapshot', count: availableRows.filter(row => row.media === 'Remote Snapshot').length },
                { value: 'local', label: 'Local Snapshot', count: availableRows.filter(row => row.media === 'Local Snapshot').length },
                { value: 'manual', label: 'Manual', count: availableRows.filter(row => row.backupMode === 'Manual').length },
                { value: 'automatic', label: 'Automatic', count: availableRows.filter(row => row.backupMode === 'Automatic').length },
                { value: 'running', label: 'Recovery Running', count: availableRows.filter(row => isActiveTaskStatus(visibleRecoveryTask(row.id)?.status)).length },
                { value: 'failed', label: 'Recovery Failed', count: availableRows.filter(row => isFailedStatus(visibleRecoveryTask(row.id)?.status)).length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={restorePointToolbarColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={async () => {
                setSelectedRestorePoints([]);
                setActiveRestorePointId('');
                try {
                  await load();
                  toast('Restore point list refreshed');
                } catch {
                  toast('Failed to refresh restore points');
                }
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={restorePointTableColumns}
          data={filteredRows}
          getRowId={row => row.id}
          onRowClick={row => {
            setActiveRestorePointId(row.id);
            setSelectedRestorePoints(prev => prev.includes(row.id) ? prev : [...prev, row.id]);
          }}
          getRowClassName={row => selectedRestorePoints.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRestorePoints.length}
          emptyMessage={loading ? 'Loading restore points...' : query || activeFilters.length ? 'No restore points match the current search.' : 'No restore points available.'}
          className="hbdr-dr-main-table hbdr-restore-point-hyper-table"
        />
      </div>
      <AnimatePresence>
        {deleteDialog && (
          <motion.div
            className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/35 px-4 py-8 backdrop-blur-sm"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <motion.div
              initial={{ opacity: 0, y: 18, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 18, scale: 0.97 }}
              className="w-full max-w-xl overflow-hidden rounded-2xl border border-slate-100 bg-white shadow-2xl shadow-slate-900/20"
            >
              <div className="flex items-start gap-3 border-b border-slate-100 px-6 py-5">
                <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl ${deleteDialog.status === 'failed' ? 'bg-rose-50 text-rose-600' : deleteDialog.status === 'succeeded' ? 'bg-emerald-50 text-emerald-600' : 'bg-amber-50 text-amber-600'}`}>
                  {deleteDialog.status === 'succeeded' ? <CheckCircle2 size={20} /> : deleteDialog.status === 'failed' ? <AlertCircle size={20} /> : <Trash2 size={20} />}
                </span>
                <div className="min-w-0 flex-1">
                  <h4 className="text-base font-black text-slate-900">
                    {deleteDialog.status === 'succeeded' ? 'Restore Points Deleted' : deleteDialog.status === 'failed' ? 'Delete Failed' : 'Delete Restore Points'}
                  </h4>
                  <p className="mt-1 text-sm font-medium leading-5 text-slate-500">{deleteDialog.message}</p>
                </div>
                <button
                  type="button"
                  disabled={deleteDialog.status === 'submitting'}
                  onClick={() => setDeleteDialog(null)}
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-600 disabled:cursor-wait disabled:opacity-40"
                  aria-label="Close delete restore points dialog"
                >
                  <X size={17} />
                </button>
              </div>
              <div className="space-y-4 px-6 py-5">
                <div className="rounded-xl border border-slate-100 bg-slate-50/70">
                  <div className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
                    <span className="text-xs font-black uppercase text-slate-500">Selected Restore Points</span>
                    <span className="rounded-full bg-white px-2.5 py-1 text-[11px] font-black text-slate-500">{deleteDialog.rows.length}</span>
                  </div>
                  <div className="max-h-52 divide-y divide-slate-100 overflow-auto">
                    {deleteDialog.rows.map(row => (
                      <div key={row.id} className="grid grid-cols-[1fr_auto] gap-3 px-4 py-3">
                        <div className="min-w-0">
                          <p className="truncate text-sm font-black text-slate-800">{row.name}</p>
                          <p className="mt-0.5 truncate text-xs font-semibold text-slate-400">{row.veleroBackupName}</p>
                        </div>
                        <div className="text-right">
                          <p className="text-xs font-bold text-slate-500">{row.backupMode}</p>
                          <p className="mt-0.5 text-[11px] font-semibold text-slate-400">{row.time}</p>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                {deleteDialog.status === 'deleting' && (
                  <div className="rounded-xl border border-blue-100 bg-blue-50/60 px-4 py-3">
                    <div className="flex items-center gap-2 text-sm font-black text-blue-700">
                      <RefreshCw size={15} className="animate-spin" />
                      Velero deletion is running
                    </div>
                    <div className="mt-3 space-y-2">
                      {deleteDialog.taskIds.length === 0 ? (
                        <p className="text-xs font-semibold text-blue-600">Delete task has been submitted. Waiting for task status...</p>
                      ) : deleteDialog.taskIds.map(taskId => {
                        const task = tasks.find(item => item.id === taskId);
                        const progress = Math.max(0, Math.min(100, task?.progress || 0));
                        return (
                          <div key={taskId}>
                            <div className="mb-1 flex items-center justify-between text-[11px] font-bold text-blue-700">
                              <span>{task ? taskStatusLabel(task.status) : 'Waiting'}</span>
                              <span>{progress}%</span>
                            </div>
                            <div className="h-1.5 overflow-hidden rounded-full bg-white">
                              <div className="h-full rounded-full bg-blue-500 transition-all" style={{ width: `${progress}%` }} />
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
              <div className="flex items-center justify-end gap-3 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
                {deleteDialog.status === 'confirm' && (
                  <>
                    <button type="button" onClick={() => setDeleteDialog(null)} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-500 transition-colors hover:bg-white">Cancel</button>
                    <button type="button" onClick={submitRestorePointDelete} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-5 py-2 text-sm font-black text-white shadow-lg shadow-rose-100 transition-colors hover:bg-rose-700">
                      <Trash2 size={15} />Delete from Velero
                    </button>
                  </>
                )}
                {deleteDialog.status === 'submitting' && (
                  <button type="button" disabled className="inline-flex cursor-wait items-center gap-2 rounded-xl bg-slate-200 px-5 py-2 text-sm font-black text-slate-500">
                    <RefreshCw size={15} className="animate-spin" />Submitting
                  </button>
                )}
                {(deleteDialog.status === 'deleting' || deleteDialog.status === 'succeeded' || deleteDialog.status === 'failed') && (
                  <button type="button" onClick={() => setDeleteDialog(null)} className="rounded-xl bg-slate-900 px-5 py-2 text-sm font-black text-white transition-colors hover:bg-slate-700">
                    Close
                  </button>
                )}
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
      <AnimatePresence>
        {recoveryTaskDetail && (
          <ErrorDetailModalFrame
            title={`${taskDetailLabel(recoveryTaskDetail.task.type)} Task Details`}
            onClose={() => setRecoveryTaskDetail(null)}
          >
            {(() => {
              const failure = recoveryTaskDetail.failure || taskFailureSummary(recoveryTaskDetail.task, recoveryTaskDetailEvents);
              const details = taskFailureDetails(recoveryTaskDetail.task, recoveryTaskDetailEvents);
              return (
                <div className="hbdr-task-detail">
                  <TaskProcessTimeline task={recoveryTaskDetail.task} events={recoveryTaskDetailEvents} />
                  {(isFailedStatus(recoveryTaskDetail.task.status) || taskHasWarning(recoveryTaskDetail.task)) && (
                    <TaskErrorDetailBlock failure={failure} details={details} onRetry={isFailedStatus(recoveryTaskDetail.task.status) ? async () => {
                      try {
                        const retried = await apiPost<ApiTask>(`/api/v1/tasks/${recoveryTaskDetail.task.id}/retry`, {});
                        setTasks(prev => [retried, ...prev.filter(task => task.id !== retried.id)]);
                        setRecoveryTaskDetail(null);
                        toast('Recovery retry submitted');
                        void load();
                      } catch (error) {
                        toast('Failed to retry recovery: ' + (error instanceof Error ? error.message : 'unknown error'));
                      }
                    } : undefined} />
                  )}
                  <TaskFinalResult task={recoveryTaskDetail.task} events={recoveryTaskDetailEvents} />

                  <details className="hbdr-task-detail-raw">
                    <summary>Technical payload</summary>
                    <pre>{JSON.stringify(recoveryTaskDetail.task.payload || {}, null, 2)}</pre>
                  </details>
                </div>
              );
            })()}
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>
      {restoreAction && (
        <RecoveryWizardModal
          open
          mode={restoreAction.mode}
          app={{ name: restoreAction.row.namespaceLabel, namespace: restoreAction.row.namespace, storage: restoreAction.row.storage, targetCluster: restoreAction.row.targetCluster }}
          profile={{ uid: restoreAction.row.uid }}
          currentClusterName={clusters.find(cluster => cluster.id === restoreAction.row.sourceClusterId)?.name || 'Source Cluster'}
          points={pointsForRow(restoreAction.row)}
          clusterOptions={clusterOptions}
          repositoryOptions={repositoryOptions}
          config={restoreAction.config}
          setConfig={updater => {
            setRestoreAction(prev => {
              if (!prev) return prev;
              const nextConfig = typeof updater === 'function' ? updater(prev.config) : updater;
              return { ...prev, config: nextConfig };
            });
          }}
          onClose={() => setRestoreAction(null)}
          submitting={recoverySubmitting}
          onSubmit={async () => {
            const action = restoreAction;
            const targetNamespace = action.config.namespaceMode === 'original' ? action.row.namespace : action.config.targetNamespace;
            const targetCluster = clusters.find(cluster => cluster.name === action.config.targetCluster);
            setRecoverySubmitting(true);
            try {
              const createdTask = await apiPost<ApiTask>(`/api/v1/tasks/${action.mode}`, {
                clusterId: targetCluster?.id || action.row.targetClusterId,
                protectionPlanId: action.row.protectionPlanId,
                restorePointId: action.row.id,
                veleroBackupName: action.row.veleroBackupName,
                sourceNamespace: action.row.namespace,
                sourceNamespaces: action.row.namespaces,
                targetNamespace,
                targetMode: action.config.targetMode,
                restoreMode: action.config.restoreMode,
                artifactMode: action.config.artifactMode,
                conflictPolicy: action.config.conflictPolicy,
                originalNamespaceConfirmed: action.config.originalNamespaceConfirmed,
                includeClusterScoped: action.config.includeClusterScoped,
                useTransforms: action.config.useTransforms,
                transformPreset: action.config.transformPreset,
                storageProfileMode: action.config.storageProfileMode,
                alternateProfileId: action.config.alternateProfileId,
				forceProceed: action.config.forceProceed,
                includedResources: action.config.includedResources,
                excludedResources: action.config.excludedResources,
                storageClassMappings: action.config.storageClassMappings,
                imageMappings: action.config.imageMappings,
                waitForWorkloads: action.config.waitForWorkloads,
                runValidation: action.config.runValidation,
                forceStart: action.config.forceStart,
                contentCatalogLoaded: action.config.contentCatalogLoaded,
                persistentDataExpected: action.config.persistentDataExpected,
              });
              setTasks(prev => [createdTask, ...prev.filter(task => task.id !== createdTask.id)]);
              setRestoreAction(null);
              toast(action.mode === 'drill' ? 'DR drill job submitted' : 'DR takeover job submitted');
              void load();
            } catch (error) {
              toast('Failed to submit recovery task: ' + (error instanceof Error ? error.message : 'unknown error'));
            } finally {
              setRecoverySubmitting(false);
            }
          }}
          loadContents={restorePointId => apiGet<{ resources: BackupContentResource[]; truncated?: boolean }>(`/api/v1/restore-points/${encodeURIComponent(restorePointId)}/contents`)}
        />
      )}
    </motion.div>
  );
}
