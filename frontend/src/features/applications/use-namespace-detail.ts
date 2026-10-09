import { useEffect, useState } from 'react';
import { listNamespaceRestorePoints, listNamespaceTasks } from '../../api/namespace-detail';
import type { AppItem } from '../clusters/types';
import { listItems, type ApiProtectionPlan, type ApiRestorePointView, type ApiTask } from '../recovery/types';

// Owns the detail drawer's lazy tab data and cancels stale responses on close
// or selection changes. Parent pages retain their existing operation handlers.
export function useNamespaceDetail(protectionPlans: ApiProtectionPlan[]) {
  const [selectedDetailApp, setSelectedDetailApp] = useState<AppItem | null>(null);
  const [namespaceDetailTab, setNamespaceDetailTab] = useState<'overview' | 'restorePoints' | 'tasks' | 'storage'>('overview');
  const [namespaceDetailTaskId, setNamespaceDetailTaskId] = useState('');
  const [namespaceRestorePointPage, setNamespaceRestorePointPage] = useState(1);
	const [restorePointMenuId, setRestorePointMenuId] = useState('');
  const [namespaceTaskPage, setNamespaceTaskPage] = useState(1);
  const [namespaceDetailRestorePoints, setNamespaceDetailRestorePoints] = useState<ApiRestorePointView[] | null>(null);
  const [namespaceDetailTasks, setNamespaceDetailTasks] = useState<ApiTask[] | null>(null);
  const [namespaceDetailLoading, setNamespaceDetailLoading] = useState(false);
  const [namespaceDetailLoadError, setNamespaceDetailLoadError] = useState('');
  const openNamespaceDetail = (app: AppItem, tab: 'overview' | 'restorePoints' | 'tasks' | 'storage' = 'overview') => {
    setSelectedDetailApp(app);
    setNamespaceDetailTab(tab);
    setNamespaceDetailTaskId('');
    setNamespaceRestorePointPage(1);
    setNamespaceTaskPage(1);
    setNamespaceDetailRestorePoints(null);
    setNamespaceDetailTasks(null);
    setNamespaceDetailLoadError('');
  };
  const selectedDetailPlanId = selectedDetailApp?.protectionPlanId
    || protectionPlans.find(plan => plan.appId === selectedDetailApp?.apiId || plan.appIds?.includes(selectedDetailApp?.apiId || ''))?.id
    || '';
  useEffect(() => {
    if (!selectedDetailApp || !selectedDetailPlanId || namespaceDetailTab === 'overview' || namespaceDetailTab === 'storage') return;
    if (namespaceDetailTab === 'restorePoints' && namespaceDetailRestorePoints !== null) return;
    if (namespaceDetailTab === 'tasks' && namespaceDetailTasks !== null) return;
    let cancelled = false;
    const loadTab = async () => {
      setNamespaceDetailLoading(true);
      setNamespaceDetailLoadError('');
      try {
        if (namespaceDetailTab === 'restorePoints') {
          const response = await listNamespaceRestorePoints(selectedDetailPlanId);
          if (cancelled) return;
          setNamespaceDetailRestorePoints(listItems(response).map(point => ({
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
        } else {
          const response = await listNamespaceTasks();
          if (!cancelled) setNamespaceDetailTasks(listItems(response).filter(task => task.protectionPlanId === selectedDetailPlanId));
        }
      } catch (error) {
        if (!cancelled) setNamespaceDetailLoadError(error instanceof Error ? error.message : 'Unable to load this detail section.');
      } finally {
        if (!cancelled) setNamespaceDetailLoading(false);
      }
    };
    void loadTab();
    return () => { cancelled = true; };
  }, [namespaceDetailRestorePoints, namespaceDetailTab, namespaceDetailTasks, selectedDetailApp, selectedDetailPlanId]);

 return { selectedDetailApp, setSelectedDetailApp, namespaceDetailTab, setNamespaceDetailTab, namespaceDetailTaskId, setNamespaceDetailTaskId, namespaceRestorePointPage, setNamespaceRestorePointPage, restorePointMenuId, setRestorePointMenuId, namespaceTaskPage, setNamespaceTaskPage, namespaceDetailRestorePoints, setNamespaceDetailRestorePoints, namespaceDetailTasks, setNamespaceDetailTasks, namespaceDetailLoading, setNamespaceDetailLoading, namespaceDetailLoadError, setNamespaceDetailLoadError, openNamespaceDetail, selectedDetailPlanId };
}
