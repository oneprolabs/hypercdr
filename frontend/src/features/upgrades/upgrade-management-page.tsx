import { useCallback, useEffect, useState } from 'react';
import { Plus, ShieldCheck, Upload } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiGet, apiPost } from '../../api/client';
import { ModalFrame } from '../../components/modal-frame';
import { SearchBar } from '../../components/search-bar';

type ApiList<T> = { items: T[] };
type ApiComponentRelease = { id:string; component:'comm-agent'|'velero'; version:string; image:string; imageDigest:string; status:'candidate'|'active'|'retired'; releaseNotes?:string; publishedBy?:string; publishedAt?:string; createdAt:string; updatedAt:string };
type ApiComponentDiscovery = { component:'comm-agent'|'velero'; registry:string; repository:string; tags:string[] };
type ApiPlatformVersion = { version:string; gitCommit:string; buildTime:string; databaseSchemaVersion:string; deployMode:string };
type ApiPlatformRelease = { id:string; version:string; apiImage:string; apiImageDigest:string; frontendImage:string; frontendImageDigest:string; databaseSchemaVersion:string; minimumAgentVersion?:string; rollbackSupported:boolean; releaseNotes?:string; status:'candidate'|'active'|'retired'; publishedBy?:string; publishedAt?:string; createdAt:string };
type ApiPlatformUpgrade = { id:string; releaseId:string; fromVersion:string; targetVersion:string; status:string; step:string; progress:number; errorMessage?:string; backupPath?:string; createdAt:string; completedAt?:string };
type ApiPlatformPrecheck = { passed:boolean; currentVersion:string; checks:Array<{id:string;label:string;passed:boolean;blocking?:boolean;detail?:unknown}> };
const listItems = <T,>(response:ApiList<T>) => response.items || [];
const shortDigest = (digest?:string) => (digest || '').replace(/^sha256:/, '').slice(0, 12);

