import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AlertTriangle, Check, CheckCircle2, ChevronRight, Cloud, Edit2, Eye, GitBranch, MoreVertical, Plus, PlusCircle, RefreshCw, Server, Star, Trash2, Upload, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { ApiRequestError, apiGet, apiHeaders, apiPatch, apiPost, ensureApiResponse } from '../../api/client';
import { SearchBar } from '../../components/search-bar';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import ClusterActivityPanel from './cluster-activity-panel';
import { Metric, getProtectedAppCount, isClusterProtected } from './cluster-presentation';
import type { Cluster, ClusterNamespaceRow, ClusterNodeRow, ClusterStorageClassRow } from './types';
import type { ApiProtectionPlan } from '../recovery/types';
import { buildDRTopology, type DRRelationship } from './dr-topology';
import DRTopologyView from './dr-topology-view';

type ApiList<T>={items:T[]};
type ClusterRegistrationType='native-kubernetes'|'huaweicloud-cce'|'openshift';
type CCERegistrationMode='platform-direct'|'command';
type CCEKubeconfigContext={name:string;cluster:string;user:string;apiServer:string;isCurrent:boolean};
type CCEKubeconfigUpload={id:string;fingerprint:string;currentContext:string;contexts:CCEKubeconfigContext[];expiresAt:string};
type CCEInspectionGate={id:string;label:string;status:'passed'|'warning'|'deferred';detail:string};
type CCEInspection={context:string;clusterName:string;clusterId:string;region?:string;serverVersion:string;nodeCount:number;storageClasses:string[];defaultStorageClass?:string;gates:CCEInspectionGate[]};
type ApiAgentToken={installCommand:string;prepareNodeCommand?:string;clusterType?:ClusterRegistrationType};
type ApiCluster={id:string;name:string};
type ApiTask={id:string;clusterId:string;type:string;status:string;progress:number;errorCode?:string;errorMessage?:string;payload?:Record<string,any>;createdAt?:string;completedAt?:string};
type ApiTaskEvent={id:string;taskId:string;level:string;reason:string;message:string;payload?:Record<string,any>;createdAt?:string};
type ApiUnregisterPrecheck={agentOnline:boolean;unregisterActive:boolean;activeTaskCount:number;restorePointCount:number;sourcePlanCount:number;targetPlanCount:number;objectStorageNeeded:boolean;allowed:boolean;blockers:string[]};
type ClusterTaskLog={task:ApiTask;events:ApiTaskEvent[];loading:boolean};
const listItems=<T,>(response:ApiList<T>)=>Array.isArray(response.items)?response.items:[];
const shortDigest=(digest?:string)=>{const cleaned=(digest||'').replace(/^sha256:/,'');return cleaned.length>12?cleaned.slice(0,12):cleaned};
const formatAge=(seconds?:number)=>!seconds&&seconds!==0?'-':seconds<60?`${seconds}s`:seconds<3600?`${Math.floor(seconds/60)}m`:seconds<86400?`${Math.floor(seconds/3600)}h`:`${Math.floor(seconds/86400)}d`;
const formatLastSeen=(value?:string)=>{if(!value)return'unknown';const timestamp=new Date(value).getTime();if(!Number.isFinite(timestamp))return'unknown';const seconds=Math.max(0,Math.floor((Date.now()-timestamp)/1000));return seconds<60?`${seconds}s ago`:seconds<3600?`${Math.floor(seconds/60)}m ago`:seconds<86400?`${Math.floor(seconds/3600)}h ago`:`${Math.floor(seconds/86400)}d ago`};
const normalizeNodeStatus=(status?:string)=>{const value=(status||'').trim();return value||'Unknown'};
const formatPercent=(value:number)=>Number.isFinite(value)?Math.max(0,Math.min(100,value)).toFixed(2):'0.00';
const taskStatusLabel=(status?:string)=>status==='succeeded'?'Succeeded':status==='failed'?'Failed':['running','accepted','dispatched','queued'].includes(status||'')?'Running':status||'Unknown';
const unregisterFailure=(task:ApiTask|null,events:ApiTaskEvent[])=>{
  const event=[...events].reverse().find(item=>item.level==='error'||item.reason?.includes('FAILED'));
  const raw=String(event?.payload?.error||event?.message||task?.errorMessage||'Cluster-side cleanup failed without a detailed agent response.');
  const code=String(event?.reason||task?.errorCode||'UNREGISTER_FAILED');
  const namespace=String(event?.payload?.namespace||task?.payload?.namespace||'');
  const advice=/forbidden|cannot list|cannot delete/i.test(raw)
    ? 'The agent RBAC is incomplete or belongs to another HyperCDR edition. Repair this edition’s scoped RBAC, then retry normal unregister.'
    : /offline|not connected|connection/i.test(raw)
      ? 'Confirm the comm-agent is online and can reach the platform WebSocket endpoint, then retry.'
      : /timeout|timed out|deadline/i.test(raw)
        ? 'The Kubernetes API or etcd did not answer in time. Check control-plane health and retry after it is stable.'
        : /bucket|object storage|repository/i.test(raw)
          ? 'Check object-storage credentials, bucket access, and the cluster prefix before retrying.'
          : 'Review the agent and platform logs with the task ID, correct the reported cause, then retry normal unregister.';
  return {code,raw,namespace,advice};
};
const agentReadiness=(cluster:Cluster)=>cluster.connectionStatus!=='online'?{label:'Offline',className:'text-slate-500'}:cluster.status==='healthy'?{label:'Ready',className:'text-emerald-600'}:cluster.status==='syncing'?{label:'Syncing',className:'text-blue-600'}:{label:'Degraded',className:'text-amber-600'};
const copyTextToClipboard=async(text:string,textarea?:HTMLTextAreaElement|null)=>{try{await navigator.clipboard.writeText(text);return true}catch{if(!textarea)return false;textarea.focus();textarea.select();return document.execCommand('copy')}};
const selectCommandText=(element:HTMLElement)=>{const selection=window.getSelection();if(!selection)return;const range=document.createRange();range.selectNodeContents(element);selection.removeAllRanges();selection.addRange(range)};

function RegistrationStep({number,title,description,children}:{number:number;title:string;description:string;children?:React.ReactNode}) {
  return <section className="flex gap-3.5">
    <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white shadow-sm shadow-blue-200">{number}</div>
    <div className="min-w-0 flex-1 pb-1">
      <h3 className="text-sm font-bold leading-7 text-slate-900">{title}</h3>
      <p className="mt-0.5 text-xs leading-5 text-slate-500">{description}</p>
      {children && <div className="mt-3">{children}</div>}
    </div>
  </section>;
}

