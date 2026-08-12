import React, { useEffect, useState } from 'react';
import { Check, Layers3, LoaderCircle, Minus } from 'lucide-react';

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
  compact?: boolean;
  customResourcesLoaded?: boolean;
  onRequestCustomResources?: () => Promise<boolean>;
  disabled?: boolean;
};

const toggle = (items: string[], key: string) => items.includes(key) ? items.filter(item => item !== key) : [...items, key].sort();

export function ScopedResourceSelector({ value, onChange, namespaceResources, purpose, compact = false, customResourcesLoaded = true, onRequestCustomResources, disabled = false }: Props) {
  const [customLoading, setCustomLoading] = useState(false);
  const [customRequested, setCustomRequested] = useState(false);
  const custom = value.mode !== 'all';
  const selectedCount = value.namespaceScoped.length;
  const customSelection = (namespaceScoped: string[]): ScopedResourceSelection => ({ mode: 'exclude', namespaceScoped: [...namespaceScoped].sort(), clusterScoped: [] });
  const setAll = () => onChange({ mode: 'all', namespaceScoped: [], clusterScoped: [] });
  const selectCustom = async () => {
    if (customLoading || disabled) return;
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
    onChange(customSelection([]));
  };
  useEffect(() => {
    if (!customRequested || !customResourcesLoaded) return;
    setCustomRequested(false);
    onChange(customSelection([]));
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
    {(customLoading || (custom && customResourcesLoaded)) && <section className="hbdr-resource-type-table">
      <header className="hbdr-resource-type-head">
        <div><button type="button" className={`hbdr-scoped-resource-select-all ${allSelected ? 'is-selected' : ''} ${partiallySelected ? 'is-partial' : ''}`} onClick={toggleAll} aria-label={allSelected ? 'Clear exclusions' : 'Exclude all resources'}>{allSelected ? <Check size={11} strokeWidth={3} /> : partiallySelected ? <Minus size={11} strokeWidth={3} /> : null}</button><span><strong>Resources to exclude</strong><small>{customLoading ? 'Loading' : `${value.namespaceScoped.length}/${namespaceResources.length}`}</small></span></div>
        <strong>Objects</strong>
        <strong>Type</strong>
      </header>
      <div className="hbdr-resource-type-body">
        {customLoading ? <div className="hbdr-scoped-resource-loading" role="status"><LoaderCircle size={18} className="animate-spin" /><strong>Discovering resource types</strong><span>Reading actual object dependencies…</span></div> : namespaceResources.map(item => {
          const selected = value.namespaceScoped.includes(item.key);
          const transient = item.key === 'events' || item.key === 'events.events.k8s.io';
          return <div className={`hbdr-resource-type-row ${selected ? 'is-selected' : ''}`} key={item.key}>
            <button type="button" className="hbdr-resource-namespace-cell" onClick={() => updateNamespace(item.key)} title={item.detail || item.key}>
              <i className="hbdr-scoped-resource-check">{selected && <Check size={11} strokeWidth={3} />}</i>
              <span><strong>{item.label}</strong><small>{item.detail || item.key}</small></span>
              {transient && <em className="hbdr-resource-dependency-badge">Transient</em>}
            </button>
            <strong>{item.count ?? 0}</strong>
            <span>{transient ? 'Transient' : item.key === 'persistentvolumeclaims' ? 'Persistent data' : 'Kubernetes resource'}</span>
          </div>;
        })}
        {!customLoading && namespaceResources.length === 0 && <p>No namespace resources found.</p>}
      </div>
      <footer>{purpose === 'restore' ? 'Checked resource types are excluded from this restore.' : 'Checked resource types are excluded; new resource types are protected by default.'}</footer>
    </section>}
  </div>;
}
