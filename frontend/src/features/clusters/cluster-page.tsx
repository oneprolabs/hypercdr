import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AlertTriangle, Check, CheckCircle2, ChevronRight, Edit2, Eye, GitBranch, MoreVertical, Plus, PlusCircle, RefreshCw, Server, ShieldCheck, Star, Terminal, Trash2, Upload, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiGet, apiPatch, apiPost } from '../../api/client';
import { SearchBar } from '../../components/search-bar';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import ClusterActivityPanel from './cluster-activity-panel';
import { Metric, getProtectedAppCount, isClusterProtected } from './cluster-presentation';
import type { Cluster, ClusterNamespaceRow, ClusterNodeRow, ClusterStorageClassRow } from './types';
import type { ApiProtectionPlan } from '../recovery/types';
import { buildDRTopology, type DRRelationship } from './dr-topology';
import DRTopologyView from './dr-topology-view';

type ApiList<T>={items:T[]};
type ApiAgentToken={installCommand:string;prepareNodeCommand?:string};
type ApiCluster={id:string;name:string};
type ApiTask={id:string;clusterId:string;type:string;status:string;progress:number;errorCode?:string;errorMessage?:string;payload?:Record<string,any>;createdAt?:string;completedAt?:string};
type ApiTaskEvent={id:string;taskId:string;level:string;reason:string;message:string;payload?:Record<string,any>;createdAt?:string};
type ApiUnregisterPrecheck={activeTaskCount:number;restorePointCount:number;sourcePlanCount:number;targetPlanCount:number;objectStorageNeeded:boolean;allowed:boolean;blockers:string[]};
type ClusterTaskLog={task:ApiTask;events:ApiTaskEvent[];loading:boolean};
const listItems=<T,>(response:ApiList<T>)=>Array.isArray(response.items)?response.items:[];
const shortDigest=(digest?:string)=>{const cleaned=(digest||'').replace(/^sha256:/,'');return cleaned.length>12?cleaned.slice(0,12):cleaned};
const formatAge=(seconds?:number)=>!seconds&&seconds!==0?'-':seconds<60?`${seconds}s`:seconds<3600?`${Math.floor(seconds/60)}m`:seconds<86400?`${Math.floor(seconds/3600)}h`:`${Math.floor(seconds/86400)}d`;
const formatLastSeen=(value?:string)=>{if(!value)return'unknown';const timestamp=new Date(value).getTime();if(!Number.isFinite(timestamp))return'unknown';const seconds=Math.max(0,Math.floor((Date.now()-timestamp)/1000));return seconds<60?`${seconds}s ago`:seconds<3600?`${Math.floor(seconds/60)}m ago`:seconds<86400?`${Math.floor(seconds/3600)}h ago`:`${Math.floor(seconds/86400)}d ago`};
const normalizeNodeStatus=(status?:string)=>{const value=(status||'').trim();return value||'Unknown'};
const formatPercent=(value:number)=>Number.isFinite(value)?Math.max(0,Math.min(100,value)).toFixed(2):'0.00';
const taskStatusLabel=(status?:string)=>status==='succeeded'?'Succeeded':status==='failed'?'Failed':['running','accepted','dispatched','queued'].includes(status||'')?'Running':status||'Unknown';
const agentReadiness=(cluster:Cluster)=>cluster.connectionStatus!=='online'?{label:'Offline',className:'text-slate-500'}:cluster.status==='healthy'?{label:'Ready',className:'text-emerald-600'}:cluster.status==='syncing'?{label:'Syncing',className:'text-blue-600'}:{label:'Degraded',className:'text-amber-600'};
const copyTextToClipboard=async(text:string,textarea?:HTMLTextAreaElement|null)=>{try{await navigator.clipboard.writeText(text);return true}catch{if(!textarea)return false;textarea.focus();textarea.select();return document.execCommand('copy')}};

