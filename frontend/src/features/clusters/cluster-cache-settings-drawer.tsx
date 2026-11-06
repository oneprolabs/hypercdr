import { useEffect, useRef, useState } from 'react';
import { apiGet, apiPut } from '../../api/client';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';

type Settings = { enabled: boolean; maxGiB: number };
type CacheTask = { id: string; status: string; errorMessage?: string };
type Response = { settings: Settings; task: CacheTask | null };
const active = (task: CacheTask | null) => !!task && !['succeeded', 'failed', 'cancelled'].includes(task.status);

export function ClusterCacheSettingsDrawer({ clusterId, name, onClose, onSaved }: { clusterId: string; name: string; onClose: () => void; onSaved: () => void }) {
  const [settings, setSettings] = useState<Settings>({ enabled: true, maxGiB: 5 });
  const [initialSettings, setInitialSettings] = useState<Settings | null>(null);
  const submittedTask = useRef<string | null>(null);
  const [task, setTask] = useState<CacheTask | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const path = `/api/v1/clusters/${encodeURIComponent(clusterId)}/cache-settings`;
  useEffect(() => {
    let cancelled = false;
    apiGet<Response>(path).then(result => { if (!cancelled) { setSettings(result.settings); setInitialSettings(result.settings); setTask(result.task); } }).catch(reason => { if (!cancelled) setError(String(reason.message || reason)); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [path]);
  useEffect(() => {
    if (!active(task)) return;
    let cancelled = false;
    const timer = window.setInterval(() => {
      apiGet<Response>(path).then(result => { if (!cancelled) { setTask(result.task); setError(''); } }).catch(reason => { if (!cancelled) setError(String(reason.message || reason)); });
    }, 2000);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [path, task?.id, task?.status]);
  useEffect(() => {
    if (!task || task.id !== submittedTask.current || active(task)) return;
    submittedTask.current = null;
    if (task.status === 'succeeded') {
      setInitialSettings(settings);
      onSaved();
    }
    else setError(task.errorMessage || 'Cache settings could not be applied. Please try again.');
  }, [task, settings, onSaved]);
  const busy = loading || saving || active(task);
  const valid = Number.isInteger(settings.maxGiB) && settings.maxGiB >= 1 && settings.maxGiB <= 1024;
  const dirty = !!initialSettings && (settings.enabled !== initialSettings.enabled || settings.maxGiB !== initialSettings.maxGiB);
  const submit = async () => {
    if (busy || !valid || !dirty) return;
    setSaving(true); setError('');
    try {
      const result = await apiPut<Response>(path, settings);
      submittedTask.current = result.task?.id || null;
      setTask(result.task);
      if (!result.task) setError('Unable to confirm application status. Please reopen settings to check.');
    } catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setSaving(false); }
  };
  return <ResourceCreateDrawer title="Cache settings" kind="cluster" className="hbdr-cache-settings" onClose={() => { if (!saving) onClose(); }} actions={<><button type="button" className="cache-cancel" onClick={onClose} disabled={saving}>Cancel</button><button type="button" className="is-primary cache-save" disabled={busy || !valid || !dirty} onClick={() => submit()}>{saving || active(task) ? 'Saving…' : 'Save'}</button></>}>
    <div className="hbdr-cache-form">
      <p className="text-slate-500">{name}</p>
      {loading ? <p role="status">Loading cache settings…</p> : <>
        <div className="hbdr-cache-row"><span>Cache persistence</span><div className="hbdr-cache-toggle"><span>{settings.enabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Cache persistence" aria-checked={settings.enabled} disabled={busy} onClick={() => setSettings({ ...settings, enabled: !settings.enabled })}><span /></button></div></div>
        <label className="hbdr-cache-row"><span>Cache size per node</span><span className="hbdr-cache-size"><input aria-label="Cache size per node" type="number" min={1} max={1024} step={1} value={Number.isNaN(settings.maxGiB) ? '' : settings.maxGiB} disabled={busy || !settings.enabled} onChange={event => setSettings({ ...settings, maxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label>
        {!valid && <p role="alert" className="text-xs text-rose-600">Enter a whole number from 1 to 1024.</p>}
        {active(task) && <span role="status" className="sr-only">Applying cache settings…</span>}
      </>}
      {error && <p role="alert" className="text-xs text-rose-600">{error}</p>}
    </div>
  </ResourceCreateDrawer>;
}
