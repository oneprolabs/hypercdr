import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle, AlertTriangle, Archive, Check, CheckCircle2, ChevronDown, ChevronRight,
  Clock, Database, DatabaseBackup, Edit2, Eye, FileCode, Filter, Grid3X3, HardDrive,
  History, Layers, ListChecks, MoreVertical, Play, Plus, RefreshCw, Search, Server,
  Settings2, ShieldCheck, ShieldOff, Trash2, Upload, X, Zap,
} from 'lucide-react';
import { DrConfigurationModal } from '../../dr-configuration-modal';
import { RecoveryWizardModal, type RecoveryWizardConfig } from '../../recovery-wizard-modal';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { SearchBar } from '../../components/search-bar';
import ListToolbarControls from '../../components/list-toolbar-controls';
import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '../../api/client';
import { formatDateTime, formatLocalDateTime } from '../../lib/date-time';
import type { AppItem, Cluster, DRSupportSummary, ResourceCategory, ResourceCategoryKey } from '../clusters/types';
import {
  listItems,
  type ApiApplication, type ApiList, type ApiProtectionPlan, type ApiRestorePointView,
  type ApiTask, type ApiTaskCancelResponse, type ApiTaskEvent, type ApiTaskResponse,
  type PolicyItem, type StorageRepo, type TagItem,
} from '../recovery/types';
import { isActiveTaskStatus, isCompletedTaskStatus, isFailedStatus, isSucceededStatus, taskHasWarning } from '../recovery/task-status';
import {
  TaskErrorDetailBlock, TaskErrorStatus, TaskFinalResult, TaskOriginLabel, TaskProcessTimeline,
  canRetryDrActivation, drStatusForPlan, formatAge, formatBytes, formatBytesPerSecond,
  formatEta, formatPercent, hasTaskEventReason, isProtectionPlanCleaning, isProtectionPlanReady,
  mapApplicationStatus, normalizeErrorCode, numberFromUnknown, recordFromUnknown,
  recoveryActionText, recoveryPreparingMessage, resourceCategoryIconMap, resourceCategoryMeta,
  resourceInventoryDetailText, resourceInventoryTitle, shortResourceKind, storageFailurePresentation,
  syncPreparingMessage, taskDetailFullLabel, taskDetailLabel, taskFailureDetails,
  taskFailureSummary, taskProgressInfo, type ApplicationStage,
} from '../recovery/task-ui';
import {
  ErrorDetailModalFrame, appOverrideKey, buildLabelSelectorOptions, formatNextSyncTime,
  formatPolicyComposition, formatPolicyRetention, formatPolicySchedule, formatScopeLabel,
  listToolbarQueryFields, matchesColumnFilterToken, namespacesFromPayload,
  normalizeResourceCategories, parseColumnFilterToken, recoveryCompletedTargetLabel,
  recoveryCompletedTargetTitle, restorePointDisplayLabel, scriptPayload, taskRestorePointId,
} from './application-support';

