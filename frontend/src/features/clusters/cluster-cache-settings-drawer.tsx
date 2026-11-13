import { useEffect, useRef, useState } from 'react';
import { CheckCircle2, Info } from 'lucide-react';
import { apiGet, apiPut } from '../../api/client';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';
import { cacheDirectoryConfirmed, cacheDisableConfirmed, confirmedCacheObservation } from './cache-settings-state';

function CacheChangeConfirmation({ name, value, onChange, disabled, disabling }: { name: string; value: string; onChange: (value: string) => void; disabled: boolean; disabling: boolean }) {
  return <div className="hbdr-cache-disable-confirm"><p>{disabling ? 'This cache will be disabled and its cached data cleared.' : 'The old cache directory will be cleared. New tasks will use the new directory.'}</p><label><span>Type <strong>yes</strong> to confirm</span><input type="text" aria-label={disabling ? `Type yes to disable ${name}` : `Type yes to change ${name} directory`} autoComplete="off" spellCheck={false} value={value} disabled={disabled} onChange={event => onChange(event.target.value)} /></label></div>;
}

function CacheHelp({ cache }: { cache: 'index' | 'drill' }) {
  const label = cache === 'index' ? 'Index cache' : 'Drill data cache';
  return <details className="hbdr-cache-help" onKeyDown={event => {
    if (event.key === 'Escape') {
      event.currentTarget.open = false;
      event.currentTarget.querySelector('summary')?.focus();
    }
  }} onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false;
  }}>
    <summary aria-label={`About ${label}`}><Info size={14} /></summary>
    <div className="hbdr-cache-help-content">
      {cache === 'index' ? <>
        <p>Caches Kopia repository indexes to reduce repeated downloads.</p>
        <p><strong>Suggested size:</strong> Start with 5 GiB per node.</p>
      </> : <>
        <p>Caches application data downloaded during Drill for reuse in later Drills.</p>
        <p><strong>Suggested size:</strong> Set the cache size larger than the total backed-up volume data size of all applications you plan to drill, not their PVC capacity.</p>
      </>}
      <p>The configured size applies to each node, not the entire cluster.</p>
    </div>
  </details>;
}

type Settings = { rootPath: string; indexPath: string; drillPath: string; indexEnabled: boolean; indexMaxGiB: number; drillEnabled: boolean; drillMaxGiB: number };
type CacheTask = { id: string; status: string; errorCode?: string; errorMessage?: string; completedAt?: string; payload?: Record<string, unknown> };
type Observed = Settings & { phase: string; observedAt?: string };
type Response = { settings: Settings; observed?: Observed | null; task: CacheTask | null };
const active = (task: CacheTask | null) => !!task && !['succeeded', 'failed', 'cancelled', 'canceled'].includes(task.status);
const comparable = (value: Settings): Settings => ({
  rootPath: value.rootPath,
  indexPath: value.indexPath || `${value.rootPath}/index`,
  drillPath: value.drillPath || `${value.rootPath}/drill-data`,
  indexEnabled: value.indexEnabled,
  indexMaxGiB: value.indexMaxGiB,
  drillEnabled: value.drillEnabled,
  drillMaxGiB: value.drillMaxGiB,
});
const same = (first: Settings, second: Settings) => {
  const left = comparable(first);
  const right = comparable(second);
  return left.rootPath === right.rootPath
    && left.indexPath === right.indexPath
    && left.drillPath === right.drillPath
    && left.indexEnabled === right.indexEnabled
    && left.indexMaxGiB === right.indexMaxGiB
    && left.drillEnabled === right.drillEnabled
    && left.drillMaxGiB === right.drillMaxGiB;
};