export default function UpgradeManagementPage({ isAdmin, toast, refreshPlatformData }: { isAdmin: boolean; toast: (message: string) => void; refreshPlatformData: () => Promise<unknown> }) {
  const [releases, setReleases] = useState<ApiComponentRelease[]>([]);
  const [discovery, setDiscovery] = useState<Partial<Record<'comm-agent' | 'velero', ApiComponentDiscovery>>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [candidateComponent, setCandidateComponent] = useState<'comm-agent' | 'velero'>('comm-agent');
  const [candidateImage, setCandidateImage] = useState('');
  const [candidateVersion, setCandidateVersion] = useState('');
  const [candidateNotes, setCandidateNotes] = useState('');
  const [candidateOpen, setCandidateOpen] = useState(false);
  const [publishTarget, setPublishTarget] = useState<ApiComponentRelease | null>(null);
  const [platformVersion, setPlatformVersion] = useState<ApiPlatformVersion | null>(null);
  const [platformReleases, setPlatformReleases] = useState<ApiPlatformRelease[]>([]);
  const [platformUpgrades, setPlatformUpgrades] = useState<ApiPlatformUpgrade[]>([]);
  const [platformVersions, setPlatformVersions] = useState<string[]>([]);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const response = await apiGet<ApiList<ApiComponentRelease>>('/api/v1/component-releases');
      setReleases(listItems(response));
      const [runtime, platformReleaseRes, platformUpgradeRes] = await Promise.all([apiGet<ApiPlatformVersion>('/api/v1/platform/version'), apiGet<ApiList<ApiPlatformRelease>>('/api/v1/platform/releases'), apiGet<ApiList<ApiPlatformUpgrade>>('/api/v1/platform/upgrades')]);
      setPlatformVersion(runtime); setPlatformReleases(listItems(platformReleaseRes)); setPlatformUpgrades(listItems(platformUpgradeRes));
      try { const found = await apiGet<{versions:string[]}>('/api/v1/platform/releases/discover'); setPlatformVersions(found.versions || []); } catch { setPlatformVersions([]); }
      const discovered = await Promise.all((['comm-agent', 'velero'] as const).map(async component => {
        try { return await apiGet<ApiComponentDiscovery>(`/api/v1/component-releases/discover?component=${component}`); }
        catch { return null; }
      }));
      const next: Partial<Record<'comm-agent' | 'velero', ApiComponentDiscovery>> = {};
      discovered.forEach(item => { if (item) next[item.component] = item; });
      setDiscovery(next);
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

  const openCandidate = (component: 'comm-agent' | 'velero', tag?: string) => {
    const source = discovery[component];
    setCandidateComponent(component);
    setCandidateVersion(tag || '');
    setCandidateImage(tag && source ? `${source.registry}/${source.repository}:${tag}` : '');
    setCandidateNotes('');
    setCandidateOpen(true);
  };

  const createCandidate = async () => {
    if (!candidateImage.trim()) { toast('Candidate image is required'); return; }
    setBusy('create');
    try {
      await apiPost<ApiComponentRelease>('/api/v1/component-releases', { component: candidateComponent, version: candidateVersion.trim(), image: candidateImage.trim(), releaseNotes: candidateNotes.trim() });
      setCandidateOpen(false);
      toast('Candidate image validated and registered');
      await load();
    } catch (error) { toast(error instanceof Error ? error.message : 'Candidate validation failed'); }
    finally { setBusy(''); }
  };

  const publish = async () => {
    if (!publishTarget) return;
    setBusy(publishTarget.id);
    try {
      await apiPost<ApiComponentRelease>(`/api/v1/component-releases/${publishTarget.id}/activate`, {});
      toast(`${publishTarget.version} is now the ${publishTarget.component} target release`);
      setPublishTarget(null);
      await Promise.all([load(), refreshPlatformData()]);
    } catch (error) { toast(error instanceof Error ? error.message : 'Release publish failed'); }
    finally { setBusy(''); }
  };

  const registerPlatformRelease = async (version: string) => { setBusy(`platform-${version}`); try { await apiPost('/api/v1/platform/releases',{version,databaseSchemaVersion:'000009',minimumAgentVersion:'v20260721.4',rollbackSupported:true,releaseNotes:`HyperCDR platform ${version}`});toast(`${version} platform package validated`);await load(); } catch(error){toast(error instanceof Error?error.message:'Platform package validation failed')} finally{setBusy('')} };
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

  const componentPanel = (component: 'comm-agent' | 'velero', title: string) => {
    const items = releases.filter(item => item.component === component);
    const active = items.find(item => item.status === 'active');
    const tags = discovery[component]?.tags || [];
    // The summary uses validated product versions from component releases.
    // Raw Harbor tags may contain build/source suffixes and are only discovery inputs.
    const availableVersions = Array.from(new Set(items.map(item => item.version).filter(Boolean))).sort((left, right) => compareReleaseVersions(right, left));
    const latestVersion = availableVersions[0] || active?.version || '';
    const newerAvailable = Boolean(active && latestVersion && compareReleaseVersions(latestVersion, active.version) > 0);
    const latestCandidate = newerAvailable ? items.find(item => item.version === latestVersion && item.status === 'candidate') : undefined;
    return (
      <div className="border-b border-slate-100 last:border-b-0">
        <div className="grid items-center gap-3 px-5 py-4 md:grid-cols-[minmax(180px,1.2fr)_minmax(150px,1fr)_minmax(150px,1fr)_120px]">
          <strong className="text-sm text-slate-900">{title}</strong>
          <div><span className="block text-[10px] font-bold uppercase tracking-wider text-slate-400">Published version</span><span className="mt-1 block text-sm font-semibold text-slate-700">{active?.version || 'Not published'}</span></div>
          <div><span className="block text-[10px] font-bold uppercase tracking-wider text-slate-400">Latest available</span><span className="mt-1 block text-sm font-bold text-slate-900">{latestVersion || 'Not available'}</span></div>
          {!active
            ? <span className="justify-self-start rounded-full bg-amber-50 px-2.5 py-1 text-[10px] font-black text-amber-700 md:justify-self-end">Not published</span>
            : newerAvailable && latestCandidate
              ? <button type="button" onClick={() => setPublishTarget(latestCandidate)} className="justify-self-start rounded bg-blue-600 px-4 py-2 text-xs font-bold text-white hover:bg-blue-700 md:justify-self-end">Publish</button>
              : newerAvailable
                ? <span className="justify-self-start rounded-full bg-blue-50 px-2.5 py-1 text-[10px] font-black text-blue-700 md:justify-self-end">Available</span>
                : <span className="justify-self-start rounded-full bg-emerald-50 px-2.5 py-1 text-[10px] font-black text-emerald-700 md:justify-self-end">Up to date</span>}
        </div>
        {advancedOpen && <><div className="border-t border-slate-100 px-5 py-4">
          <div className="rounded-lg border border-slate-200 bg-white p-4"><div className="flex items-center justify-between"><h4 className="text-xs font-black text-slate-800">Available versions</h4><button type="button" onClick={() => openCandidate(component)} className="text-[10px] font-bold text-blue-600"><Plus size={12} className="mr-1 inline" />Register version</button></div>
            <div className="mt-3 flex max-h-32 flex-wrap gap-2 overflow-auto">
              {tags.slice(0, 20).map(tag => <button key={tag} type="button" onClick={() => openCandidate(component, tag)} className="rounded border border-slate-200 bg-slate-50 px-2 py-1 font-mono text-[10px] font-semibold text-slate-600 hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700">{tag}</button>)}
              {!tags.length && <p className="text-xs text-slate-400">Harbor discovery is temporarily unavailable. A full image can still be registered manually.</p>}
            </div>
          </div>
        </div>
        <div className="border-t border-slate-100 px-5 py-4">
          <h4 className="mb-3 text-xs font-black text-slate-800">Registered versions</h4>
          <div className="space-y-2">
            {items.map(item => <div key={item.id} className="grid items-center gap-3 rounded border border-slate-100 bg-white px-3 py-2.5 md:grid-cols-[120px_1fr_110px_120px]">
              <strong className="text-xs text-slate-800">{item.version}</strong><span className="truncate font-mono text-[10px] text-slate-400" title={item.image}>{item.image}</span><span className={`w-fit rounded-full px-2 py-0.5 text-[9px] font-black uppercase ${item.status === 'active' ? 'bg-emerald-50 text-emerald-700' : item.status === 'candidate' ? 'bg-blue-50 text-blue-700' : 'bg-slate-100 text-slate-500'}`}>{item.status}</span>
              {item.status !== 'active' ? <button type="button" onClick={() => setPublishTarget(item)} className="rounded border border-blue-200 px-2 py-1 text-[10px] font-bold text-blue-700 hover:bg-blue-50">Set as target</button> : <span className="text-right text-[10px] font-semibold text-slate-400">{item.publishedBy || 'system'}</span>}
            </div>)}
            {!items.length && <p className="py-4 text-center text-xs text-slate-400">{loading ? 'Loading releases...' : 'No releases registered.'}</p>}
          </div>
        </div></>}
      </div>
    );
  };

  const compareReleaseVersions = (left: string, right: string) => left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' });
  const currentPlatformVersion = platformVersion?.version || '';
  const latestPlatformRelease = platformReleases
    .filter(item => !currentPlatformVersion || compareReleaseVersions(item.version, currentPlatformVersion) > 0)
    .sort((left, right) => compareReleaseVersions(right.version, left.version))[0]
    || platformReleases.find(item => item.version === currentPlatformVersion);
  const platformUpdateAvailable = Boolean(latestPlatformRelease && currentPlatformVersion && compareReleaseVersions(latestPlatformRelease.version, currentPlatformVersion) > 0);
  const activePlatformUpgrade = platformUpgrades.find(job => !['succeeded', 'failed', 'cancelled', 'rolled_back'].includes(job.status));

  return (
    <motion.div key="upgrades" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Upgrade" desc="Check versions, start upgrades, and follow their progress." action="Refresh" onAction={() => void load()} />
      <section className="hbdr-section-card overflow-hidden">
        <div className="hbdr-section-toolbar"><div><h3>Platform</h3><p>HyperCDR management platform</p></div></div>
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
        {advancedOpen && <><div className="border-t border-slate-100 px-5 py-4"><h4 className="mb-3 text-xs font-black">Available Harbor versions</h4><div className="flex flex-wrap gap-2">{platformVersions.slice(0,20).map(version=><button key={version} disabled={busy===`platform-${version}`} onClick={()=>void registerPlatformRelease(version)} className="rounded border border-slate-200 bg-slate-50 px-2 py-1 text-[10px] font-semibold hover:border-indigo-200 hover:bg-indigo-50">{version}</button>)}{!platformVersions.length&&<p className="text-xs text-slate-400">No matching platform versions found.</p>}</div></div><div className="border-t border-slate-100 px-5 py-4"><h4 className="mb-3 text-xs font-black">Registered platform versions</h4><div className="space-y-2">{platformReleases.map(release=><div key={release.id} className="flex items-center justify-between rounded border border-slate-100 px-3 py-2.5"><strong className="text-xs">{release.version}</strong><span className="text-[10px] font-bold uppercase text-slate-400">{release.status}</span></div>)}</div></div></>}
      </section>
      <section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>Cluster Components</h3><p>Versions reported by registered clusters</p></div></div>{componentPanel('comm-agent', 'Comm Agent')}{componentPanel('velero', 'Velero Agent')}</section>
      {platformUpgrades.length>0&&<section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>Upgrade History</h3><p>Recent platform upgrade results.</p></div></div><div className="divide-y divide-slate-100 px-5">{platformUpgrades.slice(0,5).map(job=><div key={job.id} className="flex items-center justify-between py-3 text-xs"><span>{job.fromVersion} → <strong>{job.targetVersion}</strong></span><span className={job.status==='succeeded'?'font-bold text-emerald-700':job.status==='failed'?'font-bold text-rose-700':'text-slate-500'}>{job.status==='succeeded'?'Succeeded':job.status==='failed'?'Failed':`${job.progress}%`}</span></div>)}</div></section>}
      <AnimatePresence>{candidateOpen && <ModalFrame title="Register Candidate Release" subtitle="The platform validates the image in Harbor and records its immutable digest." icon={<Upload size={18} />} onClose={() => setCandidateOpen(false)}>
        <div className="space-y-4"><label className="block text-xs font-bold text-slate-600">Component<select value={candidateComponent} onChange={event => setCandidateComponent(event.target.value as 'comm-agent' | 'velero')} className="mt-1 h-10 w-full rounded border border-slate-200 px-3"><option value="comm-agent">Comm Agent</option><option value="velero">Velero Agent</option></select></label><label className="block text-xs font-bold text-slate-600">Version<input value={candidateVersion} onChange={event => setCandidateVersion(event.target.value)} className="mt-1 h-10 w-full rounded border border-slate-200 px-3" placeholder="v20260722.1" /></label><label className="block text-xs font-bold text-slate-600">Full image<input value={candidateImage} onChange={event => setCandidateImage(event.target.value)} className="mt-1 h-10 w-full rounded border border-slate-200 px-3 font-mono text-xs" placeholder="registry/hypercdr/comm-agent:v20260722.1" /></label><label className="block text-xs font-bold text-slate-600">Release notes<textarea value={candidateNotes} onChange={event => setCandidateNotes(event.target.value)} className="mt-1 min-h-20 w-full rounded border border-slate-200 p-3 text-xs" /></label><div className="flex justify-end gap-2"><button onClick={() => setCandidateOpen(false)} className="rounded px-4 py-2 text-xs font-bold text-slate-500">Cancel</button><button disabled={busy === 'create'} onClick={() => void createCandidate()} className="rounded bg-blue-600 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">{busy === 'create' ? 'Validating...' : 'Validate & Register'}</button></div></div>
      </ModalFrame>}</AnimatePresence>
      <AnimatePresence>{publishTarget && <ModalFrame title="Publish Target Release" subtitle="This changes the upgrade target immediately but never upgrades clusters automatically." icon={<ShieldCheck size={18} />} onClose={() => setPublishTarget(null)}>
        <div className="space-y-4"><div className="rounded border border-blue-100 bg-blue-50 p-4"><strong className="text-sm text-blue-900">{publishTarget.component} · {publishTarget.version}</strong><p className="mt-1 break-all text-xs text-blue-700">{publishTarget.image}</p><p className="mt-2 font-mono text-[10px] text-blue-500">sha256:{shortDigest(publishTarget.imageDigest)}</p></div><p className="text-xs leading-5 text-slate-600">Eligible clusters will display Update after publication. Users must confirm each cluster upgrade. Existing tasks keep their original target snapshot.</p><div className="flex justify-end gap-2"><button onClick={() => setPublishTarget(null)} className="rounded px-4 py-2 text-xs font-bold text-slate-500">Cancel</button><button disabled={busy === publishTarget.id} onClick={() => void publish()} className="rounded bg-blue-600 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">{busy === publishTarget.id ? 'Publishing...' : 'Publish Target'}</button></div></div>
      </ModalFrame>}</AnimatePresence>
    </motion.div>
  );
}
