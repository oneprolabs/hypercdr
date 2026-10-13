import React, { useEffect, useRef, useState } from 'react';
import { Check, Layers3, LoaderCircle, Minus, AlertTriangle } from 'lucide-react';

export type ScopedResourceOption = {
  key: string;
  label: string;
  detail?: string;
  count?: number;
  defaultSelected?: boolean;
};

export type ScopedResourceSelection = {
  mode: 'all' | 'custom' | 'exclude';
  namespaceScoped: string[];
  clusterScoped: string[];
};

type Props = {
  value: ScopedResourceSelection;
  onChange: (value: ScopedResourceSelection) => void;
  namespaceResources: ScopedResourceOption[];
  purpose: 'backup' | 'restore';
  detailedRestore?: boolean;
  compact?: boolean;
  customResourcesLoaded?: boolean;
  onRequestCustomResources?: () => Promise<boolean>;
  disabled?: boolean;
  drafts?: React.RefObject<ScopedResourceDrafts>;
  customUnavailableReason?: string;
};

export type ScopedResourceDrafts = {
  custom?: ScopedResourceSelection;
  exclude?: ScopedResourceSelection;
  lastMode?: 'custom' | 'exclude';
};

export const resourceSelectionValid = (value: ScopedResourceSelection) => value.mode !== 'custom' || value.namespaceScoped.length > 0 || value.clusterScoped.length > 0;

const toggle = (items: string[], key: string) => items.includes(key) ? items.filter(item => item !== key) : [...items, key].sort();

// Restore retains its existing exclusion-only interaction and generic Restore
// parameter mapping. The include/exclude editor below is for protection scope.
export function ScopedResourceSelector(props: Props) {
  return props.purpose === 'restore' ? <RestoreResourceSelector {...props} /> : <ProtectionResourceSelector {...props} />;
}

