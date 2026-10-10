import React from 'react';
import { AlertCircle, ArchiveX, CheckCircle2, ChevronDown, ClockAlert, History, Lock, ShieldAlert } from 'lucide-react';
import DRTopologyView from '../clusters/dr-topology-view';
import type { Cluster } from '../clusters/types';
import { buildDRTopology } from '../clusters/dr-topology';
import type { ApiApplication, ApiPolicy, ApiProtectionPlan, ApiRestorePointView, ApiTask, StorageRepo } from '../recovery/types';
import { formatDateTime } from '../../lib/date-time';
import { buildOverviewModel, activeTask, failedTask, taskCounts, tasksInRange, taskTime, timestamp } from './overview-model';

type ProductInfo = { product?: string; edition?: string; license?: { mode?: string; status?: string; detail?: string } };
const ranges = [{ label: 'Last 24 hours', days: 1 }, { label: 'Last 7 days', days: 7 }, { label: 'Last 30 days', days: 30 }];
const taskTypeLabel = (type: string) => ({ backup: 'Data sync', drill: 'DR drill', restore: 'Restore', takeover: 'Takeover', 'storage-sync': 'Storage sync', 'schedule-sync': 'Schedule configuration', 'protection-cleanup': 'Protection cleanup' })[type] || type;
const taskStatusLabel = (status: string) => ({ succeeded: 'Succeeded', completed: 'Succeeded', canceled: 'Canceled', cancelled: 'Canceled' })[status] || (activeTask(status) ? 'Running' : failedTask(status) ? 'Failed' : status || 'Unknown');
const daysAgo = (time: number | null, now: number) => time ? `${Math.max(0, Math.floor((now - time) / 86400000))}d ago` : 'N/A';

