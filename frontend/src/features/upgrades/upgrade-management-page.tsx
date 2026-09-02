import { useCallback, useEffect, useState } from 'react';
import { motion } from 'motion/react';
import { apiGet, apiPost } from '../../api/client';
import { SearchBar } from '../../components/search-bar';

type ApiList<T> = { items: T[] };
type ApiReleaseComponent = { version:string; image:string; imageDigest:string };
type ApiPlatformVersion = { version:string; gitCommit:string; buildTime:string; databaseSchemaVersion:string; deployMode:string };
type ApiPlatformRelease = { id:string; version:string; apiImage:string; apiImageDigest:string; frontendImage:string; frontendImageDigest:string; componentManifest:Record<string,ApiReleaseComponent>; databaseSchemaVersion:string; minimumAgentVersion?:string; rollbackSupported:boolean; releaseNotes?:string; status:'candidate'|'active'|'retired'; publishedBy?:string; publishedAt?:string; createdAt:string };
type ApiPlatformUpgrade = { id:string; releaseId:string; fromVersion:string; targetVersion:string; status:string; step:string; progress:number; errorMessage?:string; backupPath?:string; createdAt:string; completedAt?:string };
type ApiPlatformPrecheck = { passed:boolean; currentVersion:string; checks:Array<{id:string;label:string;passed:boolean;blocking?:boolean;detail?:unknown}> };
const listItems = <T,>(response:ApiList<T>) => response.items || [];
const shortDigest = (digest?:string) => (digest || '').replace(/^sha256:/, '').slice(0, 12);

