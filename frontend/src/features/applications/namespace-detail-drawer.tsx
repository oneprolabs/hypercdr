import type { useNamespaceDetail } from './use-namespace-detail';
import type { AppItem, Cluster, ResourceCategoryKey } from '../clusters/types';
import type { ApiProtectionPlan, ApiRestorePointView, ApiTask, ApiTaskEvent, PolicyItem, StorageRepo } from '../recovery/types';
import type { ApplicationStage } from '../recovery/task-ui';
import { motion } from 'motion/react';
import { AlertCircle, AlertTriangle, ChevronRight, Database, DatabaseBackup, Grid3X3, HardDrive, History, Layers, ListChecks, MoreVertical, RefreshCw, ShieldCheck, X } from 'lucide-react';
import { formatDateTime, formatLocalDateTime } from '../../lib/date-time';
import { isFailedStatus, isSucceededStatus, taskHasWarning } from '../recovery/task-status';
import { TaskErrorDetailBlock, TaskFinalResult, TaskOriginLabel, TaskProcessTimeline, formatBytes, recordFromUnknown, resourceCategoryIconMap, taskDetailFullLabel, taskDetailLabel, taskFailureDetails, taskFailureSummary } from '../recovery/task-ui';
import { formatPolicySchedule, normalizeResourceCategories, restorePointDisplayLabel } from './application-support';

interface NamespaceDetailDrawerProps {
  detail: ReturnType<typeof useNamespaceDetail>;
  catalog: {
    policies: PolicyItem[];
    liveRestorePoints: ApiRestorePointView[];
    platformTasks: ApiTask[];
    storage: StorageRepo[];
    currentCluster: Cluster | null;
    drTaskEvents: Record<string, ApiTaskEvent[]>;
  };
  presentation: {
    drSupportMetaForApp: (app: AppItem) => {label: string; tone: 'unsupported' | 'unknown' | 'warning' | 'supported'; sort: number; title: string};
    drSupportFailureForApp: (app: AppItem) => {code: string; title: string; description: string; fullText: string};
    drSupportFailureDetailsForApp: (app: AppItem) => string[];
    stageOf: (app: AppItem) => ApplicationStage;
    protectionPlanForApp: (app: AppItem) => ApiProtectionPlan | undefined;
    drStatusMetaForApp: (app: AppItem) => {label: string; tone: 'ok' | 'progress' | 'warn' | 'muted'; title: string};
    unitNamespaces: (app: AppItem) => string[];
    resourceSummaryTotal: (app: AppItem) => number;
    planStorageSizeForApp: (app: AppItem) => {label: string; title: string};
    nextSyncLabelForApp: (app: AppItem) => string;
  };
  onRestorePointAction: (mode: 'drill' | 'takeover', app: AppItem, pointId: string) => void;
}

