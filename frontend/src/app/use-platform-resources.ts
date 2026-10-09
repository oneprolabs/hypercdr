import { scopedResourceSetter } from './resource-scope';
import { loadPlatformSnapshot, loadApplicationActivitySnapshot, loadTopologySnapshot } from './platform-snapshots';
import { listApplications, listTags } from '../api/applications';
import { listPolicies } from '../api/policies';
import { listStorageRepositories } from '../api/storage';
import { listClusters } from '../api/clusters';
import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import type { Cluster } from '../features/clusters/types';
import { listItems, type ApiApplication, type ApiList, type ApiPolicy, type ApiProtectionPlan, type ApiRestorePoint, type ApiRestorePointView, type ApiTask, type PolicyItem, type StorageRepo, type TagItem } from '../features/recovery/types';
import type { ApiCluster, ApiStorageRepo } from '../features/recovery/platform-types';
import { mapRestorePoint, buildAppTaskMap, mergeTaskMapKeepingActive, mapApps, mapCluster, mapStorageRepo } from './platform-data';
const SELECTED_CLUSTER_KEY = 'hypercdr.selectedClusterId';
const PLATFORM_DATA_VIEWS = new Set<string>([
  'dashboard',
  'applications',
  'dr_tasks',
  'failback',
  'clusters',
  'storage',
  'policies',
  'restore_points',
  'tags',
]);

