import { useEffect, useState } from 'react';
import { apiGet, apiPut } from '../../api/client';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';

type Values = { maxSyncTasks: number; nodeTransferConcurrency: number };
type Response = { settings: Values & { appliedMaxSyncTasks: number; appliedNodeTransferConcurrency: number }; phase: string; task?: { errorMessage?: string } | null };

export function ClusterSyncSettingsDrawer({ clusterId, name, onClose }: { clusterId: string; name: string; onClose: () => void }) {
  const [response, setResponse] = useState<Response | null>(null);
  const [values, setValues] = useState<Values>({ maxSyncTasks: 4, nodeTransferConcurrency: 4 });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const path = `/api/v1/clusters/${encodeURIComponent(clusterId)}/sync-settings`;
  useEffect(() => {
    let cancelled = false;
    let running = false;
    const refresh = async () => {
      if (running) return;
      running = true;
      try {
        const result = await apiGet<Response>(path);
        if (!cancelled) {
          setError('');
          setResponse(result);
          if (!dirty) setValues({ maxSyncTasks: result.settings.maxSyncTasks, nodeTransferConcurrency: result.settings.nodeTransferConcurrency });
        }
      } catch (cause) { if (!cancelled) setError(cause instanceof Error ? cause.message : 'Unable to load sync settings.'); }
      finally { running = false; }
    };
    void refresh();
    const timer = window.setInterval(() => { void refresh(); }, 5000);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [path, dirty]);
  const valid = Number.isInteger(values.maxSyncTasks) && values.maxSyncTasks >= 1 && values.maxSyncTasks <= 20
    && Number.isInteger(values.nodeTransferConcurrency) && values.nodeTransferConcurrency >= 1 && values.nodeTransferConcurrency <= 32;
  const save = async () => {
    setSaving(true); setError('');
    try { setResponse(await apiPut<Response>(path, values)); setDirty(false); }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to save sync settings.'); }
    finally { setSaving(false); }
  };
  const phaseLabel = response?.phase === 'applied' ? 'Applied' : response?.phase === 'applying' ? 'Applying safely…' : response?.phase === 'failed' ? 'Apply failed — save to retry' : 'Pending cluster confirmation';
  return <ResourceCreateDrawer title="Sync settings" kind="cluster" className="hbdr-cache-settings hbdr-sync-settings" onClose={onClose} actions={<><button type="button" onClick={onClose} disabled={saving}>Close</button><button type="button" className="is-primary" disabled={!response || saving || !valid || response.phase === 'applying'} onClick={() => { void save(); }}>{saving ? 'Saving…' : 'Save settings'}</button></>}>
    <div className="hbdr-cache-form">
    <div className="hbdr-sync-settings-heading"><strong>{name}</strong><span role="status">{response ? phaseLabel : 'Loading…'}</span></div>
    {error && <p role="alert" className="text-sm text-rose-600">{error}</p>}
    {response?.phase === 'failed' && <p role="alert" className="text-sm text-rose-600">{response.task?.errorMessage || 'Unable to apply settings. Save again to retry.'}</p>}
    {response && <section className="hbdr-cache-card"><div className="hbdr-cache-card-fields">
      <label className="hbdr-cache-row"><span>Concurrent sync tasks<small className="block text-xs text-slate-500">Per source cluster · extra tasks queue</small></span><span className="hbdr-cache-size"><input aria-label="Concurrent sync tasks" type="number" min={1} max={20} step={1} value={Number.isNaN(values.maxSyncTasks) ? '' : values.maxSyncTasks} disabled={saving || response.phase === 'applying'} onChange={event => { setDirty(true); setValues({ ...values, maxSyncTasks: event.target.valueAsNumber }); }} /></span></label>
      <label className="hbdr-cache-row"><span>Concurrent volume transfers<small className="block text-xs text-slate-500">Per node · shared by all tasks</small></span><span className="hbdr-cache-size"><input aria-label="Concurrent volume transfers per node" type="number" min={1} max={32} step={1} value={Number.isNaN(values.nodeTransferConcurrency) ? '' : values.nodeTransferConcurrency} disabled={saving || response.phase === 'applying'} onChange={event => { setDirty(true); setValues({ ...values, nodeTransferConcurrency: event.target.valueAsNumber }); }} /></span></label>
    </div></section>}
    <p className="text-xs text-slate-500">The task limit applies after saving. Runtime changes wait for active data tasks to finish before rolling out; running transfers are not interrupted.</p>
    <p className="text-xs text-slate-500">A DR configuration with an unfinished sync skips its next scheduled run.</p>
    </div>
  </ResourceCreateDrawer>;
}
