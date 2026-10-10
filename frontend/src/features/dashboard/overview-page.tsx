import { useState } from 'react';
import DRTopologyView from '../clusters/dr-topology-view';
import React from 'react';
import { AlertCircle, CheckCircle2, ChevronDown, Cloud, Database, History, Lock, RefreshCw, Server } from 'lucide-react';
import { motion } from 'motion/react';
import type { AppItem, Cluster } from '../clusters/types';
import { buildDRTopology } from '../clusters/dr-topology';
import { formatDateTime } from '../../lib/date-time';

type StorageRepo={status:string};
type ApiTask={id:string;clusterId:string;type:string;status:string;errorCode?:string;errorMessage?:string;createdAt?:string;completedAt?:string};
type ApiRestorePointView={id:string;sourceClusterId:string;status:string;time?:string;sourceNamespace?:string;includedNamespaces?:string[];appId?:string;protectionPlanId?:string};
type ApiPolicy={id:string;scheduleType:string;intervalValue?:number;intervalUnit?:string;boundCount:number};
import type { ApiProtectionPlan } from '../recovery/types';
type ApiApplication={id:string;clusterId:string;namespace:string};
type ProductInfo={product?:string;edition?:string;license?:{mode?:string;status?:string;detail?:string}};
const isActiveTaskStatus=(status?:string)=>['queued','dispatched','accepted','running','canceling'].includes(status||'');
const isSucceededStatus=(status?:string)=>status==='succeeded'||status==='completed';
const isFailedStatus=(status?:string)=>['failed','canceled','cancelled','error','timeout','timed_out'].includes(status||'');
const taskStatusLabel=(status?:string)=>isSucceededStatus(status)?'Succeeded':isFailedStatus(status)?'Failed':isActiveTaskStatus(status)?'Running':status||'Unknown';
const resolveRecoveryCluster=(cluster:Cluster|null,clusters:Cluster[])=>{if(!cluster)return null;const names=[...new Set(cluster.apps.filter(app=>app.isProtected&&app.targetCluster).map(app=>app.targetCluster!))];return(names.length?clusters.find(item=>names.includes(item.name)):undefined)??null};
const policyIntervalMs=(policy?:ApiPolicy)=>{if(!policy||policy.scheduleType!=='interval'||!policy.intervalValue)return null;const unit=(policy.intervalUnit||'').toLowerCase();return policy.intervalValue*(unit.startsWith('minute')?60000:unit.startsWith('day')?86400000:3600000)};
const planIncludesNamespace=(plan:ApiProtectionPlan,app:AppItem,apps:ApiApplication[])=>{const ids=new Set([plan.appId,...(plan.appIds||[])].filter(Boolean));return Boolean(app.apiId&&ids.has(app.apiId))||Boolean(app.clusterId&&apps.some(item=>ids.has(item.id)&&item.clusterId===app.clusterId&&item.namespace===(app.namespace||app.name)))};
const restorePointMatchesApp=(point:ApiRestorePointView,app:AppItem)=>{const namespace=app.namespace||app.name;if(app.protectionPlanId&&point.protectionPlanId&&point.includedNamespaces?.length)return point.protectionPlanId===app.protectionPlanId&&point.includedNamespaces.includes(namespace);if(app.apiId&&point.appId)return point.appId===app.apiId;if(app.protectionPlanId&&point.protectionPlanId)return point.protectionPlanId===app.protectionPlanId;return Boolean(app.clusterId&&point.sourceClusterId===app.clusterId&&point.sourceNamespace===namespace)};
const latestSuccessfulRestorePoint=(points:ApiRestorePointView[],app:AppItem)=>points.filter(point=>point.status==='available'&&restorePointMatchesApp(point,app)).sort((a,b)=>(b.time||'').localeCompare(a.time||''))[0];
const recentTasks=(tasks:ApiTask[],days:number)=>{const since=Date.now()-days*86400000;return tasks.filter(task=>Date.parse(task.createdAt||'')>=since)};
const taskTypeLabel=(type?:string)=>type==='backup'?'Data sync':type==='drill'?'DR drill':type==='restore'?'Restore':type==='takeover'?'Takeover':type==='storage-sync'?'Storage sync':type==='unregister'?'Cluster unregister':type==='bsl-sync'?'Storage configuration':type?type.replace(/-/g,' '):'Task';
const taskTime=(task:ApiTask)=>task.completedAt||task.createdAt||'';