export function OverviewPage(props: {
  cluster: Cluster | null; clusters: Cluster[]; storage: StorageRepo[]; tasks: ApiTask[];
  restorePoints: ApiRestorePointView[]; policies: ApiPolicy[]; protectionPlans: ApiProtectionPlan[];
  applications: ApiApplication[]; productInfo: ProductInfo | null;
  openDr: () => void; openOperations: () => void; clusterContext: React.ReactNode;
}) {
  const { cluster, clusters, productInfo, openDr, openOperations, clusterContext } = props;
  const [rangeOpen, setRangeOpen] = React.useState(false);
  const [rangeIndex, setRangeIndex] = React.useState(0);
  const rangeRef = React.useRef<HTMLDivElement>(null);
  const [selectedRelationship, setSelectedRelationship] = React.useState<string | null>(null);
  const [selectedTopologyCluster, setSelectedTopologyCluster] = React.useState<string | null>(null);
  React.useEffect(() => {
    if (!rangeOpen) return;
    const dismiss = (event: PointerEvent) => { if (!rangeRef.current?.contains(event.target as Node)) setRangeOpen(false); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setRangeOpen(false); };
    document.addEventListener('pointerdown', dismiss);
    document.addEventListener('keydown', escape);
    return () => { document.removeEventListener('pointerdown', dismiss); document.removeEventListener('keydown', escape); };
  }, [rangeOpen]);
  const model = buildOverviewModel(props);
  const { platform, latestBackup, freshness } = model;
  const topology = buildDRTopology(clusters, props.protectionPlans);
  const topologyStatuses = topology.relationships.reduce((counts, relationship) => { counts[relationship.status] = (counts[relationship.status] || 0) + 1; return counts; }, {} as Record<string, number>);
  const backupHistory = taskCounts(tasksInRange(model.tasks.filter(task => task.type === 'backup'), ranges[rangeIndex].days, model.now));
  const recoveryHistory = tasksInRange(model.recoveryTasks, ranges[rangeIndex].days, model.now);
  const recoveryCounts = taskCounts(recoveryHistory);
  const recoverySuccess = recoveryCounts.succeeded + recoveryCounts.failed ? `${Math.round(100 * recoveryCounts.succeeded / (recoveryCounts.succeeded + recoveryCounts.failed))}%` : 'N/A';
  const recovery30d = tasksInRange(model.recoveryTasks, 30, model.now);
  const drillCounts = taskCounts(recovery30d.filter(task => task.type === 'drill'));
  const events = tasksInRange(model.tasks, ranges[rangeIndex].days, model.now).sort((a, b) => timestamp(taskTime(b)) - timestamp(taskTime(a))).slice(0, 4);
  const targets = model.targets;
  const singleTarget = targets.length === 1 && model.missingTargets === 0 ? targets[0] : null;
  const targetTitle = singleTarget?.name || (targets.length + model.missingTargets ? `${targets.length + model.missingTargets} targets` : 'N/A');
  const globalAlerts = [
    platform.offline ? `${platform.offline} cluster${platform.offline === 1 ? '' : 's'} offline` : null,
    platform.unknownClusters ? `${platform.unknownClusters} cluster connection status unknown` : null,
    platform.unavailableStorage ? `${platform.unavailableStorage} storage repositor${platform.unavailableStorage === 1 ? 'y' : 'ies'} unavailable` : null,
    platform.unknownStorage ? `${platform.unknownStorage} storage repository status unknown` : null,
  ].filter((item): item is string => Boolean(item));
  const clusterAlerts = [
    cluster?.connectionStatus === 'offline' ? 'Source cluster offline' : null,
    cluster && !['online', 'offline'].includes(cluster.connectionStatus || '') ? 'Source connection status unknown' : null,
    model.relatedUnavailable ? `${model.relatedUnavailable} related storage repositor${model.relatedUnavailable === 1 ? 'y' : 'ies'} unavailable or unknown` : null,
    model.riskNamespaces ? `${model.riskNamespaces} configured namespace${model.riskNamespaces === 1 ? '' : 's'} at backup freshness risk` : null,
    latestBackup.failed ? `${latestBackup.failed} plan${latestBackup.failed === 1 ? '' : 's'} with latest backup failed` : null,
    model.unconfigured ? `${model.unconfigured} namespace${model.unconfigured === 1 ? '' : 's'} without protection configuration` : null,
    model.targetOffline ? `${model.targetOffline} related recovery target${model.targetOffline === 1 ? '' : 's'} offline` : null,
    model.missingTargets ? `${model.missingTargets} recovery target${model.missingTargets === 1 ? '' : 's'} unavailable` : null,
  ].filter((item): item is string => Boolean(item));

  return (
    <div className="hbdr-dashboard hbdr-dashboard-zones hbdr-dashboard-design-canvas hbdr-dashboard-scoped">
      <div className="hbdr-dashboard-scope-columns">
        <section className="hbdr-dashboard-platform-column" aria-label="Platform overview">
          <header className="hbdr-dashboard-scope-heading"><div><h2>Platform overview</h2><p>Shared resources across all clusters</p></div></header>
          <div className="hbdr-dashboard-platform-metrics">
            <DashboardPanel title="Registered clusters"><ScopeValue value={platform.registered} label={`${platform.online} online · ${platform.offline} offline`} />{platform.unknownClusters > 0 && <ScopeLine label="Unknown" value={platform.unknownClusters} />}</DashboardPanel>
            <DashboardPanel title="Protection plans"><ScopeValue value={platform.plans} label="Across all clusters" /></DashboardPanel>
            <DashboardPanel title="Storage repositories"><ScopeValue value={platform.repositories} label={`${platform.connectedStorage} connected`} /><ScopeLine label="Unavailable / Unknown" value={`${platform.unavailableStorage} / ${platform.unknownStorage}`} /></DashboardPanel>
          </div>
          <section className="hbdr-dashboard-zone hbdr-dashboard-zone-topology">
            <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static"><div className="hbdr-dashboard-zone-label"><div><h2>DR topology</h2><p>{topology.relationships.length} protection relationships across {clusters.length} clusters</p></div></div></header>
            <div className="hbdr-dashboard-topology-statuses"><span><i />{topologyStatuses.healthy || 0} Healthy</span><span><i className="is-warning" />{topologyStatuses.warning || 0} Degraded</span><span><i className="is-critical" />{topologyStatuses.failed || 0} Failed</span>{Boolean(topologyStatuses.configuring) && <span>{topologyStatuses.configuring} Configuring</span>}</div>
            <div className="hbdr-dashboard-topology-body">{clusters.length ? <DRTopologyView compact clusters={clusters} model={topology} selectedRelationshipId={selectedRelationship} selectedClusterId={selectedTopologyCluster || cluster?.id || null} onSelectRelationship={relationship => setSelectedRelationship(relationship.id)} onSelectCluster={setSelectedTopologyCluster} /> : <ScopeEmpty label="No registered clusters" />}</div>
          </section>
          <DashboardPanel title="Platform alerts" detailAction={openOperations}>
            <IssueList items={globalAlerts} empty="No current platform issues" onDetails={openOperations} />
            <div className="hbdr-platform-license-summary"><Lock size={16} aria-hidden="true" /><div><strong>{productInfo?.edition || 'Edition unavailable'} · {productInfo?.license?.status?.replace(/-/g, ' ') || 'License status unavailable'}</strong><p>{productInfo?.license?.detail || productInfo?.license?.mode || 'No license metadata available'}</p></div></div>
          </DashboardPanel>
        </section>
        <section className="hbdr-dashboard-cluster-column" aria-label="Cluster overview">
          <header className="hbdr-dashboard-scope-heading"><div><h2>Cluster overview</h2><p>Selected cluster and related DR resources</p></div><div className="hbdr-dashboard-design-cluster">{clusterContext}</div></header>
          <section className="hbdr-dashboard-workspace hbdr-dashboard-zone hbdr-dashboard-zone-cluster">
            <header className="hbdr-dashboard-zone-head"><div className="hbdr-dashboard-zone-label"><div><h2>Protection health</h2><p>{model.totalApps} namespaces · {model.configured} configured / {model.unconfigured} unconfigured</p></div></div><span className="hbdr-dashboard-scope-caption">Current state</span></header>
            <div className="hbdr-dashboard-health-summary hbdr-dashboard-health-expanded">
              <div className="hbdr-dashboard-health-coverage">
                <div className="hbdr-dashboard-protection-ring" style={{ '--hbdr-protection-rate': `${model.coverage ?? 0}%` } as React.CSSProperties}><strong>{model.coverage ?? 'N/A'}{model.coverage !== null && <small>%</small>}</strong><span>Configured</span></div>
                <div className="hbdr-dashboard-health-legend"><DashboardLegend color="green" label="Configured" value={model.configured} /><DashboardLegend color="gray" label="Unconfigured" value={model.unconfigured} /></div>
              </div>
              <div className="hbdr-dashboard-protection-gaps">
                <h3>Protection gaps</h3>
                <ProtectionGap icon={ShieldAlert} label="Not configured" value={model.unconfigured} unit="namespaces" />
                <ProtectionGap icon={ArchiveX} label="No available restore point" value={model.namespacesWithoutPoints} unit="namespaces" />
                <ProtectionGap icon={ClockAlert} label="Backup overdue" value={model.riskNamespaces} unit="namespaces" />
                <ProtectionGap icon={AlertCircle} label="Latest backup failed" value={latestBackup.failed} unit="plans" />
              </div>
            </div>
            <div className="hbdr-dashboard-scope-dependencies">
              <span>Source: <strong>{cluster?.connectionStatus || 'N/A'}</strong></span>
              <span>Targets: <strong>{targets.length + model.missingTargets ? `${targets.filter(target => target.connectionStatus === 'online').length} online / ${model.targetOffline} offline${targets.filter(target => !['online', 'offline'].includes(target.connectionStatus || '')).length ? ` / ${targets.filter(target => !['online', 'offline'].includes(target.connectionStatus || '')).length} unknown` : ''}${model.missingTargets ? ` / ${model.missingTargets} unavailable` : ''}` : 'Not configured'}</strong></span>
              <span>Storage: <strong>{model.relatedStorage.filter(repo => repo.status === 'connected').length} connected{model.relatedUnavailable ? ` / ${model.relatedUnavailable} unavailable or unknown` : ''}</strong></span>
            </div>
          </section>
          <div className="hbdr-dashboard-scope-metrics">
            <DashboardPanel title="Production" detailAction={openDr}><ScopeValue value={model.totalApps} label="Namespaces" /><ScopeLine label="Configured / Unconfigured" value={`${model.configured} / ${model.unconfigured}`} /><ScopeLine label="Active" value={model.activeNamespaces} /></DashboardPanel>
            <DashboardPanel title="RPO / Backup freshness"><ScopeValue value={freshness.risk} label="Overdue plans" /><ScopeLine label="Within schedule" value={freshness.meeting} /><ScopeLine label="Awaiting first backup" value={freshness.initial} />{Boolean(freshness.manual + freshness.paused + freshness.unknown) && <ScopeLine label="Manual / Paused / Unknown" value={`${freshness.manual} / ${freshness.paused} / ${freshness.unknown}`} />}</DashboardPanel>
            <DashboardPanel title="Data sync" detailAction={openDr}><ScopeValue value={model.syncRate === null ? 'N/A' : `${model.syncRate}%`} label="Plans with a restore point" /><ScopeLine label="Latest succeeded / Running" value={`${latestBackup.succeeded} / ${latestBackup.running}`} /><ScopeLine label="Failed / Not started" value={`${latestBackup.failed} / ${latestBackup.notStarted}`} />{Boolean(latestBackup.canceled + latestBackup.unknown) && <ScopeLine label="Canceled / Unknown" value={`${latestBackup.canceled} / ${latestBackup.unknown}`} />}</DashboardPanel>
            <DashboardPanel title="Restore points" detailAction={openDr}><ScopeValue value={model.points.length} label="Available restore points" /><ScopeLine label="Oldest point" value={daysAgo(model.oldestPoint, model.now)} /><ScopeLine label="Recovery running" value={model.recoveryTasks.filter(task => activeTask(task.status)).length} /><ScopeLine label="Recovery failed (30d)" value={recovery30d.filter(task => failedTask(task.status)).length} /></DashboardPanel>
            <DashboardPanel title="DR drill" detailAction={openDr}><ScopeValue value={drillCounts.total} label="Drills in last 30 days" /><ScopeLine label="Succeeded / Failed" value={`${drillCounts.succeeded} / ${drillCounts.failed}`} /><ScopeLine label="Running / Canceled" value={`${drillCounts.running} / ${drillCounts.canceled}`} /></DashboardPanel>
            <DashboardPanel title="DR site" detailAction={openDr}><ScopeValue value={targetTitle} label="Target cluster" />{singleTarget ? <><ScopeLine label="Kubernetes" value={singleTarget.version || 'N/A'} /><ScopeLine label="Nodes / Namespaces" value={`${singleTarget.nodes} / ${singleTarget.applications}`} /></> : <ScopeLine label="Online / Offline / Missing" value={`${targets.filter(target => target.connectionStatus === 'online').length} / ${model.targetOffline} / ${model.missingTargets}`} />}</DashboardPanel>
          </div>
          <DashboardPanel title="Cluster alerts" detailAction={openDr}><IssueList items={clusterAlerts} empty={cluster ? 'No current cluster issues' : 'No cluster selected'} onDetails={openDr} /></DashboardPanel>
          <DashboardPanel title="Synchronization & recovery activity" className="hbdr-dashboard-scope-activity" detailAction={openOperations}>
            <div className="hbdr-dashboard-scope-activity-controls"><p>{cluster?.name || 'No cluster selected'} · {ranges[rangeIndex].label}</p><div className="hbdr-dashboard-range-wrap" ref={rangeRef}><button type="button" className={`hbdr-dashboard-design-range ${rangeOpen ? 'is-open' : ''}`} aria-haspopup="listbox" aria-expanded={rangeOpen} onClick={() => setRangeOpen(open => !open)}>{ranges[rangeIndex].label}<ChevronDown size={13} /></button>{rangeOpen && <div className="hbdr-dashboard-range-menu" role="listbox" aria-label="Activity time range">{ranges.map((range, index) => <button type="button" key={range.days} role="option" aria-selected={rangeIndex === index} className={rangeIndex === index ? 'is-active' : ''} onClick={() => { setRangeIndex(index); setRangeOpen(false); }}>{range.label}</button>)}</div>}</div></div>
            <div className="hbdr-dashboard-scope-history" aria-live="polite"><div><h3>Synchronization</h3><ScopeLine label="Total runs" value={backupHistory.total} /><ScopeLine label="Succeeded / Failed / Running" value={`${backupHistory.succeeded} / ${backupHistory.failed} / ${backupHistory.running}`} />{Boolean(backupHistory.canceled + backupHistory.unknown) && <ScopeLine label="Canceled / Other" value={`${backupHistory.canceled} / ${backupHistory.unknown}`} />}</div><div><h3>Recovery & drills</h3><ScopeLine label="Total / Success rate" value={`${recoveryCounts.total} / ${recoverySuccess}`} /><ScopeLine label="Drill / Restore / Takeover" value={`${recoveryHistory.filter(task => task.type === 'drill').length} / ${recoveryHistory.filter(task => task.type === 'restore').length} / ${recoveryHistory.filter(task => task.type === 'takeover').length}`} /><ScopeLine label="Running / Failed" value={`${recoveryCounts.running} / ${recoveryCounts.failed}`} /></div></div>
            <div className="hbdr-dashboard-scope-events"><h3>Recent activity</h3>{events.length ? <table aria-label="Recent cluster activity"><thead><tr><th>Task</th><th>Status</th><th>Time</th></tr></thead><tbody>{events.map(task => <tr key={task.id}><td>{taskTypeLabel(task.type)}</td><td><span className={`hbdr-dashboard-task-state ${failedTask(task.status) ? 'is-failed' : activeTask(task.status) ? 'is-running' : ''}`}>{failedTask(task.status) ? <AlertCircle size={13} /> : activeTask(task.status) ? <History size={13} /> : <CheckCircle2 size={13} />}{taskStatusLabel(task.status)}</span></td><td>{formatDateTime(taskTime(task))}</td></tr>)}</tbody></table> : <ScopeEmpty label="No task events in the selected range" />}</div>
          </DashboardPanel>
        </section>
      </div>
    </div>
  );
}

