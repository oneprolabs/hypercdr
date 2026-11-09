import { useEffect, useRef, useState } from 'react';
import { apiGet, apiPut } from '../../api/client';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';

type Settings = { enabled: boolean; maxGiB: number };
type CacheTask = { id: string; status: string; errorMessage?: string };
type Observed = Settings & { phase: string; observedAt?: string };
type Response = { settings: Settings; observed?: Observed | null; task: CacheTask | null };
const active = (task: CacheTask | null) => !!task && !['succeeded', 'failed', 'cancelled', 'canceled'].includes(task.status);
const same = (first: Settings, second: Settings) => first.enabled === second.enabled && first.maxGiB === second.maxGiB;

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
  const valid = !!settings && Number.isInteger(settings.maxGiB) && settings.maxGiB >= 1 && settings.maxGiB <= 1024;
  const dirty = !!settings && !!initialSettings && !same(settings, initialSettings);
  const submit = async () => {
    if (busy || !valid || !dirty || !settings) return;
    setSaving(true); setError('');
    const requested = { enabled: settings.enabled, maxGiB: settings.maxGiB };
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
        <div className="hbdr-cache-row"><span>Cache persistence</span><div className="hbdr-cache-toggle"><span>{settings.enabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Cache persistence" aria-checked={settings.enabled} disabled={busy || phase !== 'applied'} onClick={() => change({ ...settings, enabled: !settings.enabled })}><span /></button></div></div>
        <label className="hbdr-cache-row"><span>Cache size per node</span><span className="hbdr-cache-size"><input aria-label="Cache size per node" type="number" min={1} max={1024} step={1} value={Number.isNaN(settings.maxGiB) ? '' : settings.maxGiB} disabled={busy || phase !== 'applied' || !settings.enabled} onChange={event => change({ ...settings, maxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label>
        {!valid && <p role="alert" className="text-xs text-rose-600">Enter a whole number from 1 to 1024.</p>}
        {phase !== 'applied' && <p role="status">{phase === 'applying' ? 'Applying cache settings…' : 'Cluster configuration is unavailable.'}</p>}
      </> : <p role="status">{phase === 'applying' ? 'Applying cache settings…' : phase === 'unconfigured' ? 'Cache is not configured on this cluster.' : 'Cluster configuration is unavailable.'}</p>}
      {error && <p role="alert" className="text-xs text-rose-600">{error}</p>}
    </div>
  </ResourceCreateDrawer>;
}