function ProtectionResourceSelector({ value, onChange, namespaceResources, compact = false, customResourcesLoaded = true, onRequestCustomResources, disabled = false, drafts, customUnavailableReason }: Props) {
  const localDrafts = useRef<ScopedResourceDrafts>({});
  const cache = drafts || localDrafts;
  const mounted = useRef(true);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const custom = value.mode !== 'all';
  const include = value.mode === 'custom';
  const remember = () => {
    if (value.mode !== 'all') {
      cache.current[value.mode] = { ...value, namespaceScoped: [...value.namespaceScoped], clusterScoped: [...value.clusterScoped] };
      cache.current.lastMode = value.mode;
    }
  };
  const changeMode = (mode: 'all' | 'custom' | 'exclude') => {
    remember();
    onChange(mode === 'all' ? { mode, namespaceScoped: [], clusterScoped: [] } : cache.current[mode] || { mode, namespaceScoped: [], clusterScoped: [] });
  };
  const selectCustom = async () => {
    if (custom || loading || disabled || customUnavailableReason) return;
    setLoadError('');
    if (!customResourcesLoaded && onRequestCustomResources) {
      setLoading(true);
      try {
        if (!await onRequestCustomResources()) {
          if (mounted.current) setLoadError('Resource discovery did not complete. Try Custom again.');
          return;
        }
      } catch {
        if (mounted.current) setLoadError('Resource discovery failed. Try Custom again.');
        return;
      } finally {
        if (mounted.current) setLoading(false);
      }
    }
    if (mounted.current) changeMode(cache.current.lastMode || 'exclude');
  };
  // Keep saved choices visible even if the latest inventory has no objects of
  // that type; otherwise a selection could silently disappear from the editor.
  const resources = [...namespaceResources];
  value.namespaceScoped.forEach(key => {
    if (!resources.some(item => item.key === key)) resources.push({ key, label: key, detail: 'Not present in current inventory' });
  });
  const excluded = resources.filter(item => include ? !value.namespaceScoped.includes(item.key) : value.namespaceScoped.includes(item.key));
  const dependencies = excluded.filter(item => ['secrets', 'configmaps', 'persistentvolumeclaims'].includes(item.key));
  const changeSelection = (namespaceScoped: string[]) => onChange({ ...value, namespaceScoped: [...namespaceScoped].sort() });
  const valid = resourceSelectionValid(value);
  return <div className={`hbdr-scoped-resource-selector hbdr-protection-resource-editor ${compact ? 'is-compact' : ''}`}>
    <div className="hbdr-scoped-resource-summary">
      <div className="hbdr-scoped-resource-summary-icon"><Layers3 size={17} /></div>
      <div><strong>{loading ? 'Loading custom resources' : custom ? `${value.namespaceScoped.length} resource types ${include ? 'included' : 'excluded'}` : 'Default scope'}</strong><span>{loading ? 'Reading namespace resource types…' : custom ? include ? 'Only selected resource types are included.' : 'All resource types are included except your exclusions.' : 'All namespace-scoped resources are included; cluster-scoped resources are excluded.'}</span></div>
      <div className="hbdr-scoped-resource-mode">
        <button type="button" className={!custom ? 'is-active' : ''} aria-pressed={!custom} onClick={() => changeMode('all')} disabled={loading || disabled}>Default</button>
        <button type="button" className={custom || loading ? 'is-active' : ''} aria-pressed={custom} onClick={() => void selectCustom()} disabled={loading || disabled || Boolean(customUnavailableReason)} aria-busy={loading}>{loading && <LoaderCircle size={12} className="animate-spin" />}Custom</button>
      </div>
    </div>
    {customUnavailableReason && <p className="hbdr-resource-scope-hint">{customUnavailableReason}</p>}
    {loadError && <p className="hbdr-resource-scope-warning" role="alert">{loadError}</p>}
    {custom && !customUnavailableReason && <>
      <div className="hbdr-resource-scope-modes" aria-label="Custom resource selection">
        <button type="button" aria-pressed={!include} onClick={() => changeMode('exclude')} disabled={disabled}><strong>All types, with exclusions</strong><span>New resource types are included automatically.</span></button>
        <button type="button" aria-pressed={include} onClick={() => changeMode('custom')} disabled={disabled}><strong>Selected types only</strong><span>New resource types are excluded until selected.</span></button>
      </div>
      <p className="hbdr-resource-scope-hint">{include ? 'Check resource types to include in this protection plan.' : 'Check resource types to exclude. Unchecked types remain included.'}</p>
      <section className="hbdr-resource-type-table" aria-label="Resource type scope">
        <header className="hbdr-resource-type-head"><div><strong>{include ? 'Include resource type' : 'Exclude resource type'}</strong></div><span>Objects</span><span>Scope</span></header>
        <div className="hbdr-resource-type-body">
          {resources.map(item => {
            const selected = value.namespaceScoped.includes(item.key);
            const included = include ? selected : !selected;
            const transient = item.key === 'events' || item.key === 'events.events.k8s.io';
            const volume = item.key === 'persistentvolumeclaims';
            return <div className={`hbdr-resource-type-row ${selected ? 'is-selected' : ''}`} key={item.key}>
              <label className="hbdr-resource-namespace-cell"><input type="checkbox" className="hbdr-resource-scope-checkbox" checked={selected} disabled={disabled} onChange={() => changeSelection(toggle(value.namespaceScoped, item.key))} aria-label={`${include ? 'Include' : 'Exclude'} ${item.label}`} /><span><span className="hbdr-resource-name"><strong>{item.label}</strong>{(volume || transient) && <em className={`hbdr-resource-scope-tag ${volume ? 'is-volume' : 'is-transient'}`} title={volume ? 'PVC resource. Volume data backup depends on the backup configuration.' : 'Temporary event resource.'}>{volume ? 'Volume' : 'Transient'}</em>}</span><small>{item.detail || item.key}</small></span></label>
              <span className="hbdr-resource-object-count">{item.count ?? '—'}</span>
              <span className={`hbdr-resource-scope-result ${included ? 'is-included' : 'is-excluded'}`}>{included ? <Check size={13} /> : <Minus size={13} />}{included ? 'Included' : 'Excluded'}</span>
            </div>;
          })}
          {resources.length === 0 && <p className="hbdr-resource-scope-hint">No namespace resources found.</p>}
        </div>
        <footer><span aria-live="polite">{include ? `${value.namespaceScoped.length} resource types included` : `All types included, except ${value.namespaceScoped.length} selected types`}</span><button type="button" onClick={() => changeSelection([])} disabled={disabled || value.namespaceScoped.length === 0}>Clear selection</button></footer>
      </section>
      {!valid && <p className="hbdr-resource-scope-warning" role="alert">Select at least one resource type to include.</p>}
      {dependencies.length > 0 && <p className="hbdr-resource-scope-warning"><AlertTriangle size={14} /><span>{dependencies.map(item => item.label).join(', ')} not included. Application recovery may be incomplete.</span></p>}
      <p className="hbdr-resource-scope-hint">{include ? 'New resource types are excluded until selected.' : 'New resource types are included automatically.'} {include && value.clusterScoped.length > 0 ? `Historical cluster-scoped selection retained: ${value.clusterScoped.join(', ')}.` : 'Cluster-scoped resources are excluded.'}</p>
    </>}
  </div>;
}

