import { useEffect, useState } from 'react';
import { listNamespaceRestorePoints, listNamespaceTasks } from '../../api/namespace-detail';
import type { AppItem } from '../clusters/types';
import { listItems, type ApiProtectionPlan, type ApiRestorePointView, type ApiTask } from '../recovery/types';

// Prefetch independent tab data once per drawer selection. Tab switches must
// neither discard in-flight results nor refetch; close/reopen refreshes the data.
export function useNamespaceDetail(protectionPlans: ApiProtectionPlan[]) {
  const [selectedDetailApp, setSelectedDetailApp] = useState<AppItem | null>(null);
  const [namespaceDetailTab, setNamespaceDetailTab] = useState<'overview' | 'restorePoints' | 'tasks' | 'storage'>('overview');
  const [namespaceDetailTaskId, setNamespaceDetailTaskId] = useState('');
  const [namespaceRestorePointPage, setNamespaceRestorePointPage] = useState(1);
  const [restorePointMenuId, setRestorePointMenuId] = useState('');
  const [namespaceTaskPage, setNamespaceTaskPage] = useState(1);
  const [namespaceDetailRestorePoints, setNamespaceDetailRestorePoints] = useState<ApiRestorePointView[] | null>(null);
  const [namespaceDetailTasks, setNamespaceDetailTasks] = useState<ApiTask[] | null>(null);
  const [tabErrors, setTabErrors] = useState({ restorePoints: '', tasks: '' });
  const [refreshVersion, setRefreshVersion] = useState(0);
  const refreshNamespaceRestorePoints = () => {
    setRestorePointMenuId('');
    setNamespaceDetailRestorePoints(null);
    setTabErrors(previous => ({ ...previous, restorePoints: '' }));
    setRefreshVersion(version => version + 1);
  };
  const namespaceDetailLoadError = namespaceDetailTab === 'restorePoints' ? tabErrors.restorePoints
    : namespaceDetailTab === 'tasks' ? tabErrors.tasks : '';
  const openNamespaceDetail = (app: AppItem, tab: 'overview' | 'restorePoints' | 'tasks' | 'storage' = 'overview') => {
    setSelectedDetailApp(app);
    setRefreshVersion(version => version + 1);
    setRestorePointMenuId('');
    setNamespaceDetailTab(tab);
    setNamespaceDetailTaskId('');
    setNamespaceRestorePointPage(1);
    setNamespaceTaskPage(1);
    setNamespaceDetailRestorePoints(null);
    setNamespaceDetailTasks(null);
    setTabErrors({ restorePoints: '', tasks: '' });
  };
  const selectedDetailPlanId = selectedDetailApp?.protectionPlanId
    || protectionPlans.find(plan => plan.appId === selectedDetailApp?.apiId || plan.appIds?.includes(selectedDetailApp?.apiId || ''))?.id
    || '';
  const namespaceDetailLoading = Boolean(selectedDetailApp && (selectedDetailPlanId || selectedDetailApp.apiId) && !namespaceDetailLoadError && (
    (namespaceDetailTab === 'restorePoints' && namespaceDetailRestorePoints === null)
    || (namespaceDetailTab === 'tasks' && namespaceDetailTasks === null)));
  useEffect(() => {
    if (!selectedDetailApp || (!selectedDetailPlanId && !selectedDetailApp.apiId)) return;
    let cancelled = false;
    setTabErrors({ restorePoints: '', tasks: '' });
    const fail = (tab: 'restorePoints' | 'tasks', error: unknown) => {
      if (!cancelled) setTabErrors(previous => ({ ...previous,
        [tab]: error instanceof Error ? error.message : 'Unable to load this detail section.',
      }));
    };
    // Start both requests immediately; a slow or failed section cannot block another.
    void listNamespaceRestorePoints(selectedDetailPlanId, selectedDetailApp.apiId, selectedDetailApp.clusterId).then(response => {
      if (!cancelled) setNamespaceDetailRestorePoints(listItems(response).map(point => ({
            id: point.id,
            sourceClusterId: point.sourceClusterId,
            protectionPlanId: point.protectionPlanId,
            appId: point.appId,
            storageRepoId: point.storageRepoId,
            backupTaskId: String(point.metadata?.backupTaskId || ''),
            sourceNamespace: point.sourceNamespace || String(point.metadata?.sourceNamespace || ''),
            taskCreatedAt: point.taskCreatedAt,
            createdAt: point.createdAt,
            title: point.veleroBackupName || point.id,
            time: point.completedAt || point.createdAt,
            pointType: point.pointType?.toLowerCase().includes('local') ? 'local' : 'remote',
            status: point.status,
            sizeBytes: point.sizeBytes,
            completedAt: point.completedAt,
            expiresAt: point.expiresAt,
            backupStorageName: point.backupStorageName || String(point.metadata?.backupStorageName || ''),
            veleroBackupName: point.veleroBackupName,
            includedNamespaces: Array.isArray(point.metadata?.includedNamespaces) ? point.metadata.includedNamespaces as string[] : [],
            metadata: point.metadata || {},
            sizeMetricsV2: point.sizeMetricsV2 || point.metadata?.sizeMetricsV2,
          })));
    }).catch(error => fail('restorePoints', error));
    if (selectedDetailPlanId) void listNamespaceTasks(selectedDetailPlanId).then(response => {
      if (!cancelled) setNamespaceDetailTasks(listItems(response));
    }).catch(error => fail('tasks', error));
    else setNamespaceDetailTasks([]);
    return () => { cancelled = true; };
  }, [selectedDetailApp, selectedDetailPlanId, refreshVersion]);


 return { selectedDetailApp, setSelectedDetailApp, namespaceDetailTab, setNamespaceDetailTab, namespaceDetailTaskId, setNamespaceDetailTaskId, namespaceRestorePointPage, setNamespaceRestorePointPage, restorePointMenuId, setRestorePointMenuId, namespaceTaskPage, setNamespaceTaskPage, namespaceDetailRestorePoints, setNamespaceDetailRestorePoints, namespaceDetailTasks, setNamespaceDetailTasks, namespaceDetailLoading, namespaceDetailLoadError, openNamespaceDetail, refreshNamespaceRestorePoints, selectedDetailPlanId };
}