export function usePlatformResources({ sessionToken, view, resourceSessionOwnerRef, mapPolicy }: { sessionToken: string; view: string; resourceSessionOwnerRef: RefObject<string>; mapPolicy: (policy: ApiPolicy) => PolicyItem }) {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [liveClusters, setLiveClusters] = useState<Cluster[] | null>(null);
  const [storage, setStorage] = useState<StorageRepo[]>([]);
  const [liveStorage, setLiveStorage] = useState<StorageRepo[] | null>(null);
  const [policies, setPolicies] = useState<PolicyItem[]>([]);
  const [livePolicies, setLivePolicies] = useState<PolicyItem[] | null>(null);
  const [tags, setTags] = useState<TagItem[]>([]);
  const [restorePointCount, setRestorePointCount] = useState(0);
  const [liveRestorePoints, setLiveRestorePoints] = useState<ApiRestorePointView[]>([]);
  const [liveApiClusters, setLiveApiClusters] = useState<ApiCluster[]>([]);
  const [liveApiStorageRepos, setLiveApiStorageRepos] = useState<ApiStorageRepo[]>([]);
  const [liveApiTasks, setLiveApiTasks] = useState<ApiTask[]>([]);
  const [liveApiRestorePointViews, setLiveApiRestorePointViews] = useState<ApiRestorePointView[]>([]);
  const [liveApiRestorePoints, setLiveApiRestorePoints] = useState<ApiRestorePoint[]>([]);

  const [liveApiPolicies, setLiveApiPolicies] = useState<ApiPolicy[]>([]);
  const [liveApiPlans, setLiveApiPlans] = useState<ApiProtectionPlan[]>([]);
  const [liveApiApps, setLiveApiApps] = useState<ApiApplication[]>([]);
  const liveApiAppsRef = useRef<ApiApplication[]>([]);
  const [liveAppTasks, setLiveAppTasks] = useState<Record<string, ApiTask>>({});
  const [liveRecoveryTasks, setLiveRecoveryTasks] = useState<Record<string, ApiTask>>({});
  const [selectedCluster, setSelectedCluster] = useState<Cluster | null>(null);
  const [defaultClusterId, setDefaultClusterId] = useState<string | null>(null);
  const refreshInFlightRef = useRef<Promise<Cluster[]> | null>(null);
  const refreshInFlightViewRef = useRef<string | null>(null);
  const refreshLastStartedAtRef = useRef(0);
  const refreshLastResultRef = useRef<Cluster[]>([]);

  const clearResources = useCallback(() => {
    refreshInFlightRef.current = null;
    refreshInFlightViewRef.current = null;
    refreshLastStartedAtRef.current = 0;
    refreshLastResultRef.current = [];
    setClusters([]);
    setLiveClusters(null);
    setStorage([]);
    setLiveStorage(null);
    setPolicies([]);
    setLivePolicies(null);
    setTags([]);
    setRestorePointCount(0);
    setLiveRestorePoints([]);
    setLiveApiClusters([]);
    setLiveApiStorageRepos([]);
    setLiveApiTasks([]);
    setLiveApiRestorePointViews([]);
    setLiveApiRestorePoints([]);
    setLiveApiPolicies([]);
    setLiveApiPlans([]);
    liveApiAppsRef.current = [];
    setLiveApiApps([]);
    setLiveAppTasks({});
    setLiveRecoveryTasks({});
    setSelectedCluster(null);
    setDefaultClusterId(null);
  }, []);
  const refreshPlatformData = useCallback((targetView: string = view) => {
    const owner = sessionToken;
    if (!owner || resourceSessionOwnerRef.current !== owner) return Promise.resolve([]);
    const now = Date.now();
    if (refreshInFlightRef.current && refreshInFlightViewRef.current === targetView) return refreshInFlightRef.current;
    if (refreshInFlightViewRef.current === targetView && refreshLastResultRef.current.length > 0 && now - refreshLastStartedAtRef.current < 1200) {
      return Promise.resolve(refreshLastResultRef.current);
    }
    refreshLastStartedAtRef.current = now;
    const request = (async () => {
      if (targetView === 'clusters' || targetView === 'dr_tasks' || targetView === 'restore_points' || targetView === 'failback') {
        const clusterRes = await listClusters();
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, []));
        setLiveApiClusters(apiClusters);
        // A cluster-only poll must not discard applications loaded on demand
        // for the open DR topology. Otherwise relationships briefly appear and
        // disappear again on the next 10-second cluster refresh.
        setLiveClusters(previous => apiClusters.map(cluster => mapCluster(
          cluster,
          previous?.find(item => item.id === cluster.id)?.apps || [],
        )));
        setClusters(previous => apiClusters.map(cluster => mapCluster(
          cluster,
          previous.find(item => item.id === cluster.id)?.apps || [],
        )));
        return nextClusters;
      }
      if (targetView === 'storage') {
        const [clusterRes, storageRes] = await Promise.all([
          listClusters(),
          listStorageRepositories(),
        ]);
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, []));
        const apiStorage = listItems(storageRes);
        const nextStorage = apiStorage.map(mapStorageRepo);
        setLiveApiClusters(apiClusters); setLiveClusters(nextClusters); setClusters(nextClusters);
        setLiveApiStorageRepos(apiStorage); setLiveStorage(nextStorage); setStorage(nextStorage);
        return nextClusters;
      }
      if (targetView === 'policies') {
        const policyRes = await listPolicies();
        if (resourceSessionOwnerRef.current !== owner) return [];
        const nextPolicies = listItems(policyRes).map(mapPolicy);
        setLiveApiPolicies(listItems(policyRes)); setLivePolicies(nextPolicies); setPolicies(nextPolicies);
        return refreshLastResultRef.current;
      }
      if (targetView === 'tags') {
        const [clusterRes, appRes, tagRes] = await Promise.all([
          listClusters(),
          listApplications(true),
          listTags(),
        ]);
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes); const apiApps = listItems(appRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, mapApps(apiApps.filter(app => app.clusterId === cluster.id), [], [], [], apiClusters)));
        setTags(listItems(tagRes)); setLiveApiApps(apiApps); setLiveApiClusters(apiClusters); setLiveClusters(nextClusters); setClusters(nextClusters);
        return nextClusters;
      }
      const snapshot = await loadPlatformSnapshot(() => resourceSessionOwnerRef.current === owner, apiClusters => {
        setLiveApiClusters(apiClusters);
        setLiveClusters(previous => apiClusters.map(cluster => mapCluster(cluster, previous?.find(item => item.id === cluster.id)?.apps || [])));
      });
      if (!snapshot || resourceSessionOwnerRef.current !== owner) return [];
      const { clusters: apiClusters, applications: apiApps, storage: apiStorage, policies: apiPolicies, plans: apiPlans, tasks: apiTasks } = snapshot;
      const apiRestorePoints = snapshot.restorePoints.map(mapRestorePoint);
      setTags(snapshot.tags);
      const nextAppTasks = buildAppTaskMap(apiTasks, apiApps, ['backup'], apiRestorePoints, apiPlans);
      const nextRecoveryTasks = buildAppTaskMap(apiTasks, apiApps, ['restore', 'drill', 'takeover'], apiRestorePoints, apiPlans);
      const nextStorage = apiStorage.map(mapStorageRepo);
      const nextPolicies = apiPolicies.map(mapPolicy);
      const nextClusters = apiClusters.map(cluster => {
        const apps = mapApps(
          apiApps.filter(app => app.clusterId === cluster.id),
          apiPlans,
          apiPolicies,
          apiStorage,
          apiClusters,
        );
        return mapCluster(cluster, apps);
      });
      refreshLastResultRef.current = nextClusters;
      setLiveClusters(nextClusters);
      setLiveStorage(nextStorage);
      setLivePolicies(nextPolicies.length > 0 ? nextPolicies : null);
      setLiveApiClusters(apiClusters);
      setLiveApiStorageRepos(apiStorage);

      setClusters(nextClusters);
      setStorage(nextStorage);
      setPolicies(nextPolicies);
      setRestorePointCount(apiRestorePoints.length);
      setLiveRestorePoints(apiRestorePoints);
      setLiveApiTasks(apiTasks);
      setLiveApiRestorePointViews(apiRestorePoints);
      setLiveApiRestorePoints(snapshot.restorePoints);
      setLiveApiPolicies(apiPolicies);
      setLiveApiPlans(apiPlans);
      setLiveApiApps(apiApps);
      setLiveAppTasks(previous => mergeTaskMapKeepingActive(previous, nextAppTasks));
      setLiveRecoveryTasks(previous => mergeTaskMapKeepingActive(previous, nextRecoveryTasks));
      setSelectedCluster(prev => {
        if (prev && nextClusters.some(cluster => cluster.id === prev.id)) {
          return nextClusters.find(cluster => cluster.id === prev.id) || nextClusters[0] || null;
        }
        let storedSelectedId = '';
        try {
          storedSelectedId = localStorage.getItem(SELECTED_CLUSTER_KEY) || '';
        } catch {
          // localStorage may be unavailable in private contexts.
        }
        const apiDefault = nextClusters.find(cluster => cluster.isDefault);
        return nextClusters.find(cluster => cluster.id === storedSelectedId)
          || apiDefault
          || nextClusters[0]
          || null;
      });
      setDefaultClusterId(() => {
        const apiDefault = nextClusters.find(cluster => cluster.isDefault);
        return apiDefault?.id || null;
      });
      return nextClusters;
    })().finally(() => {
      if (refreshInFlightRef.current === request) {
        refreshInFlightRef.current = null;
        refreshInFlightViewRef.current = null;
      }
    });
    refreshInFlightRef.current = request;
    refreshInFlightViewRef.current = targetView;
    return request;
  }, [sessionToken, view]);

  const loadClusterTopology = useCallback(async () => {
    const owner = sessionToken;
    if (!owner || resourceSessionOwnerRef.current !== owner) return;
    const snapshot = await loadTopologySnapshot(() => resourceSessionOwnerRef.current === owner);
    if (!snapshot || resourceSessionOwnerRef.current !== owner) return;
    const { clusters: apiClusters, applications: apiApps, plans: apiPlans } = snapshot;
    const nextClusters = apiClusters.map(cluster => mapCluster(cluster, mapApps(
      apiApps.filter(app => app.clusterId === cluster.id), apiPlans, [], [], apiClusters,
    )));
    setLiveApiClusters(apiClusters);
    setLiveApiApps(apiApps);
    setLiveApiPlans(apiPlans);
    setLiveClusters(nextClusters);
    setClusters(nextClusters);
  }, [sessionToken, resourceSessionOwnerRef]);

  useEffect(() => {
    liveApiAppsRef.current = liveApiApps;
  }, [liveApiApps]);

  useEffect(() => {
    if (!sessionToken || !PLATFORM_DATA_VIEWS.has(view)) return;
    let cancelled = false;
    const owner = sessionToken;
    const realtimeViews = new Set<string>(['dashboard', 'applications', 'clusters']);
    const loadPlatformData = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        await refreshPlatformData(view);
      } catch {
        if (!cancelled) {
          // Keep the current state visible if the backend is temporarily unavailable.
        }
      }
    };
    const loadApplicationActivity = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        // Task identity is owned by the persisted plan pointers. A recovery
        // can be submitted by another browser or directly through the API, so
        // poll the lightweight plan list in the same snapshot as tasks. Using
        // a stale latestRecoveryTaskId makes the row keep rendering the prior
        // drill until a full-page refresh.
        const snapshot = await loadApplicationActivitySnapshot(() => !cancelled && resourceSessionOwnerRef.current === owner);
        if (!snapshot || cancelled || resourceSessionOwnerRef.current !== owner) return;
        const { tasks: apiTasks, plans: apiPlans, restorePoints: apiRestorePoints } = snapshot;
        const apiRestorePointViews = apiRestorePoints.map(mapRestorePoint);
        setLiveApiTasks(apiTasks);
        setLiveApiPlans(apiPlans);
        const nextAppTasks = buildAppTaskMap(apiTasks, liveApiAppsRef.current, ['backup'], apiRestorePointViews, apiPlans);
        const nextRecoveryTasks = buildAppTaskMap(apiTasks, liveApiAppsRef.current, ['restore', 'drill', 'takeover'], apiRestorePointViews, apiPlans);
        setLiveAppTasks(previous => mergeTaskMapKeepingActive(previous, nextAppTasks));
        setLiveRecoveryTasks(previous => mergeTaskMapKeepingActive(previous, nextRecoveryTasks));
        setRestorePointCount(apiRestorePointViews.length);
        setLiveRestorePoints(apiRestorePointViews);
        setLiveApiRestorePointViews(apiRestorePointViews);
        setLiveApiRestorePoints(apiRestorePoints);
      } catch {
        // Keep the current application state visible if a status poll fails.
      }
    };
    loadPlatformData();
    const shouldPoll = realtimeViews.has(view);
    const pollIntervalMs = view === 'applications' ? 3000 : 10000;
    const poll = view === 'applications' ? loadApplicationActivity : loadPlatformData;
    const timer = shouldPoll ? window.setInterval(poll, pollIntervalMs) : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [sessionToken, refreshPlatformData, view]);

  const scopedSetters = useMemo(() => ({
    setClusters: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setClusters),
    setLiveClusters: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveClusters),
    setStorage: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setStorage),
    setLiveStorage: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveStorage),
    setPolicies: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setPolicies),
    setLivePolicies: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLivePolicies),
    setTags: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setTags),
    setRestorePointCount: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setRestorePointCount),
    setLiveRestorePoints: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveRestorePoints),
    setLiveApiClusters: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiClusters),
    setLiveApiStorageRepos: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiStorageRepos),
    setLiveApiTasks: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiTasks),
    setLiveApiRestorePointViews: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiRestorePointViews),
    setLiveApiRestorePoints: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiRestorePoints),
    setLiveApiPolicies: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiPolicies),
    setLiveApiPlans: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiPlans),
    setLiveApiApps: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveApiApps),
    setLiveAppTasks: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveAppTasks),
    setLiveRecoveryTasks: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setLiveRecoveryTasks),
    setSelectedCluster: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setSelectedCluster),
    setDefaultClusterId: scopedResourceSetter(resourceSessionOwnerRef, sessionToken, setDefaultClusterId),
  }), [sessionToken, resourceSessionOwnerRef]);

  return {
    ...scopedSetters,
    clusters,
    liveClusters,
    storage,
    liveStorage,
    policies,
    livePolicies,
    tags,
    restorePointCount,
    liveRestorePoints,
    liveApiClusters,
    liveApiStorageRepos,
    liveApiTasks,
    liveApiRestorePointViews,
    liveApiRestorePoints,
    liveApiPolicies,
    liveApiPlans,
    liveApiApps,
    liveAppTasks,
    liveRecoveryTasks,
    selectedCluster,
    defaultClusterId,
    clearResources,
    refreshPlatformData,
    loadClusterTopology
  };
}