export default function ApplicationDrPage(props: {
  apps: AppItem[];
  clusters: Cluster[];
  currentCluster: Cluster | null;
  storage: StorageRepo[];
  policies: PolicyItem[];
  protectionPlans: ApiProtectionPlan[];
  setProtectionPlans: React.Dispatch<React.SetStateAction<ApiProtectionPlan[]>>;
  tags: TagItem[];
  stage: 'select' | 'config' | 'run';
  setStage: (stage: 'select' | 'config' | 'run') => void;
  currentClusterId: string | null;
  updateAppTags: (clusterId: string | null, appNames: string[], updater: (currentTags: string[]) => string[]) => void;
  openStorage: () => void;
  openClusters: () => void;
  openPolicies: () => void;
  toast: (msg: string) => void;
  refreshPlatformData: () => Promise<unknown>;
  liveAppTasks: Record<string, ApiTask>;
  setLiveAppTasks: React.Dispatch<React.SetStateAction<Record<string, ApiTask>>>;
  liveRecoveryTasks: Record<string, ApiTask>;
  setLiveRecoveryTasks: React.Dispatch<React.SetStateAction<Record<string, ApiTask>>>;
  liveRestorePoints: ApiRestorePointView[];
  platformTasks: ApiTask[];
}) {
  const { apps, clusters, currentCluster, storage, policies, protectionPlans, setProtectionPlans, tags, stage, setStage, currentClusterId, updateAppTags, openStorage, openClusters, openPolicies, toast, refreshPlatformData, liveAppTasks, setLiveAppTasks, liveRecoveryTasks, setLiveRecoveryTasks, liveRestorePoints, platformTasks } = props;
  const [selectedSelectApps, setSelectedSelectApps] = useState<string[]>([]);
  const [selectedConfigApps, setSelectedConfigApps] = useState<string[]>([]);
  const [selectedRunApps, setSelectedRunApps] = useState<string[]>([]);
  const [submittingSyncTasks, setSubmittingSyncTasks] = useState<Record<string, ApiTask>>({});
  const [submittingRecoveryTasks, setSubmittingRecoveryTasks] = useState<Record<string, ApiTask>>({});
  const [syncSubmitting, setSyncSubmitting] = useState(false);
  const [recoverySubmitting, setRecoverySubmitting] = useState(false);
  const [configAppNames, setConfigAppNames] = useState<string[]>([]);
  const [protectedAppNames, setProtectedAppNames] = useState<string[]>(() => apps.filter(app => app.stage === 'run').map(app => app.name));
  const [appUiOverrides, setAppUiOverrides] = useState<Record<string, Partial<AppItem>>>({});
  const [protectWizardOpen, setProtectWizardOpen] = useState(false);
  const [protectWizardMode, setProtectWizardMode] = useState<'create' | 'modify'>('create');
  const [protectWizardStep, setProtectWizardStep] = useState<1 | 2 | 3 | 4 | 5 | 6>(1);
  const [protectConfig, setProtectConfig] = useState({
    scope: 'all',
    labels: '',
    labelConditions: [] as Array<{ key: string; operator: 'Equals' | 'Not Equals'; value: string }>,
    includeRules: [] as Array<{ group: string; resource: string; name: string; version: string; labels: string }>,
    includedResources: [] as string[],
    includeAllResources: true,
    labelSelector: { matchLabels: {} as Record<string, string>, matchExpressions: [] as Array<{ key: string; operator: string; values: string[] }> },
    excludedResources: [] as string[],
    storageType: 'local',
    storageId: storage[0]?.id || '',
    policy: 'manual',
    targetCluster: '',
    mergeNamespaces: false,
    excludeRules: [] as Array<{ group: string; resource: string; name: string; version: string; labels: string }>,
    preScripts: [] as Array<{ name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }>,
    postScripts: [] as Array<{ name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }>,
  });
  const [showAddRuleForm, setShowAddRuleForm] = useState(false);
  const [newExcludeRule, setNewExcludeRule] = useState({ group: '', resource: '', name: '', version: '', labels: '' });
  const [editingRuleIndex, setEditingRuleIndex] = useState<number | null>(null);
  const syncTasks: Record<string, ApiTask> = { ...liveAppTasks, ...submittingSyncTasks };
  const setSyncTasks = (updater: any) => {
    if (typeof updater === 'function') {
      setLiveAppTasks(prev => updater(prev));
    } else {
      setLiveAppTasks(updater);
    }
  };
  const [syncTaskDetail, setSyncTaskDetail] = useState<{ app: AppItem; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null>(null);
  const [drTaskEvents, setDrTaskEvents] = useState<Record<string, ApiTaskEvent[]>>({});
  const [resourceDetail, setResourceDetail] = useState<{ app: AppItem } | null>(null);
  const [resourceRefreshKey, setResourceRefreshKey] = useState('');
  const [resourceRefreshStatus, setResourceRefreshStatus] = useState<{ key: string; status: string; message?: string } | null>(null);
  const [drSupportCheckingKeys, setDrSupportCheckingKeys] = useState<string[]>([]);
  const drSupportAutoRequestedRef = useRef<Set<string>>(new Set());
  const [appBulkMenuOpen, setAppBulkMenuOpen] = useState(false);
  const [selectedDetailApp, setSelectedDetailApp] = useState<AppItem | null>(null);
  const [namespaceDetailTab, setNamespaceDetailTab] = useState<'overview' | 'restorePoints' | 'tasks' | 'storage'>('overview');
  const [namespaceDetailTaskId, setNamespaceDetailTaskId] = useState('');
  const [namespaceRestorePointPage, setNamespaceRestorePointPage] = useState(1);
  const [namespaceTaskPage, setNamespaceTaskPage] = useState(1);
  const [drSupportErrorDetail, setDrSupportErrorDetail] = useState<AppItem | null>(null);
  const openNamespaceDetail = (app: AppItem, tab: 'overview' | 'restorePoints' | 'tasks' | 'storage' = 'overview') => {
    setSelectedDetailApp(app);
    setNamespaceDetailTab(tab);
    setNamespaceDetailTaskId('');
    setNamespaceRestorePointPage(1);
    setNamespaceTaskPage(1);
  };
  const currentClusterIdRef = useRef(currentClusterId);
  useEffect(() => {
    if (currentClusterIdRef.current === currentClusterId) return;
    currentClusterIdRef.current = currentClusterId;
    setStage('select');
    setSelectedSelectApps([]);
    setSelectedConfigApps([]);
    setSelectedRunApps([]);
    setConfigAppNames([]);
    setProtectedAppNames(apps.filter(app => app.isProtected).map(app => app.name));
    setAppUiOverrides({});
    setProtectWizardOpen(false);
    setProtectWizardStep(1);
    setSelectedDetailApp(null);
    setNamespaceDetailTab('overview');
    setNamespaceDetailTaskId('');
    setNamespaceRestorePointPage(1);
    setNamespaceTaskPage(1);
    setDrSupportErrorDetail(null);
    setSyncTaskDetail(null);
    setSubmittingSyncTasks({});
    setSyncSubmitting(false);
    setRecoverySubmitting(false);
    setRestoreAction(null);
    setTagAction(null);
    setShowAddRuleForm(false);
    setEditingRuleIndex(null);
    setDrSupportCheckingKeys([]);
    drSupportAutoRequestedRef.current.clear();
    setSyncTasks({});
    setAppBulkMenuOpen(false);
  }, [currentClusterId, apps]);

  const [restoreAction, setRestoreAction] = useState<{ mode: 'drill' | 'takeover'; app: AppItem; config: RecoveryWizardConfig } | null>(null);
  const [tagAction, setTagAction] = useState<'attach' | 'detach' | null>(null);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleConfigColumns, setVisibleConfigColumns] = useState(['drSupport', 'resource', 'tags']);
  const [visibleRunColumns, setVisibleRunColumns] = useState(['resource', 'drStatus', 'scope', 'policy', 'repository', 'targetCluster', 'task', 'recoveryTask', 'tags', 'createdAt']);
  const [wizardPolicySearchQuery, setWizardPolicySearchQuery] = useState('');
  const [wizardPolicyPage, setWizardPolicyPage] = useState(1);
  const preScriptRef = useRef<HTMLInputElement>(null);
  const postScriptRef = useRef<HTMLInputElement>(null);
  const policyOptions = policies.map(policy => ({
    id: policy.id,
    name: policy.name,
    type: formatPolicyComposition(policy.composition),
    schedule: formatPolicySchedule(policy),
    retention: formatPolicyRetention(policy),
    desc: policy.composition === 'manual'
      ? 'Create recovery points only when an operator starts a backup. Retention is not defined by this manual option.'
      : `${formatPolicyComposition(policy.composition)} · ${policy.status}`,
    status: policy.status,
    hasRetention: policy.composition !== 'manual' && policy.composition !== 'schedule',
  }));
  const wizardPolicyKeyword = wizardPolicySearchQuery.trim().toLowerCase();
  const filteredPolicyOptions = policyOptions.filter(policy => {
    if (!wizardPolicyKeyword) return true;
    return [policy.name, policy.type, policy.schedule, policy.retention, policy.desc, policy.status].some(value => value.toLowerCase().includes(wizardPolicyKeyword));
  });
  const wizardPolicyPageSize = 4;
  const wizardPolicyTotalPages = Math.max(1, Math.ceil(filteredPolicyOptions.length / wizardPolicyPageSize));
  const paginatedPolicyOptions = filteredPolicyOptions.slice((wizardPolicyPage - 1) * wizardPolicyPageSize, wizardPolicyPage * wizardPolicyPageSize);
  const wizardTargetNames = protectWizardMode === 'modify' ? selectedRunApps : selectedConfigApps;
  const wizardTargetSummary = wizardTargetNames.length === 1
    ? wizardTargetNames[0]
    : `${wizardTargetNames.length} applications selected`;
  const wizardLabelOptions = buildLabelSelectorOptions(
    wizardTargetNames
      .map(name => apps.find(item => item.name === name))
      .filter((app): app is AppItem => Boolean(app))
  );
  const targetClusterOptions = clusters.map(cluster => ({
    id: cluster.id,
    name: cluster.name,
    region: cluster.region,
    version: cluster.version,
    nodes: cluster.nodes,
    applications: cluster.applications,
    isCurrent: currentCluster?.id === cluster.id,
  }));
  const displayApps = apps.map(app => ({ ...app, ...(appUiOverrides[appOverrideKey(app)] || {}) }));
  const stageOf = (app: AppItem): ApplicationStage => app.stage || (app.isProtected ? 'run' : 'select');
  const selectRows = displayApps.filter(app => stageOf(app) === 'select');
  const pendingRows = displayApps.filter(app => stageOf(app) === 'config');
  const protectedAppRows = displayApps.filter(app => stageOf(app) === 'run');
  const buildMergedProtectedRows = (rows: AppItem[]): AppItem[] => {
    const byPlan = new Map<string, AppItem[]>();
    const unplanned: AppItem[] = [];
    rows.forEach(app => {
      if (!app.protectionPlanId) {
        unplanned.push(app);
        return;
      }
      byPlan.set(app.protectionPlanId, [...(byPlan.get(app.protectionPlanId) || []), app]);
    });
    const merged: AppItem[] = [...unplanned];
    byPlan.forEach((members, planId) => {
      const ordered = [...members].sort((a, b) => (a.namespace || a.name).localeCompare(b.namespace || b.name));
      const first = ordered[0];
      const plan = protectionPlans.find(item => item.id === planId);
      const planAppCount = plan?.appIds?.length || 0;
      if (ordered.length <= 1 || planAppCount <= 1) {
        merged.push(first);
        return;
      }
      const namespaces = ordered.map(app => app.namespace || app.name);
      merged.push({
        ...first,
        name: `plan:${planId}`,
        namespace: namespaces.join(', '),
        memberApps: ordered,
        isMergedPlan: true,
        protectionPlanId: planId,
        protectionPlanCreatedAt: plan?.createdAt || first.protectionPlanCreatedAt,
        workloadCount: ordered.reduce((sum, app) => sum + (app.workloadCount || 0), 0),
        serviceCount: ordered.reduce((sum, app) => sum + (app.serviceCount || 0), 0),
        ingressCount: ordered.reduce((sum, app) => sum + (app.ingressCount || 0), 0),
        configMapCount: ordered.reduce((sum, app) => sum + (app.configMapCount || 0), 0),
        secretCount: ordered.reduce((sum, app) => sum + (app.secretCount || 0), 0),
        pvcCount: ordered.reduce((sum, app) => sum + (app.pvcCount || 0), 0),
        pvCapacityBytes: ordered.reduce((sum, app) => sum + (app.pvCapacityBytes || 0), 0),
        tags: Array.from(new Set(ordered.flatMap(app => app.tags || []))),
      });
    });
    return merged.sort((a, b) => {
      const planTimeDelta = (b.protectionPlanCreatedAt || '').localeCompare(a.protectionPlanCreatedAt || '');
      if (planTimeDelta !== 0) return planTimeDelta;
      return (a.memberApps?.[0]?.namespace || a.namespace || a.name).localeCompare(b.memberApps?.[0]?.namespace || b.namespace || b.name);
    });
  };
  const protectedRows = buildMergedProtectedRows(protectedAppRows);
  const namespaceRows = stage === 'select' ? selectRows : pendingRows;
  const protectedCount = protectedRows.length;
  const pendingCount = pendingRows.length;
  const activeDrTaskIds = [
    ...Object.values(syncTasks),
    ...Object.values(liveRecoveryTasks),
  ].filter(task => task?.id && isActiveTaskStatus(task.status)).map(task => task.id);
  const activeDrTaskKey = Array.from(new Set(activeDrTaskIds)).sort().join('|');
  const protectionPlanForApp = (app: AppItem) => {
    if (!app.protectionPlanId) return undefined;
    return protectionPlans.find(plan => plan.id === app.protectionPlanId);
  };
  const planStorageSizeForApp = (app: AppItem) => {
    const size = recordFromUnknown(protectionPlanForApp(app)?.planStorageSize);
    const totalBytes = numberFromUnknown(size.totalBytes);
    if (!totalBytes) return { label: '', title: app.storage || 'Repository' };
    const metadataBytes = numberFromUnknown(size.metadataBytes);
    const kopiaBytes = numberFromUnknown(size.kopiaBytes);
    const title = [
      `Repository: ${app.storage || 'Unknown'}`,
      `Used: ${formatBytes(totalBytes)}`,
      metadataBytes > 0 ? `Metadata: ${formatBytes(metadataBytes)}` : '',
      kopiaBytes > 0 ? `Kopia: ${formatBytes(kopiaBytes)}` : '',
    ].filter(Boolean).join('; ');
    return { label: `Used ${formatBytes(totalBytes)}`, title };
  };
  const nextSyncLabelForApp = (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan || !isProtectionPlanReady(plan.status)) return '';
    const policy = policies.find(item => item.id === plan.policyId);
    const next = formatNextSyncTime(plan.nextFireAt);
    if (next) return `Next: ${next}`;
    if (policy?.composition === 'manual') return 'Manual only';
    if (!policy && plan.scheduleEnabled === false) return 'Manual only';
    return '';
  };
  const renderNextSyncHint = (app: AppItem) => {
    const label = nextSyncLabelForApp(app);
    return label ? <small className="hbdr-dr-next-sync">{label}</small> : null;
  };
  const drStatusMetaForApp = (app: AppItem) => drStatusForPlan(protectionPlanForApp(app)?.status);
  const storageFailureForApp = (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan) return null;
    const task = platformTasks
      .filter(item => item.type === 'storage-sync' && item.protectionPlanId === plan.id && item.status.toLowerCase() === 'failed')
      .sort((a, b) => (b.completedAt || b.createdAt || '').localeCompare(a.completedAt || a.createdAt || ''))[0];
    const presentation = storageFailurePresentation(task);
    return task && presentation ? { task, presentation } : null;
  };
  const retryDrActivation = async (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan) {
      toast('No protection plan is associated with this namespace');
      return;
    }
    if (!canRetryDrActivation(plan.status)) {
      toast('Protection plan activation is not retryable in the current state');
      return;
    }
    setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: 'activating_storage' } : item));
    try {
      const updated = await apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${plan.id}/activate`, {});
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, ...updated, status: updated.status || 'activating_storage' } : item));
      toast(updated.warning ? `Activation retry submitted: ${updated.warning}` : 'Activation retry submitted');
      void refreshPlatformData();
    } catch (error) {
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: plan.status } : item));
      toast('Failed to retry activation: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const reconfigureDrStorage = async (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan) {
      toast('No protection plan is associated with this namespace');
      return;
    }
    setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: 'activating_storage' } : item));
    try {
      const updated = await apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${plan.id}/storage/reconfigure`, {});
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, ...updated, status: updated.status || 'activating_storage' } : item));
      toast(updated.warning ? `Storage reconfigure submitted: ${updated.warning}` : 'Storage reconfigure submitted');
      void refreshPlatformData();
    } catch (error) {
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: plan.status } : item));
      toast('Failed to reconfigure storage: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  useEffect(() => {
    const ids = activeDrTaskKey ? activeDrTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const loadEvents = async () => {
      const entries = await Promise.all(ids.map(async taskId => {
        try {
          const res = await apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${taskId}/events`);
          return [taskId, listItems(res)] as const;
        } catch {
          return [taskId, null] as const;
        }
      }));
      if (cancelled) return;
      setDrTaskEvents(prev => {
        const next = { ...prev };
        for (const [taskId, events] of entries) {
          if (events) next[taskId] = events;
        }
        return next;
      });
      if (entries.some(([, events]) => events?.some(event => ['completed', 'backup_completed', 'velero-schedule'].includes(event.reason)))) {
        void refreshPlatformData();
      }
    };
    loadEvents();
    const timer = window.setInterval(loadEvents, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeDrTaskKey, refreshPlatformData]);
  useEffect(() => {
    const taskId = syncTaskDetail?.task.id;
    if (!taskId) return;
    let cancelled = false;
    const refreshOpenTask = async () => {
      try {
        const [eventResult, taskResult] = await Promise.all([
          apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${taskId}/events`),
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
        ]);
        if (cancelled) return;
        setDrTaskEvents(prev => ({ ...prev, [taskId]: listItems(eventResult) }));
        const latest = listItems(taskResult).find(task => task.id === taskId);
        if (latest) setSyncTaskDetail(prev => prev?.task.id === taskId ? { ...prev, task: latest } : prev);
      } catch {
        // Keep the last successful snapshot visible while the next live refresh retries.
      }
    };
    void refreshOpenTask();
    const timer = window.setInterval(refreshOpenTask, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [syncTaskDetail?.task.id]);
  useEffect(() => {
    if (!namespaceDetailTaskId || drTaskEvents[namespaceDetailTaskId]) return;
    let cancelled = false;
    void apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${namespaceDetailTaskId}/events`)
      .then(result => {
        if (!cancelled) setDrTaskEvents(prev => ({ ...prev, [namespaceDetailTaskId]: listItems(result) }));
      })
      .catch(() => {
        if (!cancelled) setDrTaskEvents(prev => ({ ...prev, [namespaceDetailTaskId]: [] }));
      });
    return () => {
      cancelled = true;
    };
  }, [namespaceDetailTaskId, drTaskEvents]);
  const unitMembers = (app: AppItem) => app.memberApps?.length ? app.memberApps : [app];
  const unitNamespaces = (app: AppItem) => unitMembers(app).map(item => item.namespace || item.name);
  const taskForUnit = (tasks: Record<string, ApiTask>, app: AppItem) => {
    if (tasks[app.name]) return tasks[app.name];
    if (app.protectionPlanId) {
      const planTask = Object.values(tasks).find(task => task?.protectionPlanId === app.protectionPlanId);
      if (planTask) return planTask;
    }
    return unitMembers(app).map(member => tasks[member.name]).find(Boolean);
  };
  const recoveryTaskForUnit = (app: AppItem) =>
    taskForUnit(submittingRecoveryTasks, app) || taskForUnit(liveRecoveryTasks, app);
  useEffect(() => {
    const confirmedTaskIds = new Set(platformTasks.map(task => task.id));
    setSubmittingSyncTasks(prev => {
      const next = Object.fromEntries(Object.entries(prev).filter(([, task]) => !confirmedTaskIds.has(task.id)));
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
    setSubmittingRecoveryTasks(prev => {
      const next = Object.fromEntries(Object.entries(prev).filter(([, task]) => !confirmedTaskIds.has(task.id)));
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
  }, [platformTasks]);
  const normalizedQuery = query.trim().toLowerCase();
  const queryValueForApp = (app: AppItem, field: string) => {
    const profile = profileOf(app);
    const recoveryTask = recoveryTaskForUnit(app);
    if (field === 'namespace') return unitNamespaces(app).join(' ');
    if (field === 'policy') return app.policy || '';
    if (field === 'storage' || field === 'repository') return app.storage || '';
    if (field === 'target' || field === 'targetCluster') return app.targetCluster || 'Backup only';
    if (field === 'resource') return normalizeResourceCategories(app).map(category => `${category.label} ${category.total}`).join(' ');
    if (field === 'drStatus') return drStatusMetaForApp(app).label;
    if (field === 'drSupport') return drSupportMetaForApp(app).label;
    if (field === 'status') return stageOf(app) === 'run' ? 'protected' : stageOf(app) === 'config' ? 'pending_protection' : 'unprotected';
    if (field === 'scope') return formatScopeLabel(profile.scope);
    if (field === 'task') {
      const task = taskForUnit(syncTasks, app);
      return task ? `${task.type} ${task.status} ${task.progress || 0}` : app.lastBackup || '';
    }
    if (field === 'recoveryTask') return recoveryTask ? `${recoveryTask.type} ${recoveryTask.status} ${recoveryTask.payload?.targetNamespace || ''}` : '';
    if (field === 'createdAt') return app.protectionPlanCreatedAt ? formatDateTime(app.protectionPlanCreatedAt) : '';
    if (field === 'tags') return (app.tags || []).join(' ');
    return app.name;
  };
  const appHasTag = (app: AppItem, tag: string) => {
    return unitMembers(app).some(member => (member.tags || []).includes(tag));
  };
  const appMatchesFilter = (app: AppItem, filter: string) => {
    const profile = profileOf(app);
    const task = taskForUnit(syncTasks, app);
    const recoveryTask = recoveryTaskForUnit(app);
    if (filter === 'active') return unitMembers(app).some(member => member.status === 'Active' || member.status === 'Running');
    if (filter === 'protected') return stageOf(app) === 'run';
    if (filter === 'pending') return stageOf(app) === 'config';
    if (filter === 'unprotected') return stageOf(app) === 'select';
    if (filter === 'syncing') return isActiveTaskStatus(task?.status);
    if (filter === 'completed') return isCompletedTaskStatus(task?.status) || Boolean(app.lastBackup);
    if (filter === 'recovering') return isActiveTaskStatus(recoveryTask?.status);
    if (filter === 'warning') return profile.taskStatus === 'warning' || profile.score < 90 || isDRUnsupported(app);
    if (filter === 'supported') return !isDRSupportUnknown(app) && !isDRUnsupported(app);
    if (filter === 'unsupported') return isDRUnsupported(app);
    if (filter === 'notChecked') return isDRSupportUnknown(app);
    if (filter === 'hasPvc') return unitMembers(app).some(member => (member.pvcCount || member.resourceSummary?.pvcs || 0) > 0);
    if (filter === 'stateless') return unitMembers(app).every(member => (member.pvcCount || member.resourceSummary?.pvcs || 0) === 0);
    if (filter === 'manualOnly') return stageOf(app) === 'run' && !app.policy;
    if (filter === 'scheduled') return stageOf(app) === 'run' && Boolean(app.policy);
    return true;
  };
  const matchesQuery = (app: AppItem) => {
    const queryMatched = !normalizedQuery || queryValueForApp(app, queryField).toLowerCase().includes(normalizedQuery);
    const tagsMatched = activeTags.length === 0 || activeTags.some(tag => appHasTag(app, tag));
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => queryValueForApp(app, field));
      return appMatchesFilter(app, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  };
  const resourceProfiles: Record<string, {
    uid: string;
    resources: Array<[string, number]>;
    pvc: string;
    capacity: string;
    age: string;
    score: number;
    scope: string;
    scopeTag?: string;
    taskStatus: 'normal' | 'warning';
  }> = {};
  const profileOf = (app: AppItem) => resourceProfiles[app.name] || {
    uid: `${app.namespace.slice(0, 3)}-000`,
    resources: [['DEP', 1], ['SVC', 1]],
    pvc: app.storage ? '1 PVC mounted' : 'No persistent volume',
    capacity: app.storage ? '10.0 GB' : '0 GB',
    age: app.lastBackup || '1 days ago',
    score: app.isProtected ? 90 : 70,
    scope: app.isProtected
      ? (protectionPlanForApp(app)?.scopeType || 'all')
      : 'Pending Selection',
    scopeTag: undefined as string | undefined,
    taskStatus: app.isProtected ? 'normal' as const : 'warning' as const,
  };
  const resourceSummaryTotal = (app: AppItem) => normalizeResourceCategories(app).reduce((sum, category) => sum + category.total, 0);
  const resourceSummaryTotalForUnit = (app: AppItem) => unitMembers(app).reduce((sum, member) => sum + resourceSummaryTotal(member), 0);
  const drSupportForApp = (app: AppItem): DRSupportSummary | undefined => app.resourceSummary?.drSupport;
  const drSupportStatus = (app: AppItem): string => (drSupportForApp(app)?.status || '').toLowerCase();
  const drSupportKeyForApp = (app: AppItem): string => `${app.clusterId || currentClusterId || ''}:${app.namespace || app.name}`;
  const isDRSupportUnknown = (app: AppItem): boolean => unitMembers(app).some(member => !drSupportStatus(member));
  const isDRSupportChecking = (app: AppItem): boolean => unitMembers(app).some(member => drSupportCheckingKeys.includes(drSupportKeyForApp(member)));
  const isDRUnsupported = (app: AppItem): boolean => unitMembers(app).some(member => drSupportStatus(member) === 'unsupported');
  const formatUnsupportedStorageSummary = (support: DRSupportSummary): string => {
    const checks = (support.checks || []).filter(check => (check.status || '').toLowerCase() === 'unsupported');
    const detected = checks.map(check => {
      const storage = check.storageClass || 'unknown StorageClass';
      const pvType = check.volumeType || 'unknown PV type';
      const provisioner = check.provisioner ? `, provisioner ${check.provisioner}` : '';
      return `${check.name || 'PVC'}: ${storage} / ${pvType}${provisioner}`;
    });
    return [
      'Storage type is not supported for DR.',
      'Supported: stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn.',
      'Unsupported: local-path, hostPath, and local PV storage.',
      detected.length > 0 ? `Detected in this namespace:\n${detected.map(item => `- ${item}`).join('\n')}` : '',
      'Impact: this namespace cannot be moved to Setup DR until its PVCs use supported storage.',
    ].filter(Boolean).join('\n');
  };
  const drSupportMetaForApp = (app: AppItem) => {
    const unsupported = unitMembers(app).filter(member => drSupportStatus(member) === 'unsupported');
    if (unsupported.length > 0) {
      const details = unsupported.map(member => {
        const support = drSupportForApp(member) || {};
        return `${member.namespace || member.name}\n${formatUnsupportedStorageSummary(support)}`;
      });
      return { label: 'Unsupported', tone: 'unsupported' as const, sort: 0, title: details.join('\n\n') };
    }
    const unknown = unitMembers(app).find(member => !drSupportStatus(member));
    if (unknown) {
      if (isDRSupportChecking(app)) {
        return {
          label: 'Checking...',
          tone: 'unknown' as const,
          sort: 1,
          title: 'Checking namespace storage compatibility for DR.',
        };
      }
      return {
        label: 'Not checked',
        tone: 'unknown' as const,
        sort: 1,
        title: 'DR support has not been checked yet. The system will check namespace storage before moving it to Setup DR.',
      };
    }
    const warning = unitMembers(app).find(member => drSupportStatus(member) === 'warning');
    if (warning) {
      return { label: 'Warning', tone: 'warning' as const, sort: 2, title: drSupportForApp(warning)?.reason || 'DR support needs attention' };
    }
    return { label: 'Supported', tone: 'supported' as const, sort: 3, title: 'Namespace storage is eligible for DR protection.' };
  };
  const unsupportedDRChecksForApp = (app: AppItem) => unitMembers(app).flatMap(member => {
    const support = drSupportForApp(member);
    return (support?.checks || [])
      .filter(check => (check.status || '').toLowerCase() === 'unsupported')
      .map(check => ({
        namespace: member.namespace || member.name,
        kind: check.kind || 'PVC',
        resource: check.name || 'unknown',
        reason: check.reason || 'The application uses a non-portable storage type.',
        storageClass: check.storageClass || 'unknown',
        pvType: check.volumeType || 'unknown',
        provisioner: check.provisioner || 'unknown',
      }));
  });
  const drSupportFailureForApp = (app: AppItem) => {
    const namespace = app.namespace || app.name;
    const details = drSupportMetaForApp(app).title || 'This namespace contains storage that is not eligible for DR.';
    return {
      code: '100201',
      title: 'Unsupported DR Storage',
      description: `${namespace} contains storage that is not eligible for DR.`,
      fullText: details,
    };
  };
  const drSupportFailureDetailsForApp = (app: AppItem) => {
    const checks = unsupportedDRChecksForApp(app);
    if (checks.length === 0) {
      return drSupportFailureForApp(app).fullText.split('\n').map(line => line.trim()).filter(Boolean);
    }
    return [
      'Storage type is not supported for DR.',
      'Supported: stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn.',
      'Unsupported: local-path, hostPath, and local PV storage.',
      ...checks.map(check => `${check.namespace} / ${check.kind}/${check.resource}: storageClass=${check.storageClass}, volumeType=${check.pvType}, provisioner=${check.provisioner}`),
      'Impact: this namespace cannot be moved to Setup DR until its PVCs use supported storage.',
    ];
  };
  const renderDRSupportBadge = (app: AppItem) => {
    const meta = drSupportMetaForApp(app);
    const Icon = meta.tone === 'unsupported' ? AlertTriangle : meta.tone === 'warning' ? AlertCircle : meta.tone === 'unknown' ? Clock : CheckCircle2;
    const checks = unsupportedDRChecksForApp(app);
    const reasons = Array.from(new Set(checks.map(check => check.reason).filter(Boolean)));
    const recommendation = checks.some(check => check.kind.toLowerCase() === 'pvc' || check.storageClass !== 'unknown')
      ? 'Move the affected PVC to portable CSI storage, such as Longhorn.'
      : 'Replace or reconfigure the affected resource before enabling DR.';
    const badgeContent = (
      <>
        <Icon size={13} />
        <span>{meta.label}</span>
      </>
    );
    return (
      <span className="hbdr-dr-support-wrap">
        {meta.tone === 'unsupported' ? (
          <span className={`hbdr-dr-support-badge hbdr-dr-support-${meta.tone}`} aria-label={meta.title}>
            {badgeContent}
          </span>
        ) : (
          <span className={`hbdr-dr-support-badge hbdr-dr-support-${meta.tone}`} aria-label={meta.title}>
            {badgeContent}
          </span>
        )}
        {meta.tone === 'unsupported' && (
          <span className="hbdr-dr-support-error-popover" role="tooltip">
            <strong><AlertTriangle size={15} /> DR is not supported</strong>
            <em>This application does not meet the requirements for DR.</em>
            <span className="hbdr-dr-support-error-section">
              <b>Reason</b>
              <small>{reasons[0] || 'One or more application resources are not supported for DR.'}</small>
              {reasons.length > 1 && <small>+{reasons.length - 1} more reason{reasons.length > 2 ? 's' : ''}</small>}
            </span>
            {checks.length > 0 && (
              <span className="hbdr-dr-support-error-section">
                <b>Affected resources</b>
                <span className="hbdr-dr-support-error-list">
                  {checks.slice(0, 3).map((check, index) => (
                    <small key={`${check.namespace}-${check.resource}-${index}`}>
                      {check.kind}/{check.resource} · {check.storageClass !== 'unknown' ? check.storageClass : check.pvType}
                    </small>
                  ))}
                  {checks.length > 3 && <small>+{checks.length - 3} more resources</small>}
                </span>
              </span>
            )}
            <span className="hbdr-dr-support-error-section">
              <b>Recommendation</b>
              <small>{recommendation}</small>
            </span>
          </span>
        )}
      </span>
    );
  };
  const appSortValue = (app: AppItem, column: string): string | number => {
    const profile = profileOf(app);
    const task = taskForUnit(syncTasks, app);
    const recoveryTask = recoveryTaskForUnit(app);
    if (column === 'name') return app.name;
    if (column === 'namespace') return unitNamespaces(app).join(' ');
    if (column === 'status') return app.status || '';
    if (column === 'resource') return resourceSummaryTotalForUnit(app);
    if (column === 'drSupport') return drSupportMetaForApp(app).sort;
    if (resourceCategoryMeta.some(meta => meta.key === column)) {
      return normalizeResourceCategories(app).find(category => category.key === column)?.total || 0;
    }
    if (column === 'protection') return app.stage === 'run' ? 2 : app.stage === 'config' ? 1 : 0;
    if (column === 'drStatus') return drStatusMetaForApp(app).label;
    if (column === 'score') return profile.score;
    if (column === 'scope') return profile.scopeTag ? `${formatScopeLabel(profile.scope)} ${profile.scopeTag}` : formatScopeLabel(profile.scope);
    if (column === 'policy') return app.policy || '';
    if (column === 'repository') return app.storage || '';
    if (column === 'targetCluster') return app.targetCluster || 'Backup only';
    if (column === 'task') return task ? `${task.status} ${task.progress}` : 'no task';
    if (column === 'recoveryTask') return recoveryTask ? `${recoveryTask.type} ${recoveryTask.status} ${recoveryTask.progress}` : 'no recovery task';
    if (column === 'createdAt') return app.protectionPlanCreatedAt || '';
    if (column === 'tags') return (app.tags || [])
      .map(tagId => tags.find(item => item.id === tagId)?.name || tagId)
      .join(' ');
    if (column === 'lastBackup') return app.lastBackup || '';
    return queryValueForApp(app, column);
  };
  const visibleNamespaceRows = namespaceRows.filter(matchesQuery);
  const visibleProtectedRows = protectedRows.filter(matchesQuery);
  const stageCards = [
    { key: 'select' as const, title: 'Select Application', desc: 'Add namespaces to the platform for protection.', list: 'List - Unconfigured application resources', count: selectRows.filter(app => !isDRUnsupported(app)).length, metric: 'Eligible Apps', icon: Layers, tone: 'blue' },
    { key: 'config' as const, title: 'Setup DR', desc: 'Configure DR strategy for the application.', list: 'List - Application resources to be configured', count: pendingCount, metric: 'Pending Configuration', icon: ShieldCheck, tone: 'orange' },
    { key: 'run' as const, title: 'Start DR', desc: 'Operate the configured applications.', list: 'List - Application resources have been configured', count: protectedCount, metric: 'Protected', icon: Play, tone: 'green' },
  ];
  const activeStage = stageCards.find(card => card.key === stage) || stageCards[0];
  const isRunStage = stage === 'run';
  const currentRows = isRunStage ? visibleProtectedRows : visibleNamespaceRows;
  const appConfigColumns = [
    { value: 'drSupport', label: 'DR Support' },
    { value: 'resource', label: 'Resource' },
    { value: 'tags', label: 'Tags' },
  ];
  const appRunColumns = [
    { value: 'resource', label: 'Resource' },
    { value: 'drStatus', label: 'DR Config' },
    { value: 'scope', label: 'Scope' },
    { value: 'policy', label: 'Policy' },
    { value: 'repository', label: 'Repository' },
    { value: 'targetCluster', label: 'DR Pair' },
    { value: 'task', label: 'Sync Status' },
    { value: 'recoveryTask', label: 'Recovery Status' },
    { value: 'tags', label: 'Tags' },
    { value: 'createdAt', label: 'Create Time' },
  ];
  const visibleAppColumns = isRunStage ? visibleRunColumns : visibleConfigColumns;
  const appColumns = isRunStage ? appRunColumns : appConfigColumns;
  const appQueryFields = listToolbarQueryFields([
    { value: 'namespace', label: 'Namespace' },
  ], appColumns, visibleAppColumns);
  const appFilterChoices = isRunStage
    ? [
      { value: 'protected', label: 'Protected', count: currentRows.filter(app => appMatchesFilter(app, 'protected')).length },
      { value: 'syncing', label: 'Syncing', count: currentRows.filter(app => appMatchesFilter(app, 'syncing')).length },
      { value: 'recovering', label: 'Recovering', count: currentRows.filter(app => appMatchesFilter(app, 'recovering')).length },
      { value: 'completed', label: 'Has Restore Point', count: currentRows.filter(app => appMatchesFilter(app, 'completed')).length },
      { value: 'scheduled', label: 'Schedule Enabled', count: currentRows.filter(app => appMatchesFilter(app, 'scheduled')).length },
      { value: 'manualOnly', label: 'Manual Only', count: currentRows.filter(app => appMatchesFilter(app, 'manualOnly')).length },
      { value: 'warning', label: 'Needs Attention', count: currentRows.filter(app => appMatchesFilter(app, 'warning')).length },
    ]
    : [
      { value: 'active', label: 'Runtime Active', count: currentRows.filter(app => appMatchesFilter(app, 'active')).length },
      { value: 'supported', label: 'DR Supported', count: currentRows.filter(app => appMatchesFilter(app, 'supported')).length },
      { value: 'unsupported', label: 'DR Unsupported', count: currentRows.filter(app => appMatchesFilter(app, 'unsupported')).length },
      { value: 'notChecked', label: 'Not Checked', count: currentRows.filter(app => appMatchesFilter(app, 'notChecked')).length },
      { value: 'hasPvc', label: 'Has PVC', count: currentRows.filter(app => appMatchesFilter(app, 'hasPvc')).length },
      { value: 'stateless', label: 'Stateless', count: currentRows.filter(app => appMatchesFilter(app, 'stateless')).length },
      { value: 'warning', label: 'Needs Attention', count: currentRows.filter(app => appMatchesFilter(app, 'warning')).length },
    ];
  const appColumnMinWidth = (column: string) => {
    if (column === 'name') return 150;
    if (column === 'resource') return 420;
    if (column === 'drSupport') return 126;
    if (column === 'protection') return 116;
    if (column === 'drStatus') return 144;
    if (column === 'score') return 108;
    if (column === 'scope') return 96;
    if (column === 'policy') return 104;
    if (column === 'repository') return 130;
    if (column === 'targetCluster') return 140;
    if (column === 'task') return 160;
    if (column === 'recoveryTask') return 150;
    if (column === 'tags') return 96;
    if (column === 'createdAt') return 150;
    return 92;
  };
  const resourceCategoryTitle = (app: AppItem, category: ResourceCategory): string => {
    const items = (category.items || []).filter(item => item.count > 0);
    if (category.key === 'storage') {
      const capacity = app.pvCapacityBytes || app.resourceSummary?.pvCapacityBytes || 0;
      const capacityLabel = capacity > 0 ? formatBytes(capacity) : '0 B';
      const pvcItem = items.find(item => item.kind === 'PersistentVolumeClaim');
      const pvcLines = (pvcItem?.resources || []).slice(0, 8).map(resource => {
        const capacityValue = resource.fields?.CAPACITY || resource.fields?.Capacity || '';
        const statusValue = resource.fields?.STATUS || resource.fields?.Status || '';
        const storageClass = resource.fields?.STORAGECLASS || resource.fields?.StorageClass || '';
        return [resource.name, statusValue, storageClass, capacityValue].filter(Boolean).join(' · ');
      });
      return [`Storage`, `${category.total} PVC${category.total === 1 ? '' : 's'} · ${capacityLabel} total`, ...pvcLines].join('\n');
    }
    return [category.label, ...items.map(item => `${item.kind}: ${item.count}`)].join('\n');
  };
  const renderResourceSummaryCell = (app: AppItem) => {
    if (app.memberApps?.length) {
      return (
        <div className="hbdr-merged-resource-list">
          {app.memberApps.map(member => (
            <div key={member.name} className="hbdr-merged-resource-row">
              <strong>{member.namespace || member.name}</strong>
              {renderResourceSummaryCell(member)}
            </div>
          ))}
        </div>
      );
    }
    const categories = normalizeResourceCategories(app).filter(category => category.total > 0);
    if (categories.length === 0) {
      return <span className="hbdr-dr-resource-empty" aria-label="No namespaced resources found" />;
    }
    return (
      <div className="hbdr-dr-resource-summary">
        {categories.slice(0, 8).map(category => {
          const Icon = resourceCategoryIconMap[category.key as ResourceCategoryKey] || MoreVertical;
          const capacity = app.pvCapacityBytes || app.resourceSummary?.pvCapacityBytes || 0;
          const capacityLabel = category.key === 'storage' && capacity > 0 ? formatBytes(capacity) : '';
          return (
          <button
            key={category.key}
            type="button"
            className={`hbdr-dr-resource-summary-chip hbdr-dr-resource-summary-${category.key}`}
            title={resourceCategoryTitle(app, category)}
            aria-label={resourceCategoryTitle(app, category)}
            onClick={event => {
              event.stopPropagation();
              setResourceDetail({ app });
            }}
          >
            <Icon size={13} />
            <strong>{category.total}</strong>
            {capacityLabel && <em>{capacityLabel}</em>}
          </button>
          );
        })}
      </div>
    );
  };
  const resourceDetailGroups = resourceDetail
    ? normalizeResourceCategories(resourceDetail.app)
      .map(category => ({
        category,
        items: (category.items || []).filter(item => item.count > 0),
      }))
      .filter(group => group.items.length > 0)
    : [];
  const renderAppTags = (app: AppItem) => {
    const appTags = app.tags || [];
    if (appTags.length === 0) return <span className="hbdr-app-tag-empty" aria-label="No tags" />;
    return (
      <span className="hbdr-app-tag-list">
        {appTags.map(tagId => {
          const tag = tags.find(item => item.id === tagId);
          return <em key={tagId}>{tag?.name || tagId}</em>;
        })}
      </span>
    );
  };
  const selectedNames = stage === 'select' ? selectedSelectApps : stage === 'config' ? selectedConfigApps : selectedRunApps;
  const setSelectedNames = stage === 'select' ? setSelectedSelectApps : stage === 'config' ? setSelectedConfigApps : setSelectedRunApps;
  const selectableCurrentRows = currentRows;
  const selectedApps = selectedNames
    .map(name => currentRows.find(app => app.name === name) || apps.find(app => app.name === name))
    .filter((app): app is AppItem => Boolean(app));
  const selectedUnsupportedDRApp = stage === 'select' ? selectedApps.find(isDRUnsupported) || null : null;
  const selectedHasUnsupportedDR = Boolean(selectedUnsupportedDRApp);
  const unsupportedDRSelectionMessage = selectedUnsupportedDRApp
    ? `[100201] Unsupported DR Storage. ${selectedUnsupportedDRApp.namespace || selectedUnsupportedDRApp.name} contains storage that is not eligible for DR.`
    : '';
  const primaryDisabled = selectedNames.length === 0 || selectedHasUnsupportedDR;
  const selectedAttachedTagIds = Array.from(new Set(selectedApps.flatMap(app => app.tags || [])));
  const attachTagToSelected = (tagId: string) => {
    updateAppTags(currentClusterId, selectedNames, currentTags => Array.from(new Set([...currentTags, tagId])));
    setTagAction(null);
    setAppBulkMenuOpen(false);
    toast(`Tag attached to ${selectedNames.length} application${selectedNames.length === 1 ? '' : 's'}`);
  };
  const detachTagFromSelected = (tagId: string) => {
    updateAppTags(currentClusterId, selectedNames, currentTags => currentTags.filter(item => item !== tagId));
    setTagAction(null);
    setAppBulkMenuOpen(false);
    toast(`Tag detached from ${selectedNames.length} application${selectedNames.length === 1 ? '' : 's'}`);
  };
  const refreshNamespaceResources = async (app: AppItem) => {
    const clusterId = app.clusterId || currentClusterId;
    const namespace = app.namespace || app.name;
    if (!clusterId) {
      toast('Select a source cluster first');
      return;
    }
    const refreshKey = `${clusterId}:${namespace}`;
    setResourceRefreshKey(refreshKey);
    setResourceRefreshStatus({ key: refreshKey, status: 'pending', message: 'Requesting latest resources...' });
    try {
      const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
        scope: 'namespaceResources',
        namespace,
        includeDetails: true,
        reason: 'resource_modal_refresh',
        includeRecentVeleroObjects: false,
      });
      setResourceRefreshStatus({ key: refreshKey, status: 'pending', message: 'Waiting for agent response...' });
      for (let attempt = 0; attempt < 12; attempt += 1) {
        await new Promise(resolve => window.setTimeout(resolve, 1000));
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${clusterId}/inventory/requests/${submitted.requestId}`);
        if (status.status === 'succeeded') {
          setResourceRefreshStatus({ key: refreshKey, status: 'succeeded', message: 'Resource inventory updated.' });
          await refreshPlatformData();
          toast('Resource inventory updated');
          return;
        }
        if (status.status === 'failed' || status.status === 'timeout') {
          const message = status.message || status.errorCode || 'Resource refresh failed';
          setResourceRefreshStatus({ key: refreshKey, status: status.status, message });
          toast(message);
          return;
        }
      }
      setResourceRefreshStatus({ key: refreshKey, status: 'timeout', message: 'Agent response timed out.' });
      toast('Agent response timed out');
    } catch (error) {
      setResourceRefreshStatus({ key: refreshKey, status: 'failed', message: error instanceof Error ? error.message : 'unknown error' });
      toast('Failed to request resource refresh: ' + (error instanceof Error ? error.message : 'unknown error'));
    } finally {
      window.setTimeout(() => {
        setResourceRefreshKey(current => current === refreshKey ? '' : current);
      }, 1200);
    }
  };
  const refreshCurrentClusterInventory = async () => {
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    try {
      const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${currentClusterId}/inventory/request`, {
        scope: 'summary',
        includeDetails: true,
        reason: 'application_dr_manual_refresh',
        includeRecentVeleroObjects: false,
      });
      for (let attempt = 0; attempt < 35; attempt += 1) {
        await new Promise(resolve => window.setTimeout(resolve, 1000));
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${currentClusterId}/inventory/requests/${submitted.requestId}`);
        if (status.status === 'succeeded') {
          await refreshPlatformData();
          toast('Namespace inventory updated');
          return;
        }
        if (status.status === 'failed' || status.status === 'timeout') {
          toast(status.message || status.errorCode || 'Inventory refresh failed');
          return;
        }
      }
      toast('Inventory refresh timed out');
    } catch (error) {
      toast('Failed to request inventory refresh: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const checkDRSupportBeforeStageMove = async (targetApps: AppItem[]): Promise<ApiApplication[]> => {
    const requests = await Promise.all(targetApps.map(app => {
      const clusterId = app.clusterId || currentClusterId;
      const namespace = app.namespace || app.name;
      if (!clusterId) throw new Error(`Missing cluster for ${namespace}`);
      return apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
        scope: 'namespaceResources',
        namespace,
        includeDetails: true,
        reason: 'dr_support_precheck',
        includeRecentVeleroObjects: false,
      }).then(response => ({ clusterId, namespace, requestId: response.requestId }));
    }));

    const pending = new Map(requests.map(request => [request.requestId, request]));
    for (let attempt = 0; attempt < 35 && pending.size > 0; attempt += 1) {
      await new Promise(resolve => window.setTimeout(resolve, 1000));
      for (const request of Array.from(pending.values())) {
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${request.clusterId}/inventory/requests/${request.requestId}`);
        if (status.status === 'succeeded') {
          pending.delete(request.requestId);
        } else if (status.status === 'failed' || status.status === 'timeout') {
          throw new Error(`${request.namespace}: ${status.message || status.errorCode || 'DR support check failed'}`);
        }
      }
    }
    if (pending.size > 0) {
      throw new Error('DR support check timed out. Please refresh inventory and try again.');
    }
    await refreshPlatformData();
    const latest = await apiGet<ApiList<ApiApplication>>('/api/v1/applications');
    return latest.items;
  };
  useEffect(() => {
    if (stage !== 'select') return;
    const unknownApps = selectRows.filter(app => isDRSupportUnknown(app));
    if (unknownApps.length === 0) return;
    const unknownKeys = Array.from(new Set(unknownApps.map(drSupportKeyForApp)));
    if (unknownKeys.every(key => drSupportAutoRequestedRef.current.has(key))) return;
    unknownKeys.forEach(key => drSupportAutoRequestedRef.current.add(key));
    setDrSupportCheckingKeys(prev => Array.from(new Set([...prev, ...unknownKeys])));

    const probe = unknownApps[0];
    const clusterId = probe.clusterId || currentClusterId;
    const namespace = probe.namespace || probe.name;
    if (!clusterId) {
      setDrSupportCheckingKeys(prev => prev.filter(key => !unknownKeys.includes(key)));
      return;
    }

    let cancelled = false;
    void (async () => {
      try {
        const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
          scope: 'namespaceResources',
          namespace,
          includeDetails: true,
          reason: 'dr_support_auto_check',
          includeRecentVeleroObjects: false,
        });
        for (let attempt = 0; attempt < 35; attempt += 1) {
          await new Promise(resolve => window.setTimeout(resolve, 1000));
          const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${clusterId}/inventory/requests/${submitted.requestId}`);
          if (status.status === 'succeeded') {
            if (!cancelled) await refreshPlatformData();
            return;
          }
          if (status.status === 'failed' || status.status === 'timeout') {
            if (!cancelled) toast('DR support check failed: ' + (status.message || status.errorCode || 'unknown error'));
            return;
          }
        }
        if (!cancelled) toast('DR support check timed out. Please refresh inventory and try again.');
      } catch (error) {
        if (!cancelled) toast('DR support check failed: ' + (error instanceof Error ? error.message : 'unknown error'));
      } finally {
        if (!cancelled) {
          setDrSupportCheckingKeys(prev => prev.filter(key => !unknownKeys.includes(key)));
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [stage, selectRows, currentClusterId]);
  const allVisibleSelected = selectableCurrentRows.length > 0 && selectableCurrentRows.every(app => selectedNames.includes(app.name));
  const toggleVisibleRows = () => {
    setSelectedNames(allVisibleSelected ? [] : selectableCurrentRows.map(app => app.name));
  };
  const toggleSelectedName = (name: string) => {
    setSelectedNames(prev => prev.includes(name) ? prev.filter(item => item !== name) : [...prev, name]);
  };
  const appSelectColumn: HyperTableColumn<AppItem> = {
    id: 'select',
    header: () => (
      <input
        type="checkbox"
        checked={allVisibleSelected}
        disabled={selectableCurrentRows.length === 0}
        onClick={event => event.stopPropagation()}
        onChange={toggleVisibleRows}
      />
    ),
    cell: info => {
      return (
        <input
          type="checkbox"
          checked={selectedNames.includes(info.row.original.name)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedName(info.row.original.name)}
        />
      );
    },
    size: 42,
    minSize: 42,
    maxSize: 54,
    enableSorting: false,
    enableResizing: false,
    meta: { align: 'center' },
  };
  const appNameColumn: HyperTableColumn<AppItem> = {
    id: 'name',
    header: 'Namespace',
    accessorFn: app => unitNamespaces(app).join(' '),
    size: 240,
    minSize: 150,
    maxSize: 520,
    cell: info => {
      const app = info.row.original;
      const namespaces = unitNamespaces(app);
      return (
        <button
          type="button"
          className="hbdr-dr-name-cell hbdr-dr-namespace-link"
          aria-label={`View details for ${namespaces.join(', ')}`}
          onClick={event => {
            event.stopPropagation();
            openNamespaceDetail(app);
          }}
        >
          <div className="hbdr-dr-namespace-icon"><Layers size={18} /></div>
          <div>
            {app.memberApps?.length ? (
              <div className="hbdr-merged-namespace-list">
                {namespaces.map(namespace => <p key={namespace} className="hbdr-dr-app-name">{namespace}</p>)}
              </div>
            ) : (
              <p className="hbdr-dr-app-name">{app.name}</p>
            )}
          </div>
        </button>
      );
    },
    meta: { title: app => unitNamespaces(app).join(', ') },
  };
  const appConfigTableColumns = useMemo<HyperTableColumn<AppItem>[]>(() => {
    const columns: HyperTableColumn<AppItem>[] = [appSelectColumn, appNameColumn];
    if (visibleConfigColumns.includes('status')) {
      columns.push({
        id: 'status',
        header: 'Status',
        accessorFn: app => appSortValue(app, 'status'),
        size: 120,
        minSize: 92,
        maxSize: 220,
        cell: info => {
          const app = info.row.original;
          return <span className={`hbdr-dr-status hbdr-dr-status-${(app.status || 'unknown').toLowerCase()}`}>{app.status || 'Unknown'}</span>;
        },
        meta: { title: app => app.status || 'Unknown' },
      });
    }
    if (stage === 'select' && visibleConfigColumns.includes('drSupport')) {
      columns.push({
        id: 'drSupport',
        header: 'DR Support',
        accessorFn: app => appSortValue(app, 'drSupport'),
        size: 150,
        minSize: appColumnMinWidth('drSupport'),
        maxSize: 240,
        cell: info => renderDRSupportBadge(info.row.original),
        meta: { className: 'hbdr-dr-support-cell' },
      });
    }
    if (visibleConfigColumns.includes('resource')) {
      columns.push({
        id: 'resource',
        header: 'Resource',
        accessorFn: app => appSortValue(app, 'resource'),
        size: 480,
        minSize: appColumnMinWidth('resource'),
        maxSize: 760,
        cell: info => renderResourceSummaryCell(info.row.original),
        meta: { title: app => `${resourceSummaryTotalForUnit(app)} namespaced resources` },
      });
    }
    if (visibleConfigColumns.includes('tags')) {
      columns.push({
        id: 'tags',
        header: 'Tags',
        accessorFn: app => appSortValue(app, 'tags'),
        size: 112,
        minSize: 96,
        maxSize: 260,
        cell: info => renderAppTags(info.row.original),
        meta: { title: app => appSortValue(app, 'tags').toString() },
      });
    }
    return columns;
  }, [allVisibleSelected, selectableCurrentRows, selectedNames, visibleConfigColumns, tags, syncTasks, liveRecoveryTasks, stage]);
  const appRunTableColumns = useMemo<HyperTableColumn<AppItem>[]>(() => {
    const columns: HyperTableColumn<AppItem>[] = [appSelectColumn, appNameColumn];
    if (visibleRunColumns.includes('drStatus')) {
      columns.push({
        id: 'drStatus',
        header: 'DR Config',
        accessorFn: app => appSortValue(app, 'drStatus'),
        size: 220,
        minSize: 190,
        maxSize: 300,
        cell: info => {
          const app = info.row.original;
          const plan = protectionPlanForApp(app);
          const meta = drStatusForPlan(plan?.status);
          const retryable = canRetryDrActivation(plan?.status);
          const normalizedPlanStatus = (plan?.status || '').toLowerCase();
          const canReconfigureStorage = normalizedPlanStatus === 'storage_failed' || normalizedPlanStatus === 'active_with_warning';
          const reconfigureTitle = normalizedPlanStatus === 'active_with_warning'
            ? 'Retry target BackupStorageLocation configuration'
            : 'Reconfigure BackupStorageLocation';
          const storageFailure = canReconfigureStorage ? storageFailureForApp(app) : null;
          return (
            <span className={`hbdr-dr-status-cell ${storageFailure ? 'is-storage-failure' : ''}`}>
              {storageFailure ? (
                <span className="hbdr-dr-storage-failure">
                  <TaskErrorStatus
                    code={storageFailure.task.errorCode}
                    title={storageFailure.presentation.message}
                    onClick={() => setSyncTaskDetail({ app, task: storageFailure.task, failure: taskFailureSummary(storageFailure.task) })}
                  />
                  <span className="hbdr-dr-storage-failure-help">{storageFailure.presentation.solution}</span>
                </span>
              ) : <span className="hbdr-dr-status-line">
                <span className={`hbdr-dr-status hbdr-dr-status-${meta.tone}`} title={meta.title}>
                  {meta.tone === 'ok' && <CheckCircle2 size={14} />}
                  {meta.tone === 'progress' && <RefreshCw size={14} />}
                  {meta.tone === 'warn' && <AlertTriangle size={14} />}
                  {meta.label}
                </span>
              </span>}
              {retryable && (
                <span className="hbdr-dr-status-actions">
                  <button
                    type="button"
                    className="hbdr-dr-status-retry"
                    title="Retry activation"
                    onClick={event => {
                      event.stopPropagation();
                      void retryDrActivation(app);
                    }}
                  >
                    <RefreshCw size={12} />
                    Retry
                  </button>
                </span>
              )}
              {canReconfigureStorage && (
                <span className="hbdr-dr-status-actions">
                  <button
                    type="button"
                    className="hbdr-dr-status-retry"
                    aria-label={reconfigureTitle}
                    title={storageFailure ? `${reconfigureTitle}. ${storageFailure.presentation.solution}` : reconfigureTitle}
                    onClick={event => {
                      event.stopPropagation();
                      void reconfigureDrStorage(app);
                    }}
                  >
                    <RefreshCw size={12} />
                    Retry
                  </button>
                </span>
              )}
            </span>
          );
        },
        meta: { title: app => drStatusMetaForApp(app).title },
      });
    }
    if (visibleRunColumns.includes('score')) {
      columns.push({
        id: 'score',
        header: 'Compliance',
        accessorFn: app => appSortValue(app, 'score'),
        size: 136,
        minSize: 108,
        maxSize: 220,
        cell: info => {
          const profile = profileOf(info.row.original);
          return <span className={`hbdr-dr-score ${profile.score === 100 ? 'hbdr-dr-score-ok' : 'hbdr-dr-score-warn'}`}><strong>{profile.score}</strong><em>POINTS</em></span>;
        },
      });
    }
    if (visibleRunColumns.includes('scope')) {
      columns.push({
        id: 'scope',
        header: 'Scope',
        accessorFn: app => appSortValue(app, 'scope'),
        size: 116,
        minSize: 96,
        maxSize: 260,
        cell: info => {
          const profile = profileOf(info.row.original);
          return <span className="hbdr-dr-scope">{formatScopeLabel(profile.scope)}{profile.scopeTag && <em>{profile.scopeTag}</em>}</span>;
        },
        meta: { title: app => String(appSortValue(app, 'scope')) },
      });
    }
    if (visibleRunColumns.includes('policy')) {
      columns.push({
        id: 'policy',
        header: 'Policy',
        accessorFn: app => appSortValue(app, 'policy'),
        size: 122,
        minSize: 104,
        maxSize: 260,
        cell: info => <span className="hbdr-dr-policy"><Clock size={15} />{info.row.original.policy || 'Daily Incremental'}</span>,
        meta: { title: app => app.policy || 'Daily Incremental' },
      });
    }
    if (visibleRunColumns.includes('repository')) {
      columns.push({
        id: 'repository',
        header: 'Repository',
        accessorFn: app => appSortValue(app, 'repository'),
        size: 190,
        minSize: 170,
        maxSize: 340,
        cell: info => {
          const usage = planStorageSizeForApp(info.row.original);
          return (
            <span className="hbdr-dr-storage-cell" title={usage.title}>
              <span className="hbdr-dr-storage">
                <Database size={15} />
                <span className="hbdr-dr-storage-text">
                  <strong>{info.row.original.storage || 'AWS-...'}</strong>
                </span>
              </span>
              {usage.label && <em className="hbdr-dr-storage-used">{usage.label}</em>}
            </span>
          );
        },
        meta: { title: app => planStorageSizeForApp(app).title || app.storage || 'AWS-...' },
      });
    }
    if (visibleRunColumns.includes('targetCluster')) {
      columns.push({
        id: 'targetCluster',
		header: 'DR Pair',
        accessorFn: app => appSortValue(app, 'targetCluster'),
        size: 260,
        minSize: 240,
        maxSize: 360,
        cell: info => {
          const target = info.row.original.targetCluster;
		  const source = clusters.find(cluster => cluster.id === info.row.original.clusterId)?.name || currentCluster?.name || 'Source';
		  return (
			<div className={`hbdr-dr-pair ${target ? '' : 'hbdr-dr-pair-empty'}`} title={`${source} → ${target || 'Not configured'}`}>
			  <span className="hbdr-dr-pair-route"><strong title={source}>{source}</strong><ChevronRight size={13} /><strong title={target || 'Not configured'}>{target || 'Not configured'}</strong></span>
			</div>
		  );
        },
		meta: { title: app => app.targetCluster ? `DR pair target: ${app.targetCluster}` : 'Target cluster is not configured' },
      });
    }
    if (visibleRunColumns.includes('task')) {
      columns.push({
        id: 'task',
        header: 'Sync Status',
        accessorFn: app => appSortValue(app, 'task'),
        size: 300,
        minSize: 280,
        maxSize: 520,
        cell: info => <span>{renderSyncTaskStatus(info.row.original)}</span>,
      });
    }
    if (visibleRunColumns.includes('recoveryTask')) {
      columns.push({
        id: 'recoveryTask',
        header: 'Recovery Status',
        accessorFn: app => appSortValue(app, 'recoveryTask'),
        size: 340,
        minSize: 300,
        maxSize: 580,
        cell: info => <span>{renderRecoveryTaskStatus(info.row.original)}</span>,
      });
    }
    if (visibleRunColumns.includes('resource')) {
      columns.push({
        id: 'resource',
        header: 'Resource',
        accessorFn: app => appSortValue(app, 'resource'),
        size: 480,
        minSize: appColumnMinWidth('resource'),
        maxSize: 760,
        cell: info => renderResourceSummaryCell(info.row.original),
        meta: { title: app => `${resourceSummaryTotalForUnit(app)} namespaced resources` },
      });
    }
    if (visibleRunColumns.includes('tags')) {
      columns.push({
        id: 'tags',
        header: 'Tags',
        accessorFn: app => appSortValue(app, 'tags'),
        size: 112,
        minSize: 96,
        maxSize: 260,
        cell: info => renderAppTags(info.row.original),
        meta: { title: app => appSortValue(app, 'tags').toString() },
      });
    }
    if (visibleRunColumns.includes('createdAt')) {
      columns.push({
        id: 'createdAt',
        header: 'Create Time',
        accessorFn: app => appSortValue(app, 'createdAt'),
        size: 170,
        minSize: appColumnMinWidth('createdAt'),
        maxSize: 260,
        cell: info => {
          const value = info.row.original.protectionPlanCreatedAt;
          return <span className="text-xs font-semibold text-slate-500">{value ? formatDateTime(value) : '-'}</span>;
        },
        meta: { title: app => app.protectionPlanCreatedAt ? formatDateTime(app.protectionPlanCreatedAt) : '-' },
      });
    }
    return columns;
  }, [allVisibleSelected, selectedNames, visibleRunColumns, tags, syncTasks, liveRecoveryTasks, liveRestorePoints, protectionPlans, platformTasks]);
  const singleSelectedApp = selectedNames.length === 1 ? currentRows.find(app => app.name === selectedNames[0]) || apps.find(app => app.name === selectedNames[0]) || null : null;
  const selectedRunRows = selectedRunApps
    .map(name => protectedRows.find(app => app.name === name) || apps.find(app => app.name === name))
    .filter((app): app is AppItem => Boolean(app));
  const selectedRunNotReadyRows = selectedRunRows.filter(app => !isProtectionPlanReady(protectionPlanForApp(app)?.status));
  const selectedRunHasCleanupRunning = selectedRunRows.some(app => isProtectionPlanCleaning(protectionPlanForApp(app)?.status));
  const selectedRunActiveSyncTasks = selectedRunRows
    .map(row => taskForUnit(syncTasks, row))
    .filter((task): task is ApiTask => Boolean(task?.id && isActiveTaskStatus(task.status)));
  const selectedRunCancelableSyncTasks = Array.from(new Map(
    selectedRunActiveSyncTasks
      .filter(task => (task.status || '').toLowerCase() !== 'canceling')
      .map(task => [task.id, task] as const),
  ).values());
  const selectedRunHasRunningSync = selectedRunActiveSyncTasks.length > 0;
  const canStartSync = selectedRunApps.length > 0 && !syncSubmitting && !selectedRunHasRunningSync && !selectedRunHasCleanupRunning && selectedRunNotReadyRows.length === 0;
  const canStopSync = selectedRunCancelableSyncTasks.length > 0;
  const restorePointsForApp = (app: AppItem) => {
    const realPoints = liveRestorePoints
      .filter(point => {
        if (point.status !== 'available') return false;
        if (point.sourceClusterId !== currentClusterId) return false;
        if (app.apiId && point.appId) return point.appId === app.apiId;
        if (app.protectionPlanId && point.protectionPlanId) return point.protectionPlanId === app.protectionPlanId;
        return point.sourceClusterId === currentClusterId && point.sourceNamespace === (app.namespace || app.name);
      })
      .sort((a, b) => (b.time || '').localeCompare(a.time || ''))
      .map(point => ({
        id: point.id,
        taskCreatedAt: point.taskCreatedAt,
        createdAt: point.createdAt,
        backupTaskId: point.backupTaskId,
        veleroBackupName: point.veleroBackupName,
        title: point.title || point.veleroBackupName || 'Restore point',
        rawTime: point.time,
        time: restorePointDisplayLabel(point),
        type: point.pointType === 'local' ? 'Local Snapshot' : 'Remote Snapshot',
        status: 'Available',
      }));
    if (realPoints.length > 0) return realPoints;
    return [];
  };
  const selectedRunActiveRecoveryTask = selectedRunRows.length === 1 ? recoveryTaskForUnit(selectedRunRows[0]) : undefined;
  const hasSelectedRunActiveRecoveryTask = isActiveTaskStatus(selectedRunActiveRecoveryTask?.status);
  const canRestoreAction = selectedRunRows.length === 1 && isProtectionPlanReady(protectionPlanForApp(selectedRunRows[0])?.status) && restorePointsForApp(selectedRunRows[0]).length > 0 && !hasSelectedRunActiveRecoveryTask;
  const buildRecoveryDraft = (mode: 'drill' | 'takeover', app: AppItem, pointId: string): RecoveryWizardConfig => {
    const selectedPoint = restorePointsForApp(app).find(point => point.id === pointId);
    const sourceType = selectedPoint?.type.toLowerCase().includes('local') ? 'snapshot' : 'export';
    const currentClusterName = currentCluster?.name || clusters[0]?.name || '';
    const targetCluster = app.targetCluster || clusters.find(cluster => cluster.id !== currentCluster?.id)?.name || currentClusterName;
    const targetMode = targetCluster === currentClusterName
      ? mode === 'drill' ? 'sandbox' : 'inPlace'
      : 'crossCluster';
    const primaryNamespace = unitNamespaces(app)[0] || app.name;
    return {
      pointId,
      sourceType,
      targetMode,
      targetCluster,
      namespaceMode: mode === 'takeover' ? 'original' : 'generated',
      targetNamespace: mode === 'takeover' ? primaryNamespace : `${primaryNamespace}-drill`,
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
        ? 'Validate service startup, storage attachment, and application smoke test after recovery.'
        : 'Confirm routing cutover, service dependencies, and production freeze before takeover.',
	  forceProceed: false,
    };
  };
  const openRestoreAction = (mode: 'drill' | 'takeover') => {
    if (selectedRunRows.length !== 1) {
      toast('Select one namespace first');
      return;
    }
    const activeTask = recoveryTaskForUnit(selectedRunRows[0]);
    if (isActiveTaskStatus(activeTask?.status)) {
      const label = activeTask?.type === 'drill' ? 'drill' : activeTask?.type === 'takeover' ? 'takeover' : 'recovery';
      toast(`A ${label} task is already running for this namespace`);
      return;
    }
    const points = restorePointsForApp(selectedRunRows[0]);
    if (points.length === 0) {
      toast('No recovery point is available for this namespace');
      return;
    }
    setRestoreAction({ mode, app: selectedRunRows[0], config: buildRecoveryDraft(mode, selectedRunRows[0], points[0].id) });
  };
  const confirmRestoreAction = async () => {
    if (!restoreAction) return;
    const point = restorePointsForApp(restoreAction.app).find(item => item.id === restoreAction.config.pointId);
    const livePoint = liveRestorePoints.find(item => item.id === restoreAction.config.pointId);
    if (!livePoint) {
      toast('Select a real restore point before starting recovery');
      return;
    }
    const sourceNamespaces = unitNamespaces(restoreAction.app);
    const targetNamespaces = restoreAction.config.namespaceMode === 'original'
      ? {}
      : sourceNamespaces.reduce<Record<string, string>>((acc, namespace) => {
          acc[namespace] = restoreAction.app.isMergedPlan ? `${namespace}-drill` : restoreAction.config.targetNamespace;
          return acc;
        }, {});
    const targetNamespace = restoreAction.config.namespaceMode === 'original'
      ? sourceNamespaces[0] || restoreAction.app.name
      : restoreAction.app.isMergedPlan ? targetNamespaces[sourceNamespaces[0]] || restoreAction.config.targetNamespace : restoreAction.config.targetNamespace;
    const targetCluster = clusters.find(cluster => cluster.name === restoreAction.config.targetCluster)
      || clusters.find(cluster => cluster.name === restoreAction.app.targetCluster)
      || clusters.find(cluster => cluster.id !== currentCluster?.id);
    if (!targetCluster) {
      toast('Select a target cluster before starting recovery');
      return;
    }
    const action = restoreAction;
    const submittedMessage = `${action.mode === 'drill' ? 'Drill' : 'Takeover'} job submitted: ${action.config.targetCluster} / ${targetNamespace} / ${point?.time || 'selected recovery point'}`;
    setRecoverySubmitting(true);
    try {
      const createdTask = await apiPost<ApiTask>(`/api/v1/tasks/${action.mode}`, {
          clusterId: targetCluster.id,
          protectionPlanId: livePoint.protectionPlanId || action.app.protectionPlanId,
          restorePointId: livePoint.id,
          veleroBackupName: livePoint.veleroBackupName,
          sourceNamespace: sourceNamespaces[0] || livePoint.sourceNamespace || action.app.namespace || action.app.name,
          sourceNamespaces,
          targetNamespace,
          targetNamespaces,
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
      });
      setLiveRecoveryTasks(prev => ({ ...prev, [action.app.name]: createdTask }));
      setSubmittingRecoveryTasks(prev => ({ ...prev, [action.app.name]: createdTask }));
      setDrTaskEvents(prev => ({
        ...prev,
        [createdTask.id]: prev[createdTask.id] || [],
      }));
      setRestoreAction(null);
      toast(submittedMessage);
      void refreshPlatformData();
    } catch (error) {
      toast('Failed to submit recovery task: ' + (error instanceof Error ? error.message : 'unknown error'));
    } finally {
      setRecoverySubmitting(false);
    }
  };
  const renderSyncTaskStatus = (app: AppItem) => {
    const task = taskForUnit(syncTasks, app);
    const sourceClusterId = unitMembers(app).map(member => member.clusterId).find(Boolean) || app.clusterId || currentClusterId;
    const sourceCluster = clusters.find(cluster => cluster.id === sourceClusterId) || currentCluster || clusters[0] || null;
    const sourceClusterOffline = Boolean(sourceCluster && sourceCluster.connectionStatus !== 'online');
    const appRestorePoints = restorePointsForApp(app);
    const taskPointId = task ? taskRestorePointId(task) : '';
    const taskRestorePoint = task
      ? appRestorePoints.find(point => (taskPointId && point.id === taskPointId) || point.backupTaskId === task.id)
      : undefined;
    const latestPoint = appRestorePoints[0];
    const taskBackupName = task ? String(task.payload?.veleroBackupName || task.payload?.backupName || '') : '';
    const taskVeleroRestorePoint = taskBackupName
      ? appRestorePoints.find(point => point.veleroBackupName === taskBackupName || point.title === taskBackupName || point.title.endsWith(`· ${taskBackupName}`))
      : undefined;
    const taskVisibleRestorePoint = taskRestorePoint || taskVeleroRestorePoint;
    const lastSnapshotTime = latestPoint?.time || '';
    const lastSnapshotLabel = lastSnapshotTime;
    const nextSyncHint = renderNextSyncHint(app);
    if (!task) {
      return lastSnapshotTime ? (
        <span className="hbdr-dr-last-snapshot">
          <strong>Last snapshot</strong>
          <em title={latestPoint?.veleroBackupName || latestPoint?.title || lastSnapshotLabel}>{lastSnapshotLabel}</em>
          {nextSyncHint}
        </span>
      ) : (
        <span className="hbdr-dr-sync-empty">
          <span className="hbdr-dr-task-neutral">No snapshot yet</span>
          {nextSyncHint}
        </span>
      );
    }

    if (isActiveTaskStatus(task.status)) {
      const events = drTaskEvents[task.id] || [];
      const finalizing = (task.status || '').toLowerCase() === 'finalizing';
      const canceling = (task.status || '').toLowerCase() === 'canceling';
      const waitingForAgent = sourceClusterOffline || (task.errorCode || '').toUpperCase() === 'AGENT_OFFLINE';
      const volume = taskProgressInfo(task, events);
      const hasVolumeBytes = Boolean(volume && volume.knownTotal && volume.totalBytes > 0);
      const configuringStorage = hasTaskEventReason(events, ['storage_preflight_started'])
        && !hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
      const progress = volume ? Math.max(0, Math.min(100, volume.percent)) : 0;
      const showProgressBar = !configuringStorage && !canceling && !finalizing && !waitingForAgent;
      if (waitingForAgent) {
        const failure = {
          code: normalizeErrorCode('AGENT_OFFLINE'),
          title: 'Cluster is offline',
          description: 'Reconnecting to source cluster agent. Pending tasks will resume automatically.',
          fullText: `Source cluster ${sourceCluster?.name || sourceClusterId || ''} agent is offline. The task will resume after the WebSocket connection is restored.`,
        };
        return (
          <TaskErrorStatus
            code={failure.code}
            title={failure.title}
            description={failure.description}
            detail={failure.fullText}
            onClick={event => {
              event.stopPropagation();
              setSyncTaskDetail({ app, task, failure });
            }}
          />
        );
      }
      const preparingMessage = syncPreparingMessage(events);
      const primary = canceling
        ? 'Canceling sync...'
        : finalizing
            ? 'Finalizing restore point...'
            : configuringStorage
              ? preparingMessage
              : volume
                ? `Syncing... ${formatPercent(progress)}%`
                : 'Syncing...';
      const details = !finalizing && hasVolumeBytes && volume
          ? [
              `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
              formatBytesPerSecond(volume.speedBytesPerSecond),
              formatEta(volume.etaSeconds),
            ].filter(Boolean).join(' · ')
          : '';
      return (
        <button type="button" className={`hbdr-dr-progress-cell hbdr-task-status-clickable ${canceling ? 'is-stopped' : 'is-syncing'}`} aria-label="View sync process" onPointerDown={event => { event.stopPropagation(); setSyncTaskDetail({ app, task }); }} onMouseDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
          {!showProgressBar && <em className="hbdr-sync-label">{primary}</em>}
          {showProgressBar && <i className="hbdr-progress-track"><b style={{ width: `${progress}%` }} /><span>{formatPercent(progress)}%</span></i>}
          {details && <small>{details}</small>}
        </button>
      );
    }

    if (isCompletedTaskStatus(task.status)) {
      if (taskHasWarning(task)) {
        const warning = taskFailureSummary(task, drTaskEvents[task.id] || []);
        return (
          <TaskErrorStatus
            code={warning.code}
            title={warning.title}
            description={warning.description}
            detail={warning.fullText}
            onClick={event => {
              event.stopPropagation();
              setSyncTaskDetail({ app, task, failure: warning });
            }}
          />
        );
      }
      const completedPoint = taskVisibleRestorePoint;
      const completedPointLabel = restorePointDisplayLabel(completedPoint) || 'Restore point creating...';
      return (
        <button type="button" className="hbdr-dr-last-snapshot hbdr-task-status-clickable" aria-label="View sync process" onPointerDown={event => { event.stopPropagation(); setSyncTaskDetail({ app, task }); }} onMouseDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
          <strong>Sync complete</strong>
          <em title={completedPoint?.veleroBackupName || completedPoint?.title || completedPointLabel}>{completedPointLabel}</em>
          {nextSyncHint}
        </button>
      );
    }

    const failure = taskFailureSummary(task, drTaskEvents[task.id] || []);
    return (
      <TaskErrorStatus
        code={failure.code}
        title={failure.title}
        description={failure.description}
        detail={failure.fullText}
        onClick={event => {
          event.stopPropagation();
          setSyncTaskDetail({ app, task, failure });
        }}
      />
    );
  };
  const renderRecoveryTaskStatus = (app: AppItem) => {
    const task = recoveryTaskForUnit(app);
    if (!task) return <span className="hbdr-dr-task-neutral">No recovery task</span>;

    const actionText = recoveryActionText(task.type);
    const targetNamespace = String(task.payload?.targetNamespace || app.namespace || app.name);
    const targetClusterId = String(task.clusterId || task.payload?.targetClusterId || '');
    const targetClusterName = clusters.find(cluster => cluster.id === targetClusterId)?.name
      || String(task.payload?.targetCluster || app.targetCluster || '');
    if (isActiveTaskStatus(task.status)) {
      const events = drTaskEvents[task.id] || [];
      const volume = taskProgressInfo(task, events);
      const hasVolumeBytes = Boolean(volume && volume.knownTotal && volume.totalBytes > 0);
      const configuringStorage = hasTaskEventReason(events, ['storage_preflight_started'])
        && !hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
      const progress = volume ? Math.max(0, Math.min(100, volume.percent)) : Math.max(0, Math.min(100, task.progress || 0));
      const showProgressBar = !configuringStorage;
      const details = hasVolumeBytes && volume
        ? [
            `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
            formatBytesPerSecond(volume.speedBytesPerSecond),
            formatEta(volume.etaSeconds),
          ].filter(Boolean).join(' · ')
        : '';
      const preparingMessage = recoveryPreparingMessage(events, task.type);
      const primary = configuringStorage ? preparingMessage : `${actionText.running} ${formatPercent(progress)}%`;
      return (
        <button type="button" className="hbdr-dr-progress-cell hbdr-recovery-task-progress hbdr-task-status-clickable is-syncing" aria-label="View recovery process" onPointerDown={event => { event.stopPropagation(); setSyncTaskDetail({ app, task }); }} onMouseDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
          {!showProgressBar && <em className="hbdr-sync-label">{primary}</em>}
          {showProgressBar && <i className="hbdr-progress-track"><b style={{ width: `${progress}%` }} /><span>{formatPercent(progress)}%</span></i>}
          {details && <small>{details}</small>}
        </button>
      );
    }

    if (isCompletedTaskStatus(task.status)) {
      const restorePointId = taskRestorePointId(task);
      const restorePoint = restorePointsForApp(app).find(point => point.id === restorePointId);
      const restorePointLabel = restorePointDisplayLabel(restorePoint) || 'restore point';
      const completedTitle = recoveryCompletedTargetTitle(restorePointLabel, task.completedAt, targetClusterName, targetNamespace, actionText.complete);
      const targetLabel = recoveryCompletedTargetLabel(targetClusterName, targetNamespace);
      return (
        <button type="button" className="hbdr-recovery-task-complete hbdr-task-status-clickable" aria-label="View recovery process" onPointerDown={event => { event.stopPropagation(); setSyncTaskDetail({ app, task }); }} onMouseDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
          <strong title={completedTitle}>[{restorePointLabel}] {actionText.complete.toLowerCase()}</strong>
          <em title={completedTitle}>{targetLabel}</em>
        </button>
      );
    }

    const failure = taskFailureSummary(task, drTaskEvents[task.id] || []);
    return (
      <TaskErrorStatus
        code={failure.code}
        title={failure.title}
        description={failure.description}
        detail={failure.fullText}
        onClick={event => {
          event.stopPropagation();
          setSyncTaskDetail({ app, task, failure });
        }}
      />
    );
  };
  const moveAppsToStage = async (names: string[], target: ApplicationStage) => {
    if (names.length === 0) return;
    const protection = target === 'run' ? 'protected' : target === 'config' ? 'pending_protection' : 'unprotected';
    const targetApps = apps.filter(app => names.includes(app.name) && app.apiId);
    if (targetApps.length === 0) {
      toast('No applications to update');
      return;
    }
    try {
      await Promise.all(targetApps.map(app => apiPatch(`/api/v1/applications/${app.apiId}`, { protectionStatus: protection })));
      setAppUiOverrides(prev => {
        const next = { ...prev };
        targetApps.forEach(app => {
          const key = appOverrideKey(app);
          next[key] = {
            ...(next[key] || {}),
            stage: target,
            protectionStatus: protection,
            isProtected: target === 'run',
            status: target === 'run' ? 'Protected' : mapApplicationStatus(app.status, false),
          };
        });
        return next;
      });
      toast(target === 'config' ? `Moved ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} to Setup DR`
        : target === 'select' ? `Moved ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} back to Select`
        : `Marked ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} as protected`);
      void refreshPlatformData();
    } catch (error) {
      toast('Failed to update application stage: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const handlePrimaryAction = async () => {
    if (stage === 'select') {
      if (selectedSelectApps.length === 0) {
        toast('Select applications to protect first');
        return;
      }
      const selectedAppItems = selectedSelectApps
        .map(name => apps.find(app => app.name === name))
        .filter((app): app is AppItem => Boolean(app));
      const unchecked = selectedAppItems.filter(isDRSupportUnknown);
      if (unchecked.length > 0) {
        toast(`Checking DR support for ${unchecked.length} namespace${unchecked.length === 1 ? '' : 's'}...`);
        try {
          const latestApps = await checkDRSupportBeforeStageMove(unchecked);
          const latestByKey = new Map(latestApps.map(app => [`${app.clusterId}:${app.namespace || app.name}`, app]));
          const unresolved = selectedAppItems.filter(app => {
            const latest = latestByKey.get(`${app.clusterId || currentClusterId}:${app.namespace || app.name}`);
            return !latest?.resourceSummary?.drSupport?.status;
          });
          if (unresolved.length > 0) {
            toast(`${unresolved[0].namespace || unresolved[0].name} DR support is still not checked. Please refresh inventory and try again.`);
            return;
          }
          const unsupported = selectedAppItems.filter(app => {
            const latest = latestByKey.get(`${app.clusterId || currentClusterId}:${app.namespace || app.name}`);
            return (latest?.resourceSummary?.drSupport?.status || '').toLowerCase() === 'unsupported';
          });
          if (unsupported.length > 0) {
            setDrSupportErrorDetail(unsupported[0]);
            return;
          }
        } catch (error) {
          toast('DR support check failed: ' + (error instanceof Error ? error.message : 'unknown error'));
          return;
        }
      }
      const blocked = selectedAppItems.filter(isDRUnsupported);
      if (blocked.length > 0) {
        setDrSupportErrorDetail(blocked[0]);
        return;
      }
      const names = selectedSelectApps.filter(name => {
        const app = apps.find(item => item.name === name);
        return !app || !isDRUnsupported(app);
      });
      setSelectedSelectApps([]);
      await moveAppsToStage(names, 'config');
      setStage('config');
      return;
    }

    if (stage === 'config') {
      if (selectedConfigApps.length === 0) {
        toast('Select one application to configure protection');
        return;
      }
      setProtectWizardMode('create');
      setProtectWizardStep(1);
      setProtectWizardOpen(true);
      return;
    }

    if (!canStartSync) {
      const notReady = selectedRunNotReadyRows[0];
      if (selectedRunApps.length === 0) {
        toast('Select applications to start sync');
      } else if (notReady) {
        const meta = drStatusMetaForApp(notReady);
        toast(`DR configuration is not ready: ${meta.label}. ${meta.title}`);
      } else {
        toast('Selected applications are syncing. Please cancel or wait for completion first');
      }
      return;
    }

    if (selectedRunApps.length === 0) {
      toast('Select applications to start backup');
      return;
    }
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    const appsWithoutPlan = selectedRunRows.filter(app => !app.protectionPlanId);
    if (appsWithoutPlan.length > 0) {
      toast('Configure DR before starting sync');
      return;
    }
    let syncWarning = '';
    setSyncSubmitting(true);
    try {
      const responses = await Promise.all(selectedRunRows.map(app => {
        const plan = protectionPlanForApp(app);
        return apiPost<ApiTaskResponse>('/api/v1/tasks/backup', {
          clusterId: plan?.sourceClusterId || app.clusterId || currentClusterId,
          appId: app.isMergedPlan ? '' : app.apiId || '',
          protectionPlanId: app.protectionPlanId || '',
          sourceNamespace: app.isMergedPlan ? '' : app.namespace,
          sourceNamespaces: app.isMergedPlan ? unitNamespaces(app) : undefined,
          trigger: 'manual',
        });
      }));
      syncWarning = responses.map(response => 'warning' in response ? response.warning : '').find(Boolean) || '';
      setSyncTasks(prev => {
        const next = { ...prev };
        selectedRunRows.forEach((app, index) => {
          const response = responses[index];
          const task = 'task' in response ? response.task : response;
          if (task?.id) {
            next[app.name] = task;
            unitMembers(app).forEach(member => {
              next[member.name] = task;
            });
          }
        });
        return next;
      });
      setSubmittingSyncTasks(prev => {
        const next = { ...prev };
        selectedRunRows.forEach((app, index) => {
          const response = responses[index];
          const task = 'task' in response ? response.task : response;
          if (!task?.id) return;
          [app.name, ...unitMembers(app).map(member => member.name)].forEach(key => {
            next[key] = task;
          });
        });
        return next;
      });
    } catch (error) {
      toast('Failed to submit sync job: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    } finally {
      setSyncSubmitting(false);
    }
    void refreshPlatformData();
    window.setTimeout(() => {
      void refreshPlatformData();
    }, 1300);
    setSelectedRunApps(selectedRunApps);
    toast(syncWarning || 'Sync job started');
  };
  const stopSync = async () => {
    if (!canStopSync) {
      toast(selectedRunApps.length === 0 ? 'Select applications to cancel sync' : 'Selected applications have no running sync job');
      return;
    }
    const taskCount = selectedRunCancelableSyncTasks.length;
    const confirmed = window.confirm(
      taskCount === 1
        ? 'Cancel this sync task? The source cluster agent will delete the running Velero backup. No restore point will be created for this sync.'
        : `Cancel ${taskCount} sync tasks? The source cluster agent will delete the running Velero backups. No restore point will be created for these syncs.`,
    );
    if (!confirmed) return;
    try {
      const responses = await Promise.all(selectedRunCancelableSyncTasks.map(task => apiPost<ApiTaskCancelResponse>(`/api/v1/tasks/${task.id}/cancel`, {})));
      setSyncTasks(prev => {
        const next = { ...prev };
        selectedRunRows.forEach(row => {
          const current = taskForUnit(next, row);
          const response = responses.find(item => item.task?.id === current?.id);
          if (response?.task) {
            next[row.name] = response.task;
            unitMembers(row).forEach(member => {
              next[member.name] = response.task;
            });
          }
        });
        return next;
      });
      await refreshPlatformData();
      const warning = responses.map(response => response.warning).find(Boolean);
      toast(warning ? warning.replace(/force stop/gi, 'cancel sync') : 'Cancel sync requested');
    } catch (error) {
      toast('Failed to cancel sync: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const modifyProtection = () => {
    if (selectedRunApps.length !== 1) {
      toast('Select one protected application to modify protection');
      return;
    }
    setProtectWizardMode('modify');
    setProtectWizardStep(1);
    setProtectWizardOpen(true);
  };
  const deleteDrConfiguration = async () => {
    if (selectedRunApps.length === 0) {
      toast('Select applications to cleanup resources');
      return;
    }
    const targetRows = selectedRunRows;
    const targetApps = targetRows.flatMap(app => app.memberApps?.length ? app.memberApps : [app]);
    const planIds = Array.from(new Set(targetRows.map(app => app.protectionPlanId).filter(Boolean) as string[]));
    const cleaningRows = targetRows.filter(app => isProtectionPlanCleaning(protectionPlanForApp(app)?.status));
    if (cleaningRows.length > 0) {
      toast('Selected resources are already being cleaned');
      return;
    }
    try {
      await Promise.all(planIds.map(planId => apiDelete<ApiProtectionPlan>(`/api/v1/protection-plans/${planId}`)));
      const appsWithoutPlan = targetApps.filter(app => !app.protectionPlanId && app.apiId);
      if (appsWithoutPlan.length > 0) {
        await Promise.all(appsWithoutPlan.map(app =>
          apiPatch(`/api/v1/applications/${app.apiId}`, { protectionStatus: 'pending_protection' })
        ));
      }
    } catch (error) {
      toast('Failed to cleanup resources: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    }
    setProtectionPlans(prev => prev.map(plan => planIds.includes(plan.id) ? { ...plan, status: 'cleanup_running' } : plan));
    setAppUiOverrides(prev => {
      const next = { ...prev };
      targetApps.forEach(app => {
        const key = appOverrideKey(app);
        next[key] = {
          ...(next[key] || {}),
          stage: 'run',
          protectionStatus: 'protected',
          isProtected: true,
          status: 'Protected',
        };
      });
      return next;
    });
    setSelectedRunApps([]);
    toast('Cleanup submitted');
    const pollCleanup = (remaining: number) => {
      window.setTimeout(async () => {
        try {
          await refreshPlatformData();
          const response = await apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
          const remainingPlans = listItems(response).filter(plan => planIds.includes(plan.id));
          if (remainingPlans.length > 0 && remaining > 0) {
            pollCleanup(remaining - 1);
          } else if (remainingPlans.length === 0) {
            setProtectionPlans(prev => prev.filter(plan => !planIds.includes(plan.id)));
            await refreshPlatformData();
            setAppUiOverrides(prev => {
              const next = { ...prev };
              targetApps.forEach(app => {
                const key = appOverrideKey(app);
                next[key] = {
                  ...(next[key] || {}),
                  stage: 'config',
                  protectionStatus: 'pending_protection',
                  isProtected: false,
                  status: mapApplicationStatus(app.status, false),
                  protectionPlanId: '',
                  protectionPlanCreatedAt: '',
                };
              });
              return next;
            });
            setSelectedConfigApps([]);
            setStage('config');
          }
        } catch {
          if (remaining > 0) pollCleanup(remaining - 1);
        }
      }, 3000);
    };
    void refreshPlatformData();
    pollCleanup(40);
  };
  const pollProtectionPlanActivation = (planIds: string[], remaining: number) => {
    if (planIds.length === 0 || remaining <= 0) return;
    window.setTimeout(async () => {
      try {
        await refreshPlatformData();
        const response = await apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
        const plans = listItems(response);
        const watched = plans.filter(plan => planIds.includes(plan.id));
        setProtectionPlans(prev => {
          const byId = new Map(prev.map(plan => [plan.id, plan]));
          watched.forEach(plan => {
            byId.set(plan.id, { ...(byId.get(plan.id) || {}), ...plan });
          });
          return Array.from(byId.values()).sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
        });
        const done = watched.length === planIds.length && watched.every(plan =>
          ['active', 'active_with_warning', 'storage_failed', 'schedule_failed', 'cleanup_failed'].includes((plan.status || '').toLowerCase())
        );
        if (!done) {
          pollProtectionPlanActivation(planIds, remaining - 1);
        } else {
          void refreshPlatformData();
        }
      } catch {
        pollProtectionPlanActivation(planIds, remaining - 1);
      }
    }, 1500);
  };
  const finishProtectWizard = async () => {
    const targetRows = protectWizardMode === 'modify'
      ? selectedRunRows
      : selectedConfigApps
        .map(name => apps.find(item => item.name === name))
        .filter((app): app is AppItem => Boolean(app));
    const targetApps = Array.from(new Set(targetRows.flatMap(row => unitMembers(row).map(app => app.name))));
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    if (!protectConfig.storageId) {
      toast('Select a storage repository before saving DR configuration');
      return;
    }
    const targetCluster = clusters.find(cluster => cluster.name === protectConfig.targetCluster);
    const policyId = policies.some(policy => policy.id === protectConfig.policy) ? protectConfig.policy : '';
    const scopeType = protectConfig.scope === 'filter' ? 'filtered' : 'all';
    const targetAppMeta = targetApps
      .map(name => apps.find(item => item.name === name))
      .filter((app): app is AppItem => Boolean(app?.apiId))
      .map(app => ({ apiId: app.apiId as string, name: app.name }));
    if (targetAppMeta.length === 0) {
      toast('No applications selected to protect');
      return;
    }
    const planGroups = protectConfig.mergeNamespaces || protectWizardMode === 'modify'
      ? [targetAppMeta]
      : targetAppMeta.map(meta => [meta]);
    const createdPlans: ApiProtectionPlan[] = [];
    const planIdByAppName: Record<string, string> = {};
    try {
      for (const group of planGroups) {
        const createdPlan = await apiPost<ApiProtectionPlan>('/api/v1/protection-plans', {
          sourceClusterId: currentClusterId,
          appIds: group.map(meta => meta.apiId),
          scopeType,
          includedResources: protectConfig.scope === 'filter' && !protectConfig.includeAllResources ? protectConfig.includedResources : [],
          labelSelector: protectConfig.scope === 'filter' ? protectConfig.labelSelector : { matchLabels: {}, matchExpressions: [] },
          excludedResources: protectConfig.scope === 'filter' ? protectConfig.excludedResources : [],
          includeClusterScoped: false,
          storageRepoId: protectConfig.storageId,
          policyId,
          targetClusterId: targetCluster?.id || currentClusterId,
          preHooks: protectConfig.preScripts.map(scriptPayload),
          postHooks: protectConfig.postScripts.map(scriptPayload),
        });
        createdPlans.push(createdPlan);
        group.forEach(meta => {
          planIdByAppName[meta.name] = createdPlan.id;
        });
      }
      // Promote the applications to "protected" so the protection_status in
      // the database matches the stage-3 UI state, and a fresh page load
      // still shows these apps in stage 3.
      await Promise.all(targetAppMeta.map(meta =>
        apiPatch(`/api/v1/applications/${meta.apiId}`, { protectionStatus: 'protected' })
      ));
	  // Capability evidence is collected on demand after DR configuration.
	  // Periodic inventory intentionally does not upload cluster-wide API data.
	  await Promise.allSettled(targetAppMeta.flatMap(meta => {
		const requests: Promise<unknown>[] = [apiPost(`/api/v1/clusters/${currentClusterId}/inventory/request`, { scope: 'capabilities', namespace: meta.name, includeDetails: true, reason: 'dr_configuration' })];
		if (targetCluster?.id && targetCluster.id !== currentClusterId) {
		  requests.push(apiPost(`/api/v1/clusters/${targetCluster.id}/inventory/request`, { scope: 'capabilities', namespace: meta.name, includeDetails: true, reason: 'dr_configuration' }));
		}
		return requests;
	  }));
      const selectedPolicy = policies.find(policy => policy.id === policyId);
      const selectedStorage = storage.find(repo => repo.id === protectConfig.storageId);
      const selectedTarget = targetCluster || clusters.find(cluster => cluster.id === currentClusterId);
      setProtectionPlans(prev => {
        const byId = new Map(prev.map(plan => [plan.id, plan]));
        createdPlans.forEach(plan => {
          byId.set(plan.id, { ...(byId.get(plan.id) || {}), ...plan });
        });
        return Array.from(byId.values()).sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
      });
      setAppUiOverrides(prev => {
        const next = { ...prev };
        targetAppMeta.forEach(meta => {
          const createdPlan = createdPlans.find(plan => plan.id === planIdByAppName[meta.name]);
          const app = apps.find(item => item.name === meta.name);
          const key = app ? appOverrideKey(app) : `${currentClusterId}::${meta.name}`;
          next[key] = {
            ...(next[key] || {}),
            stage: 'run',
            protectionStatus: 'protected',
            protectionPlanId: planIdByAppName[meta.name],
            protectionPlanCreatedAt: createdPlan?.createdAt,
            policy: selectedPolicy?.name,
            storage: selectedStorage?.name,
            targetCluster: selectedTarget?.name,
            isProtected: true,
            status: 'Protected',
            lastBackup: 'synced recently',
          };
        });
        return next;
      });
    } catch (error) {
      toast('Failed to create protection plan: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    }
    setProtectedAppNames(prev => Array.from(new Set([...prev, ...targetApps])));
    if (protectWizardMode === 'create') {
      setConfigAppNames(prev => prev.filter(name => !targetApps.includes(name)));
    }
    setSelectedRunApps(protectConfig.mergeNamespaces && createdPlans.length === 1 ? [`plan:${createdPlans[0].id}`] : targetApps);
    setSelectedConfigApps([]);
    setProtectWizardOpen(false);
    setProtectWizardMode('create');
    setProtectWizardStep(1);
    setStage('run');
    void refreshPlatformData();
    pollProtectionPlanActivation(createdPlans.map(plan => plan.id), 40);
    const warning = createdPlans.map(plan => plan.warning).find(Boolean);
    const activating = createdPlans.some(plan => plan.status && plan.status !== 'active');
    if (warning) {
      toast(`DR configuration saved, activation pending: ${warning}`);
    } else if (activating) {
      toast('DR configuration saved. Protection plan activation is in progress.');
    } else {
      toast(protectWizardMode === 'modify' ? 'DR configuration updated' : 'DR protection enabled');
    }
  };
  const resetExcludeRuleForm = () => {
    setNewExcludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
    setEditingRuleIndex(null);
    setShowAddRuleForm(false);
  };
  const saveExcludeRule = () => {
    const normalizedRule = {
      group: '',
      resource: newExcludeRule.resource,
      name: '',
      version: '',
      labels: '',
    };
    if (!Object.values(normalizedRule).some(Boolean)) {
      resetExcludeRuleForm();
      return;
    }
    setProtectConfig(prev => {
      const rules = [...prev.excludeRules];
      if (editingRuleIndex === null) {
        rules.push(normalizedRule);
      } else {
        rules[editingRuleIndex] = normalizedRule;
      }
      return { ...prev, excludeRules: rules };
    });
    resetExcludeRuleForm();
  };
  const editExcludeRule = (rule: typeof newExcludeRule, index: number) => {
    setNewExcludeRule({
      group: rule.group || '',
      resource: rule.resource || '',
      name: rule.name || '',
      version: rule.version || '',
      labels: rule.labels || '',
    });
    setEditingRuleIndex(index);
    setShowAddRuleForm(true);
  };
  const saveScript = (
    type: 'preScripts' | 'postScripts',
    script: { name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean },
    index: number | null = null
  ) => {
    setProtectConfig(prev => {
      const currentScripts = [...prev[type]];
      if (index === null) {
        const existingIndex = currentScripts.findIndex(item => item.name === script.name);
        const hasEntry = currentScripts.some((item, itemIndex) => (item.isEntry ?? itemIndex === 0));
        const nextScript = { ...script, isEntry: script.isEntry ?? !hasEntry };
        if (existingIndex >= 0) {
          currentScripts[existingIndex] = { ...nextScript, isEntry: currentScripts[existingIndex].isEntry ?? existingIndex === 0 };
        } else {
          currentScripts.push(nextScript);
        }
      } else {
        currentScripts[index] = { ...script, isEntry: script.isEntry ?? currentScripts[index]?.isEntry ?? index === 0 };
      }
      return { ...prev, [type]: currentScripts };
    });
  };
  const handleFileUpload = async (type: 'preScripts' | 'postScripts', event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const content = await file.text().catch(() => '');
    saveScript(type, {
      name: file.name,
      size: file.size,
      lastModified: file.lastModified,
      content,
      source: 'upload',
    });
    event.target.value = '';
  };
  const removeScript = (type: 'preScripts' | 'postScripts', index: number) => {
    setProtectConfig(prev => {
      const removedWasEntry = prev[type][index]?.isEntry ?? index === 0;
      const list = prev[type].filter((_, itemIndex) => itemIndex !== index);
      if (removedWasEntry && list.length > 0 && !list.some(item => item.isEntry)) {
        list[0] = { ...list[0], isEntry: true };
      }
      return { ...prev, [type]: list };
    });
  };
  const setEntryScript = (type: 'preScripts' | 'postScripts', index: number) => {
    setProtectConfig(prev => {
      if (index < 0 || index >= prev[type].length) return prev;
      const list = prev[type].map((script, itemIndex) => ({ ...script, isEntry: itemIndex === index }));
      return { ...prev, [type]: list };
    });
  };

  useEffect(() => {
    const activeIds = [...Object.values(liveAppTasks), ...Object.values(liveRecoveryTasks)]
      .filter(t => isActiveTaskStatus(t.status))
      .map(t => t.id);
    if (activeIds.length === 0) return;
    let cancelled = false;
    const activeIdSet = new Set(activeIds);
    const pollTaskStatus = async () => {
      try {
        const taskRes = await apiGet<ApiList<ApiTask>>('/api/v1/tasks');
        if (cancelled) return;
        const latestTasks = new Map(listItems(taskRes).filter(task => activeIdSet.has(task.id)).map(task => [task.id, task]));
        if (latestTasks.size === 0) return;
        const hasSettledTask = Array.from(latestTasks.values()).some(task => !isActiveTaskStatus(task.status));
        setLiveAppTasks(prev => {
          let changed = false;
          const next = Object.fromEntries(Object.entries(prev).map(([key, task]) => {
            const latest = latestTasks.get(task.id);
            if (!latest) return [key, task];
            changed = true;
            return [key, { ...task, ...latest }];
          }));
          return changed ? next : prev;
        });
        setLiveRecoveryTasks(prev => {
          let changed = false;
          const next = Object.fromEntries(Object.entries(prev).map(([key, task]) => {
            const latest = latestTasks.get(task.id);
            if (!latest) return [key, task];
            changed = true;
            return [key, { ...task, ...latest }];
          }));
          return changed ? next : prev;
        });
        if (hasSettledTask) {
          void refreshPlatformData();
        }
      } catch {
        // Keep the current list stable if status polling fails.
      }
    };
    pollTaskStatus();
    const timer = window.setInterval(pollTaskStatus, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [liveAppTasks, liveRecoveryTasks, setLiveAppTasks, setLiveRecoveryTasks, refreshPlatformData]);

  return (
    <>
      <div className="hbdr-stage-panel">
      <div className="hbdr-stage-grid hbdr-screenshot-stage-grid">
        {stageCards.map((card, index) => (
          <React.Fragment key={card.key}>
            <button onClick={() => setStage(card.key)} className={`hbdr-stage-card hbdr-dr-stage-card hbdr-dr-stage-${card.tone} ${stage === card.key ? 'hbdr-dr-stage-active' : ''}`}>
              <div className="hbdr-dr-stage-icon"><card.icon size={32} /></div>
              <div className="hbdr-dr-stage-copy">
                <h4>{index + 1}. {card.title}</h4>
                <p>{card.desc}</p>
                <span>{card.list}</span>
              </div>
              <div className="hbdr-dr-stage-check">{stage === card.key ? <Check size={17} /> : null}</div>
              <div className="hbdr-dr-stage-count">
                <strong>{card.count}</strong>
              </div>
            </button>
            {index < stageCards.length - 1 && <div className="hbdr-stage-arrow"><ChevronRight size={28} /></div>}
          </React.Fragment>
        ))}
      </div>
      </div>

      <div className="hbdr-dr-table-card">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className={`hbdr-dr-action-group hbdr-dr-action-group-managed hbdr-dr-action-group-${stage}`}>
            {stage === 'select' && (
              <button
                type="button"
                onClick={handlePrimaryAction}
                disabled={primaryDisabled}
                title={selectedHasUnsupportedDR ? `${unsupportedDRSelectionMessage} Open View Details or check DR Support for details.` : undefined}
                className="hbdr-dr-action-primary"
              >
                <ChevronRight size={15} />Next
              </button>
            )}
            {stage === 'config' && (
              <>
                <button
                  type="button"
                  onClick={async () => {
                    if (selectedConfigApps.length === 0) {
                      toast('Select applications to move back');
                      return;
                    }
                    const names = [...selectedConfigApps];
                    setSelectedConfigApps([]);
                    await moveAppsToStage(names, 'select');
                    setStage('select');
                  }}
                  disabled={selectedConfigApps.length === 0}
                  className="hbdr-dr-action-primary hbdr-dr-action-ghost"
                >
                  Move Back
                </button>
                <button onClick={handlePrimaryAction} disabled={primaryDisabled} className="hbdr-dr-action-primary">DR Configuration</button>
              </>
            )}
            {isRunStage && (
              <>
                <button onClick={handlePrimaryAction} disabled={!canStartSync} className="hbdr-dr-action-primary">{syncSubmitting ? 'Submitting…' : 'Start Sync'}</button>
                <button disabled={!canRestoreAction} title={hasSelectedRunActiveRecoveryTask ? 'A recovery task is already running' : undefined} onClick={() => openRestoreAction('drill')} className="hbdr-dr-action-primary hbdr-dr-action-ghost">Drill</button>
                <button disabled={!canRestoreAction} title={hasSelectedRunActiveRecoveryTask ? 'A recovery task is already running' : undefined} onClick={() => openRestoreAction('takeover')} className="hbdr-dr-action-danger">Takeover</button>
              </>
            )}
            <div className="relative">
              <button onClick={() => setAppBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                More <ChevronDown size={15} className={appBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
              </button>
              <AnimatePresence>
                {appBulkMenuOpen && (
                  <>
                    <div className="fixed inset-0 z-30" onClick={() => setAppBulkMenuOpen(false)} />
                    <motion.div
                      initial={{ opacity: 0, y: 8, scale: 0.96 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 8, scale: 0.96 }}
                      className="absolute right-0 top-11 z-40 w-52 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"
                    >
                      <button
                        disabled={!singleSelectedApp}
                        onClick={() => {
                          if (!singleSelectedApp) return;
                          openNamespaceDetail(singleSelectedApp);
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <span className="flex items-center gap-2"><Eye size={14} className="text-indigo-500" />View Details</span>
                        {!singleSelectedApp && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                      </button>
                      {isRunStage && (
                        <>
                          <button
                            disabled={selectedRunApps.length !== 1 || selectedRunHasCleanupRunning}
                            title="Edit protection configuration"
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              modifyProtection();
                            }}
                            className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <span className="flex items-center gap-2"><Edit2 size={14} className="text-blue-500" />Edit</span>
                            {selectedRunApps.length !== 1 && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                          </button>
                          <button
                            disabled={!canStopSync}
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              void stopSync();
                            }}
                            className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <AlertCircle size={14} />Cancel Sync
                          </button>
                          <button
                            disabled={selectedRunApps.length === 0 || selectedRunHasCleanupRunning}
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              deleteDrConfiguration();
                            }}
                            className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <ShieldOff size={14} />Cleanup Resources
                          </button>
                        </>
                      )}
                      <button
                        disabled={selectedNames.length === 0 || tags.length === 0}
                        onClick={() => {
                          setTagAction('attach');
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <Archive size={14} className="text-blue-500" />Attach Tag
                      </button>
                      <button
                        disabled={selectedAttachedTagIds.length === 0}
                        onClick={() => {
                          setTagAction('detach');
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <Archive size={14} className="text-amber-500" />Detach Tag
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
              queryFields={appQueryFields}
              tags={[
                ...tags.map(tag => ({ value: tag.id, label: tag.name, count: currentRows.filter(app => appHasTag(app, tag.id)).length })),
              ]}
              activeTags={activeTags}
              setActiveTags={setActiveTags}
              filters={appFilterChoices}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={appColumns}
              visibleColumns={visibleAppColumns}
              setVisibleColumns={isRunStage ? setVisibleRunColumns : setVisibleConfigColumns}
              onRefresh={async () => {
                setSelectedSelectApps([]);
                setSelectedConfigApps([]);
                setSelectedRunApps([]);
                if (isRunStage) {
                  try {
                    await refreshPlatformData();
                    toast('Application list refreshed');
                  } catch {
                    toast('Failed to refresh application list');
                  }
                  return;
                }
                await refreshCurrentClusterInventory();
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={isRunStage ? appRunTableColumns : appConfigTableColumns}
          data={currentRows}
          getRowId={row => row.name}
          getRowClassName={row => [
            selectedNames.includes(row.name) ? 'hbdr-dr-row-selected' : '',
            stage === 'select' && isDRUnsupported(row) ? 'hbdr-dr-row-unsupported' : '',
          ].filter(Boolean).join(' ')}
          selectedCount={selectedNames.length}
          emptyMessage={query ? 'No applications match the current search.' : 'No applications in the current stage. Switch to another stage to view data.'}
          className="hbdr-dr-main-table"
        />
      </div>

      <AnimatePresence>
        {resourceDetail && (
          <div className="fixed inset-0 z-50">
            {(() => {
              const namespace = resourceDetail.app.namespace || resourceDetail.app.name;
              const clusterId = resourceDetail.app.clusterId || currentClusterId || '';
              const isRefreshing = resourceRefreshKey === `${clusterId}:${namespace}`;
              const refreshState = resourceRefreshStatus?.key === `${clusterId}:${namespace}` ? resourceRefreshStatus : null;
              return (
                <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="hbdr-filter-drawer-backdrop"
              onClick={() => setResourceDetail(null)}
            />
            <motion.div
              initial={{ opacity: 0, x: 32 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 32 }}
              transition={{ duration: 0.16, ease: 'easeOut' }}
              className="hbdr-filter-drawer hbdr-resource-drawer"
            >
              <div className="hbdr-filter-drawer-head hbdr-resource-drawer-head">
                <div className="hbdr-resource-modal-title">
                  <span className="hbdr-resource-modal-icon"><Grid3X3 size={18} /></span>
                  <div>
                    <h3>Namespace Resources</h3>
                    <p>{resourceDetail.app.namespace || resourceDetail.app.name}</p>
                  </div>
                </div>
                <div className="hbdr-resource-modal-head-right">
                  <div className="hbdr-resource-modal-meta">
                    <span>
                      <em>Groups</em>
                      <strong>{resourceDetailGroups.length}</strong>
                    </span>
                    <span>
                      <em>Objects</em>
                      <strong>{resourceSummaryTotal(resourceDetail.app)}</strong>
                    </span>
                    <span>
                      <em>Storage</em>
                      <strong>{resourceDetail.app.pvCapacityBytes ? formatBytes(resourceDetail.app.pvCapacityBytes) : '0 B'}</strong>
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => void refreshNamespaceResources(resourceDetail.app)}
                    disabled={isRefreshing}
                    title="Refresh namespace resources from the cluster agent"
                    aria-label="Refresh namespace resources"
                  >
                    <RefreshCw size={18} className={isRefreshing ? 'animate-spin' : ''} />
                  </button>
                  <button type="button" onClick={() => setResourceDetail(null)} aria-label="Close resource details"><X size={18} /></button>
                </div>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-resource-detail">
                {refreshState && (
                  <div className={`hbdr-sync-detail-message ${refreshState.status === 'failed' || refreshState.status === 'timeout' ? 'is-warning' : ''}`}>
                    <strong>{refreshState.status === 'succeeded' ? 'Inventory refreshed' : refreshState.status === 'pending' ? 'Refreshing inventory' : 'Inventory refresh needs attention'}</strong>
                    <p>{refreshState.message || refreshState.status}</p>
                  </div>
                )}
                {resourceDetailGroups.length > 0 ? (
                  <div className="hbdr-resource-inventory">
                    <div className="hbdr-resource-overview-strip">
                      {resourceDetailGroups.map(group => {
                        const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                        return (
                          <span key={group.category.key}>
                            <Icon size={15} />
                            <strong>{group.category.total}</strong>
                            <em>{group.category.label}</em>
                          </span>
                        );
                      })}
                    </div>
                    {resourceDetailGroups.map(group => {
                      const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                      return (
                        <section key={group.category.key} className="hbdr-resource-inventory-section">
                          <div className="hbdr-resource-inventory-divider">
                            <span><Icon size={14} />{group.category.label}</span>
                            <em>{group.category.total} objects</em>
                          </div>
                          <div className="hbdr-resource-inventory-table">
                            <div className="hbdr-resource-inventory-columns">
                              <span>Resource</span>
                              <span>Type</span>
                              <span>Details</span>
                            </div>
                            {group.items.flatMap(item => {
                              const rows = item.resources || [];
                              if (rows.length === 0) {
                                return [(
                                  <div key={`${group.category.key}-${item.kind}-count`} className="hbdr-resource-inventory-count-row">
                                    <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                    <strong>{item.kind}</strong>
                                    <em>{item.count} reported, object details pending</em>
                                  </div>
                                )];
                              }
                              return rows.map(resource => {
                                const detailText = resourceInventoryDetailText(resource, item, resourceDetail.app.namespace);
                                return (
                                  <div
                                    key={`${item.kind}-${resource.namespace || resourceDetail.app.namespace}-${resource.name}`}
                                    className="hbdr-resource-inventory-item"
                                    title={resourceInventoryTitle(resource, item, resourceDetail.app.namespace)}
                                  >
                                    <div className="hbdr-resource-inventory-resource">
                                      <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                      <div>
                                        <strong>{resource.name}</strong>
                                        <em>{resource.namespace || resourceDetail.app.namespace} · {formatAge(resource.ageSeconds)}</em>
                                      </div>
                                    </div>
                                    <div className="hbdr-resource-inventory-type">{item.kind}</div>
                                    <div className="hbdr-resource-inventory-detail-text">{detailText}</div>
                                  </div>
                                );
                              });
                            })}
                          </div>
                        </section>
                      );
                    })}
                  </div>
                ) : (
                  <div className="hbdr-resource-detail-empty">
                    <Database size={18} />
                    <strong>No namespaced resources found</strong>
                    <span>The agent has not reported resources for this namespace yet.</span>
                  </div>
                )}
              </div>
            </motion.div>
                </>
              );
            })()}
          </div>
        )}
      </AnimatePresence>

	  <AnimatePresence>
        {tagAction && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0 bg-slate-900/45 backdrop-blur-sm" onClick={() => setTagAction(null)} />
            <motion.div initial={{ opacity: 0, y: 14, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 14, scale: 0.98 }} className="hbdr-tag-action-modal">
              <div className="hbdr-tag-action-head">
                <div>
                  <strong>{tagAction === 'attach' ? 'Attach Tag' : 'Detach Tag'}</strong>
                  <span>{selectedNames.length} selected application{selectedNames.length === 1 ? '' : 's'}</span>
                </div>
                <button type="button" onClick={() => setTagAction(null)} aria-label="Close"><X size={16} /></button>
              </div>
              <div className="hbdr-tag-action-list">
                {(tagAction === 'attach' ? tags : tags.filter(tag => selectedAttachedTagIds.includes(tag.id))).map(tag => (
                  <button key={tag.id} type="button" onClick={() => tagAction === 'attach' ? attachTagToSelected(tag.id) : detachTagFromSelected(tag.id)}>
                    <Archive size={15} />
                    <span>
                      <strong>{tag.name}</strong>
                      <em>{tagAction === 'attach' ? 'Available tag' : 'Attached tag'}</em>
                    </span>
                    <ChevronRight size={15} />
                  </button>
                ))}
                {(tagAction === 'attach' ? tags : tags.filter(tag => selectedAttachedTagIds.includes(tag.id))).length === 0 && (
                  <div className="hbdr-tag-action-empty">
                    <Archive size={24} />
                    <strong>No tags available</strong>
                    <span>{tagAction === 'attach' ? 'Create tags in Tag Management first.' : 'Selected applications do not have attached tags.'}</span>
                  </div>
                )}
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {drSupportErrorDetail && (
          <ErrorDetailModalFrame title="DR Support Details" onClose={() => setDrSupportErrorDetail(null)}>
            <TaskErrorDetailBlock
              failure={drSupportFailureForApp(drSupportErrorDetail)}
              details={drSupportFailureDetailsForApp(drSupportErrorDetail)}
            />
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {syncTaskDetail && (
          <ErrorDetailModalFrame title={`${taskDetailLabel(syncTaskDetail.task.type)} Task Details`} onClose={() => setSyncTaskDetail(null)}>
            {(() => {
              const events = drTaskEvents[syncTaskDetail.task.id] || [];
              const failure = syncTaskDetail.failure || taskFailureSummary(syncTaskDetail.task, events);
              const details = taskFailureDetails(syncTaskDetail.task, events);
              const showError = isFailedStatus(syncTaskDetail.task.status) || taskHasWarning(syncTaskDetail.task);
              return (
                <div className="hbdr-sync-detail">
                  <TaskProcessTimeline task={syncTaskDetail.task} events={events} />
                  {showError && <TaskErrorDetailBlock failure={failure} details={details} />}
                  <TaskFinalResult task={syncTaskDetail.task} events={events} />
                </div>
              );
            })()}
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {restoreAction && (
          <RecoveryWizardModal
            open
            mode={restoreAction.mode}
            app={restoreAction.app}
            profile={profileOf(restoreAction.app)}
            currentClusterName={currentCluster?.name || 'Current Cluster'}
            points={restorePointsForApp(restoreAction.app)}
            clusterOptions={clusters.map(cluster => ({
              id: cluster.id,
              name: cluster.name,
              region: cluster.region,
              version: cluster.version,
              isCurrent: currentCluster?.id === cluster.id,
            }))}
            repositoryOptions={storage.map(repo => ({
              id: repo.id,
              name: repo.name,
              type: repo.type,
              endpoint: repo.endpoint,
              bucket: repo.bucket,
            }))}
            config={restoreAction.config}
            setConfig={updater => {
              setRestoreAction(prev => {
                if (!prev) return prev;
                const nextConfig = typeof updater === 'function' ? updater(prev.config) : updater;
                return { ...prev, config: nextConfig };
              });
            }}
            onClose={() => setRestoreAction(null)}
            onSubmit={confirmRestoreAction}
            submitting={recoverySubmitting}
			readinessBlockers={0}
          />
        )}
      </AnimatePresence>

      <DrConfigurationModal
        open={protectWizardOpen}
        step={protectWizardStep}
        setStep={setProtectWizardStep}
        onClose={() => setProtectWizardOpen(false)}
        onFinish={finishProtectWizard}
        targetSummary={wizardTargetSummary}
        targetCount={wizardTargetNames.length}
        targetNames={wizardTargetNames}
        protectConfig={protectConfig}
        setProtectConfig={setProtectConfig}
        showAddRuleForm={showAddRuleForm}
        setShowAddRuleForm={setShowAddRuleForm}
        newExcludeRule={newExcludeRule}
        setNewExcludeRule={setNewExcludeRule}
        editingRuleIndex={editingRuleIndex}
        resetExcludeRuleForm={resetExcludeRuleForm}
        saveExcludeRule={saveExcludeRule}
        editExcludeRule={editExcludeRule}
        storage={storage}
        policyOptions={policyOptions}
        filteredPolicyOptions={filteredPolicyOptions}
        paginatedPolicyOptions={paginatedPolicyOptions}
        wizardPolicySearchQuery={wizardPolicySearchQuery}
        setWizardPolicySearchQuery={setWizardPolicySearchQuery}
        setWizardPolicyPage={setWizardPolicyPage}
        wizardPolicyPage={wizardPolicyPage}
        wizardPolicyTotalPages={wizardPolicyTotalPages}
        wizardPolicyPageSize={wizardPolicyPageSize}
        targetClusterOptions={targetClusterOptions}
        labelOptions={wizardLabelOptions}
        preScriptRef={preScriptRef}
        postScriptRef={postScriptRef}
        handleFileUpload={handleFileUpload}
        saveScript={saveScript}
        removeScript={removeScript}
        setEntryScript={setEntryScript}
        onCreateStorage={openStorage}
        onRegisterCluster={openClusters}
        onCreatePolicy={openPolicies}
      />
      <AnimatePresence>
        {selectedDetailApp && (() => {
          const detailProfile = profileOf(selectedDetailApp);
          const protectedState = selectedDetailApp?.stage === 'run';
          const detailStorage = selectedDetailApp.storage || 'No backup repository';
          const detailPolicy = selectedDetailApp.policy || 'No policy bound';
          const detailStatus = protectedState ? 'Protected' : selectedDetailApp.stage === 'config' ? 'Configured' : 'Discovered';
          const supportMeta = drSupportMetaForApp(selectedDetailApp);
          const supportFailure = drSupportFailureForApp(selectedDetailApp);
          const supportDetails = drSupportFailureDetailsForApp(selectedDetailApp);
          const categories = normalizeResourceCategories(selectedDetailApp).filter(category => category.total > 0);
          const storageCategory = categories.find(category => category.key === 'storage');
          const pvcItem = storageCategory?.items?.find(item => item.kind === 'PersistentVolumeClaim');
          const pvcRows = pvcItem?.resources || [];
          const capacityBytes = selectedDetailApp.pvCapacityBytes || selectedDetailApp.resourceSummary?.pvCapacityBytes || 0;
          const detailStage = stageOf(selectedDetailApp);
          const detailPlan = protectionPlanForApp(selectedDetailApp);
          const detailPlanId = selectedDetailApp.protectionPlanId || detailPlan?.id || '';
          const detailPlanMeta = drStatusMetaForApp(selectedDetailApp);
          const detailPolicyObject = detailPlan ? policies.find(item => item.id === detailPlan.policyId) : undefined;
          const namespaces = unitNamespaces(selectedDetailApp);
          const namespaceTitle = selectedDetailApp.isMergedPlan ? `${namespaces.length} namespaces` : (namespaces[0] || selectedDetailApp.namespace || selectedDetailApp.name);
          const stageLabel = detailStage === 'run' ? 'Protected' : detailStage === 'config' ? 'Pending setup' : 'Discovered';
          const stageToneClass = detailStage === 'run'
            ? 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-100'
            : detailStage === 'config'
              ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-100'
              : 'bg-slate-100 text-slate-600 ring-1 ring-slate-200';
          const resourceCount = resourceSummaryTotal(selectedDetailApp);
          const storageUsage = planStorageSizeForApp(selectedDetailApp);
          const nextSyncLabel = nextSyncLabelForApp(selectedDetailApp);
          const primaryFacts = detailStage === 'run'
            ? [
              ['DR Config', detailPlanMeta.label],
              ['Repository', detailStorage],
              ['Policy', detailPolicy],
              ['Next Sync', nextSyncLabel || 'Manual only'],
            ]
            : detailStage === 'config'
              ? [
                ['Setup Status', 'Waiting for DR configuration'],
                ['DR Support', supportMeta.label],
                ['PVCs', String(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0)],
                ['Storage Request', formatBytes(capacityBytes)],
              ]
              : [
                ['DR Support', supportMeta.label],
                ['Runtime Status', selectedDetailApp.status || 'Unknown'],
                ['PVCs', String(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0)],
                ['Storage Request', formatBytes(capacityBytes)],
              ];
          const detailMembers = selectedDetailApp.memberApps?.length ? selectedDetailApp.memberApps : [selectedDetailApp];
          const detailAppIds = new Set(detailMembers.map(member => member.apiId).filter(Boolean));
          const detailNamespaceSet = new Set(namespaces);
          const detailRestorePoints = liveRestorePoints
            .filter(point => {
              if (detailPlanId && point.protectionPlanId === detailPlanId) return true;
              if (point.appId && detailAppIds.has(point.appId)) return true;
              return point.sourceClusterId === (selectedDetailApp.clusterId || currentClusterId)
                && [point.sourceNamespace, ...(point.includedNamespaces || [])].some(namespace => detailNamespaceSet.has(namespace));
            })
            .sort((a, b) => (b.taskCreatedAt || b.createdAt || b.time || '').localeCompare(a.taskCreatedAt || a.createdAt || a.time || ''));
          const detailTasks = platformTasks
            .filter(task => {
              if (detailPlanId && task.protectionPlanId === detailPlanId) return true;
              if (task.appId && detailAppIds.has(task.appId)) return true;
              const taskNamespaces = namespacesFromPayload(task.payload || {});
              return task.clusterId === (selectedDetailApp.clusterId || currentClusterId)
                && taskNamespaces.some(namespace => detailNamespaceSet.has(namespace));
            })
            .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
          const detailRepository = storage.find(repo => repo.id === detailPlan?.storageRepoId)
            || storage.find(repo => repo.name === selectedDetailApp.storage);
          const selectedNamespaceTask = detailTasks.find(task => task.id === namespaceDetailTaskId) || null;
          const generatedRestorePoints = selectedNamespaceTask && (selectedNamespaceTask.type === 'backup' || selectedNamespaceTask.type.includes('sync'))
            ? liveRestorePoints.filter(point => point.backupTaskId === selectedNamespaceTask.id || point.id === selectedNamespaceTask.restorePointId)
            : [];
          const cleanupRestorePoints = selectedNamespaceTask && ['retention-cleanup', 'protection-cleanup'].includes(selectedNamespaceTask.type)
            ? (Array.isArray(selectedNamespaceTask.payload?.restorePoints) ? selectedNamespaceTask.payload.restorePoints : []).map((raw: any) => {
              const id = String(raw?.id || raw?.restorePointId || '');
              const point = liveRestorePoints.find(item => item.id === id);
              const rawTime = String(raw?.taskCreatedAt || '');
              const label = point
                ? restorePointDisplayLabel(point)
                : rawTime ? `RP-${formatLocalDateTime(rawTime)}` : `Restore point ${id.slice(0, 8) || '-'}`;
              return { id, point, label, time: point?.taskCreatedAt || point?.createdAt || rawTime };
            })
            : [];
          const namespaceDetailPageSize = 10;
          const restorePointPageCount = Math.max(1, Math.ceil(detailRestorePoints.length / namespaceDetailPageSize));
          const taskPageCount = Math.max(1, Math.ceil(detailTasks.length / namespaceDetailPageSize));
          const activeRestorePointPage = Math.min(namespaceRestorePointPage, restorePointPageCount);
          const activeTaskPage = Math.min(namespaceTaskPage, taskPageCount);
          const pagedDetailRestorePoints = detailRestorePoints.slice((activeRestorePointPage - 1) * namespaceDetailPageSize, activeRestorePointPage * namespaceDetailPageSize);
          const pagedDetailTasks = detailTasks.slice((activeTaskPage - 1) * namespaceDetailPageSize, activeTaskPage * namespaceDetailPageSize);
          const detailTabs = detailStage === 'run'
            ? [
              { id: 'overview' as const, label: 'Overview' },
              { id: 'restorePoints' as const, label: 'Restore Points', count: detailRestorePoints.length },
              { id: 'tasks' as const, label: 'Tasks', count: detailTasks.length },
              { id: 'storage' as const, label: 'Storage' },
            ]
            : [{ id: 'overview' as const, label: 'Overview' }];
          return (
            <div className="fixed inset-0 z-50 flex justify-end">
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="hbdr-filter-drawer-backdrop"
                onClick={() => setSelectedDetailApp(null)}
              />
              <motion.div
                initial={{ opacity: 0, x: 32 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 32 }}
                transition={{ duration: 0.18, ease: 'easeOut' }}
                className="hbdr-filter-drawer hbdr-app-detail-drawer"
              >
                <div className="hbdr-filter-drawer-head hbdr-app-detail-drawer-head">
                  <div className="flex items-start justify-between gap-4">
                    <div className="hbdr-app-detail-title">
                      <div className="hbdr-app-detail-icon"><Layers size={18} /></div>
                      <div className="min-w-0">
                        <div className="hbdr-app-detail-title-line">
                          <h3>{detailStage === 'run' ? 'DR Plan' : namespaceTitle}</h3>
                          <span className={`hbdr-app-detail-status ${stageToneClass}`}>{stageLabel}</span>
                        </div>
                        {detailStage === 'run' && (
                          <p className="hbdr-app-detail-plan-id" title={detailPlanId || 'No DR plan ID'}>
                            <span>Plan ID</span><strong>{detailPlanId || '-'}</strong>
                          </p>
                        )}
                        {detailStage !== 'run' && (
                          <p className="mt-1 text-xs font-medium text-slate-500">
                            {resourceCount} resources · {formatBytes(capacityBytes)} requested storage
                          </p>
                        )}
                      </div>
                    </div>
                    <button onClick={() => setSelectedDetailApp(null)}><X size={18} /></button>
                  </div>
                </div>
                <div className="hbdr-filter-drawer-body hbdr-app-detail-drawer-body">
                  <nav className="hbdr-namespace-detail-tabs" aria-label="Namespace detail sections">
                    {detailTabs.map(tab => (
                      <button
                        key={tab.id}
                        type="button"
                        className={namespaceDetailTab === tab.id ? 'is-active' : ''}
                        onClick={() => {
                          setNamespaceDetailTab(tab.id);
                          setNamespaceDetailTaskId('');
                        }}
                      >
                        {tab.label}
                        {'count' in tab && <span>{tab.count}</span>}
                      </button>
                    ))}
                  </nav>
                  {namespaceDetailTab === 'overview' && <div className="hbdr-namespace-detail-panel">
                  <section className="hbdr-app-detail-section hbdr-app-detail-namespace-section">
                    <div className="hbdr-app-detail-section-title">
                      <Layers size={15} className="text-indigo-500" />
                      <h4>{namespaces.length > 1 ? 'Namespaces' : 'Namespace'}</h4>
                    </div>
                    <div className="hbdr-app-detail-chip-list">
                      {namespaces.map(namespace => (
                        <span key={namespace}>{namespace}</span>
                      ))}
                    </div>
                  </section>

                  <div className="grid grid-cols-2 gap-3">
                    {primaryFacts.map(([label, value]) => (
                      <div key={label} className="hbdr-app-detail-fact">
                        <p>{label}</p>
                        <strong title={value}>{value}</strong>
                      </div>
                    ))}
                  </div>

                  <div className="mt-4 grid grid-cols-1 gap-4">
                    {detailStage !== 'run' && (
                      <section className="hbdr-app-detail-section">
                        <div className="hbdr-app-detail-section-title">
                          {supportMeta.tone === 'unsupported' ? <AlertTriangle size={15} className="text-rose-600" /> : <ShieldCheck size={15} className="text-slate-400" />}
                          <h4>DR Readiness</h4>
                        </div>
                        {supportMeta.tone === 'unsupported' ? (
                          <TaskErrorDetailBlock failure={supportFailure} details={supportDetails} />
                        ) : (
                          <div className="rounded-xl border border-emerald-100 bg-emerald-50 px-3 py-3">
                            <p className="text-sm font-black text-emerald-800">{supportMeta.label}</p>
                            <p className="mt-1 text-xs font-semibold leading-relaxed text-emerald-700">{supportMeta.title}</p>
                          </div>
                        )}
                      </section>
                    )}

                    {detailStage === 'run' && (
                      <section className="hbdr-app-detail-section">
                        <div className="hbdr-app-detail-section-title">
                          <Database size={15} className="text-slate-500" />
                          <h4>DR Configuration</h4>
                        </div>
                        <div className="hbdr-app-detail-config-grid">
                          <div className="hbdr-app-detail-config-item">
                            <p>Repository</p>
                            <strong title={detailStorage}>{detailStorage}</strong>
                            {storageUsage.label && <em>{storageUsage.label}</em>}
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>DR Pair</p>
                            <strong title={`${currentCluster?.name || 'Source'} → ${selectedDetailApp.targetCluster || 'Not configured'}`}>{currentCluster?.name || 'Source'} → {selectedDetailApp.targetCluster || 'Not configured'}</strong>
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>Policy</p>
                            <strong title={detailPolicy}>{detailPolicy}</strong>
                            {detailPolicyObject && <em>{formatPolicySchedule(detailPolicyObject)}</em>}
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>Create Time</p>
                            <strong>{selectedDetailApp.protectionPlanCreatedAt ? formatDateTime(selectedDetailApp.protectionPlanCreatedAt) : '-'}</strong>
                          </div>
                        </div>
                      </section>
                    )}

                  </div>

                  <div className="hbdr-app-detail-resource">
                    <div className="hbdr-app-detail-section-title">
                      <Grid3X3 size={15} className="text-slate-500" />
                      <h4>Resource Overview</h4>
                    </div>
                    <div className="hbdr-app-detail-chip-list hbdr-app-detail-resource-chips">
                      {categories.length > 0 ? categories.map(category => {
                        const Icon = resourceCategoryIconMap[category.key as ResourceCategoryKey] || MoreVertical;
                        return (
                          <span key={category.key}>
                            <Icon size={13} />
                            <strong>{category.total}</strong>
                            {category.label}
                          </span>
                        );
                      }) : (
                        <span>No resources reported</span>
                      )}
                    </div>
                    <div className="hbdr-app-detail-pvc">
                      <div className="hbdr-app-detail-section-title">
                        <HardDrive size={14} className="text-slate-500" />
                        <h5>PVC / Storage</h5>
                      </div>
                      {pvcRows.length > 0 ? (
                        <div className="hbdr-app-detail-pvc-list">
                          {pvcRows.slice(0, 6).map(pvc => {
                            const status = pvc.fields?.STATUS || pvc.fields?.Status || '-';
                            const storageClass = pvc.fields?.STORAGECLASS || pvc.fields?.StorageClass || '-';
                            const capacity = pvc.fields?.CAPACITY || pvc.fields?.Capacity || '-';
                            const usedBy = pvc.fields?.['USED BY'] || pvc.fields?.UsedBy || pvc.fields?.['Used By'] || '';
                            return (
                              <div key={`${pvc.namespace || selectedDetailApp.namespace}-${pvc.name}`} className="hbdr-app-detail-pvc-row">
                                <div className="min-w-0">
                                  <strong>{pvc.name}</strong>
                                  <p>{storageClass} · {capacity}{usedBy ? ` · Used by ${usedBy}` : ''}</p>
                                </div>
                                <span className={status === 'Bound' ? 'is-bound' : 'is-warning'}>{status}</span>
                              </div>
                            );
                          })}
                          {pvcRows.length > 6 && <p className="hbdr-app-detail-more">+{pvcRows.length - 6} more PVCs</p>}
                        </div>
                      ) : (
                        <p className="hbdr-app-detail-empty-line">{(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0) > 0 ? 'PVC count is available, detailed PVC inventory is not reported yet.' : 'No PVC reported.'}</p>
                      )}
                    </div>
                  </div>
                  </div>}

                  {namespaceDetailTab === 'restorePoints' && (
                    <div className="hbdr-namespace-detail-panel">
                      <div className="hbdr-namespace-detail-section-head">
                        <div><strong>Restore Points</strong><span>Recovery points stored for this namespace or DR plan.</span></div>
                        <em>{detailRestorePoints.length}</em>
                      </div>
                      {detailRestorePoints.length > 0 ? (
                        <div className="hbdr-namespace-detail-list">
                          {pagedDetailRestorePoints.map(point => (
                            <section key={point.id} className="hbdr-namespace-rp-row">
                              <div className="hbdr-namespace-rp-icon" title="Stored recovery point"><DatabaseBackup size={17} /></div>
                              <div className="min-w-0">
                                <strong title={restorePointDisplayLabel(point)}>{restorePointDisplayLabel(point)}</strong>
                                <span>{formatLocalDateTime(point.taskCreatedAt || point.createdAt) || '-'} · {point.pointType === 'local' ? 'Local Snapshot' : 'Remote Snapshot'}</span>
                              </div>
                              <div className="hbdr-namespace-rp-meta">
                                <em className={`is-${(point.status || 'unknown').toLowerCase()}`}>{point.status || 'Unknown'}</em>
                                <span>{point.sizeBytes ? formatBytes(point.sizeBytes) : '-'}</span>
                              </div>
                            </section>
                          ))}
                          <div className="hbdr-namespace-detail-pagination">
                            <span>{(activeRestorePointPage - 1) * namespaceDetailPageSize + 1}-{Math.min(activeRestorePointPage * namespaceDetailPageSize, detailRestorePoints.length)} of {detailRestorePoints.length}</span>
                            <div>
                              <button type="button" disabled={activeRestorePointPage <= 1} onClick={() => setNamespaceRestorePointPage(page => Math.max(1, page - 1))}>Prev</button>
                              <em>{activeRestorePointPage} / {restorePointPageCount}</em>
                              <button type="button" disabled={activeRestorePointPage >= restorePointPageCount} onClick={() => setNamespaceRestorePointPage(page => Math.min(restorePointPageCount, page + 1))}>Next</button>
                            </div>
                          </div>
                        </div>
                      ) : (
                        <div className="hbdr-namespace-detail-empty"><DatabaseBackup size={25} /><strong>No restore points</strong><span>No restore point has been created for this namespace or DR plan.</span></div>
                      )}
                    </div>
                  )}

                  {namespaceDetailTab === 'tasks' && (
                    <div className="hbdr-namespace-detail-panel">
                      {selectedNamespaceTask ? (
                        <div className="hbdr-namespace-task-detail">
                          <button type="button" className="hbdr-namespace-task-back" onClick={() => setNamespaceDetailTaskId('')}>← Back to tasks</button>
                          <div className="hbdr-namespace-task-summary">
                            <div><span>Task</span><strong>{taskDetailFullLabel(selectedNamespaceTask.type)}</strong></div>
                            <div><span>Source</span><strong><TaskOriginLabel task={selectedNamespaceTask} /></strong></div>
                            <div><span>Status</span><strong>{selectedNamespaceTask.status}</strong></div>
                            <div><span>Created</span><strong>{formatLocalDateTime(selectedNamespaceTask.createdAt) || '-'}</strong></div>
                            <div><span>Started</span><strong>{formatLocalDateTime(selectedNamespaceTask.startedAt) || '-'}</strong></div>
                            <div><span>Completed</span><strong>{formatLocalDateTime(selectedNamespaceTask.completedAt) || '-'}</strong></div>
                            <div><span>Progress</span><strong>{selectedNamespaceTask.progress || 0}%</strong></div>
                          </div>
                          {selectedNamespaceTask.type === 'retention-cleanup' && (
                            <div className="hbdr-task-purpose">
                              <DatabaseBackup size={17} />
                              <div><strong>Retention Cleanup</strong><span>Removes expired restore points according to the protection policy retention settings.</span></div>
                            </div>
                          )}
                          {generatedRestorePoints.length > 0 && (
                            <section className="hbdr-task-restore-points">
                              <div className="hbdr-namespace-detail-section-head">
                                <div><strong>Generated Restore Point</strong><span>Recovery point created by this sync task.</span></div>
                                <em>{generatedRestorePoints.length}</em>
                              </div>
                              <div className="hbdr-namespace-detail-list">
                                {generatedRestorePoints.map(point => (
                                  <div key={point.id} className="hbdr-task-restore-point-row">
                                    <span className="hbdr-namespace-rp-icon"><DatabaseBackup size={16} /></span>
                                    <span><strong>{restorePointDisplayLabel(point)}</strong><small>{formatLocalDateTime(point.taskCreatedAt || point.createdAt) || '-'}</small></span>
                                    <em>{point.status || 'Unknown'}</em>
                                  </div>
                                ))}
                              </div>
                            </section>
                          )}
                          {cleanupRestorePoints.length > 0 && (
                            <section className="hbdr-task-restore-points">
                              <div className="hbdr-namespace-detail-section-head">
                                <div>
                                  <strong>{isSucceededStatus(selectedNamespaceTask.status) ? 'Cleaned Restore Points' : 'Restore Points to Clean'}</strong>
                                  <span>Restore points included in this retention cleanup task.</span>
                                </div>
                                <em>{cleanupRestorePoints.length}</em>
                              </div>
                              <div className="hbdr-namespace-detail-list">
                                {cleanupRestorePoints.map((item, index) => (
                                  <div key={item.id || index} className="hbdr-task-restore-point-row">
                                    <span className="hbdr-namespace-rp-icon"><DatabaseBackup size={16} /></span>
                                    <span><strong>{item.label}</strong><small>{formatLocalDateTime(item.time) || '-'}</small></span>
                                    <em>{isSucceededStatus(selectedNamespaceTask.status) ? 'Cleaned' : isFailedStatus(selectedNamespaceTask.status) ? 'Failed' : 'Pending'}</em>
                                  </div>
                                ))}
                              </div>
                            </section>
                          )}
                          <TaskProcessTimeline task={selectedNamespaceTask} events={drTaskEvents[selectedNamespaceTask.id] || []} />
                          {(isFailedStatus(selectedNamespaceTask.status) || taskHasWarning(selectedNamespaceTask)) && (
                            <TaskErrorDetailBlock
                              failure={taskFailureSummary(selectedNamespaceTask, drTaskEvents[selectedNamespaceTask.id] || [])}
                              details={taskFailureDetails(selectedNamespaceTask, drTaskEvents[selectedNamespaceTask.id] || [])}
                            />
                          )}
                          <TaskFinalResult task={selectedNamespaceTask} events={drTaskEvents[selectedNamespaceTask.id] || []} />
                        </div>
                      ) : (
                        <>
                          <div className="hbdr-namespace-detail-section-head">
                            <div><strong>Tasks</strong><span>Sync and recovery activity for this namespace or DR plan.</span></div>
                            <em>{detailTasks.length}</em>
                          </div>
                          {detailTasks.length > 0 ? (
                            <div className="hbdr-namespace-detail-list">
                              {pagedDetailTasks.map(task => (
                                <button key={task.id} type="button" className="hbdr-namespace-task-row" onClick={() => setNamespaceDetailTaskId(task.id)}>
                                  <span className="hbdr-namespace-task-icon" title="Task execution"><ListChecks size={17} /></span>
                                  <span className="min-w-0">
                                    <span className="hbdr-namespace-task-title">
                                      <strong>{taskDetailLabel(task.type)}</strong>
                                      <TaskOriginLabel task={task} />
                                    </span>
                                    <small>{formatLocalDateTime(task.createdAt) || '-'}{task.completedAt ? ` · Completed ${formatLocalDateTime(task.completedAt)}` : ''}</small>
                                  </span>
                                  <em className={`is-${(task.status || 'unknown').toLowerCase()}`}>{task.status || 'Unknown'}</em>
                                  <ChevronRight size={15} />
                                </button>
                              ))}
                              <div className="hbdr-namespace-detail-pagination">
                                <span>{(activeTaskPage - 1) * namespaceDetailPageSize + 1}-{Math.min(activeTaskPage * namespaceDetailPageSize, detailTasks.length)} of {detailTasks.length}</span>
                                <div>
                                  <button type="button" disabled={activeTaskPage <= 1} onClick={() => setNamespaceTaskPage(page => Math.max(1, page - 1))}>Prev</button>
                                  <em>{activeTaskPage} / {taskPageCount}</em>
                                  <button type="button" disabled={activeTaskPage >= taskPageCount} onClick={() => setNamespaceTaskPage(page => Math.min(taskPageCount, page + 1))}>Next</button>
                                </div>
                              </div>
                            </div>
                          ) : (
                            <div className="hbdr-namespace-detail-empty"><History size={24} /><strong>No tasks</strong><span>No task has been recorded for this namespace or DR plan.</span></div>
                          )}
                        </>
                      )}
                    </div>
                  )}

                  {namespaceDetailTab === 'storage' && (
                    <div className="hbdr-namespace-detail-panel">
                      <div className="hbdr-namespace-detail-section-head">
                        <div><strong>Storage</strong><span>Repository binding and non-sensitive connection details.</span></div>
                        {detailRepository && <em className={`is-${detailRepository.status}`}>{detailRepository.status}</em>}
                      </div>
                      {detailRepository ? (
                        <div className="hbdr-namespace-storage-grid">
                          <div><span>Repository</span><strong>{detailRepository.name}</strong></div>
                          <div><span>Type</span><strong>{detailRepository.type || '-'}</strong></div>
                          <div className="is-wide"><span>Endpoint</span><strong title={detailRepository.endpoint || '-'}>{detailRepository.endpoint || '-'}</strong></div>
                          <div><span>Bucket</span><strong>{detailRepository.bucket || '-'}</strong></div>
                          <div><span>Region</span><strong>{detailRepository.region || '-'}</strong></div>
                          <div><span>TLS</span><strong>{detailRepository.useTls ? 'Enabled' : 'Disabled'}</strong></div>
                          <div><span>URL Style</span><strong>{detailRepository.urlStyle || 'path'}</strong></div>
                          <div><span>Status</span><strong>{detailRepository.status}</strong></div>
                          <div><span>Storage Used</span><strong>{storageUsage.label || '-'}</strong></div>
                          <div><span>Last Verified</span><strong>{detailRepository.lastValidatedAt ? formatLocalDateTime(detailRepository.lastValidatedAt) : 'Never'}</strong></div>
                        </div>
                      ) : (
                        <div className="hbdr-namespace-detail-empty"><Database size={24} /><strong>No repository bound</strong><span>This namespace does not currently have a storage repository configuration.</span></div>
                      )}
                    </div>
                  )}
                </div>
                <div className="hbdr-filter-drawer-actions hbdr-app-detail-drawer-actions">
                  <button onClick={() => setSelectedDetailApp(null)}>Close</button>
                </div>
              </motion.div>
            </div>
          );
        })()}
      </AnimatePresence>
    </>
  );
}