export default function ClusterPage(props: {
  clusters: Cluster[];
  loading: boolean;
  protectionPlans: ApiProtectionPlan[];
  canUpgrade: boolean;
  defaultClusterId: string | null;
  clusterMenuId: string | null;
  setClusterMenuId: (id: string | null) => void;
  setSelectedCluster: (cluster: Cluster) => void;
  setDefaultCluster: (cluster: Cluster, event?: React.MouseEvent) => void;
  clearDefaultCluster: (event?: React.MouseEvent) => void;
  unregisterCluster: (cluster: Cluster, event?: React.MouseEvent, deleteBackupData?: boolean) => Promise<ApiTask | null>;
  onRenameCluster: (clusterId: string, name: string) => void;
  onUpgradeCluster: (clusterId: string) => Promise<ApiTask>;
  onUpgradeVelero: (clusterId: string) => Promise<ApiTask>;
  onRegisterCluster: (cluster: Cluster) => void;
  onRefreshRegistration: () => Promise<Cluster[]>;
  clusterTaskLogs: Record<string, ClusterTaskLog[]>;
  getAgentTokenForRegistration: () => Promise<ApiAgentToken>;
  prefetchAgentToken: () => Promise<ApiAgentToken | null> | null;
  openDashboard: () => void;
  toast: (msg: string) => void;
}) {
  const { clusters, loading, protectionPlans, canUpgrade, defaultClusterId, clusterMenuId, setClusterMenuId, setSelectedCluster, setDefaultCluster, clearDefaultCluster, unregisterCluster, onRenameCluster, onUpgradeCluster, onUpgradeVelero, onRegisterCluster, onRefreshRegistration, clusterTaskLogs, getAgentTokenForRegistration, prefetchAgentToken, openDashboard, toast } = props;
  const [registerOpen, setRegisterOpen] = useState(false);
  const [registerStep, setRegisterStep] = useState<1 | 2 | 3>(1);
  const [copied, setCopied] = useState(false);
  const [caCopied, setCaCopied] = useState(false);
  const [prepareNodeCommand, setPrepareNodeCommand] = useState('');
  const [installCommand, setInstallCommand] = useState(`curl -sSL ${window.location.origin}/install.sh | bash -s -- --token pending --endpoint ${window.location.origin.replace(/^http/, 'ws')}/ws/agent --executor-mode kubernetes`);
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
  const [upgradeSubmitting, setUpgradeSubmitting] = useState(false);
  const [veleroUpgradeSubmitting, setVeleroUpgradeSubmitting] = useState(false);
  const [highlightedTaskId, setHighlightedTaskId] = useState<string | null>(null);
  const [unregisterTaskId, setUnregisterTaskId] = useState<string | null>(null);
  const [unregisterTask, setUnregisterTask] = useState<ApiTask | null>(null);
  const [unregisterEvents, setUnregisterEvents] = useState<ApiTaskEvent[]>([]);
  const [clusterResourceDetail, setClusterResourceDetail] = useState<{ cluster: Cluster; type: 'overview' | 'namespaces' | 'nodes' | 'storageClasses' } | null>(null);
  const [actionCopied, setActionCopied] = useState(false);
  const [topologyOpen, setTopologyOpen] = useState(false);
  const [selectedRelationshipId, setSelectedRelationshipId] = useState<string | null>(null);
  const [selectedTopologyClusterId, setSelectedTopologyClusterId] = useState<string | null>(null);
  const topology = useMemo(() => buildDRTopology(clusters, protectionPlans), [clusters, protectionPlans]);
  const selectRelationship = (relationship: DRRelationship) => { setSelectedRelationshipId(relationship.id); setSelectedTopologyClusterId(null); };
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

  const openRegister = async () => {
    setRegisterStep(1);
    setCopied(false);
    setInstallError(null);
    setRegistrationBaseline(clusters.map(cluster => cluster.id));
    setRegistrationWaiting(false);
    setInstallLoading(true);
    setPrepareNodeCommand('');
    try {
      const token = await getAgentTokenForRegistration();
      setPrepareNodeCommand(token.prepareNodeCommand || '');
      setInstallCommand(token.installCommand);
      setRegisterStep(3);
      setRegisterOpen(true);
      void prefetchAgentToken();
    } catch {
      setInstallError('Install token generation failed. Check whether the platform API is running.');
      setRegisterOpen(true);
      toast('Failed to generate install token');
    } finally {
      setInstallLoading(false);
    }
  };

  const closeRegister = () => {
    setRegisterOpen(false);
    setRegisterStep(1);
    setCopied(false);
    setCaCopied(false);
    setRegistrationWaiting(false);
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
    setUnregisterTaskId(null);
    setUnregisterTask(null);
    setUnregisterEvents([]);
    setActionCopied(false);
  };

  const copyInstallCommand = async () => {
    if (await copyTextToClipboard(installCommand, installCommandRef.current)) {
      setCopied(true);
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
    if (!registerOpen || registrationWaiting) return;
    const latest = clusters.find(cluster => !registrationBaseline.includes(cluster.id));
    if (!latest) return;
    if (latest) {
      setSelectedCluster(latest);
      toast(`${latest.name === 'unknown-cluster' ? 'Cluster' : latest.name} registered and connected`);
    }
    closeRegister();
  }, [clusters, registerOpen, registrationBaseline, registrationWaiting, setSelectedCluster, toast]);

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
          toast(task.errorMessage || 'Cluster unregister failed');
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
    const task = await unregisterCluster(unregisterTarget, undefined, deleteBackupData);
    if (task) {
      setUnregisterTaskId(task.id);
      setUnregisterTask(task);
      setUnregisterEvents([]);
      setUnregisterTarget(null);
      toast('Unregister task created. Track progress in Recent Tasks.');
    }
    setUnregistering(false);
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

  return (
    <motion.div key="clusters" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-clusters-page">
      <div className="hbdr-clusters-workspace">
      <SearchBar title="Clusters" desc="Register clusters and maintain the default cluster." />

      {loading ? (
        <div className="hbdr-section-card flex min-h-48 items-center justify-center text-xs font-semibold text-slate-400" role="status">Loading clusters...</div>
      ) : clusters.length > 0 ? (
        <div className="hbdr-section-card hbdr-clusters-card-region">
          <div className="hbdr-section-toolbar">
            <div>
              <h3>Registered Clusters</h3>
            </div>
            <div className="hbdr-cluster-toolbar-actions">
              <button type="button" onClick={() => { setSelectedRelationshipId(null); setSelectedTopologyClusterId(null); setTopologyOpen(true); }} className="hbdr-cluster-topology-trigger"><GitBranch size={14} />DR Topology</button>
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
          <p className="mt-1 text-xs text-slate-400">Register first Kubernetes cluster, Agent After reconnection, enter the Container DR console.</p>
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
                <DRTopologyView clusters={clusters} model={topology} selectedRelationshipId={selectedRelationshipId} selectedClusterId={selectedTopologyClusterId} onSelectRelationship={selectRelationship} onSelectCluster={(clusterId) => { setSelectedTopologyClusterId(clusterId); setSelectedRelationshipId(null); }} />
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
        {registerOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeRegister} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative max-h-[82vh] w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="max-h-[82vh] overflow-y-auto p-4">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-xl font-bold tracking-tight text-slate-900"><PlusCircle className="text-blue-600" />Register New Cluster</h2>
                    <p className="mt-1 text-xs text-slate-500">{prepareNodeCommand ? 'Install private registry trust, then install the agent stack.' : 'Install the agent stack with cluster-admin access.'}</p>
                  </div>
                  <button onClick={closeRegister} className="rounded-full p-2 transition-colors hover:bg-slate-100"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="space-y-4">
                  {prepareNodeCommand && <><div className="flex gap-3 rounded-xl border border-blue-100 bg-blue-50 p-3">
                    <div className="mt-1"><ShieldCheck size={20} className="text-blue-600" /></div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-blue-900">1. Install Harbor CA on every node</p>
                      <p className="leading-relaxed text-blue-700">Run on every Kubernetes node to trust the internal registry.</p>
                    </div>
                  </div>

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
                        <textarea
                          ref={registryCACommandRef}
                          readOnly
                          value={prepareNodeCommand}
                          className="h-[34px] flex-1 resize-none overflow-auto border-0 bg-transparent p-0 font-mono text-[11px] leading-5 text-blue-300 outline-none"
                          aria-label="Registry CA command"
                          onFocus={event => event.currentTarget.select()}
                        />
                      </div>
                    </div>
                    <button onClick={copyRegistryCACommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95">
                      {caCopied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {caCopied ? 'Copied' : 'Copy'}
                    </button>
                  </div></>}

                  <div className="flex gap-3 rounded-xl border border-blue-100 bg-blue-50 p-3">
                    <div className="mt-1"><Terminal size={18} className="text-blue-600" /></div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-blue-900">{prepareNodeCommand ? '2' : '1'}. Install agent stack</p>
                      <p className="leading-relaxed text-blue-700">Run once with cluster-admin kubectl access to install Velero and comm-agent.</p>
                    </div>
                  </div>

                  <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - install agent</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <textarea
                          ref={installCommandRef}
                          readOnly
                          value={installLoading ? 'Generating install command...' : installCommand}
                          className="h-[48px] flex-1 resize-none overflow-auto border-0 bg-transparent p-0 font-mono text-[11px] leading-5 text-blue-300 outline-none"
                          aria-label="Install command"
                          onFocus={event => event.currentTarget.select()}
                        />
                      </div>
                    </div>
                    <button disabled={installLoading} onClick={copyInstallCommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95 disabled:cursor-wait disabled:opacity-60">
                      {copied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  {installError && <p className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-medium text-rose-700">{installError}</p>}

                  <div className="flex gap-3 rounded-xl border border-emerald-100 bg-emerald-50 p-3">
                    <div className="mt-1">
                      {registrationWaiting ? <RefreshCw size={20} className="animate-spin text-emerald-600" /> : <CheckCircle2 size={20} className="text-emerald-600" />}
                    </div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-emerald-900">3. Wait for connection</p>
                      <p className="leading-relaxed text-emerald-700">Connection detection starts automatically. The cluster appears as soon as the agent registers.</p>
                    </div>
                  </div>

                  <div className="flex justify-end gap-3 pt-1">
                    <button onClick={closeRegister} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                    <button onClick={finishRegisterCluster} className="rounded-xl bg-emerald-600 px-6 py-2 font-bold text-white shadow-lg shadow-emerald-200 transition-all hover:bg-emerald-700 active:scale-95">Continue in Background</button>
                  </div>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {upgradeTarget && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
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
                  {unregisterPrecheck?.restorePointCount ? <label className="mt-3 flex items-start gap-2 border-t border-slate-100 pt-3 text-xs text-slate-600"><input type="checkbox" checked={deleteBackupData} onChange={event=>setDeleteBackupData(event.target.checked)} disabled={unregistering||forceRemoveEnabled} className="mt-0.5"/><span><strong className="block text-slate-800">Delete backup data and restore points</strong>This is required before normal unregister and cannot be undone.</span></label> : unregisterPrecheck?.objectStorageNeeded ? <p className="mt-3 border-t border-slate-100 pt-3 text-xs leading-5 text-blue-700"><strong className="block text-blue-800">Historical storage data detected</strong>The cluster storage prefix will be checked and cleaned automatically before unregister.</p> : <p className="mt-3 border-t border-slate-100 pt-3 text-xs text-emerald-700">This cluster has never used object storage. Object storage will not be accessed.</p>}
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
              </div>

              <div className="hbdr-filter-drawer-actions hbdr-unregister-drawer-actions">
                  <button onClick={closeUnregister} disabled={unregistering} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-wait disabled:opacity-60">Cancel</button>
                  <button
                    disabled={unregistering || unregisterPrecheckLoading || (!forceRemoveEnabled && (!unregisterPrecheck?.allowed || Boolean(unregisterPrecheck.restorePointCount && !deleteBackupData))) || (forceRemoveEnabled && (Boolean(unregisterPrecheck?.targetPlanCount) || forceRemoveConfirmation !== (unregisterTarget.name === 'unknown-cluster' ? unregisterTarget.id : unregisterTarget.name)))}
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