function ProtectionGap({ icon: Icon, label, value, unit }: { icon: typeof AlertCircle; label: string; value: number; unit: string }) {
  return <div className={`hbdr-dashboard-protection-gap ${value > 0 ? 'has-gap' : ''}`}><span><Icon size={14} aria-hidden="true" />{label}</span><span><strong>{value}</strong><small>{unit}</small></span></div>;
}

function ScopeValue({ value, label }: { value: number | string; label: string }) { return <div className="hbdr-dashboard-big-number"><strong title={String(value)}>{value}</strong><span>{label}</span></div>; }
function ScopeLine({ label, value }: { label: string; value: number | string }) { return <div className="hbdr-dashboard-scope-line"><span>{label}</span><strong>{value}</strong></div>; }
function ScopeEmpty({ label }: { label: string }) { return <div className="hbdr-dashboard-empty-list"><History size={18} /><p>{label}</p></div>; }
function IssueList({ items, empty, onDetails }: { items: string[]; empty: string; onDetails: () => void }) { return items.length ? <div className="hbdr-dashboard-alert-list">{items.map(item => <button type="button" className="hbdr-dashboard-scope-issue" key={item} onClick={onDetails}><AlertCircle size={14} /><span>{item}</span><ChevronDown size={13} /></button>)}</div> : <div className="hbdr-dashboard-empty-list hbdr-dashboard-empty-list-compact"><CheckCircle2 size={18} /><p>{empty}</p></div>; }

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