export function OverviewPage(props: {
  cluster: Cluster | null;
  clusters: Cluster[];
  storage: StorageRepo[];
  protectedApps: number;
  restorePointCount: number;
  tasks: ApiTask[];
  restorePoints: ApiRestorePointView[];
  policies: ApiPolicy[];
  protectionPlans: ApiProtectionPlan[];
  applications: ApiApplication[];
  defaultClusterId: string | null;
  productInfo: ProductInfo | null;
  openDr: () => void;
  openOperations: () => void;
  clusterContext: React.ReactNode;
}) {
  const { cluster, clusters, storage, protectedApps, restorePointCount, tasks, restorePoints, policies, protectionPlans, applications, defaultClusterId, productInfo, openDr, openOperations, clusterContext } = props;
  const [rangeOpen, setRangeOpen] = React.useState(false);
  const [timeRange, setTimeRange] = React.useState('Last 24 hours');
  const [healthFilter, setHealthFilter] = React.useState<'All' | 'At risk' | 'Healthy'>('All');
  const clusterApps = cluster?.apps ?? [];
  const clusterTasks = cluster ? tasks.filter(task => task.clusterId === cluster.id) : [];
  const clusterRestorePoints = cluster ? restorePoints.filter(point => point.sourceClusterId === cluster.id && point.status === 'available') : [];
  const clusterPlans = cluster ? protectionPlans.filter(plan => clusterApps.some(app => planIncludesNamespace(plan, app, applications))) : [];
  const totalApps = clusterApps.length;
  const namespaceCount = cluster?.applications ?? 0;
  const activeNamespaces = clusterApps.filter(app => app.status === 'Active' || app.status === 'Running' || app.status === 'Protected').length;
  const connectedStorage = storage.filter(item => item.status === 'connected').length;
  const recoveryCluster = resolveRecoveryCluster(cluster, clusters);
  const targetClusterNames = [...new Set(
    clusterApps.filter(app => app.isProtected && app.targetCluster).map(app => app.targetCluster!),
  )];
  const notConfigured = Math.max(totalApps - protectedApps, 0);
  const now = Date.now();
  const rpoStats = clusterApps.reduce((acc, app) => {
    if (!app.isProtected) return acc;
    const plan = clusterPlans.find(item => planIncludesNamespace(item, app, applications));
    const policy = policies.find(item => item.id === plan?.policyId);
    const intervalMs = policyIntervalMs(policy);
    const latestPoint = latestSuccessfulRestorePoint(clusterRestorePoints, app);
    if (!intervalMs || !latestPoint?.time) {
      acc.risk += 1;
      return acc;
    }
    const ageMs = now - Date.parse(latestPoint.time);
    if (Number.isFinite(ageMs) && ageMs <= intervalMs) acc.meeting += 1;
    else acc.risk += 1;
    return acc;
  }, { meeting: 0, risk: 0 });
  const syncConfiguredApps = clusterApps.filter(app => app.isProtected);
  const activeBackupTasks = clusterTasks.filter(task => task.type === 'backup' && isActiveTaskStatus(task.status));
  const failedBackupTasks = clusterTasks.filter(task => task.type === 'backup' && isFailedStatus(task.status));
  const syncCompleted = syncConfiguredApps.filter(app => Boolean(latestSuccessfulRestorePoint(clusterRestorePoints, app))).length;
  const syncNotStarted = Math.max(syncConfiguredApps.length - syncCompleted - activeBackupTasks.length - failedBackupTasks.length, 0);
  const syncRate = syncConfiguredApps.length > 0 ? Math.round((syncCompleted / syncConfiguredApps.length) * 100) : 0;
  const restoreTasks = clusterTasks.filter(task => ['restore', 'drill', 'takeover'].includes(task.type));
  const recoveryTasks30d = recentTasks(restoreTasks, 30);
  const finishedRecoveryTasks30d = recoveryTasks30d.filter(task => isSucceededStatus(task.status) || isFailedStatus(task.status));
  const recoverySuccessRate = finishedRecoveryTasks30d.length
    ? `${Math.round(100 * finishedRecoveryTasks30d.filter(task => isSucceededStatus(task.status)).length / finishedRecoveryTasks30d.length)}%`
    : 'N/A';
  const restoreInProgress = restoreTasks.filter(task => isActiveTaskStatus(task.status)).length;
  const restoreFailed = restoreTasks.filter(task => isFailedStatus(task.status)).length;
  const drillTasks30d = recentTasks(clusterTasks.filter(task => task.type === 'drill'), 30);
  const drillInProgress = drillTasks30d.filter(task => isActiveTaskStatus(task.status)).length;
  const drillCompleted = drillTasks30d.filter(task => isSucceededStatus(task.status)).length;
  const drillFailed = drillTasks30d.filter(task => isFailedStatus(task.status)).length;
  const storageUnavailable = storage.length - connectedStorage;
  const offlineClusters = clusters.filter(item => item.connectionStatus !== 'online').length;
  const failedRecentTasks = recentTasks(tasks, 30).filter(task => isFailedStatus(task.status)).length;
  const warningRecentTasks = recentTasks(tasks, 30).filter(task => task.status === 'warning' || task.errorCode).length;
  const criticalAlerts = offlineClusters + storageUnavailable + failedRecentTasks;
  const urgentAlerts = rpoStats.risk + syncConfiguredApps.filter(app => !latestSuccessfulRestorePoint(clusterRestorePoints, app)).length;
  const normalAlerts = notConfigured + warningRecentTasks;
  const recentEventTasks = [...tasks]
    .filter(task => ['backup', 'drill', 'restore', 'takeover', 'storage-sync', 'unregister'].includes(task.type))
    .sort((a, b) => taskTime(b).localeCompare(taskTime(a)))
    .slice(0, 4);
  const registeredClusters = clusters.length;
  const defaultClusterCount = defaultClusterId ? 1 : 0;
  const plansInUse = protectionPlans.length;
  const policiesInUse = new Set(protectionPlans.map(plan => plan.policyId).filter(Boolean)).size;
  const policiesAvailable = policies.filter(policy => !policy.boundCount && !protectionPlans.some(plan => plan.policyId === policy.id)).length;
  const alertRows = [
    offlineClusters > 0 ? `${offlineClusters} cluster${offlineClusters === 1 ? '' : 's'} offline` : null,
    storageUnavailable > 0 ? `${storageUnavailable} storage repositor${storageUnavailable === 1 ? 'y is' : 'ies are'} unavailable` : null,
    failedRecentTasks > 0 ? `${failedRecentTasks} failed task${failedRecentTasks === 1 ? '' : 's'} in 30 days` : null,
    rpoStats.risk > 0 ? `${rpoStats.risk} protected namespace${rpoStats.risk === 1 ? '' : 's'} at RPO risk` : null,
    notConfigured > 0 ? `${notConfigured} namespace${notConfigured === 1 ? '' : 's'} not protected` : null,
  ].filter((item): item is string => Boolean(item)).slice(0, 3);
  const clusterZoneKey = cluster?.id || 'no-cluster';
  const drSiteTitle = targetClusterNames.length > 1
    ? `${targetClusterNames.length} Targets`
    : (recoveryCluster?.name ?? 'N/A');
  const drSiteSubtitle = targetClusterNames.length > 1 ? 'Target Clusters' : 'Target Cluster';
  const [selectedTopologyRelationship, setSelectedTopologyRelationship] = useState<string | null>(null);
  const [selectedTopologyCluster, setSelectedTopologyCluster] = useState<string | null>(null);
  const topology = buildDRTopology(clusters, protectionPlans);
  const topologyPairs = topology.relationships.length;
  const topologyStatuses = topology.relationships.reduce((counts, relationship) => {
    counts[relationship.status] = (counts[relationship.status] || 0) + 1;
    return counts;
  }, {} as Record<string, number>);
  const protectedRate = totalApps > 0 ? Math.round((protectedApps / totalApps) * 100) : 0;
  const oldestRestorePoint = clusterRestorePoints
    .map(point => point.time ? Date.parse(point.time) : NaN)
    .filter(value => Number.isFinite(value))
    .sort((a, b) => a - b)[0];
  const oldestRestorePointLabel = oldestRestorePoint
    ? `${Math.max(0, Math.floor((now - oldestRestorePoint) / 86400000))}d ago`
    : 'N/A';

  return (
    <div className="hbdr-dashboard hbdr-dashboard-zones hbdr-dashboard-design-canvas">
      <div className="hbdr-dashboard-design-header">
        <h1>Overview</h1>
        <div className="hbdr-dashboard-design-controls">
          <div className="hbdr-dashboard-range-wrap">
            <button type="button" className={`hbdr-dashboard-design-range ${rangeOpen ? 'is-open' : ''}`} onClick={() => setRangeOpen(open => !open)} aria-haspopup="listbox" aria-expanded={rangeOpen}>
              {timeRange} <ChevronDown size={13} className={rangeOpen ? 'rotate-180' : ''} />
            </button>
            {rangeOpen && (
              <div className="hbdr-dashboard-range-menu" role="listbox" aria-label="Time range">
                {['Last 24 hours', 'Last 7 days', 'Last 30 days'].map(option => (
                  <button key={option} type="button" role="option" aria-selected={timeRange === option} className={timeRange === option ? 'is-active' : ''} onClick={() => { setTimeRange(option); setRangeOpen(false); }}>
                    {option}
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="hbdr-dashboard-design-cluster">{clusterContext}</div>
        </div>
      </div>
      <div className="hbdr-dashboard-upper">
      <section className="hbdr-dashboard-workspace hbdr-dashboard-zone hbdr-dashboard-zone-cluster">
        <header className="hbdr-dashboard-zone-head">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-cluster" aria-hidden="true" />
            <div>
              <h2>Protection health</h2>
              <p><strong>{totalApps}</strong> namespaces</p>
            </div>
          </div>
          <div className="hbdr-dashboard-health-filter" role="tablist" aria-label="Protection health filter">
            {(['All', 'At risk', 'Healthy'] as const).map(option => (
              <button key={option} type="button" role="tab" aria-selected={healthFilter === option} className={healthFilter === option ? 'is-active' : ''} onClick={() => setHealthFilter(option)}>{option}</button>
            ))}
          </div>
        </header>
        <motion.div
          key={clusterZoneKey}
          className="hbdr-dashboard-cluster-body"
          initial={{ opacity: 0.82 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.22 }}
        >
        <div className="hbdr-dashboard-health-issues" aria-label="Active issues">
          <span className="hbdr-dashboard-health-kicker">Active issues</span>
          <div className="hbdr-dashboard-health-issue-grid">
            <div><strong className="is-critical">{offlineClusters}</strong><span>Cluster offline</span></div>
            <i aria-hidden="true" />
            <div><strong>{storageUnavailable}</strong><span>Storage unreachable</span></div>
            <i aria-hidden="true" />
            <div><strong className="is-warning">{failedRecentTasks}</strong><span>Failed tasks</span></div>
          </div>
        </div>
        <div className="hbdr-dashboard-health-summary">
          <div className="hbdr-dashboard-protection-ring" style={{ '--hbdr-protection-rate': `${protectedRate}%` } as React.CSSProperties}>
            <strong>{protectedRate}<small>%</small></strong>
            <span>Protected</span>
          </div>
          <div className="hbdr-dashboard-health-legend">
            <DashboardLegend color="green" label="Protected" value={protectedApps} />
            <DashboardLegend color="red" label="Unprotected" value={notConfigured} />
            <DashboardLegend color="gray" label="At RPO risk" value={rpoStats.risk} />
            <div className="hbdr-dashboard-oldest-point"><span>Oldest restore point</span><strong>{oldestRestorePointLabel}</strong></div>
          </div>
        </div>
        <div className="hbdr-dashboard-flow">
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-prod-icon"><Server size={44} /></div>
            <strong>Production</strong>
          </div>
          <div className="hbdr-dashboard-flow-line"><span>Backup</span></div>
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-storage-icon"><Database size={44} /></div>
            <strong>Storage</strong>
          </div>
          <div className="hbdr-dashboard-flow-line"><span>DR Drill</span></div>
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-cloud-icon"><Cloud size={44} /></div>
            <strong>DR Site</strong>
          </div>
        </div>

        <div className="hbdr-dashboard-cards">
          <DashboardPanel title="Production" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{totalApps}</strong>
              <span>Namespaces</span>
            </div>
            <DashboardLegend color="green" label="Protected" value={protectedApps} />
            <DashboardLegend color="red" label="Unprotected" value={notConfigured} />
            {cluster && <DashboardLegend color="blue" label="Active" value={activeNamespaces || namespaceCount} />}
          </DashboardPanel>

          <DashboardPanel title="RPO">
            <div className="hbdr-dashboard-big-number">
              <strong>{rpoStats.risk}</strong>
              <span>At SLA Risk</span>
            </div>
            <DashboardLegend color="green" label="Meeting SLA" value={rpoStats.meeting} />
            <DashboardLegend color="red" label="Not Protected" value={notConfigured} />
          </DashboardPanel>

          <DashboardPanel title="Data Sync" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{syncRate}%</strong>
              <span>Sync Completion</span>
            </div>
            <DashboardLegend color="gray" label="Not Started" value={syncNotStarted} />
            <DashboardLegend color="green" label="In Progress" value={activeBackupTasks.length} />
            <DashboardLegend color="green" label="Completed" value={syncCompleted} />
            <DashboardLegend color="red" label="Failed" value={failedBackupTasks.length} />
          </DashboardPanel>

          <DashboardPanel title="Restore Points" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{clusterRestorePoints.length}</strong>
              <span>Restore Points</span>
            </div>
            <DashboardLegend color="blue" label="In Progress" value={restoreInProgress} />
            <DashboardLegend color="green" label="Completed" value={clusterRestorePoints.length} />
            <DashboardLegend color="red" label="Failed" value={restoreFailed} />
          </DashboardPanel>

          <DashboardPanel title="DR Drill" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{drillTasks30d.length}</strong>
              <span>Drills in 30 Days</span>
            </div>
            <DashboardLegend color="blue" label="In Progress" value={drillInProgress} />
            <DashboardLegend color="green" label="Completed" value={drillCompleted} />
            <DashboardLegend color="red" label="Failed" value={drillFailed} />
          </DashboardPanel>

          <DashboardPanel title="DR Site" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number hbdr-dashboard-big-number-clip">
              <strong>{drSiteTitle}</strong>
              <span>{drSiteSubtitle}</span>
            </div>
            <DashboardLegend color="gray" label="Kubernetes" value={recoveryCluster?.version || '-'} />
            <DashboardLegend color="green" label="Nodes" value={recoveryCluster ? recoveryCluster.nodes : 0} />
            <DashboardLegend color="gray" label="Namespaces" value={recoveryCluster ? recoveryCluster.applications : 0} />
          </DashboardPanel>
        </div>
        </motion.div>
      </section>

      <section className="hbdr-dashboard-side hbdr-dashboard-zone hbdr-dashboard-zone-topology">
        <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-platform" aria-hidden="true" />
            <div>
              <h2>DR topology</h2>
              <p>{topologyPairs} protection relationship{topologyPairs === 1 ? '' : 's'} across {clusters.length} cluster{clusters.length === 1 ? '' : 's'}</p>
            </div>
          </div>
          <div className="hbdr-dashboard-topology-statuses">
            <span><i className="is-healthy" />{topologyStatuses.healthy || 0} Healthy</span>
            <span><i className="is-warning" />{topologyStatuses.warning || 0} Degraded</span>
            <span><i className="is-critical" />{topologyStatuses.failed || 0} Failed</span>
          </div>
        </header>
        <div className="hbdr-dashboard-topology-body">
          {clusters.length > 0 ? <DRTopologyView compact clusters={clusters} model={topology} selectedRelationshipId={selectedTopologyRelationship} selectedClusterId={selectedTopologyCluster} onSelectRelationship={relationship => setSelectedTopologyRelationship(relationship.id)} onSelectCluster={id => setSelectedTopologyCluster(id)} /> : <div className="hbdr-dashboard-topology-empty"><History size={18} /><span>No registered clusters</span></div>}
        </div>
      </section>
      <aside className="hbdr-dashboard-side hbdr-dashboard-zone hbdr-dashboard-zone-platform">
        <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static">
          <div className="hbdr-dashboard-zone-label"><span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-platform" aria-hidden="true" /><div><h2>Platform</h2><p>Shared resources across all clusters</p></div></div>
        </header>
        <dl className="hbdr-platform-summary">
          <div><dt>Registered clusters</dt><dd><strong>{registeredClusters}</strong><small>{clusters.filter(item => item.connectionStatus === 'online').length} online</small></dd></div>
          <div><dt>Protection plans</dt><dd><strong>{plansInUse}</strong></dd></div>
          <div><dt>Agent version</dt><dd><strong>{cluster?.agentVersion || 'N/A'}</strong></dd></div>
        </dl>
        <div className="hbdr-platform-license-summary">
          <Lock size={18} aria-hidden="true" />
          <div><strong>{productInfo?.edition || 'Edition unavailable'} · {productInfo?.license?.status?.replace(/-/g, ' ') || 'License status unavailable'}</strong>
          <p>{productInfo?.license?.detail || productInfo?.license?.mode || 'No license metadata available'}</p></div>
        </div>
      </aside>
      </div>

      <div className="hbdr-dashboard-reference-metrics">
        <DashboardPanel title="Synchronization">
          <div className="hbdr-dashboard-reference-stat-grid"><div><strong>{clusterTasks.filter(task => task.type === 'backup').length}</strong><span>Total runs</span></div><div className="is-success"><strong>{clusterTasks.filter(task => task.type === 'backup' && isSucceededStatus(task.status)).length}</strong><span>Succeeded</span></div><div className="is-critical"><strong>{clusterTasks.filter(task => task.type === 'backup' && isFailedStatus(task.status)).length}</strong><span>Failed</span></div></div>
          <div className="hbdr-dashboard-reference-empty"><span>Runs per 2h</span><small>Task history is available from Operations</small></div>
        </DashboardPanel>
        <DashboardPanel title="Recovery & drills">
          <div className="hbdr-dashboard-reference-stat-grid"><div><strong>{recentTasks(restoreTasks, 30).length}</strong><span>Last 30 days</span></div><div className="is-info"><strong>{restoreInProgress}</strong><span>Running now</span></div><div className="is-success"><strong>{recoverySuccessRate}</strong><span>Success rate</span></div></div>
          <div className="hbdr-dashboard-reference-empty"><span>By operation</span><small>Drill {recoveryTasks30d.filter(task => task.type === 'drill').length} · Restore {recoveryTasks30d.filter(task => task.type === 'restore').length} · Takeover {recoveryTasks30d.filter(task => task.type === 'takeover').length}</small></div>
        </DashboardPanel>
        <DashboardPanel title="Storage & retention">
          <div className="hbdr-dashboard-reference-stat-grid"><div><strong>{clusterRestorePoints.length}</strong><span>Restore points</span></div><div className="is-success"><strong>{connectedStorage}</strong><span>Repos online</span></div><div><strong>N/A</strong><span>Capacity reduction</span></div></div>
          <div className="hbdr-dashboard-reference-empty"><span>Repositories</span><small>{storage.length ? `${connectedStorage} connected · ${storageUnavailable} unavailable` : 'No storage repositories configured'}</small></div>
        </DashboardPanel>
      </div>

      <section className="hbdr-dashboard-zone hbdr-dashboard-zone-operations">
        <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-operations" aria-hidden="true" />
            <div>
              <h2>Operations</h2>
              <p>Monitoring, events, and alerts</p>
            </div>
          </div>
        </header>

        <div className="hbdr-dashboard-operations-row">
          <section className="hbdr-dashboard-card hbdr-dashboard-monitor">
            <header>
              <h3>DR Resources Monitoring & Analysis</h3>
              <button onClick={openOperations}>Details &gt;</button>
            </header>
            <div className="hbdr-dashboard-filters">
              <button>DR Agent <ChevronDown size={16} /></button>
              <span className="hbdr-dashboard-filter-sep">Filter</span>
              <button>{cluster?.name || 'All clusters'} <ChevronDown size={16} /></button>
              <button><RefreshCw size={18} /></button>
            </div>
            <div className="hbdr-dashboard-charts">
              <div className="hbdr-dashboard-empty-panel">
                <strong>CPU Usage</strong>
                <Cloud size={68} />
                <span>No monitoring data yet</span>
              </div>
              <div className="hbdr-dashboard-empty-panel">
                <strong>Network (bytes)</strong>
                <Cloud size={68} />
                <span>No monitoring data yet</span>
              </div>
            </div>
          </section>

          <section className="hbdr-dashboard-card hbdr-dashboard-events">
            <header>
              <h3>Recent activity</h3>
              <button onClick={openOperations}>Logs &gt;</button>
            </header>
            {recentEventTasks.length > 0 ? recentEventTasks.map(task => (
              <div key={task.id} className="hbdr-dashboard-event">
                <span />
                <p>{taskTypeLabel(task.type)} ({taskStatusLabel(task.status)})</p>
                <small>{formatDateTime(taskTime(task))}</small>
              </div>
            )) : (
              <div className="hbdr-dashboard-empty-list">
                <History size={20} />
                <p>No recent task events</p>
              </div>
            )}
          </section>

          <DashboardPanel className="hbdr-dashboard-zone-alert" title="Alerts">
            <div className="hbdr-dashboard-alert-metrics">
              <div><strong>{criticalAlerts}</strong><span>Critical</span></div>
              <div><strong>{urgentAlerts}</strong><span>Urgent</span></div>
              <div><strong>{normalAlerts}</strong><span>Alert</span></div>
            </div>
            <div className="hbdr-dashboard-alert-list">
            {alertRows.length > 0 ? alertRows.map(item => (
              <div key={item} className="hbdr-dashboard-alert-row">
                <AlertCircle size={15} />
                <span>{item}</span>
              </div>
            )) : (
              <div className="hbdr-dashboard-empty-list hbdr-dashboard-empty-list-compact">
                <CheckCircle2 size={20} />
                <p>No active alerts</p>
              </div>
            )}
            </div>
          </DashboardPanel>
        </div>
      </section>
    </div>
  );
}

export function DashboardPanel({
  title,
  detailAction,
  className,
  children,
}: {
  title: string;
  detailAction?: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={`hbdr-dashboard-card${className ? ` ${className}` : ''}`}>
      <header>
        <h3>{title}</h3>
        <div className="hbdr-dashboard-card-actions">
          {detailAction && <button onClick={detailAction}>Details&gt;</button>}
        </div>
      </header>
      {children}
    </section>
  );
}

export function PlatformLicenseCard({ productInfo }: { productInfo: ProductInfo | null }) {
  const license = productInfo?.license;
  const status = license?.status
    ? license.status.replace(/-/g, ' ').replace(/\b\w/g, character => character.toUpperCase())
    : 'No license data available';
  const metadata = [productInfo?.edition, license?.mode]
    .filter(Boolean)
    .map(value => value!.replace(/-/g, ' '))
    .join(' · ');
  return (
    <section className="hbdr-dashboard-card hbdr-platform-card-wide hbdr-platform-license-card">
      <header>
        <h3>License Status</h3>
        <div className="hbdr-dashboard-card-actions">
          <button type="button">Details&gt;</button>
        </div>
      </header>
      <div className="hbdr-platform-wide-body">
        <div className="hbdr-dashboard-empty-list hbdr-dashboard-license-empty">
          <Lock size={22} />
          <p>{status}</p>
          <small>{license?.detail || metadata || 'License metrics will appear after the platform license API is connected.'}</small>
        </div>
      </div>
    </section>
  );
}

export function DashboardLegend({ color, label, value }: { color: 'green' | 'red' | 'blue' | 'gray'; label: string; value: number | string }) {
  return (
    <div className="hbdr-dashboard-legend">
      <span className={`hbdr-dashboard-dot hbdr-dashboard-dot-${color}`} />
      <p>{label}</p>
      <strong>{value}</strong>
    </div>
  );
}

export function ProtectionLegend({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div>
      <span className={color} />
      <p>{label}</p>
      <strong>{value}</strong>
    </div>
  );
}