// Rendering and tab presentation are isolated from the page's selection,
// protection, recovery and inventory workflows. Data remains owned by the
// shared detail hook and the persisted platform catalog.
export default function NamespaceDetailDrawer({detail, catalog, presentation, onRestorePointAction: openRestorePointAction}: NamespaceDetailDrawerProps) {
  const {selectedDetailApp, setSelectedDetailApp, namespaceDetailTab, setNamespaceDetailTab,
    namespaceDetailTaskId, setNamespaceDetailTaskId, namespaceRestorePointPage, setNamespaceRestorePointPage,
    restorePointMenuId, setRestorePointMenuId, namespaceTaskPage, setNamespaceTaskPage,
    namespaceDetailRestorePoints, namespaceDetailTasks, namespaceDetailLoading, namespaceDetailLoadError,
    openNamespaceDetail} = detail;
  const {policies, liveRestorePoints, platformTasks, storage, currentCluster, drTaskEvents} = catalog;
  const {drSupportMetaForApp, drSupportFailureForApp, drSupportFailureDetailsForApp, stageOf,
    protectionPlanForApp, drStatusMetaForApp, unitNamespaces, resourceSummaryTotal,
    planStorageSizeForApp, nextSyncLabelForApp} = presentation;
  if (!selectedDetailApp) return null;

  const detailStorage = selectedDetailApp.storage || 'No backup repository';
  const detailPolicy = selectedDetailApp.policy || 'No policy bound';
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
  const detailRestorePoints = (namespaceDetailRestorePoints || [])
    .filter(point => Boolean(detailPlanId && point.protectionPlanId === detailPlanId))
    .sort((a, b) => (b.taskCreatedAt || b.createdAt || b.time || '').localeCompare(a.taskCreatedAt || a.createdAt || a.time || ''));
  const detailTasks = (namespaceDetailTasks || [])
    .filter(task => Boolean(detailPlanId && task.protectionPlanId === detailPlanId))
    .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
  const restorePointSummaryCount = liveRestorePoints.filter(point => Boolean(detailPlanId && point.protectionPlanId === detailPlanId)).length;
  const taskSummaryCount = platformTasks.filter(task => Boolean(detailPlanId && task.protectionPlanId === detailPlanId)).length;
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
      { id: 'restorePoints' as const, label: 'Restore Points', count: namespaceDetailRestorePoints === null ? restorePointSummaryCount : detailRestorePoints.length },
      { id: 'tasks' as const, label: 'Tasks', count: namespaceDetailTasks === null ? taskSummaryCount : detailTasks.length },
      { id: 'storage' as const, label: 'Storage' },
    ]
    : [{ id: 'overview' as const, label: 'Overview' }];
  return (
    <div className="fixed inset-0 z-[230] flex justify-end">
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
              {namespaceDetailLoading ? (
                <div className="hbdr-namespace-detail-empty" role="status"><RefreshCw className="animate-spin" size={22} /><strong>Loading restore points...</strong><span>Recovery-point details are loaded only when this tab is opened.</span></div>
              ) : namespaceDetailLoadError ? (
                <div className="hbdr-namespace-detail-empty" role="alert"><AlertCircle size={22} /><strong>Unable to load restore points</strong><span>{namespaceDetailLoadError}</span></div>
              ) : detailRestorePoints.length > 0 ? (
                <div className="hbdr-namespace-detail-list">
                  {pagedDetailRestorePoints.map(point => {
							const metrics = recordFromUnknown(point.sizeMetricsV2 || point.metadata?.sizeMetricsV2);
							const logical = recordFromUnknown(metrics.logical);
							const newData = recordFromUnknown(metrics.newData);
							const reuse = recordFromUnknown(metrics.reuse);
							const logicalKnown = Boolean(logical.known) || Number(logical.totalBytes || 0) > 0;
							const metadataOnly = Number(logical.metadataBytes || 0) > 0
								&& Number(newData.metadataBytes || 0) > 0
								&& Number(logical.volumeBytes || 0) === 0
								&& Number(logical.metadataBytes || 0) === Number(logical.totalBytes || 0)
								&& Number(newData.metadataBytes || 0) === Number(newData.totalBytes || 0);
							const newDataKnown = Boolean(newData.known) || metadataOnly;
							const reuseStatus = String(reuse.status || 'unavailable');
							const logicalBytes = Number(logical.totalBytes || 0);
							const newDataBytes = Number(newData.totalBytes || 0);
							// Derive the card value from the two displayed overall totals. This
							// keeps historical V2 points consistent even when their persisted
							// reuse ratio was produced by the former volume-only definition.
							const overallReuseRatio = logicalKnown && newDataKnown && logicalBytes > 0
								? Math.max(0, Math.min(1, 1 - newDataBytes / logicalBytes))
								: null;
							const reuseRatio = overallReuseRatio ?? Number(reuse.ratio || 0);
							const reuseLabel = `${Number((reuseRatio * 100).toFixed(2))}%`;
							return (
                    <section key={point.id} className="hbdr-namespace-rp-row">
                      <div className="hbdr-namespace-rp-icon" title="Stored recovery point"><DatabaseBackup size={17} /></div>
                      <div className="min-w-0">
                        <strong title={restorePointDisplayLabel(point)}>{restorePointDisplayLabel(point)}</strong>
                        <span>{formatLocalDateTime(point.taskCreatedAt || point.createdAt) || '-'} · {point.pointType === 'local' ? 'Local Snapshot' : 'Remote Snapshot'}</span>
                      </div>
                      <div className="hbdr-namespace-rp-metrics">
								<div title="Complete data represented by this recovery point"><small>Logical size</small><strong>{logicalKnown ? formatBytes(Number(logical.totalBytes || point.sizeBytes || 0)) : (point.sizeBytes ? formatBytes(point.sizeBytes) : '—')}</strong></div>
								<div title="New or changed data recorded by this backup"><small>New data</small><strong>{newDataKnown ? formatBytes(Number(newData.totalBytes || 0)) : '—'}</strong></div>
								<div title="Percentage of the complete recovery-point data reused from existing repository content"><small>Reuse</small><strong>{overallReuseRatio !== null || reuseStatus === 'available' ? reuseLabel : reuseStatus === 'baseline' ? 'Baseline' : reuseStatus === 'not_applicable' ? 'N/A' : '—'}</strong></div>
							  </div>
							  <div className="hbdr-namespace-rp-meta">
								<button type="button" className="hbdr-namespace-rp-menu-button" aria-label="Recovery point actions" onClick={event => { event.stopPropagation(); setRestorePointMenuId(current => current === point.id ? '' : point.id); }}><MoreVertical size={15} /></button>
								{restorePointMenuId === point.id && <div className="hbdr-namespace-rp-menu">
								  <button type="button" onClick={() => { setRestorePointMenuId(''); openNamespaceDetail(selectedDetailApp, 'restorePoints'); }}>View details</button>
								  <button type="button" onClick={() => { setRestorePointMenuId(''); openRestorePointAction('drill', selectedDetailApp, point.id); }}>Drill</button>
								  <button type="button" onClick={() => { setRestorePointMenuId(''); openRestorePointAction('takeover', selectedDetailApp, point.id); }}>Takeover</button>
								</div>}
                      </div>
                    </section>
						  );})}
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
              {namespaceDetailLoading ? (
                <div className="hbdr-namespace-detail-empty" role="status"><RefreshCw className="animate-spin" size={22} /><strong>Loading tasks...</strong><span>Task details are loaded only when this tab is opened.</span></div>
              ) : namespaceDetailLoadError ? (
                <div className="hbdr-namespace-detail-empty" role="alert"><AlertCircle size={22} /><strong>Unable to load tasks</strong><span>{namespaceDetailLoadError}</span></div>
              ) : selectedNamespaceTask ? (
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

}