export default function UpgradeManagementPage({ isAdmin, toast, refreshPlatformData }: { isAdmin: boolean; toast: (message: string) => void; refreshPlatformData: () => Promise<unknown> }) {
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [platformVersion, setPlatformVersion] = useState<ApiPlatformVersion | null>(null);
  const [platformReleases, setPlatformReleases] = useState<ApiPlatformRelease[]>([]);
  const [platformUpgrades, setPlatformUpgrades] = useState<ApiPlatformUpgrade[]>([]);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [runtime, platformReleaseRes, platformUpgradeRes] = await Promise.all([apiGet<ApiPlatformVersion>('/api/v1/platform/version'), apiGet<ApiList<ApiPlatformRelease>>('/api/v1/platform/releases'), apiGet<ApiList<ApiPlatformUpgrade>>('/api/v1/platform/upgrades')]);
      setPlatformVersion(runtime); setPlatformReleases(listItems(platformReleaseRes)); setPlatformUpgrades(listItems(platformUpgradeRes));
    } catch (error) {
      toast(error instanceof Error ? error.message : 'Failed to load component releases');
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    let cancelled = false;
    const pollUpgradeState = async () => {
      try {
        const [runtime, releaseRes, upgradeRes] = await Promise.all([
          apiGet<ApiPlatformVersion>('/api/v1/platform/version'),
          apiGet<ApiList<ApiPlatformRelease>>('/api/v1/platform/releases'),
          apiGet<ApiList<ApiPlatformUpgrade>>('/api/v1/platform/upgrades'),
        ]);
        if (cancelled) return;
        setPlatformVersion(runtime);
        setPlatformReleases(listItems(releaseRes));
        setPlatformUpgrades(listItems(upgradeRes));
      } catch {
        // The API is expected to be briefly unavailable while its container is replaced.
        // Keep retrying so progress resumes without requiring a browser refresh.
      }
    };
    const timer = window.setInterval(pollUpgradeState, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  const startPlatformUpgrade = async (release: ApiPlatformRelease) => {
    setBusy(`upgrade-${release.id}`);
    try {
      const precheck = await apiGet<ApiPlatformPrecheck>(`/api/v1/platform/upgrades/precheck?releaseId=${release.id}`);
      const blocked = precheck.checks.filter(check => !check.passed && check.blocking !== false);
      if (blocked.length > 0) {
        const message = blocked.map(check => {
          if (check.id === 'tasks') return `Upgrade blocked: ${String(check.detail ?? 0)} active DR task(s). Stop them or wait for completion, then retry.`;
          if (check.id === 'release') return 'Upgrade blocked: the target release is not registered.';
          if (check.id === 'mode') return 'Upgrade blocked: in-place upgrade requires formal deployment mode.';
          if (check.id === 'version') return 'Upgrade blocked: select a version different from the running version.';
          return `Upgrade blocked: ${check.label}.`;
        }).join(' ');
        toast(message);
        return;
      }
      await apiPost('/api/v1/platform/upgrades', { releaseId: release.id });
      toast(`Upgrading the platform to ${release.version}. Management services may be briefly unavailable.`);
      await load();
    } catch (error) {
      toast(error instanceof Error ? error.message : 'Platform upgrade could not start');
    } finally {
      setBusy('');
    }
  };

  const compareReleaseVersions = (left: string, right: string) => left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' });
  const currentPlatformVersion = platformVersion?.version || '';
  const latestPlatformRelease = platformReleases
    .filter(item => !currentPlatformVersion || compareReleaseVersions(item.version, currentPlatformVersion) > 0)
    .sort((left, right) => compareReleaseVersions(right.version, left.version))[0]
    || platformReleases.find(item => item.version === currentPlatformVersion);
  const platformUpdateAvailable = Boolean(latestPlatformRelease && currentPlatformVersion && compareReleaseVersions(latestPlatformRelease.version, currentPlatformVersion) > 0);
  const activePlatformUpgrade = platformUpgrades.find(job => !['succeeded', 'failed', 'cancelled', 'rolled_back'].includes(job.status));
  const currentRelease = platformReleases.find(item => item.status === 'active') || platformReleases.find(item => item.version === currentPlatformVersion);
  const displayedManifest = (platformUpdateAvailable ? latestPlatformRelease : currentRelease)?.componentManifest || {};
  const clusterComponents = ['comm-agent', 'velero', 'velero-plugin-for-aws', 'velero-plugin-for-microsoft-azure', 'velero-plugin-for-gcp'];

  return (
    <motion.div key="upgrades" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Upgrade" desc="Check versions, start upgrades, and follow their progress." action="Refresh" onAction={() => void load()} />
      <section className="hbdr-section-card overflow-hidden">
        <div className="hbdr-section-toolbar"><div><h3>Platform</h3><p>HyperCDR management platform</p></div>{isAdmin && <button type="button" className="hbdr-dr-action-secondary" aria-expanded={advancedOpen} onClick={() => setAdvancedOpen(value => !value)}>{advancedOpen ? 'Hide Release Management' : 'Release Management'}</button>}</div>
        <div className="grid items-center gap-3 px-5 py-4 md:grid-cols-[minmax(180px,1.2fr)_minmax(150px,1fr)_minmax(150px,1fr)_120px]">
          <strong className="text-sm text-slate-900">HyperCDR Platform</strong>
          <div><span className="block text-[10px] font-bold uppercase tracking-wider text-slate-400">Current version</span><span className="mt-1 block text-sm font-semibold text-slate-700">{platformVersion?.version || 'Unknown'}</span></div>
          <div><span className="block text-[10px] font-bold uppercase tracking-wider text-slate-400">Latest version</span><span className="mt-1 block text-sm font-bold text-slate-900">{platformUpdateAvailable ? latestPlatformRelease?.version : platformVersion?.version || latestPlatformRelease?.version || 'Not available'}</span></div>
          {activePlatformUpgrade
            ? <span className="justify-self-start rounded-full bg-blue-50 px-3 py-1.5 text-[10px] font-black text-blue-700 md:justify-self-end">Upgrading...</span>
            : isAdmin && latestPlatformRelease && platformUpdateAvailable
              ? <button type="button" disabled={busy===`upgrade-${latestPlatformRelease.id}`} onClick={()=>void startPlatformUpgrade(latestPlatformRelease)} className="justify-self-start rounded bg-blue-600 px-4 py-2 text-xs font-bold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50 md:justify-self-end">{busy===`upgrade-${latestPlatformRelease.id}` ? 'Starting...' : 'Upgrade'}</button>
              : <span className="justify-self-start rounded-full bg-emerald-50 px-3 py-1.5 text-[10px] font-black text-emerald-700 md:justify-self-end">Up to date</span>}
        </div>
        {activePlatformUpgrade && <div className="border-t border-slate-100 px-5 py-4"><div className="flex items-center justify-between text-xs"><strong>{activePlatformUpgrade.fromVersion} → {activePlatformUpgrade.targetVersion}</strong><span className="text-slate-500">{activePlatformUpgrade.progress}%</span></div><div className="mt-2 h-2 overflow-hidden rounded-full bg-slate-100"><div className="h-full rounded-full bg-blue-600 transition-all" style={{width:`${activePlatformUpgrade.progress}%`}} /></div><p className="mt-2 text-xs text-slate-500">{activePlatformUpgrade.step}</p></div>}
        {advancedOpen && <div className="border-t border-slate-100 px-5 py-4"><div className="mb-3"><h4 className="text-xs font-black text-slate-800">Registered HyperCDR releases</h4><p className="mt-1 text-xs text-slate-500">Release packages are registered by the build pipeline with a complete immutable component manifest.</p></div><div className="space-y-2">{platformReleases.map(release=><div key={release.id} className="rounded-lg border border-slate-100 bg-white px-3 py-3"><div className="flex items-center justify-between"><strong className="text-xs text-slate-800">HyperCDR {release.version}</strong><span className={`rounded-full px-2 py-0.5 text-[9px] font-black uppercase ${release.status === 'active' ? 'bg-emerald-50 text-emerald-700' : release.status === 'candidate' ? 'bg-blue-50 text-blue-700' : 'bg-slate-100 text-slate-500'}`}>{release.status}</span></div><div className="mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{Object.entries(release.componentManifest || {}).map(([name, component])=><div key={name} className="min-w-0 rounded-md bg-slate-50 px-2.5 py-2"><span className="block truncate text-[10px] font-bold text-slate-600">{name}</span><span className="mt-0.5 block truncate text-[10px] text-slate-500">{component.version}</span><span className="mt-0.5 block font-mono text-[9px] text-slate-400">sha256:{shortDigest(component.imageDigest)}</span></div>)}</div></div>)}{!platformReleases.length&&<p className="py-4 text-center text-xs text-slate-400">{loading ? 'Loading releases…' : 'No complete release package has been registered.'}</p>}</div></div>}
      </section>
      <section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>Cluster Components</h3><p>{platformUpdateAvailable ? `Included with HyperCDR ${latestPlatformRelease?.version}` : `Included with the current HyperCDR release`}</p></div></div><div className="divide-y divide-slate-100">{clusterComponents.map(name => { const component=displayedManifest[name]; return <div key={name} className="grid items-center gap-3 px-5 py-3.5 md:grid-cols-[minmax(220px,1fr)_160px_minmax(260px,1.5fr)]"><strong className="text-xs text-slate-800">{name}</strong><span className="text-xs font-semibold text-slate-600">{component?.version || 'Not included'}</span><div className="min-w-0"><span className="block truncate font-mono text-[10px] text-slate-400">{component?.image || 'No image in release manifest'}</span>{component?.imageDigest&&<span className="mt-0.5 block font-mono text-[9px] text-slate-400">sha256:{shortDigest(component.imageDigest)}</span>}</div></div>})}</div></section>
      {platformUpgrades.length>0&&<section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>Upgrade History</h3><p>Recent platform upgrade results.</p></div></div><div className="divide-y divide-slate-100 px-5">{platformUpgrades.slice(0,5).map(job=><div key={job.id} className="flex items-center justify-between py-3 text-xs"><span>{job.fromVersion} → <strong>{job.targetVersion}</strong></span><span className={job.status==='succeeded'?'font-bold text-emerald-700':job.status==='failed'?'font-bold text-rose-700':'text-slate-500'}>{job.status==='succeeded'?'Succeeded':job.status==='failed'?'Failed':`${job.progress}%`}</span></div>)}</div></section>}
    </motion.div>
  );
}
