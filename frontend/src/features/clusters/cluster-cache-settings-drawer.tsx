import { useEffect, useRef, useState } from 'react';
import { apiGet, apiPut } from '../../api/client';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';

type Settings = { rootPath: string; indexPath: string; drillPath: string; indexEnabled: boolean; indexMaxGiB: number; drillEnabled: boolean; drillMaxGiB: number };
type CacheTask = { id: string; status: string; errorMessage?: string };
type Observed = Settings & { phase: string; observedAt?: string };
type Response = { settings: Settings; observed?: Observed | null; task: CacheTask | null };
const active = (task: CacheTask | null) => !!task && !['succeeded', 'failed', 'cancelled', 'canceled'].includes(task.status);
const same = (first: Settings, second: Settings) => JSON.stringify(first) === JSON.stringify(second);

export function ClusterCacheSettingsDrawer({ clusterId, name, onClose, onSaved }: { clusterId: string; name: string; onClose: () => void; onSaved: () => void }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [initialSettings, setInitialSettings] = useState<Settings | null>(null);
  const [task, setTask] = useState<CacheTask | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState('');
  const [phase, setPhase] = useState('unknown');
  const editing = useRef(false);
  const submitted = useRef<{ id: string; settings: Settings; at: number } | null>(null);
  const saved = useRef(onSaved);
  saved.current = onSaved;
  const path = `/api/v1/clusters/${encodeURIComponent(clusterId)}/cache-settings`;
  useEffect(() => {
    let cancelled = false;
    let running = false;
    const refresh = async () => {
      if (running) return;
      running = true;
      try {
        const result = await apiGet<Response>(path);
        if (cancelled) return;
        setTask(result.task);
        const observed = result.observed?.phase === 'applied' ? result.observed : null;
        setPhase(result.observed?.phase || 'unknown');
        if (!editing.current) {
          setSettings(observed);
          setInitialSettings(observed);
        }
        const pending = submitted.current;
        if (pending && result.task?.id === pending.id && !active(result.task)) {
          if (result.task.status !== 'succeeded') {
            setError(result.task.errorMessage || 'Cache settings could not be applied. Please try again.');
            submitted.current = null;
            setConfirming(false);
          } else if (observed && same(observed, pending.settings)) {
            setSettings(observed);
            setInitialSettings(observed);
            editing.current = false;
            submitted.current = null;
            setConfirming(false);
            setError('');
            saved.current();
          } else if (Date.now() - pending.at > 120000) {
            submitted.current = null;
            setConfirming(false);
            setError('Unable to confirm the cluster configuration. Please check the cluster connection.');
          }
        }
      } catch (reason) {
        if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason));
      } finally {
        running = false;
        if (!cancelled) setLoading(false);
      }
    };
    void refresh();
    const timer = window.setInterval(() => { void refresh(); }, 2000);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [path]);
  const busy = loading || saving || confirming || active(task) || phase !== 'applied';
  const valid = !!settings && settings.rootPath.startsWith('/') && (settings.indexPath || `${settings.rootPath}/index`).startsWith('/') && (settings.drillPath || `${settings.rootPath}/drill-data`).startsWith('/') && Number.isInteger(settings.indexMaxGiB) && settings.indexMaxGiB >= 1 && settings.indexMaxGiB <= 1024 && Number.isInteger(settings.drillMaxGiB) && settings.drillMaxGiB >= 1 && settings.drillMaxGiB <= 4096;
  const dirty = !!settings && !!initialSettings && !same(settings, initialSettings);
  const submit = async () => {
    if (busy || !valid || !dirty || !settings) return;
    setSaving(true); setError('');
    const requested = { ...settings, indexPath: settings.indexPath || `${settings.rootPath}/index`, drillPath: settings.drillPath || `${settings.rootPath}/drill-data` };
    const startedAt = Date.now();
    try {
      const result = await apiPut<Response>(path, requested);
      setTask(result.task);
      if (result.task) {
        submitted.current = { id: result.task.id, settings: requested, at: startedAt };
        setConfirming(true);
      } else setError('Unable to confirm application status. Please reopen settings to check.');
    } catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setSaving(false); }
  };
  const change = (next: Settings) => { editing.current = true; setSettings(next); };
  return <ResourceCreateDrawer title="Cache settings" kind="cluster" className="hbdr-cache-settings" onClose={() => { if (!saving) onClose(); }} actions={<><button type="button" className="cache-cancel" onClick={onClose} disabled={saving}>Cancel</button><button type="button" className="is-primary cache-save" disabled={busy || !valid || !dirty} onClick={() => submit()}>{saving || confirming || active(task) ? 'Saving…' : 'Save'}</button></>}>
    <div className="hbdr-cache-form">
      <p className="text-slate-500">{name}</p>
      {loading ? <p role="status">Loading cache settings…</p> : settings ? <>
        <section className="hbdr-cache-card" aria-labelledby="index-cache-title">
          <div className="hbdr-cache-card-header"><div><h3 id="index-cache-title">Index cache</h3><p>Kopia index data</p></div><div className="hbdr-cache-toggle"><span>{settings.indexEnabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Index cache" aria-checked={settings.indexEnabled} disabled={busy || phase !== 'applied'} onClick={() => change({ ...settings, indexEnabled: !settings.indexEnabled })}><span /></button></div></div>
          {settings.indexEnabled && <div className="hbdr-cache-card-fields"><label className="hbdr-cache-row"><span>Cache size per node</span><span className="hbdr-cache-size"><input aria-label="Index cache size per node" type="number" min={1} max={1024} step={1} value={Number.isNaN(settings.indexMaxGiB) ? '' : settings.indexMaxGiB} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, indexMaxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label><label className="hbdr-cache-row"><span>Cache path per node</span><input aria-label="Index cache path per node" value={settings.indexPath || `${settings.rootPath}/index`} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, indexPath: event.target.value })} /></label></div>}
        </section>
        <section className="hbdr-cache-card" aria-labelledby="drill-cache-title">
          <div className="hbdr-cache-card-header"><div><h3 id="drill-cache-title">Drill data cache</h3><p>Downloaded application data for Drill</p></div><div className="hbdr-cache-toggle"><span>{settings.drillEnabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Drill data cache" aria-checked={settings.drillEnabled} disabled={busy || phase !== 'applied'} onClick={() => change({ ...settings, drillEnabled: !settings.drillEnabled })}><span /></button></div></div>
          {settings.drillEnabled && <div className="hbdr-cache-card-fields"><label className="hbdr-cache-row"><span>Cache size per node</span><span className="hbdr-cache-size"><input aria-label="Drill data cache size per node" type="number" min={1} max={4096} step={1} value={Number.isNaN(settings.drillMaxGiB) ? '' : settings.drillMaxGiB} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, drillMaxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label><label className="hbdr-cache-row"><span>Cache path per node</span><input aria-label="Drill data cache path per node" value={settings.drillPath || `${settings.rootPath}/drill-data`} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, drillPath: event.target.value })} /></label></div>}
        </section>
        {!valid && <p role="alert" className="text-xs text-rose-600">Enter a whole number from 1 to 1024.</p>}
        {phase !== 'applied' && <p role="status">{phase === 'applying' ? 'Applying cache settings…' : 'Cluster configuration is unavailable.'}</p>}
      </> : <p role="status">{phase === 'applying' ? 'Applying cache settings…' : phase === 'unconfigured' ? 'Cache is not configured on this cluster.' : 'Cluster configuration is unavailable.'}</p>}
      {error && <p role="alert" className="text-xs text-rose-600">{error}</p>}
    </div>
  </ResourceCreateDrawer>;
}