export default function ClusterPage(props: {
  clusters: Cluster[];
  loading: boolean;
  protectionPlans: ApiProtectionPlan[];
  onLoadTopology: () => Promise<void>;
  canUpgrade: boolean;
  defaultClusterId: string | null;
  clusterMenuId: string | null;
  setClusterMenuId: (id: string | null) => void;
  setSelectedCluster: (cluster: Cluster) => void;
  setDefaultCluster: (cluster: Cluster, event?: React.MouseEvent) => void;
  clearDefaultCluster: (event?: React.MouseEvent) => void;
  unregisterCluster: (cluster: Cluster, event?: React.MouseEvent, deleteBackupData?: boolean) => Promise<ApiTask>;
  onRenameCluster: (clusterId: string, name: string) => void;
  onUpgradeCluster: (clusterId: string) => Promise<ApiTask>;
  onUpgradeVelero: (clusterId: string) => Promise<ApiTask>;
  onRegisterCluster: (cluster: Cluster) => void;
  onRefreshRegistration: () => Promise<Cluster[]>;
  clusterTaskLogs: Record<string, ClusterTaskLog[]>;
  getAgentTokenForRegistration: (clusterType?: ClusterRegistrationType) => Promise<ApiAgentToken>;
  prefetchAgentToken: () => Promise<ApiAgentToken | null> | null;
  openDashboard: () => void;
  registrationAllowed?: boolean;
  openLicenseManagement?: () => void;
  toast: (msg: string) => void;
}) {
  const { clusters, loading, protectionPlans, onLoadTopology, canUpgrade, defaultClusterId, clusterMenuId, setClusterMenuId, setSelectedCluster, setDefaultCluster, clearDefaultCluster, unregisterCluster, onRenameCluster, onUpgradeCluster, onUpgradeVelero, onRegisterCluster, onRefreshRegistration, clusterTaskLogs, getAgentTokenForRegistration, prefetchAgentToken, openDashboard, registrationAllowed = true, openLicenseManagement, toast } = props;
  const [registerOpen, setRegisterOpen] = useState(false);
  const [licenseGuideOpen, setLicenseGuideOpen] = useState(false);
  const [openshiftKubeconfigGuideOpen, setOpenshiftKubeconfigGuideOpen] = useState(false);
  const [registrationType, setRegistrationType] = useState<ClusterRegistrationType>('native-kubernetes');
  const [cceRegistrationMode, setCCERegistrationMode] = useState<CCERegistrationMode>('platform-direct');
  const [cceUpload, setCCEUpload] = useState<CCEKubeconfigUpload | null>(null);
  const [cceContext, setCCEContext] = useState('');
  const [cceUploadLoading, setCCEUploadLoading] = useState(false);
  const [cceUploadError, setCCEUploadError] = useState('');
  const [cceInspection, setCCEInspection] = useState<CCEInspection | null>(null);
  const [cceInspectionLoading, setCCEInspectionLoading] = useState(false);
  const [cceStorageClass, setCCEStorageClass] = useState('');
  const [cceRegistrationTask, setCCERegistrationTask] = useState<ApiTask | null>(null);
  const [cceIdempotencyKey, setCCEIdempotencyKey] = useState('');
  const [registerStep, setRegisterStep] = useState<1 | 2 | 3>(1);
  const [copied, setCopied] = useState(false);
  const [caCopied, setCaCopied] = useState(false);
  const [prepareNodeCommand, setPrepareNodeCommand] = useState('');
  const [installCommand, setInstallCommand] = useState('');
  const [installLoading, setInstallLoading] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [registrationBaseline, setRegistrationBaseline] = useState<string[]>([]);
  const [registrationWaiting, setRegistrationWaiting] = useState(false);
  const [upgradeTarget, setUpgradeTarget] = useState<Cluster | null>(null);
  const [veleroUpgradeTarget, setVeleroUpgradeTarget] = useState<Cluster | null>(null);
  const [unregisterTarget, setUnregisterTarget] = useState<Cluster | null>(null);
  const [forceRemoveEnabled, setForceRemoveEnabled] = useState(false);
  const [forceRemoveConfirmation, setForceRemoveConfirmation] = useState('');
  const [unregisterPrecheck, setUnregisterPrecheck] = useState<ApiUnregisterPrecheck | null>(null);
  const [unregisterPrecheckLoading, setUnregisterPrecheckLoading] = useState(false);
  const [deleteBackupData, setDeleteBackupData] = useState(false);
  const [renameTarget, setRenameTarget] = useState<Cluster | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [unregistering, setUnregistering] = useState(false);
  const [unregisterSubmitError, setUnregisterSubmitError] = useState('');
  const [upgradeSubmitting, setUpgradeSubmitting] = useState(false);
  const [veleroUpgradeSubmitting, setVeleroUpgradeSubmitting] = useState(false);
  const [highlightedTaskId, setHighlightedTaskId] = useState<string | null>(null);
  const [unregisterTaskId, setUnregisterTaskId] = useState<string | null>(null);
  const [unregisterTask, setUnregisterTask] = useState<ApiTask | null>(null);
  const [unregisterEvents, setUnregisterEvents] = useState<ApiTaskEvent[]>([]);
  const [clusterResourceDetail, setClusterResourceDetail] = useState<{ cluster: Cluster; type: 'overview' | 'namespaces' | 'nodes' | 'storageClasses' } | null>(null);
  const [actionCopied, setActionCopied] = useState(false);
  const [topologyOpen, setTopologyOpen] = useState(false);
  const [topologyLoading, setTopologyLoading] = useState(false);
  const [topologyLoadError, setTopologyLoadError] = useState('');
  const [selectedRelationshipId, setSelectedRelationshipId] = useState<string | null>(null);
  const [selectedTopologyClusterId, setSelectedTopologyClusterId] = useState<string | null>(null);
  const topology = useMemo(() => buildDRTopology(clusters, protectionPlans), [clusters, protectionPlans]);
  const selectRelationship = (relationship: DRRelationship) => { setSelectedRelationshipId(relationship.id); setSelectedTopologyClusterId(null); };
  const openTopology = async () => {
    setSelectedRelationshipId(null);
    setSelectedTopologyClusterId(null);
    setTopologyLoadError('');
    setTopologyOpen(true);
    setTopologyLoading(true);
    try {
      await onLoadTopology();
    } catch (error) {
      setTopologyLoadError(error instanceof Error ? error.message : 'Unable to load DR topology.');
    } finally {
      setTopologyLoading(false);
    }
  };
  const registryCACommandRef = useRef<HTMLTextAreaElement | null>(null);
  const installCommandRef = useRef<HTMLTextAreaElement | null>(null);
  const actionCommandRef = useRef<HTMLTextAreaElement | null>(null);
  const namespaceRowsForCluster = (cluster: Cluster): ClusterNamespaceRow[] => [...cluster.apps]
    .sort((a, b) => a.namespace.localeCompare(b.namespace))
    .map(app => ({
      name: app.namespace,
      status: app.namespaceStatus || app.status || 'Unknown',
      age: formatAge(app.resourceSummary?.ageSeconds),
    }));
  const nodeRowsForCluster = (cluster: Cluster) => {
    const details = cluster.nodeDetails || [];
    return [...details]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map(node => ({
        name: node.name,
        status: normalizeNodeStatus(node.status),
        roles: node.roles || '<none>',
        age: formatAge(node.ageSeconds),
      version: node.kubeletVersion || '-',
    }));
  };
  const storageClassRowsForCluster = (cluster: Cluster): ClusterStorageClassRow[] => [...(cluster.storageClasses || [])]
    .sort((a, b) => {
      if (Boolean(a.default) !== Boolean(b.default)) return a.default ? -1 : 1;
      return a.name.localeCompare(b.name);
    })
    .map(storageClass => ({
      name: `${storageClass.name}${storageClass.default ? ' (default)' : ''}`,
      provisioner: storageClass.provisioner || '-',
      reclaimPolicy: storageClass.reclaimPolicy || '-',
      volumeBindingMode: storageClass.volumeBindingMode || '-',
      allowVolumeExpansion: storageClass.allowVolumeExpansion || 'false',
      age: formatAge(storageClass.ageSeconds),
    }));
  const namespaceColumns = useMemo<HyperTableColumn<ClusterNamespaceRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 260,
      minSize: 150,
      maxSize: 560,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'primary', title: row => row.name },
    },
    {
      accessorKey: 'status',
      header: 'STATUS',
      size: 112,
      minSize: 92,
      maxSize: 220,
      cell: info => <span className="hbdr-cluster-status-pill">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'status', title: row => row.status },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.age },
    },
  ], []);
  const nodeColumns = useMemo<HyperTableColumn<ClusterNodeRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 240,
      minSize: 150,
      maxSize: 520,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'primary', title: row => row.name },
    },
    {
      accessorKey: 'status',
      header: 'STATUS',
      size: 110,
      minSize: 92,
      maxSize: 180,
      cell: info => <span className="hbdr-cluster-status-pill">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'status', title: row => row.status },
    },
    {
      accessorKey: 'roles',
      header: 'ROLES',
      size: 160,
      minSize: 110,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.roles },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.age },
    },
    {
      accessorKey: 'version',
      header: 'VERSION',
      size: 132,
      minSize: 108,
      maxSize: 220,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.version },
    },
  ], []);
  const storageClassColumns = useMemo<HyperTableColumn<ClusterStorageClassRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 210,
      minSize: 150,
      maxSize: 420,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'primary', title: row => row.name },
    },
    {
      accessorKey: 'provisioner',
      header: 'PROVISIONER',
      size: 230,
      minSize: 160,
      maxSize: 460,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'code', title: row => row.provisioner },
    },
    {
      accessorKey: 'reclaimPolicy',
      header: 'RECLAIMPOLICY',
      size: 128,
      minSize: 112,
      maxSize: 220,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.reclaimPolicy },
    },
    {
      accessorKey: 'volumeBindingMode',
      header: 'VOLUMEBINDINGMODE',
      size: 190,
      minSize: 150,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.volumeBindingMode },
    },
    {
      accessorKey: 'allowVolumeExpansion',
      header: 'ALLOWVOLUMEEXPANSION',
      size: 190,
      minSize: 150,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.allowVolumeExpansion },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { kind: 'secondary', title: row => row.age },
    },
  ], []);
  useEffect(() => {
    if (!clusterMenuId) return;
    const closeMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest('[data-cluster-menu-root]')) return;
      setClusterMenuId(null);
    };
    window.addEventListener('click', closeMenu, true);
    return () => window.removeEventListener('click', closeMenu, true);
  }, [clusterMenuId, setClusterMenuId]);

  const loadRegistrationCommand = async (clusterType: ClusterRegistrationType) => {
    setRegistrationType(clusterType);
    setInstallLoading(true);
    setInstallError(null);
    setPrepareNodeCommand('');
    try {
      const token = await getAgentTokenForRegistration(clusterType);
      setPrepareNodeCommand(token.prepareNodeCommand || '');
      setInstallCommand(token.installCommand);
      if (clusterType === 'native-kubernetes') void prefetchAgentToken();
    } catch (error) {
      setInstallCommand('');
      setPrepareNodeCommand('');
      const message = error instanceof ApiRequestError && error.code === 'LICENSE_NOT_ACTIVE'
        ? 'License required: start a trial or import a formal license in Settings > License Management before registering a cluster.'
        : error instanceof Error ? error.message : 'Install command generation failed.';
      setInstallError(message);
      toast(message);
    } finally { setInstallLoading(false); }
  };

  const openRegister = async () => {
    if (!registrationAllowed) {
      setLicenseGuideOpen(true);
      return;
    }
    setRegisterStep(1);
    setCopied(false);
    setInstallError(null);
    setRegistrationBaseline(clusters.map(cluster => cluster.id));
    setRegistrationWaiting(false);
    setRegisterOpen(true);
    await loadRegistrationCommand('native-kubernetes');
  };

  const resetDirectRegistration = () => {
    if (cceUpload?.id) void fetch(`/api/v1/cluster-registrations/kubeconfigs/${encodeURIComponent(cceUpload.id)}`, { method: 'DELETE', headers: apiHeaders() });
    setCCEUpload(null);
    setCCEContext('');
    setCCEUploadError('');
    setCCEInspection(null);
    setCCEStorageClass('');
    setCCERegistrationTask(null);
    setCCEIdempotencyKey('');
  };

  const closeRegister = () => {
	if (cceRegistrationTask && ['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(cceRegistrationTask.status)) {
	  toast('Registration is still running. Cancel it explicitly before closing this panel.');
	  return;
	}
    setRegisterOpen(false);
    setRegisterStep(1);
    setCopied(false);
    setCaCopied(false);
    setRegistrationWaiting(false);
    resetDirectRegistration();
  };

  const inspectCCECluster = async () => {
    if (!cceUpload || !cceContext) return;
    setCCEInspectionLoading(true);
    setCCEUploadError('');
    try {
      const inspection = await apiPost<CCEInspection>('/api/v1/cluster-registrations/inspections', { sessionId: cceUpload.id, context: cceContext, clusterType: registrationType });
      setCCEInspection(inspection);
      setCCEStorageClass(inspection.defaultStorageClass || (inspection.storageClasses.length === 1 ? inspection.storageClasses[0] : ''));
    } catch (error) {
      setCCEInspection(null);
      setCCEUploadError(error instanceof Error ? error.message : 'Cluster inspection failed.');
    } finally { setCCEInspectionLoading(false); }
  };

  const startCCEDirectRegistration = async () => {
    if (!cceUpload || !cceContext || !cceInspection) return;
    setCCEUploadError('');
    setCCEInspectionLoading(true);
    try {
      const key = cceIdempotencyKey || `cce-${crypto.randomUUID()}`;
      setCCEIdempotencyKey(key);
      setCCERegistrationTask(await apiPost<ApiTask>('/api/v1/cluster-registrations/tasks', { sessionId: cceUpload.id, context: cceContext, storageClass: cceStorageClass, idempotencyKey: key, clusterType: registrationType }));
    } catch (error) {
      setCCEUploadError(error instanceof Error ? error.message : 'Cluster registration could not be started.');
    } finally { setCCEInspectionLoading(false); }
  };

  const cancelCCEDirectRegistration = async () => {
    if (!cceRegistrationTask) return;
    setCCEInspectionLoading(true);
    try {
      const response = await apiPost<{task: ApiTask}>(`/api/v1/tasks/${encodeURIComponent(cceRegistrationTask.id)}/cancel`, {});
      setCCERegistrationTask(response.task);
    } catch (error) {
      setCCEUploadError(error instanceof Error ? error.message : 'Registration cancellation failed.');
    } finally { setCCEInspectionLoading(false); }
  };

  useEffect(() => {
    const task = cceRegistrationTask;
    if (!task || !['queued', 'running', 'accepted', 'dispatched'].includes(task.status)) return;
    const timer = window.setInterval(() => {
      void apiGet<ApiTask>(`/api/v1/tasks/${encodeURIComponent(task.id)}`).then(updated => setCCERegistrationTask(updated)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [cceRegistrationTask?.id, cceRegistrationTask?.status]);

  // A direct registration changes the cluster list (and may assign the first
  // cluster as the platform default).  Refresh the canonical list as soon as
  // the task reaches a terminal state so the card and Default badge never
  // remain stale after the drawer is closed or the page is refreshed.
  useEffect(() => {
    if (!cceRegistrationTask || cceRegistrationTask.status !== 'succeeded') return;
    let cancelled = false;
    void onRefreshRegistration().then(nextClusters => {
      if (cancelled) return;
      const latest = nextClusters.find(cluster => cluster.id === cceRegistrationTask.clusterId)
        || nextClusters.find(cluster => !registrationBaseline.includes(cluster.id));
      if (latest) setSelectedCluster(latest);
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [cceRegistrationTask?.id, cceRegistrationTask?.status, cceRegistrationTask?.clusterId, onRefreshRegistration, registrationBaseline, setSelectedCluster]);

  const uploadCCEKubeconfig = async (file: File | undefined) => {
    if (!file) return;
    setCCEUploadLoading(true);
    setCCEUploadError('');
    setCCEInspection(null);
    setCCEStorageClass('');
    setCCERegistrationTask(null);
    setCCEIdempotencyKey('');
    try {
      const body = new FormData();
      body.append('kubeconfig', file);
      body.append('clusterType', registrationType);
      const response = await ensureApiResponse(await fetch('/api/v1/cluster-registrations/kubeconfigs', { method: 'POST', headers: apiHeaders(), body }), '/api/v1/cluster-registrations/kubeconfigs');
      const upload = await response.json() as CCEKubeconfigUpload;
      if (cceUpload?.id && cceUpload.id !== upload.id) void fetch(`/api/v1/cluster-registrations/kubeconfigs/${encodeURIComponent(cceUpload.id)}`, { method: 'DELETE', headers: apiHeaders() });
      setCCEUpload(upload);
      const current = upload.contexts.find(context => context.isCurrent)?.name || (upload.contexts.length === 1 ? upload.contexts[0].name : '');
      setCCEContext(current);
    } catch (error) {
      setCCEUpload(null);
      setCCEContext('');
      setCCEUploadError(error instanceof Error ? error.message : 'Kubeconfig validation failed.');
    } finally {
      setCCEUploadLoading(false);
    }
  };

  const openUpgrade = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setActionCopied(false);
    setUpgradeTarget(cluster);
  };

  const closeUpgrade = () => {
    setUpgradeTarget(null);
    setActionCopied(false);
  };

  const openVeleroUpgrade = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setVeleroUpgradeTarget(cluster);
  };

  const closeVeleroUpgrade = () => {
    if (veleroUpgradeSubmitting) return;
    setVeleroUpgradeTarget(null);
  };

  const openUnregister = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setClusterMenuId(null);
    setActionCopied(false);
    setForceRemoveEnabled(false);
    setForceRemoveConfirmation('');
    setDeleteBackupData(false);
    setUnregisterPrecheck(null);
    setUnregisterSubmitError('');
    setUnregisterTarget(cluster);
    setUnregisterPrecheckLoading(true);
    void apiGet<ApiUnregisterPrecheck>(`/api/v1/clusters/${cluster.id}/unregister/precheck`)
      .then(setUnregisterPrecheck)
      .catch(error => toast(error instanceof Error ? error.message : 'Failed to check cluster unregister readiness'))
      .finally(() => setUnregisterPrecheckLoading(false));
  };

  const openRename = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setClusterMenuId(null);
    setRenameValue(cluster.name === 'unknown-cluster' ? '' : cluster.name);
    setRenameTarget(cluster);
  };

  const closeRename = () => {
    setRenameTarget(null);
    setRenameValue('');
    setRenaming(false);
  };

  const closeUnregister = () => {
    if (unregistering) return;
    setUnregisterTarget(null);
    setForceRemoveEnabled(false);
    setForceRemoveConfirmation('');
    setDeleteBackupData(false);
    setUnregisterPrecheck(null);
    setUnregisterSubmitError('');
    setUnregisterTaskId(null);
    setUnregisterTask(null);
    setUnregisterEvents([]);
    setActionCopied(false);
  };

  const copyInstallCommand = async () => {
    if (await copyTextToClipboard(installCommand, installCommandRef.current)) {
      setCopied(true);
      setRegisterStep(3);
      toast('Install command copied');
      window.setTimeout(() => setCopied(false), 1800);
    } else {
      installCommandRef.current?.focus();
      installCommandRef.current?.select();
      toast('Clipboard is unavailable. The install command is selected; press Ctrl+C to copy it.');
    }
  };

  const copyRegistryCACommand = async () => {
    if (await copyTextToClipboard(prepareNodeCommand, registryCACommandRef.current)) {
      setCaCopied(true);
      toast('Node prepare command copied');
      window.setTimeout(() => setCaCopied(false), 1800);
    } else {
      registryCACommandRef.current?.focus();
      registryCACommandRef.current?.select();
      toast('Clipboard is unavailable. The node prepare command is selected; press Ctrl+C to copy it.');
    }
  };

  const copyActionCommand = async (command: string) => {
    if (await copyTextToClipboard(command, actionCommandRef.current)) {
      setActionCopied(true);
      toast('Command copied');
      window.setTimeout(() => setActionCopied(false), 1800);
    } else {
      actionCommandRef.current?.focus();
      actionCommandRef.current?.select();
      toast('Clipboard is unavailable. The command is selected; press Ctrl+C to copy it.');
    }
  };

  const finishRegisterCluster = () => {
    toast('Waiting for the agent to connect. The cluster card appears after registration succeeds.');
    closeRegister();
  };

  useEffect(() => {
    if (!registerOpen || registerStep !== 3) return;
    let cancelled = false;
    setRegistrationWaiting(true);
    const poll = async () => {
      try {
        const nextClusters = await onRefreshRegistration();
        if (cancelled) return;
        const latest = nextClusters.find(cluster => !registrationBaseline.includes(cluster.id));
        if (latest) {
          setSelectedCluster(latest);
          toast(`${latest?.name || 'Cluster'} registered and connected`);
          closeRegister();
        }
      } catch {
        if (!cancelled) setInstallError('Waiting for agent connection. The platform API is temporarily unreachable.');
      }
    };
    poll();
    const timer = window.setInterval(poll, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [registerOpen, registerStep, registrationBaseline, onRefreshRegistration, setSelectedCluster, toast]);

  useEffect(() => {
    if (!unregisterTaskId) return;
    let cancelled = false;
    const loadTask = async () => {
      try {
        const [taskRes, eventRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks?types=unregister'),
          apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${unregisterTaskId}/events`),
        ]);
        if (cancelled) return;
        const task = listItems(taskRes).find(item => item.id === unregisterTaskId) || null;
        setUnregisterTask(task);
        setUnregisterEvents(listItems(eventRes));
        if (task?.status === 'succeeded') {
          toast('Cluster unregister completed');
          void onRefreshRegistration();
          setUnregisterTaskId(null);
        }
        if (task?.status === 'failed') {
          const failure=unregisterFailure(task,listItems(eventRes));
          toast(`Cluster unregister failed: ${failure.raw}`);
          setUnregisterTaskId(null);
        }
      } catch {
        if (!cancelled) toast('Failed to refresh unregister task status');
      }
    };
    loadTask();
    const timer = window.setInterval(loadTask, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [unregisterTaskId, onRefreshRegistration, toast]);

  const finishUpgradeCluster = async () => {
    if (!upgradeTarget) return;
    setUpgradeSubmitting(true);
    try {
      const task = await onUpgradeCluster(upgradeTarget.id);
      setHighlightedTaskId(task.id);
      toast(`${upgradeTarget.name} agent upgrade submitted`);
      closeUpgrade();
    } catch (error) {
      toast(error instanceof Error ? error.message : 'Agent upgrade failed to submit');
    } finally {
      setUpgradeSubmitting(false);
    }
  };

  const finishVeleroUpgrade = async () => {
    if (!veleroUpgradeTarget) return;
    setVeleroUpgradeSubmitting(true);
    try {
      const task = await onUpgradeVelero(veleroUpgradeTarget.id);
      setHighlightedTaskId(task.id);
      toast(`${veleroUpgradeTarget.name} Velero upgrade task created`);
      setVeleroUpgradeTarget(null);
    } catch (error) {
      toast(error instanceof Error ? error.message : 'Velero upgrade failed to submit');
    } finally {
      setVeleroUpgradeSubmitting(false);
    }
  };

  const finishUnregisterCluster = async () => {
    if (!unregisterTarget) return;
    setUnregistering(true);
    setUnregisterSubmitError('');
    if (forceRemoveEnabled) {
      const target = unregisterTarget;
      try {
        const result = await apiPost<{ warning?: string }>(`/api/v1/clusters/${target.id}/force-cleanup`, {
          reason: 'force remove requested from unregister dialog',
        });
        setUnregisterTarget(null);
        setForceRemoveEnabled(false);
        setForceRemoveConfirmation('');
        toast(result.warning || `${target.name} force remove completed`);
        await onRefreshRegistration();
      } catch (error) {
        toast(`Force remove failed: ${error instanceof Error ? error.message : 'unknown error'}`);
      } finally {
        setUnregistering(false);
      }
      return;
    }
    try {
      const task = await unregisterCluster(unregisterTarget, undefined, deleteBackupData);
      setUnregisterTaskId(task.id);
      setUnregisterTask(task);
      setUnregisterEvents([]);
      setUnregisterTarget(null);
      toast('Unregister task created. Track progress in Recent Tasks.');
    } catch (error) {
      const detail = error instanceof ApiRequestError
        ? `${error.message} (${error.code}, HTTP ${error.status}${error.requestId ? `, request ${error.requestId}` : ''})`
        : error instanceof Error ? error.message : 'Unknown request failure';
      setUnregisterSubmitError(detail);
      toast(`Unregister failed: ${detail}`);
    } finally {
      setUnregistering(false);
    }
  };

  const finishRenameCluster = async () => {
    if (!renameTarget) return;
    const target = renameTarget;
    const previousName = target.name;
    const nextName = renameValue.trim();
    if (!nextName) {
      toast('Cluster name is required');
      return;
    }
    if (nextName === previousName) {
      closeRename();
      return;
    }
    setRenaming(true);
    onRenameCluster(target.id, nextName);
    closeRename();
    try {
      const updated = await apiPatch<ApiCluster>(`/api/v1/clusters/${target.id}`, { name: nextName });
      onRenameCluster(target.id, updated.name || nextName);
      void onRefreshRegistration().catch(() => {
        toast('Cluster name updated, but refresh failed');
      });
      toast('Cluster name updated');
    } catch (err) {
      onRenameCluster(target.id, previousName);
      toast(`Failed to update cluster name: ${err instanceof Error ? err.message : 'unknown error'}`);
    }
  };

  const failedUnregister = unregisterTask?.status === 'failed' ? unregisterFailure(unregisterTask, unregisterEvents) : null;

  return (
    <motion.div key="clusters" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-clusters-page">
      <div className="hbdr-clusters-workspace">
      <SearchBar title="Clusters" desc="Register clusters and maintain the default cluster." />

      {failedUnregister && (
        <section role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs leading-5 text-rose-800">
          <div className="flex items-start gap-3">
            <AlertTriangle size={18} className="mt-0.5 shrink-0 text-rose-600" />
            <div className="min-w-0">
              <strong className="block text-sm text-rose-900">Cluster unregister failed</strong>
              <span className="mt-1 block break-words">{failedUnregister.raw}</span>
              <span className="mt-2 block text-rose-700">{failedUnregister.advice}</span>
              <span className="mt-2 block break-all font-mono text-[10px] text-rose-600">Code: {failedUnregister.code}{failedUnregister.namespace ? ` · Namespace: ${failedUnregister.namespace}` : ''} · Task: {unregisterTask?.id}</span>
            </div>
          </div>
        </section>
      )}

      {loading ? (
        <div className="hbdr-section-card flex min-h-48 items-center justify-center text-xs font-semibold text-slate-400" role="status">Loading clusters...</div>
      ) : clusters.length > 0 ? (
        <div className="hbdr-section-card hbdr-clusters-card-region">
          <div className="hbdr-section-toolbar">
            <div>
              <h3>Registered Clusters</h3>
            </div>
            <div className="hbdr-cluster-toolbar-actions">
              <button type="button" onClick={() => void openTopology()} className="hbdr-cluster-topology-trigger"><GitBranch size={14} />DR Topology</button>
              <button type="button" onClick={openRegister} className="hbdr-dr-action-primary inline-flex items-center gap-1.5"><Plus size={14} />Register Cluster</button>
            </div>
          </div>
          <div className={`hbdr-cluster-card-grid ${clusters.length <= 3 ? 'is-single-row' : 'is-multi-row'}`}>
            {clusters.map(cluster => (
              <motion.div key={cluster.id} whileHover={{ y: -2 }} className={`cluster-card-premium ${cluster.connectionStatus !== 'online' ? 'cluster-card-offline' : ''} relative w-full cursor-default overflow-visible rounded-xl border border-slate-200 bg-white p-3.5 shadow-sm transition-all hover:border-blue-200 hover:shadow-lg md:w-[340px] group ${clusterMenuId === cluster.id ? 'z-40' : 'z-0'}`}>
              {(() => {
                const readiness = agentReadiness(cluster);
                const unregisterTaskForCluster = unregisterTask?.clusterId === cluster.id ? unregisterTask : null;
                const unregisterActive = unregisterTaskForCluster && !['succeeded', 'failed'].includes(unregisterTaskForCluster.status);
                return (
                  <>
              {unregisterTaskForCluster && (
                <div className={`absolute left-5 top-5 z-20 rounded-full border px-2 py-0.5 text-[10px] font-bold ${unregisterTaskForCluster.status === 'failed' ? 'border-rose-100 bg-rose-50 text-rose-700' : unregisterTaskForCluster.status === 'succeeded' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>
                  {unregisterActive ? `Unregistering ${formatPercent(unregisterTaskForCluster.progress || 0)}%` : `Unregister ${taskStatusLabel(unregisterTaskForCluster.status)}`}
                </div>
              )}
              <div className="absolute right-5 top-5 z-20" data-cluster-menu-root>
                <button onClick={(event) => { event.stopPropagation(); setClusterMenuId(clusterMenuId === cluster.id ? null : cluster.id); }} className="cluster-card-action-button flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700" aria-label="Cluster Actions">
                  <MoreVertical size={17} />
                </button>
                <AnimatePresence>
                  {clusterMenuId === cluster.id && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={(event) => { event.stopPropagation(); setClusterMenuId(null); }} />
                      <motion.div data-cluster-menu-root initial={{ opacity: 0, scale: 0.96, y: 8 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.96, y: 8 }} className="cluster-card-action-menu absolute right-0 top-9 z-50 w-44 rounded-xl border border-slate-100 bg-white py-2 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5" onClick={(event) => event.stopPropagation()}>
                        <button onClick={(event) => { event.stopPropagation(); setClusterMenuId(null); setClusterResourceDetail({ cluster, type: 'overview' }); }} className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-slate-600 hover:bg-slate-50"><Eye size={15} />View Detail</button>
                        <button onClick={(event) => openUnregister(cluster, event)} className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-rose-600 hover:bg-rose-50"><Trash2 size={15} />Unregister Cluster</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>

              <div className="cluster-card-head mb-2 flex items-start justify-between">
                <div className="cluster-card-icon rounded-lg bg-slate-50 p-2 transition-colors group-hover:bg-blue-50"><Server className="text-blue-600" size={20} /></div>
                <div className="cluster-card-state-stack flex flex-col items-end gap-1.5 pr-10">
                  {cluster.id === defaultClusterId ? (
                    <button type="button" onClick={(event) => clearDefaultCluster(event)} className="cluster-default-button cluster-default-button-active inline-flex items-center gap-1 rounded-full border border-blue-100 bg-blue-50 px-2 py-0.5 text-[10px] font-semibold text-blue-700 transition-colors hover:border-blue-200 hover:bg-blue-100">
                      <Star size={10} className="fill-blue-500 text-blue-500" />Default
                    </button>
                  ) : (
                    <button type="button" onClick={(event) => setDefaultCluster(cluster, event)} className="cluster-default-button inline-flex items-center gap-1 rounded-full border border-slate-200 bg-white px-2 py-0.5 text-[10px] font-semibold text-slate-500 transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700">
                      <Star size={10} />Default
                    </button>
                  )}
                  {(topology.summaries[cluster.id]?.outboundRelationships > 0 || topology.summaries[cluster.id]?.inboundRelationships > 0) && (
                    <div className="hbdr-cluster-role-badges" aria-label="DR roles">
                      {topology.summaries[cluster.id]?.outboundRelationships > 0 && <span className="is-source">Source</span>}
                      {topology.summaries[cluster.id]?.inboundRelationships > 0 && <span className="is-target">Target</span>}
                    </div>
                  )}
                </div>
              </div>
              {renameTarget?.id === cluster.id ? (
                <div className="mb-1 flex min-w-0 items-center gap-1.5 pr-10">
                  <input
                    value={renameValue}
                    onChange={event => setRenameValue(event.target.value)}
                    onClick={event => event.stopPropagation()}
                    onKeyDown={event => {
                      if (event.key === 'Enter') {
                        event.preventDefault();
                        finishRenameCluster();
                      }
                      if (event.key === 'Escape') {
                        event.preventDefault();
                        closeRename();
                      }
                    }}
                    autoFocus
                    placeholder="source-cluster-01"
                    className="h-8 min-w-0 flex-1 rounded border border-blue-200 bg-white px-2.5 text-[1rem] font-extrabold tracking-tight text-slate-900 outline-none focus:border-blue-500"
                    aria-label="Cluster display name"
                  />
                  <button type="button" disabled={renaming} onClick={(event) => { event.stopPropagation(); finishRenameCluster(); }} className="flex h-8 w-8 shrink-0 items-center justify-center rounded bg-blue-600 text-white transition-colors hover:bg-blue-700 disabled:cursor-wait disabled:bg-blue-300" aria-label="Save cluster name">
                    <Check size={14} />
                  </button>
                  <button type="button" disabled={renaming} onClick={(event) => { event.stopPropagation(); closeRename(); }} className="flex h-8 w-8 shrink-0 items-center justify-center rounded border border-slate-200 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-600 disabled:cursor-wait disabled:opacity-60" aria-label="Cancel cluster name edit">
                    <X size={14} />
                  </button>
                </div>
              ) : (
                <div className="mb-1 flex min-w-0 items-center gap-2 pr-10">
                  <h4 className={`cluster-card-title min-w-0 truncate text-[1.08rem] font-extrabold tracking-tight transition-colors group-hover:text-blue-700 ${cluster.name === 'unknown-cluster' ? 'text-slate-500' : 'text-slate-950'}`}>{cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : cluster.name}</h4>
                  <button type="button" onClick={(event) => openRename(cluster, event)} className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded text-slate-400 transition-colors hover:bg-blue-50 hover:text-blue-600" aria-label="Edit cluster name">
                    <Edit2 size={14} />
                  </button>
                </div>
              )}
              <p className="mb-2 break-all font-mono text-[10px] font-semibold leading-4 text-slate-500">{cluster.id.slice(0,8)}…</p>
              <p className="cluster-card-meta mb-2 text-[11px] font-medium text-slate-500">Kubernetes {cluster.version} · {cluster.connectionStatus === 'online' ? 'Online' : 'Offline'}</p>
              {cluster.connectionStatus !== 'online' && (
                <div className="cluster-offline-alert mb-2">
                  <AlertTriangle size={13} />
                  <span>Agent offline. Reconnecting...</span>
                </div>
              )}
              <div className="cluster-agent-panel mb-2 grid grid-cols-3 gap-1.5 rounded-md border border-transparent bg-slate-50 px-2 py-1.5">
                <div className="cluster-components-cell">
                  <div className="cluster-component-row">
                    <span>Comm-agent</span>
                    <p className="cluster-component-version" title={cluster.agentUpgradeAvailable && cluster.latestAgentVersion ? `${cluster.agentVersion} → ${cluster.latestAgentVersion}` : `Current version: ${cluster.agentVersion}`}>
                      {cluster.agentVersion}{cluster.agentUpgradeAvailable && cluster.latestAgentVersion ? ` → ${cluster.latestAgentVersion}` : ''}
                    </p>
                  {canUpgrade && cluster.agentUpgradeAvailable && cluster.agentUpgradeStatus !== 'upgrading' && cluster.connectionStatus === 'online' && (
                    <button
                      type="button"
                      onClick={(event) => openUpgrade(cluster, event)}
                      className="cluster-component-update"
                      title={`Update available: ${cluster.latestAgentVersion || ''}@${shortDigest(cluster.latestAgentImageDigest)}`}
                      aria-label={`Update Comm-agent to ${cluster.latestAgentVersion || 'the latest version'}`}
                    >
                      Update
                    </button>
                  )}
                  {cluster.agentUpgradeStatus === 'upgrading' && (
                    <span className="cluster-component-progress">Upgrading</span>
                  )}
                  {cluster.agentUpgradeStatus !== 'upgrading' && !cluster.agentUpgradeAvailable && cluster.agentVersion !== 'pending' && cluster.agentVersion !== 'unknown' && (
                    <span className="cluster-component-state is-current">Up to date</span>
                  )}
                  {cluster.agentUpgradeStatus !== 'upgrading' && !cluster.agentUpgradeAvailable && (cluster.agentVersion === 'pending' || cluster.agentVersion === 'unknown') && (
                    <span className="cluster-component-state is-unknown">Not detected</span>
                  )}
                  {(!canUpgrade || cluster.connectionStatus !== 'online') && cluster.agentUpgradeStatus !== 'upgrading' && cluster.agentUpgradeAvailable && (
                    <span className="cluster-component-state is-available">Update available</span>
                  )}
                  </div>
                  <div className="cluster-component-row">
                    <span>Velero-agent</span>
                    <p className="cluster-component-version" title={cluster.veleroUpgradeAvailable && cluster.latestVeleroVersion ? `${cluster.veleroVersion || 'unknown'} → ${cluster.latestVeleroVersion}` : `Current version: ${cluster.veleroVersion || 'unknown'}`}>
                      {cluster.veleroVersion || 'unknown'}{cluster.veleroUpgradeAvailable && cluster.latestVeleroVersion ? ` → ${cluster.latestVeleroVersion}` : ''}
                    </p>
                  {canUpgrade && cluster.veleroUpgradeAvailable && cluster.veleroUpgradeStatus !== 'upgrading' && cluster.connectionStatus === 'online' && (
                    <button type="button" onClick={(event) => openVeleroUpgrade(cluster, event)} className="cluster-component-update" title={`Update available: ${cluster.latestVeleroVersion || ''}@${shortDigest(cluster.latestVeleroImageDigest)}`} aria-label={`Update Velero-agent to ${cluster.latestVeleroVersion || 'the latest version'}`}>
                      Update
                    </button>
                  )}
                  {cluster.veleroUpgradeStatus === 'upgrading' && <span className="cluster-component-progress">{formatPercent(cluster.veleroUpgradeProgress || 0)}%</span>}
                  {cluster.veleroUpgradeStatus !== 'upgrading' && !cluster.veleroUpgradeAvailable && cluster.veleroVersion && cluster.veleroVersion !== 'unknown' && (
                    <span className="cluster-component-state is-current">Up to date</span>
                  )}
                  {cluster.veleroUpgradeStatus !== 'upgrading' && !cluster.veleroUpgradeAvailable && (!cluster.veleroVersion || cluster.veleroVersion === 'unknown') && (
                    <span className="cluster-component-state is-unknown">Not detected</span>
                  )}
                  {(!canUpgrade || cluster.connectionStatus !== 'online') && cluster.veleroUpgradeStatus !== 'upgrading' && cluster.veleroUpgradeAvailable && (
                    <span className="cluster-component-state is-available">Update available</span>
                  )}
                  </div>
                </div>
                <div className="cluster-runtime-cell">
                  <div><span>Status</span><p className={readiness.className}>{readiness.label}</p></div>
                  <div><span>Last Seen</span><p>{formatLastSeen(cluster.lastSeenAt)}</p></div>
                </div>
              </div>
              <div className="cluster-metrics-grid grid grid-cols-3 gap-2 border-t border-slate-50 pt-2 text-xs">
                <Metric label="Nodes" value={cluster.nodes} onClick={() => setClusterResourceDetail({ cluster, type: 'nodes' })} />
                <Metric label="Namespaces" value={cluster.applications} onClick={() => setClusterResourceDetail({ cluster, type: 'namespaces' })} />
                <Metric label="Protected" value={topology.summaries[cluster.id]?.protectedApps || 0} success={(topology.summaries[cluster.id]?.protectedApps || 0) > 0} />
              </div>
              <button type="button" onClick={() => { setSelectedCluster(cluster); openDashboard(); }} className="cluster-entry-bar mt-2 flex w-full items-center justify-between rounded-md border border-transparent bg-slate-50/70 px-2.5 py-1.5 text-left text-[11px] font-semibold text-slate-500 transition-all hover:border-blue-100 hover:bg-blue-50/80 hover:text-blue-700">
                <span>DR Workspace</span><span className="flex items-center gap-1">Enter <ChevronRight size={13} /></span>
              </button>
                  </>
                );
              })()}
              </motion.div>
            ))}
          </div>
        </div>
      ) : (

        <div className="rounded-2xl border border-dashed border-slate-200 bg-white p-14 text-center shadow-sm">
          <Server size={36} className="mx-auto mb-3 text-slate-300" />
          <h3 className="text-sm font-bold text-slate-800">No registered clusters yet</h3>
          <p className="mt-1 text-xs text-slate-400">Register your first Kubernetes cluster. The DR workspace becomes available after the Agent connects.</p>
          <button onClick={openRegister} className="mt-5 inline-flex items-center gap-1.5 rounded-xl bg-blue-600 px-5 py-2.5 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700"><Plus size={15} />Register Cluster</button>
        </div>
      )}

      <ClusterActivityPanel logs={clusterTaskLogs} clusters={clusters} highlightedTaskId={highlightedTaskId} onHighlightComplete={() => setHighlightedTaskId(null)} />
      </div>

      <AnimatePresence>
        {topologyOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => setTopologyOpen(false)} />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-topology-drawer" role="dialog" aria-modal="true" aria-label="DR Topology">
              <div className="hbdr-filter-drawer-head">
                <div><strong>DR Topology</strong><span>Namespace protection relationships between registered clusters</span></div>
                <button type="button" onClick={() => setTopologyOpen(false)} aria-label="Close DR topology"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-topology-drawer-body">
                {topologyLoading ? (
                  <div className="hbdr-section-card flex min-h-48 items-center justify-center text-xs font-semibold text-slate-400" role="status">Loading DR topology...</div>
                ) : topologyLoadError ? (
                  <div className="hbdr-section-card flex min-h-48 flex-col items-center justify-center gap-3 text-xs text-rose-700" role="alert"><span>{topologyLoadError}</span><button type="button" className="hbdr-cluster-topology-trigger" onClick={() => void openTopology()}><RefreshCw size={14} />Retry</button></div>
                ) : (
                  <DRTopologyView clusters={clusters} model={topology} selectedRelationshipId={selectedRelationshipId} selectedClusterId={selectedTopologyClusterId} onSelectRelationship={selectRelationship} onSelectCluster={(clusterId) => { setSelectedTopologyClusterId(clusterId); setSelectedRelationshipId(null); }} />
                )}
              </div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {clusterResourceDetail && (() => {
          const isNamespaces = clusterResourceDetail.type === 'namespaces';
          const isStorageClasses = clusterResourceDetail.type === 'storageClasses';
          const isNodes = clusterResourceDetail.type === 'nodes';
          const cluster = clusterResourceDetail.cluster;
          const namespaceRows = namespaceRowsForCluster(clusterResourceDetail.cluster);
          const nodeRows = nodeRowsForCluster(clusterResourceDetail.cluster);
          const storageClassRows = storageClassRowsForCluster(clusterResourceDetail.cluster);
          const hasRealNodeDetails = (clusterResourceDetail.cluster.nodeDetails || []).length > 0;
          const protectedCount = getProtectedAppCount(cluster);
          const readiness = agentReadiness(cluster);
          return (
            <>
              <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => setClusterResourceDetail(null)} />
              <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-cluster-detail-drawer">
                <div className="hbdr-filter-drawer-head hbdr-cluster-detail-drawer-head">
                  <div>
                    <strong>Cluster Details</strong>
                    <p>{cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : cluster.name}</p>
                    <div className="hbdr-cluster-resource-status-line">
                      <span className={`hbdr-cluster-connection-dot ${cluster.connectionStatus === 'online' ? 'is-online' : 'is-offline'}`} />
                      <strong>{cluster.connectionStatus === 'online' ? 'Online' : 'Offline'}</strong>
                      <span>Kubernetes {cluster.version}</span>
                      <span>Last seen {formatLastSeen(cluster.lastSeenAt)}</span>
                    </div>
                  </div>
                  <button type="button" onClick={() => setClusterResourceDetail(null)} aria-label="Close details"><X size={18} /></button>
                </div>
                <div className="hbdr-filter-drawer-body hbdr-cluster-detail-drawer-body">
                  <section className="hbdr-cluster-detail-section hbdr-cluster-detail-overview">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Overview</h4>
                    </div>
                    <div className="hbdr-cluster-overview-grid">
                      <div><span>Cluster Type</span><strong>{cluster.clusterType === 'huaweicloud-cce' ? 'Huawei Cloud CCE' : cluster.clusterType === 'openshift' ? 'OpenShift' : 'Native Kubernetes'}</strong></div>
                      <div><span>Cloud Region</span><strong>{cluster.cloudRegion || 'N/A'}</strong></div>
                      <div><span>Nodes</span><strong>{cluster.nodes}</strong></div>
                      <div><span>Namespaces</span><strong>{cluster.namespaces}</strong></div>
                      <div><span>Storage Classes</span><strong>{(cluster.storageClasses || []).length}</strong></div>
                      <div><span>Namespaces</span><strong>{cluster.applications}</strong></div>
                      <div><span>Protected</span><strong>{protectedCount}</strong></div>
                      <div><span>Restore Status</span><strong>{cluster.veleroStatus || 'Unknown'}</strong></div>
                    </div>
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isNodes ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Nodes</h4>
                        <p>{nodeRows.length || cluster.nodes} total</p>
                      </div>
                      <span>kubectl get nodes</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={nodeColumns}
                      data={nodeRows}
                      getRowId={row => row.name}
                      emptyMessage="Node details are waiting for the next agent inventory report."
                    />
                    {!hasRealNodeDetails && (
                      <p className="hbdr-cluster-resource-note">Node detail rows will show real Kubernetes node names after the next detailed inventory report from the agent.</p>
                    )}
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isNamespaces ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Namespaces</h4>
                        <p>{namespaceRows.length || cluster.namespaces} total</p>
                      </div>
                      <span>kubectl get namespaces</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={namespaceColumns}
                      data={namespaceRows}
                      getRowId={row => row.name}
                      emptyMessage="Namespace details are waiting for the next agent inventory report."
                    />
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isStorageClasses ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Storage Classes</h4>
                        <p>{storageClassRows.length} total</p>
                      </div>
                      <span>kubectl get storageclass</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={storageClassColumns}
                      data={storageClassRows}
                      getRowId={row => row.name}
                      emptyMessage="StorageClass details are waiting for the next agent inventory report."
                    />
                  </section>

                  <section className="hbdr-cluster-detail-section">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Agent</h4>
                    </div>
                    <div className="hbdr-cluster-key-values">
                      <div><span>Status</span><strong className={readiness.className}>{readiness.label}</strong></div>
                      <div><span>Version</span><strong>{cluster.agentVersion}</strong></div>
                      <div><span>Latest</span><strong>{cluster.latestAgentVersion}{cluster.latestAgentImageDigest ? `@${shortDigest(cluster.latestAgentImageDigest)}` : ''}</strong></div>
                      <div><span>Namespace</span><strong>hypercdr-agent</strong></div>
                      <div><span>Last Heartbeat</span><strong>{formatLastSeen(cluster.lastSeenAt)}</strong></div>
                      <div><span>Upgrade</span><strong>{cluster.agentUpgradeAvailable ? 'Available' : cluster.agentUpgradeStatus === 'upgrading' ? 'Upgrading' : 'Current'}</strong></div>
                    </div>
                  </section>

                  <section className="hbdr-cluster-detail-section">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Protection</h4>
                    </div>
                    <div className="hbdr-cluster-key-values">
                      <div><span>Protected Namespaces</span><strong>{protectedCount}</strong></div>
                      <div><span>Unprotected Namespaces</span><strong>{Math.max(0, cluster.apps.length - protectedCount)}</strong></div>
                      <div><span>Protection State</span><strong>{isClusterProtected(cluster) ? 'Protected' : 'Unprotected'}</strong></div>
                    </div>
                  </section>
                </div>
              </motion.div>
            </>
          );
        })()}
      </AnimatePresence>

      <AnimatePresence>
        {openshiftKubeconfigGuideOpen && (
          <div className="fixed inset-0 z-[245]">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setOpenshiftKubeconfigGuideOpen(false)} className="absolute inset-0 bg-slate-900/15" />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-labelledby="openshift-kubeconfig-guide-title">
              <div className="hbdr-filter-drawer-head"><div><strong id="openshift-kubeconfig-guide-title">Get an OpenShift kubeconfig</strong><span>Create a self-contained credential from an OpenShift administrator session.</span></div><button onClick={() => setOpenshiftKubeconfigGuideOpen(false)} aria-label="Close kubeconfig guide"><X size={18} /></button></div>
              <div className="hbdr-filter-drawer-body space-y-4 text-sm leading-6 text-slate-600">
                <ol className="space-y-4">
                  <li><p className="text-xs font-bold uppercase tracking-wide text-slate-500">1. Get the login command</p><p className="mt-1">Sign in to the OpenShift Web Console. Open the user menu in the top-right, select <strong>Copy login command</strong>, authenticate again if prompted, then select <strong>Display Token</strong> and copy the complete <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">oc login</code> command.</p></li>
                  <li><p className="text-xs font-bold uppercase tracking-wide text-slate-500">2. Log in from a Linux administration host</p><p className="mt-1">Use a host that has the <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">oc</code> CLI and can reach the OpenShift API. Run the copied command. If this lab cluster uses an untrusted certificate, add the option shown below.</p><pre className="mt-2 whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 font-mono text-xs leading-5 text-blue-200">oc login --token=&lt;token&gt; --server=https://api.&lt;cluster-domain&gt;:6443 --insecure-skip-tls-verify=true</pre></li>
                  <li><p className="text-xs font-bold uppercase tracking-wide text-slate-500">3. Verify the active administrator session</p><pre className="mt-2 whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 font-mono text-xs leading-5 text-blue-200">oc whoami{`\n`}oc get nodes{`\n`}oc auth can-i create clusterroles.rbac.authorization.k8s.io</pre><p className="mt-1 text-xs">The final command must return <strong>yes</strong>. Use a cluster-admin account; project-only credentials cannot install OADP and HyperCDR.</p></li>
                  <li><p className="text-xs font-bold uppercase tracking-wide text-slate-500">4. Export a self-contained kubeconfig</p><pre className="mt-2 whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 font-mono text-xs leading-5 text-blue-200">oc config view --raw --minify --flatten &gt; hypercdr-openshift-kubeconfig.yaml{`\n`}chmod 600 hypercdr-openshift-kubeconfig.yaml</pre><p className="mt-1 text-xs">The file is created in the current directory. For example, when run as <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">core</code> from its home directory, the path is <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">/home/core/hypercdr-openshift-kubeconfig.yaml</code>.</p></li>
                  <li><p className="text-xs font-bold uppercase tracking-wide text-slate-500">5. Verify and upload</p><pre className="mt-2 whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 font-mono text-xs leading-5 text-blue-200">oc --kubeconfig ./hypercdr-openshift-kubeconfig.yaml whoami{`\n`}oc --kubeconfig ./hypercdr-openshift-kubeconfig.yaml get nodes</pre><p className="mt-1">Copy the YAML file to your workstation if necessary, then upload it in the registration panel and select the displayed context.</p></li>
                </ol>
                <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">This file contains an administrator token. Store it securely and delete local copies after registration. A token-based kubeconfig stops working when its OpenShift token expires or is revoked.</p>
              </div>
              <div className="hbdr-filter-drawer-actions"><button onClick={() => setOpenshiftKubeconfigGuideOpen(false)}>Done</button></div>
            </motion.aside>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {licenseGuideOpen && (
          <div className="fixed inset-0 z-[240]">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setLicenseGuideOpen(false)} className="absolute inset-0 bg-slate-900/15" />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-labelledby="cluster-license-guide-title">
              <div className="hbdr-filter-drawer-head"><div><strong id="cluster-license-guide-title">License required</strong><span>Activate HyperCDR Enterprise before registering a cluster.</span></div><button aria-label="Close license guide" onClick={() => setLicenseGuideOpen(false)}><X size={18} /></button></div>
              <div className="hbdr-filter-drawer-body"><div className="rounded-xl border border-amber-100 bg-amber-50 p-4 text-sm leading-6 text-amber-900">Start the included trial or import a signed formal license. Query, recovery, unregister and cleanup operations remain available without an active license.</div></div>
              <div className="hbdr-filter-drawer-actions"><button onClick={() => { setLicenseGuideOpen(false); openLicenseManagement?.(); }}>Start Trial</button><button onClick={() => { setLicenseGuideOpen(false); openLicenseManagement?.(); }}>Import License</button><button onClick={() => setLicenseGuideOpen(false)}>Cancel</button></div>
            </motion.aside>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {registerOpen && (
          <div className="fixed inset-0 z-[230]">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeRegister} className="absolute inset-0 bg-slate-900/15" />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-cluster-register-drawer" role="dialog" aria-modal="true" aria-label="Register New Cluster">
              <div className="hbdr-filter-drawer-head">
                <div>
                    <strong className="flex items-center gap-2 text-xl tracking-tight"><PlusCircle className="text-blue-600" />Register New Cluster</strong>
                    <span>Follow the steps below to connect a Kubernetes cluster to HyperCDR.</span>
                </div>
                <button onClick={closeRegister} aria-label="Close registration"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-cluster-register-drawer-body">
                <div className="space-y-4">
                  <section>
                    <div className="mb-2"><strong className="text-xs uppercase tracking-wide text-slate-500">1. Cluster type</strong><p className="mt-1 text-[11px] text-slate-500">Select the Kubernetes environment you want to register.</p></div>
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-3" role="radiogroup" aria-label="Cluster type">
                    {([
                      ['native-kubernetes', 'Native Kubernetes', 'Self-managed Kubernetes cluster', Server],
                      ['huaweicloud-cce', 'Huawei Cloud CCE', 'Huawei-managed Kubernetes service', Cloud],
                      ['openshift', 'OpenShift', 'Red Hat OpenShift 4.14 or 4.15 with OADP', Cloud],
                    ] as const).map(([value, label, description, Icon]) => <button key={value} type="button" role="radio" aria-checked={registrationType === value} disabled={installLoading} onClick={() => { if (registrationType !== value) resetDirectRegistration(); void loadRegistrationCommand(value); }} className={`relative flex min-h-20 items-start gap-3 rounded-xl border p-3 text-left transition ${registrationType === value ? 'border-blue-300 bg-blue-50/70 ring-1 ring-blue-100' : 'border-slate-200 bg-white hover:border-blue-200 hover:bg-slate-50'}`}>
                      <span className={`mt-0.5 rounded-lg p-2 ${registrationType === value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-500'}`}><Icon size={16} /></span>
                      <span className="min-w-0"><strong className="block text-sm text-slate-800">{label}</strong><span className="mt-1 block text-[11px] leading-4 text-slate-500">{description}</span></span>
                      {registrationType === value && <CheckCircle2 size={15} className="absolute right-3 top-3 text-blue-600" />}
                    </button>)}
                    </div>
                  </section>
                  <section>
                  <div className="mb-2"><strong className="text-xs uppercase tracking-wide text-slate-500">2. Registration method</strong><p className="mt-1 text-[11px] text-slate-500">Choose who will run the Agent installation.</p></div>
                  <div className="grid grid-cols-2 gap-2 rounded-xl border border-slate-200 bg-slate-50 p-1.5" role="radiogroup" aria-label="Registration method">
                    {([
                      ['platform-direct', 'Platform direct install', 'Upload a temporary kubeconfig'],
                      ['command', 'Run installation command', registrationType === 'native-kubernetes' ? 'Run on the control-plane node' : 'Use a Linux administration host'],
                    ] as const).map(([value, label, description]) => <button key={value} type="button" role="radio" aria-checked={cceRegistrationMode === value} onClick={() => setCCERegistrationMode(value)} className={`rounded-lg border px-3 py-2.5 text-left transition ${cceRegistrationMode === value ? 'border-blue-200 bg-white shadow-sm ring-1 ring-blue-100' : 'border-transparent text-slate-500 hover:bg-white/70'}`}>
                      <span className={`block text-sm font-bold ${cceRegistrationMode === value ? 'text-blue-700' : 'text-slate-700'}`}>{label}</span>
                      <span className="mt-0.5 block text-[11px] leading-4">{description}</span>
                    </button>)}
                  </div>
                  </section>
                  {cceRegistrationMode === 'platform-direct' && <div className="space-y-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
                    {registrationType === 'native-kubernetes' && <RegistrationStep number={1} title="Export an administrator kubeconfig" description="Run this on the control-plane node. It exports only the current context into a self-contained file.">
                      <pre className="whitespace-pre-wrap break-all rounded-lg bg-slate-900 p-3 font-mono text-[11px] leading-5 text-blue-200">kubectl config view --raw --minify --flatten &gt; hypercdr-native-kubeconfig.yaml</pre>
                    </RegistrationStep>}
                    <RegistrationStep number={registrationType === 'native-kubernetes' ? 2 : 1} title={registrationType === 'huaweicloud-cce' ? 'Upload CCE kubeconfig' : registrationType === 'openshift' ? 'Upload OpenShift kubeconfig' : 'Upload exported kubeconfig'} description="The credential is encrypted in transit and used only for this registration attempt.">
                      <div className="mb-3 flex items-start gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-[11px] leading-4 text-emerald-800">
                        <CheckCircle2 size={14} className="mt-0.5 shrink-0" />
                        <span><strong className="block">Temporary file · automatically deleted</strong>The uploaded kubeconfig is permanently deleted when registration succeeds or fails, when you cancel, or when the temporary session expires. No manual cleanup is required.</span>
                      </div>
                      <label className={`flex min-h-24 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed px-4 text-center transition ${cceUploadLoading ? 'cursor-wait border-blue-200 bg-blue-50' : 'border-slate-300 bg-slate-50 hover:border-blue-300 hover:bg-blue-50/40'}`}>
                        <Upload size={20} className={cceUploadLoading ? 'animate-pulse text-blue-600' : 'text-slate-400'} />
                        <span className="mt-2 text-xs font-bold text-slate-700">{cceUploadLoading ? 'Validating kubeconfig…' : cceUpload ? 'Replace kubeconfig' : 'Choose YAML or JSON kubeconfig'}</span>
                        <span className="mt-1 text-[11px] text-slate-500">Maximum 1 MiB. External credential plugins are not executed.</span>
                        <input type="file" className="sr-only" accept=".yaml,.yml,.json,application/yaml,application/json" disabled={cceUploadLoading} onChange={event => void uploadCCEKubeconfig(event.target.files?.[0])} />
                      </label>
                      {registrationType === 'openshift' && <button type="button" onClick={() => setOpenshiftKubeconfigGuideOpen(true)} className="mt-2 inline-flex items-center gap-1 text-xs font-bold text-blue-700 underline decoration-blue-200 underline-offset-4 hover:text-blue-800">How do I get an OpenShift kubeconfig?</button>}
                      {cceUploadError && <p role="alert" className="mt-3 rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-xs font-medium leading-5 text-rose-700">{cceUploadError}</p>}
                    </RegistrationStep>
                    <RegistrationStep number={registrationType === 'native-kubernetes' ? 3 : 2} title="Select Kubernetes context" description="Confirm the exact cluster that HyperCDR may inspect. No cluster resources are changed at this stage.">
                      {cceUpload ? <>
                        <select value={cceContext} onChange={event => setCCEContext(event.target.value)} className="h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm font-semibold text-slate-800 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100">
                          <option value="" disabled>Select a context</option>
                          {cceUpload.contexts.map(context => <option key={context.name} value={context.name}>{context.name} · {context.apiServer}</option>)}
                        </select>
                        <div className="mt-3 grid grid-cols-2 gap-3 rounded-lg bg-slate-50 p-3 text-[11px] text-slate-500">
                          <div><span className="block font-bold uppercase tracking-wide text-slate-400">Credential fingerprint</span><span className="mt-1 block truncate font-mono text-slate-700" title={cceUpload.fingerprint}>{cceUpload.fingerprint}</span></div>
                          <div><span className="block font-bold uppercase tracking-wide text-slate-400">Session expires</span><span className="mt-1 block text-slate-700">{new Date(cceUpload.expiresAt).toLocaleTimeString()}</span></div>
                        </div>
                      </> : <div className="flex min-h-10 items-center rounded-lg border border-dashed border-slate-200 bg-slate-50 px-3 text-xs font-medium text-slate-400">Upload a kubeconfig to discover its available contexts.</div>}
                    </RegistrationStep>
                    <RegistrationStep number={registrationType === 'native-kubernetes' ? 4 : 3} title="Inspect and register" description="Inspect identity, version, permissions, capacity, and StorageClass. After confirmation, an isolated preflight verifies network and image pulls before installation.">
                      <button type="button" onClick={() => void inspectCCECluster()} disabled={!cceContext || cceUploadLoading || cceInspectionLoading} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-xs font-bold text-white shadow-sm transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-slate-300">{cceInspectionLoading && <RefreshCw size={13} className="animate-spin" />}{cceInspectionLoading ? 'Inspecting cluster…' : 'Inspect cluster'}</button>
                      {cceInspection && <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 rounded-xl border border-emerald-100 bg-emerald-50/60 p-3 text-xs">
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Cluster</span><strong className="mt-0.5 block text-slate-800">{cceInspection.clusterName}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Kubernetes</span><strong className="mt-0.5 block text-slate-800">{cceInspection.serverVersion}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Worker nodes</span><strong className="mt-0.5 block text-slate-800">{cceInspection.nodeCount}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">StorageClass</span><strong className="mt-0.5 block text-slate-800">{cceInspection.defaultStorageClass || 'Selection required'}</strong></div>
                      </div>}
                      {cceInspection?.gates?.length > 0 && <div className="mt-3 divide-y divide-slate-100 rounded-xl border border-slate-200 bg-white px-3">
                        {cceInspection.gates.map(gate => <div key={gate.id} className="flex items-start gap-2.5 py-2.5">
                          <span className={`mt-1 h-2 w-2 shrink-0 rounded-full ${gate.status === 'passed' ? 'bg-emerald-500' : gate.status === 'warning' ? 'bg-amber-500' : 'bg-blue-400'}`} />
                          <div className="min-w-0"><strong className="block text-xs text-slate-800">{gate.label}</strong><span className="mt-0.5 block text-[11px] leading-4 text-slate-500">{gate.detail}</span></div>
                        </div>)}
                      </div>}
                      {cceInspection && !cceRegistrationTask && <div className="mt-3 flex items-end gap-3">
                        <label className="min-w-0 flex-1 text-[10px] font-bold uppercase tracking-wide text-slate-500">StorageClass
                          <select value={cceStorageClass} onChange={event => setCCEStorageClass(event.target.value)} className="mt-1 h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs font-semibold normal-case tracking-normal text-slate-800 outline-none focus:border-blue-500">
                            <option value="" disabled>Select a StorageClass</option>
                            {cceInspection.storageClasses.map(name => <option key={name} value={name}>{name}{name === cceInspection.defaultStorageClass ? ' (default)' : ''}</option>)}
                          </select>
                        </label>
                        <button type="button" onClick={() => void startCCEDirectRegistration()} disabled={!cceStorageClass || cceInspectionLoading} className="h-9 rounded-lg bg-emerald-600 px-4 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 disabled:cursor-not-allowed disabled:bg-slate-300">Register cluster</button>
                      </div>}
                      {cceRegistrationTask && <div className={`mt-3 rounded-xl border px-3 py-3 text-xs ${cceRegistrationTask.status === 'failed' ? 'border-rose-100 bg-rose-50 text-rose-700' : cceRegistrationTask.status === 'succeeded' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : cceRegistrationTask.status === 'canceled' ? 'border-slate-200 bg-slate-50 text-slate-600' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>
                        <div className="flex items-center justify-between gap-3"><strong>{cceRegistrationTask.status === 'succeeded' ? 'Registration completed' : cceRegistrationTask.status === 'failed' ? 'Registration failed' : cceRegistrationTask.status === 'canceled' ? 'Registration canceled' : 'Registration in progress'}</strong><span className="tabular-nums">{cceRegistrationTask.status === 'succeeded' ? '100%' : cceRegistrationTask.status === 'failed' ? 'Stopped' : cceRegistrationTask.status === 'canceled' ? 'Canceled' : `${cceRegistrationTask.progress || 0}%`}</span></div>
                        <p className="mt-1 leading-5">{cceRegistrationTask.status === 'failed' ? cceRegistrationTask.errorMessage || 'The executor reported a registration failure.' : cceRegistrationTask.status === 'succeeded' ? 'Agent registration was confirmed and the temporary kubeconfig was destroyed.' : cceRegistrationTask.status === 'canceled' ? 'Installation stopped, rollback completed, and the temporary kubeconfig was destroyed.' : 'Preflight, installation, and agent readiness are being verified. You may keep this drawer open.'}</p>
                        {['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(cceRegistrationTask.status) && <button type="button" onClick={() => void cancelCCEDirectRegistration()} disabled={cceInspectionLoading || cceRegistrationTask.status === 'canceling'} className="mt-2 rounded-lg border border-current px-3 py-1.5 text-[11px] font-bold transition hover:bg-white/60 disabled:cursor-wait disabled:opacity-60">{cceRegistrationTask.status === 'canceling' ? 'Canceling and rolling back…' : 'Cancel registration'}</button>}
                      </div>}
                    </RegistrationStep>
                  </div>}
                  {cceRegistrationMode === 'command' && <>
                  <div className="space-y-5 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
                  {(() => {
                    let step = 0;
                    const nextStep = () => ++step;
                    return <>
                  {registrationType === 'huaweicloud-cce' && <>
                    <RegistrationStep number={nextStep()} title="Install kubectl" description="Install kubectl on the Linux host that will run the registration command." />
                    <RegistrationStep number={nextStep()} title="Download the CCE kubeconfig" description="Download the cluster credential from Huawei Cloud CCE and save it as ~/.kube/hypercdr-cce.yaml. The installer will detect it automatically." />
                  </>}
                  {prepareNodeCommand && <RegistrationStep number={nextStep()} title="Install the registry CA" description="Run this command on every Kubernetes node to trust the private image registry.">
                  <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - prepare node</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <pre className="hbdr-cluster-register-command min-w-0 flex-1 cursor-text whitespace-pre-wrap break-all font-mono text-[11px] leading-5 text-blue-300" aria-label="Registry CA command" onClick={event=>selectCommandText(event.currentTarget)}>{prepareNodeCommand}</pre>
                        <textarea ref={registryCACommandRef} readOnly value={prepareNodeCommand} className="sr-only" tabIndex={-1} aria-hidden="true" />
                      </div>
                    </div>
                    <button onClick={copyRegistryCACommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95">
                      {caCopied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {caCopied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  </RegistrationStep>}

                  <RegistrationStep
                    number={nextStep()}
                    title="Install HyperCDR agent"
                    description={registrationType === 'huaweicloud-cce' ? 'Run this command on the host with access to the CCE cluster. The installer will verify the kubeconfig before making changes.' : registrationType === 'openshift' ? 'Run this command on a Linux administration host with cluster-admin access to OpenShift. Do not modify RHCOS nodes.' : 'Log in to the Kubernetes control-plane node and run this command.'}
                  >
                  {!installError && <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - install agent</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <pre className="hbdr-cluster-register-command min-w-0 flex-1 cursor-text whitespace-pre-wrap break-all font-mono text-[11px] leading-5 text-blue-300" aria-label="Install command" onClick={event=>selectCommandText(event.currentTarget)}>{installLoading ? 'Generating install command...' : installCommand}</pre>
                        <textarea ref={installCommandRef} readOnly value={installCommand} className="sr-only" tabIndex={-1} aria-hidden="true" />
                      </div>
                    </div>
                    <button disabled={installLoading || !installCommand} onClick={copyInstallCommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95 disabled:cursor-wait disabled:opacity-60">
                      {copied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>}
                  {installError && <p className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-medium text-rose-700">{installError}</p>}
                  </RegistrationStep>

                  <RegistrationStep number={nextStep()} title="Wait for connection" description={registrationWaiting ? 'Detecting the cluster connection. It will appear in the cluster list as soon as the agent registers.' : 'Connection monitoring starts after you copy the install command.'}>
                    {registrationWaiting && <div className="flex items-center gap-2 text-xs font-semibold text-emerald-700"><RefreshCw size={14} className="animate-spin" />Waiting for cluster connection...</div>}
                  </RegistrationStep>
                  </>;
                  })()}
                  </div>
                  </>}

                  <div className="flex justify-end gap-3 pt-1">
					{cceRegistrationMode === 'command' ? <>
					  <button onClick={closeRegister} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
					  <button onClick={finishRegisterCluster} className="rounded-xl bg-emerald-600 px-6 py-2 font-bold text-white shadow-lg shadow-emerald-200 transition-all hover:bg-emerald-700 active:scale-95">Continue in Background</button>
					</> : cceRegistrationTask && ['succeeded', 'failed', 'canceled'].includes(cceRegistrationTask.status) ?
					  <button onClick={closeRegister} className="rounded-xl bg-emerald-600 px-6 py-2 font-bold text-white shadow-lg shadow-emerald-200 transition-all hover:bg-emerald-700 active:scale-95">Done</button> :
					  <button onClick={closeRegister} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>}
                  </div>
                </div>
              </div>
            </motion.aside>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {upgradeTarget && (
          <div className="fixed inset-0 z-[230] flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeUpgrade} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="p-8">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-2xl font-bold tracking-tight text-slate-900"><Upload className="text-blue-600" />Upgrade Agent</h2>
                    <p className="mt-1 text-sm text-slate-500">The agent deployment will roll out and reconnect automatically after the new pod starts.</p>
                  </div>
                  <button onClick={closeUpgrade} className="rounded-full p-2 transition-colors hover:bg-slate-100"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="mb-5 rounded-xl border border-blue-100 bg-blue-50 p-4 text-sm text-blue-700">
                  <p className="font-bold text-blue-900">{upgradeTarget.name}</p>
                  <p className="mt-1">Current Version {upgradeTarget.agentVersion}{upgradeTarget.agentImageDigest ? `@${shortDigest(upgradeTarget.agentImageDigest)}` : ''}</p>
                  <p className="mt-1">Target Version {upgradeTarget.latestAgentVersion}{upgradeTarget.latestAgentImageDigest ? `@${shortDigest(upgradeTarget.latestAgentImageDigest)}` : ''}</p>
                </div>

                <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
                  Confirm the upgrade only when no backup, restore, or cleanup task is running on this cluster. The agent connection may briefly show offline during rollout.
                </div>

                <div className="mt-8 flex justify-end gap-3">
                  <button onClick={closeUpgrade} disabled={upgradeSubmitting} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60">Cancel</button>
                  <button onClick={finishUpgradeCluster} disabled={upgradeSubmitting} className="rounded-xl bg-blue-600 px-6 py-2 font-bold text-white shadow-lg shadow-blue-200 transition-all hover:bg-blue-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-60">{upgradeSubmitting ? 'Upgrading...' : 'Upgrade'}</button>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {veleroUpgradeTarget && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeVeleroUpgrade} className="hbdr-filter-drawer-backdrop" />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-velero-upgrade-drawer" role="dialog" aria-modal="true" aria-label="Upgrade Velero">
              <div className="hbdr-filter-drawer-head">
                <div><strong>Upgrade Velero</strong><span>Upgrade the Velero server and node agents on every scheduled node.</span></div>
                <button type="button" onClick={closeVeleroUpgrade} disabled={veleroUpgradeSubmitting} aria-label="Close Velero upgrade drawer"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-velero-upgrade-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Version</h4>
                  <div className="hbdr-advanced-filter-box hbdr-velero-version-box">
                    <div><span>Current</span><strong>{veleroUpgradeTarget.veleroVersion || 'Unknown'}</strong><small>{shortDigest(veleroUpgradeTarget.veleroImageDigest)}</small></div>
                    <ChevronRight size={18} />
                    <div><span>Target</span><strong>{veleroUpgradeTarget.latestVeleroVersion || 'Latest'}</strong><small>{shortDigest(veleroUpgradeTarget.latestVeleroImageDigest)}</small></div>
                  </div>
                </section>
                <section className="hbdr-advanced-filter-section">
                  <h4>Upgrade Scope</h4>
                  <div className="hbdr-advanced-filter-box hbdr-velero-scope-box">
                    <div><CheckCircle2 size={16} /><span>Velero Server Deployment</span></div>
                    <div><CheckCircle2 size={16} /><span>Node Agent DaemonSet · {veleroUpgradeTarget.veleroNodeAgentReady || 0}/{veleroUpgradeTarget.veleroNodeAgentDesired || 0} ready</span></div>
                  </div>
                </section>
                <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-xs leading-5 text-amber-800">The platform creates an upgrade task only after you confirm. Active backup, drill, restore, cleanup, or upgrade tasks will block this operation.</div>
              </div>
              <div className="hbdr-filter-drawer-actions hbdr-velero-upgrade-actions">
                <button type="button" onClick={closeVeleroUpgrade} disabled={veleroUpgradeSubmitting}>Cancel</button>
                <button type="button" onClick={finishVeleroUpgrade} disabled={veleroUpgradeSubmitting}>{veleroUpgradeSubmitting ? 'Creating Task...' : 'Upgrade Velero'}</button>
              </div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {unregisterTarget && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeUnregister} className="hbdr-filter-drawer-backdrop" />
            <motion.aside
              initial={{ opacity: 0, x: 34 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 34 }}
              transition={{ duration: 0.18, ease: 'easeOut' }}
              className="hbdr-filter-drawer hbdr-unregister-drawer"
              role="dialog"
              aria-modal="true"
              aria-label="Unregister Cluster"
            >
              <div className="hbdr-filter-drawer-head">
                <div className="hbdr-unregister-drawer-title">
                  <div className="hbdr-unregister-drawer-title-icon" aria-hidden="true"><Trash2 size={17} /></div>
                  <div className="hbdr-unregister-drawer-title-copy">
                    <strong>Unregister Cluster</strong>
                    <span>Review the impact before removing this cluster from HyperCDR.</span>
                  </div>
                </div>
                <button type="button" onClick={closeUnregister} disabled={unregistering} aria-label="Close unregister drawer"><X size={18} /></button>
              </div>

              <div className="hbdr-filter-drawer-body hbdr-unregister-drawer-body">
                <section className="hbdr-unregister-warning rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700">
                  <p className="font-bold text-rose-900">Unregister impact</p>
                  <p className="mt-2 leading-5 text-rose-800">HyperCDR and managed Velero resources will be removed from the cluster. Application workloads are not removed.</p>
                </section>

                <section className="hbdr-unregister-cluster rounded-xl border border-slate-100 bg-slate-50 p-4 text-sm">
                  <span>Selected Cluster</span>
                  <p className="font-bold text-slate-900">{unregisterTarget.name === 'unknown-cluster' ? 'Unnamed cluster' : unregisterTarget.name}</p>
                  <p className="mt-1 break-all font-mono text-[11px] text-slate-500">{unregisterTarget.id}</p>
                </section>

                <section className="rounded-xl border border-slate-200 bg-white p-4 text-sm">
                  <div className="flex items-center justify-between"><strong className="text-slate-900">Readiness check</strong><span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${unregisterPrecheck?.allowed ? 'bg-emerald-50 text-emerald-700' : 'bg-amber-50 text-amber-700'}`}>{unregisterPrecheckLoading ? 'Checking' : unregisterPrecheck?.allowed ? 'Ready' : 'Blocked'}</span></div>
                  {unregisterPrecheck && <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-500"><span>Source configurations <strong className="text-slate-800">{unregisterPrecheck.sourcePlanCount}</strong></span><span>Target references <strong className="text-slate-800">{unregisterPrecheck.targetPlanCount}</strong></span><span>Restore points <strong className="text-slate-800">{unregisterPrecheck.restorePointCount}</strong></span><span>Active tasks <strong className="text-slate-800">{unregisterPrecheck.activeTaskCount}</strong></span></div>}
                  {unregisterPrecheck?.blockers.map(blocker => <p key={blocker} className="mt-2 rounded bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">{blocker}</p>)}
                  {(unregisterPrecheck?.restorePointCount || unregisterPrecheck?.sourcePlanCount || unregisterPrecheck?.targetPlanCount) ? <label className="mt-3 flex items-start gap-2 border-t border-slate-100 pt-3 text-xs text-slate-600"><input type="checkbox" checked={deleteBackupData} onChange={event=>setDeleteBackupData(event.target.checked)} disabled={unregistering||forceRemoveEnabled} className="mt-0.5"/><span><strong className="block text-slate-800">Clean up related data</strong>{unregisterPrecheck.sourcePlanCount ? 'This cluster is a DR source. Its DR configurations, restore points, and backup data will be removed. ' : ''}{unregisterPrecheck.targetPlanCount ? 'This cluster is a DR target. Other source configurations and backup data will be preserved; only their default target will be cleared. ' : ''}Business namespaces, workloads, and PVCs will not be deleted.</span></label> : unregisterPrecheck?.objectStorageNeeded ? <p className="mt-3 border-t border-slate-100 pt-3 text-xs leading-5 text-blue-700"><strong className="block text-blue-800">Historical storage data detected</strong>The cluster storage prefix will be checked and cleaned automatically before unregister.</p> : <p className="mt-3 border-t border-slate-100 pt-3 text-xs text-emerald-700">This cluster has never used object storage. Object storage will not be accessed.</p>}
                </section>

                <section className={`hbdr-unregister-force rounded-xl border p-4 transition-colors ${forceRemoveEnabled ? 'is-active border-rose-300 bg-rose-50/70' : 'border-slate-200 bg-white'}`}>
                  <label className="flex cursor-pointer items-start gap-3">
                    <input
                      type="checkbox"
                      checked={forceRemoveEnabled}
                      onChange={event => {
                        setForceRemoveEnabled(event.target.checked);
                        setForceRemoveConfirmation('');
                      }}
                      disabled={unregistering}
                      className="mt-0.5 h-4 w-4 rounded border-slate-300 text-rose-600 focus:ring-rose-500"
                    />
                    <span>
                      <span className="block text-sm font-bold text-slate-900">Force remove</span>
                      <span className="mt-1 block text-xs leading-5 text-slate-500">Use only when the agent or Kubernetes cluster is permanently unavailable and normal unregister cannot complete.</span>
                    </span>
                  </label>

                  {forceRemoveEnabled && (
                    <div className="mt-4 border-t border-rose-200 pt-4">
                      <p className="text-xs leading-5 text-rose-800">This bypasses the agent and removes platform records only. Kubernetes resources and backup objects are not deleted and may require manual cleanup.</p>
                      <label className="mt-3 block text-xs font-semibold text-slate-700">
                        Type <span className="font-mono text-rose-700">{unregisterTarget.name === 'unknown-cluster' ? unregisterTarget.id : unregisterTarget.name}</span> to confirm
                        <input
                          value={forceRemoveConfirmation}
                          onChange={event => setForceRemoveConfirmation(event.target.value)}
                          disabled={unregistering}
                          autoComplete="off"
                          className="mt-2 h-10 w-full rounded-lg border border-rose-200 bg-white px-3 font-mono text-sm text-slate-900 outline-none transition focus:border-rose-400 focus:ring-2 focus:ring-rose-100"
                        />
                      </label>
                    </div>
                  )}
                </section>
                {unregisterSubmitError && (
                  <section role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs leading-5 text-rose-800">
                    <strong className="block text-sm text-rose-900">Unregister could not start</strong>
                    <span className="mt-1 block break-words">{unregisterSubmitError}</span>
                    <span className="mt-2 block text-rose-700">Review the readiness blockers above, confirm the agent is online, then retry. Use the request ID when checking platform logs.</span>
                  </section>
                )}
              </div>

              <div className="hbdr-filter-drawer-actions hbdr-unregister-drawer-actions">
                  <button onClick={closeUnregister} disabled={unregistering} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-wait disabled:opacity-60">Cancel</button>
                  <button
                    disabled={unregistering || unregisterPrecheckLoading || (!forceRemoveEnabled && ((!unregisterPrecheck?.agentOnline || Boolean(unregisterPrecheck?.activeTaskCount) || Boolean(unregisterPrecheck?.unregisterActive)) || ((!unregisterPrecheck?.allowed || Boolean(unregisterPrecheck?.restorePointCount)) && !deleteBackupData))) || (forceRemoveEnabled && (Boolean(unregisterPrecheck?.targetPlanCount) || forceRemoveConfirmation !== (unregisterTarget.name === 'unknown-cluster' ? unregisterTarget.id : unregisterTarget.name)))}
                    onClick={finishUnregisterCluster}
                    className="rounded-xl bg-rose-600 px-6 py-2 font-bold text-white shadow-lg shadow-rose-200 transition-all hover:bg-rose-700 active:scale-95 disabled:cursor-not-allowed disabled:bg-rose-300 disabled:shadow-none"
                  >
                    {unregistering ? (forceRemoveEnabled ? 'Removing...' : 'Creating Task...') : (forceRemoveEnabled ? 'Force Remove' : 'Confirm Unregister')}
                  </button>
              </div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>

    </motion.div>
  );
}