export function ClusterCacheSettingsDrawer({ clusterId, name, onClose, onSaved }: { clusterId: string; name: string; onClose: () => void; onSaved: () => void }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [initialSettings, setInitialSettings] = useState<Settings | null>(null);
  const [task, setTask] = useState<CacheTask | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState('');
  const [phase, setPhase] = useState('unknown');
  const [pathEditing, setPathEditing] = useState<'index' | 'drill' | null>(null);
  const [discardConfirm, setDiscardConfirm] = useState(false);
  const [successNotice, setSuccessNotice] = useState(0);
  const [disableAnswers, setDisableAnswers] = useState({ index: '', drill: '' });
  useEffect(() => {
    if (!successNotice) return;
    const timer = window.setTimeout(() => setSuccessNotice(0), 3000);
    return () => window.clearTimeout(timer);
  }, [successNotice]);
  const editing = useRef(false);
  const submitted = useRef<{ id: string; settings: Settings; at: number; applied?: boolean } | null>(null);
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
        const effective = confirmedCacheObservation(result.settings, result.observed, result.task);
        const observed = effective?.phase === 'applied' ? effective : null;
        setPhase(effective?.phase || 'unknown');
        if (!editing.current) {
          setSettings(previous => observed || previous);
          setInitialSettings(previous => observed || previous);
        }
        const pending = submitted.current;
        if (pending && result.task?.id === pending.id) {
          if (result.task.status === 'succeeded') {
            if (!same(result.settings, pending.settings)) {
              setError('The saved settings do not match this request. Please reopen settings to check.');
              return;
            }
            setPhase('applied');
            setSettings(comparable(pending.settings));
            setInitialSettings(comparable(pending.settings));
            setConfirming(false);
            setError('');
            if (!pending.applied) {
              pending.applied = true;
              setSuccessNotice(Date.now());
              saved.current();
            }
            editing.current = false;
            submitted.current = null;
          } else if (!active(result.task) && result.task.status !== 'succeeded') {
            setError(result.task.errorMessage || 'Cache settings could not be applied. Please try again.');
            submitted.current = null;
            setConfirming(false);
          } else if (Date.now() - pending.at > 120000) {
            setError('Configuration is still being applied. New tasks remain paused until this completes.');
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
    let idleTicks = 0;
    const timer = window.setInterval(() => {
      if (submitted.current || ++idleTicks % 4 === 0) void refresh();
    }, 500);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [path]);
  const busy = loading || saving || confirming || active(task) || phase !== 'applied';
  const valid = !!settings && settings.rootPath.startsWith('/') && (settings.indexPath || `${settings.rootPath}/index`).startsWith('/') && (settings.drillPath || `${settings.rootPath}/drill-data`).startsWith('/') && Number.isInteger(settings.indexMaxGiB) && settings.indexMaxGiB >= 1 && settings.indexMaxGiB <= 1024 && Number.isInteger(settings.drillMaxGiB) && settings.drillMaxGiB >= 1 && settings.drillMaxGiB <= 4096;
  const dirty = !!settings && !!initialSettings && !same(settings, initialSettings);
  const disableConfirmed = !!settings && !!initialSettings
    && cacheDisableConfirmed(initialSettings.indexEnabled, settings.indexEnabled, disableAnswers.index)
    && cacheDisableConfirmed(initialSettings.drillEnabled, settings.drillEnabled, disableAnswers.drill)
    && cacheDirectoryConfirmed(comparable(initialSettings).indexPath, comparable(settings).indexPath, disableAnswers.index)
    && cacheDirectoryConfirmed(comparable(initialSettings).drillPath, comparable(settings).drillPath, disableAnswers.drill);
  const submit = async () => {
    if (busy || !valid || !dirty || !disableConfirmed || !settings || pathEditing) return;
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
  const change = (next: Settings) => {
    if (submitted.current?.applied) submitted.current = null;
    editing.current = true;
    setSettings(next);
  };
  const requestToggle = (cache: 'index' | 'drill') => {
    if (!settings) return;
    if (pathEditing === cache) setPathEditing(null);
    const enabled = cache === 'index' ? settings.indexEnabled : settings.drillEnabled;
    const apply = () => change(cache === 'index' ? { ...settings, indexEnabled: !enabled } : { ...settings, drillEnabled: !enabled });
    setDisableAnswers(previous => ({ ...previous, [cache]: '' }));
    apply();
  };
  const requestPath = (cache: 'index' | 'drill', nextPath: string) => {
    if (!settings) return;
    const oldPath = cache === 'index' ? settings.indexPath : settings.drillPath;
    if (nextPath === oldPath) { setPathEditing(null); return; }
    setDisableAnswers(previous => ({ ...previous, [cache]: '' }));
    change(cache === 'index' ? { ...settings, indexPath: nextPath } : { ...settings, drillPath: nextPath });
    setPathEditing(null);
  };
  const applying = saving || confirming || active(task);
  const requestClose = () => {
    if (applying) return;
    if (dirty || pathEditing) setDiscardConfirm(true);
    else onClose();
  };
  return <ResourceCreateDrawer title="Cache settings" kind="cluster" className="hbdr-cache-settings" onClose={requestClose} actions={<><button type="button" className="cache-cancel" onClick={requestClose} disabled={applying}>{dirty || pathEditing ? 'Cancel' : 'Close'}</button><button type="button" className="is-primary cache-save" disabled={busy || !valid || !dirty || !disableConfirmed || !!pathEditing} onClick={() => submit()}>{applying ? 'Saving…' : 'Save'}</button></>}>
    {successNotice > 0 && <div className="hbdr-toast fixed right-5 top-20 flex items-center gap-2 rounded border border-slate-200 bg-white px-4 py-3 text-sm text-slate-700 shadow-xl" role="status" aria-live="polite"><CheckCircle2 size={16} className="text-emerald-600" />Cache settings saved</div>}
    {discardConfirm && <div className="hbdr-cache-confirm" role="alertdialog" aria-label="Discard cache changes"><strong>Discard unsaved changes?</strong><p>Your changes have not been saved.</p><div><button type="button" onClick={() => setDiscardConfirm(false)}>Keep editing</button><button type="button" className="is-primary" onClick={onClose}>Discard changes</button></div></div>}
    <div className="hbdr-cache-form">
      <p className="text-slate-500">{name}</p>
      {loading ? <p role="status">Loading cache settings…</p> : settings ? <>
        <section className="hbdr-cache-card" aria-labelledby="index-cache-title">
          <div className="hbdr-cache-card-header"><div><div className="hbdr-cache-title"><h3 id="index-cache-title">Index cache</h3><CacheHelp cache="index" /></div><p>Per node</p></div><div className="hbdr-cache-toggle"><span>{settings.indexEnabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Index cache" aria-checked={settings.indexEnabled} disabled={busy || phase !== 'applied'} onClick={() => requestToggle('index')}><span /></button></div></div>
          {settings.indexEnabled && <div className="hbdr-cache-card-fields"><label className="hbdr-cache-row"><span>Cache size</span><span className="hbdr-cache-size"><input aria-label="Index cache size per node" type="number" min={1} max={1024} step={1} value={Number.isNaN(settings.indexMaxGiB) ? '' : settings.indexMaxGiB} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, indexMaxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label><div className="hbdr-cache-row hbdr-cache-path-row"><span>Cache path</span>{pathEditing === 'index' ? <span className="hbdr-cache-path-editor"><input aria-label="Index cache path per node" defaultValue={settings.indexPath || `${settings.rootPath}/index`} /><button type="button" onClick={event => { const input = event.currentTarget.parentElement?.querySelector('input'); if (input) requestPath('index', input.value); }}>Confirm</button><button type="button" onClick={() => setPathEditing(null)}>Cancel</button></span> : <span className="hbdr-cache-path-value"><code title={settings.indexPath || `${settings.rootPath}/index`}>{settings.indexPath || `${settings.rootPath}/index`}</code><button type="button" aria-label="Change index cache directory" onClick={() => setPathEditing('index')} disabled={busy || phase !== 'applied'}>Change</button></span>}</div></div>}
          {initialSettings && ((initialSettings.indexEnabled && !settings.indexEnabled) || comparable(initialSettings).indexPath !== comparable(settings).indexPath) && <CacheChangeConfirmation name="Index cache" disabling={!settings.indexEnabled} value={disableAnswers.index} disabled={busy} onChange={value => setDisableAnswers(previous => ({ ...previous, index: value }))} />}
        </section>
        <section className="hbdr-cache-card" aria-labelledby="drill-cache-title">
          <div className="hbdr-cache-card-header"><div><div className="hbdr-cache-title"><h3 id="drill-cache-title">Drill data cache</h3><CacheHelp cache="drill" /></div><p>Per node</p></div><div className="hbdr-cache-toggle"><span>{settings.drillEnabled ? 'Enabled' : 'Disabled'}</span><button type="button" className="hbdr-settings-switch" role="switch" aria-label="Drill data cache" aria-checked={settings.drillEnabled} disabled={busy || phase !== 'applied'} onClick={() => requestToggle('drill')}><span /></button></div></div>
          {settings.drillEnabled && <div className="hbdr-cache-card-fields"><label className="hbdr-cache-row"><span>Cache size</span><span className="hbdr-cache-size"><input aria-label="Drill data cache size per node" type="number" min={1} max={4096} step={1} value={Number.isNaN(settings.drillMaxGiB) ? '' : settings.drillMaxGiB} disabled={busy || phase !== 'applied'} onChange={event => change({ ...settings, drillMaxGiB: event.target.valueAsNumber })} /><span>GiB</span></span></label><div className="hbdr-cache-row hbdr-cache-path-row"><span>Cache path</span>{pathEditing === 'drill' ? <span className="hbdr-cache-path-editor"><input aria-label="Drill data cache path per node" defaultValue={settings.drillPath || `${settings.rootPath}/drill-data`} /><button type="button" onClick={event => { const input = event.currentTarget.parentElement?.querySelector('input'); if (input) requestPath('drill', input.value); }}>Confirm</button><button type="button" onClick={() => setPathEditing(null)}>Cancel</button></span> : <span className="hbdr-cache-path-value"><code title={settings.drillPath || `${settings.rootPath}/drill-data`}>{settings.drillPath || `${settings.rootPath}/drill-data`}</code><button type="button" aria-label="Change drill data cache directory" onClick={() => setPathEditing('drill')} disabled={busy || phase !== 'applied'}>Change</button></span>}</div></div>}
          {initialSettings && ((initialSettings.drillEnabled && !settings.drillEnabled) || comparable(initialSettings).drillPath !== comparable(settings).drillPath) && <CacheChangeConfirmation name="Drill data cache" disabling={!settings.drillEnabled} value={disableAnswers.drill} disabled={busy} onChange={value => setDisableAnswers(previous => ({ ...previous, drill: value }))} />}
        </section>
        {!valid && <p role="alert" className="text-xs text-rose-600">Enter a whole number from 1 to 1024.</p>}
        {phase !== 'applied' && <p role="status">{phase === 'applying' ? 'Applying cache settings…' : 'Cluster configuration is unavailable.'}</p>}
      </> : <p role="status">{phase === 'applying' ? 'Applying cache settings…' : phase === 'unconfigured' ? 'Cache is not configured on this cluster.' : 'Cluster configuration is unavailable.'}</p>}
      {error && <p role="alert" className="text-xs text-rose-600">{error}</p>}
    </div>
  </ResourceCreateDrawer>;
}