function RestoreResourceSelector({ detailedRestore = false, value, onChange, namespaceResources, purpose, compact = false, customResourcesLoaded = true, onRequestCustomResources, disabled = false }: Props) {
  const [customLoading, setCustomLoading] = useState(false);
  const [customRequested, setCustomRequested] = useState(false);
  const custom = value.mode !== 'all';
  const selectedCount = value.namespaceScoped.length;
  const exclusionDraft = useRef<string[]>([]);
  useEffect(() => { if (custom) exclusionDraft.current = value.namespaceScoped; }, [custom, value.namespaceScoped]);
  const customSelection = (namespaceScoped: string[]): ScopedResourceSelection => ({ mode: 'exclude', namespaceScoped: [...namespaceScoped].sort(), clusterScoped: [] });
  const setAll = () => onChange({ mode: 'all', namespaceScoped: [], clusterScoped: [] });
  const selectCustom = async () => {
    if (customLoading || disabled || (detailedRestore && custom)) return;
    if (!customResourcesLoaded && onRequestCustomResources) {
      setCustomLoading(true);
      try {
        if (!await onRequestCustomResources()) return;
        setCustomRequested(true);
        return;
      } finally {
        setCustomLoading(false);
      }
    }
    onChange(customSelection(detailedRestore ? exclusionDraft.current : []));
  };
  useEffect(() => {
    if (!customRequested || !customResourcesLoaded) return;
    setCustomRequested(false);
    onChange(customSelection(detailedRestore ? exclusionDraft.current : []));
  }, [customRequested, customResourcesLoaded, namespaceResources, onChange, purpose]);
  const updateNamespace = (key: string) => onChange(customSelection(toggle(value.namespaceScoped, key)));
  const allSelected = namespaceResources.length > 0 && namespaceResources.every(item => value.namespaceScoped.includes(item.key));
  const partiallySelected = !allSelected && namespaceResources.some(item => value.namespaceScoped.includes(item.key));
  const toggleAll = () => onChange(customSelection(allSelected ? [] : namespaceResources.map(item => item.key)));
  return <div className={`hbdr-scoped-resource-selector ${compact ? 'is-compact' : ''}`}>
    <div className="hbdr-scoped-resource-summary">
      <div className="hbdr-scoped-resource-summary-icon"><Layers3 size={17} /></div>
      <div><strong>{customLoading ? 'Loading custom resources' : custom ? `${selectedCount} resource types excluded` : 'Default scope'}</strong><span>{customLoading ? 'The scope panels will update automatically.' : custom ? `Checked resource types will be excluded from ${purpose === 'backup' ? 'protection' : 'restore'}.` : `All namespace-scoped resources are included; cluster-scoped resources are excluded.`}</span></div>
      <div className="hbdr-scoped-resource-mode">
        <button type="button" className={!custom ? 'is-active' : ''} onClick={setAll} disabled={customLoading || disabled}>Default</button>
        <button type="button" className={custom || customLoading ? 'is-active' : ''} onClick={() => void selectCustom()} disabled={customLoading || disabled} aria-busy={customLoading || disabled}>
          {customLoading && <LoaderCircle size={12} className="animate-spin" />}Custom
        </button>
      </div>
    </div>
    {detailedRestore && custom && value.namespaceScoped.some(key => ['persistentvolumeclaims','secrets','configmaps'].includes(key)) && <p className="hbdr-restore-scope-warning"><AlertTriangle size={14}/>Excluding PVCs, Secrets or ConfigMaps may leave the restored application without its data or required configuration.</p>}
    {(customLoading || (custom && customResourcesLoaded)) && <section className="hbdr-resource-type-table">
      <header className="hbdr-resource-type-head">
        <div><button type="button" className={`hbdr-scoped-resource-select-all ${allSelected ? 'is-selected' : ''} ${partiallySelected ? 'is-partial' : ''}`} onClick={toggleAll} aria-label={allSelected ? 'Clear exclusions' : 'Exclude all resources'}>{allSelected ? <Check size={11} strokeWidth={3} /> : partiallySelected ? <Minus size={11} strokeWidth={3} /> : null}</button><span><strong>Resources to exclude</strong><small>{customLoading ? 'Loading' : `${value.namespaceScoped.length}/${namespaceResources.length}`}</small></span></div>
        <strong>Objects</strong>
        <strong>{detailedRestore ? 'Restore' : 'Type'}</strong>
      </header>
      <div className="hbdr-resource-type-body">
        {customLoading ? <div className="hbdr-scoped-resource-loading" role="status"><LoaderCircle size={18} className="animate-spin" /><strong>Discovering resource types</strong><span>Reading actual object dependencies…</span></div> : namespaceResources.map(item => {
          const selected = value.namespaceScoped.includes(item.key);
          const transient = item.key === 'events' || item.key === 'events.events.k8s.io';
          return <div className={`hbdr-resource-type-row ${selected ? 'is-selected' : ''}`} key={item.key}>
            <button type="button" className="hbdr-resource-namespace-cell" onClick={() => updateNamespace(item.key)} title={item.detail || item.key}>
              <i className="hbdr-scoped-resource-check">{selected && <Check size={11} strokeWidth={3} />}</i>
              <span><strong>{item.label}</strong><small>{item.detail || item.key}{detailedRestore && item.key === 'persistentvolumeclaims' ? ' · Volume' : ''}</small></span>
              {transient && <em className="hbdr-resource-dependency-badge">Transient</em>}
            </button>
            <span>{item.count ?? '—'}</span>
            <span className={detailedRestore ? 'hbdr-restore-result' : ''}>{detailedRestore ? <>{selected ? <Minus size={13}/> : <Check size={13}/>} {selected ? 'Excluded' : 'Included'}</> : transient ? 'Transient' : item.key === 'persistentvolumeclaims' ? 'Persistent data' : 'Kubernetes resource'}</span>
          </div>;
        })}
        {!customLoading && namespaceResources.length === 0 && <p>No namespace resources found.</p>}
      </div>
      <footer>{purpose === 'restore' ? 'Checked resource types are excluded from this restore.' : 'Checked resource types are excluded; new resource types are protected by default.'}</footer>
    </section>}
  </div>;
}
