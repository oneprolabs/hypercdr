import React, { useEffect, useState } from 'react';
import { Check, Layers3, LoaderCircle, Minus, ShieldAlert } from 'lucide-react';

export type ScopedResourceOption = {
  key: string;
  label: string;
  detail?: string;
  count?: number;
};

export type ScopedResourceSelection = {
  mode: 'all' | 'custom';
  namespaceScoped: string[];
  clusterScoped: string[];
};

type Props = {
  value: ScopedResourceSelection;
  onChange: (value: ScopedResourceSelection) => void;
  namespaceResources: ScopedResourceOption[];
  clusterResources: ScopedResourceOption[];
  purpose: 'backup' | 'restore';
  compact?: boolean;
  customResourcesLoaded?: boolean;
  onRequestCustomResources?: () => Promise<boolean>;
  disabled?: boolean;
};

const toggle = (items: string[], key: string) => items.includes(key) ? items.filter(item => item !== key) : [...items, key].sort();

export function ScopedResourceSelector({ value, onChange, namespaceResources, clusterResources, purpose, compact = false, customResourcesLoaded = true, onRequestCustomResources, disabled = false }: Props) {
  const [customLoading, setCustomLoading] = useState(false);
  const [customRequested, setCustomRequested] = useState(false);
  const custom = value.mode === 'custom';
  const selectedCount = value.namespaceScoped.length + value.clusterScoped.length;
  const setAll = () => onChange({ mode: 'all', namespaceScoped: [], clusterScoped: [] });
  // Custom starts from the application-safe baseline: all namespaced
  // resources, no cluster-wide resources. Cluster objects are opt-in because
  // they can affect workloads beyond the selected namespace.
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
    onChange({ mode: 'custom', namespaceScoped: namespaceResources.map(item => item.key), clusterScoped: [] });
  };
  useEffect(() => {
    if (!customRequested || !customResourcesLoaded) return;
    setCustomRequested(false);
    onChange({ mode: 'custom', namespaceScoped: namespaceResources.map(item => item.key), clusterScoped: [] });
  }, [customRequested, customResourcesLoaded, namespaceResources, onChange]);
  const updateScope = (scope: 'namespaceScoped' | 'clusterScoped', key: string) => {
    const base = custom ? value : { mode: 'custom' as const, namespaceScoped: namespaceResources.map(item => item.key), clusterScoped: [] };
    onChange({ ...base, mode: 'custom', [scope]: toggle(base[scope], key) });
  };
  const toggleAllInScope = (scope: 'namespaceScoped' | 'clusterScoped', items: ScopedResourceOption[]) => {
    const selected = value[scope];
    const allSelected = items.length > 0 && items.every(item => selected.includes(item.key));
    onChange({ ...value, mode: 'custom', [scope]: allSelected ? [] : items.map(item => item.key) });
  };
  const renderScope = (title: string, description: string, items: ScopedResourceOption[], scope: 'namespaceScoped' | 'clusterScoped', caution = false) => {
    const allSelected = items.length > 0 && items.every(item => value[scope].includes(item.key));
    const partiallySelected = !allSelected && items.some(item => value[scope].includes(item.key));
    const selectedCount = items.filter(item => value[scope].includes(item.key)).length;
    return <section className={`hbdr-scoped-resource-card is-${scope}`}>
      <header>
        <div><strong>{title}</strong><span className={caution ? 'is-caution' : undefined}>{caution && <ShieldAlert size={11} />}{description}</span></div>
        <div className="hbdr-scoped-resource-card-actions">
          <small>{customLoading ? 'Loading' : `${selectedCount}/${items.length}`}</small>
          {!customLoading && items.length > 0 && <button type="button" className={`hbdr-scoped-resource-select-all ${allSelected ? 'is-selected' : ''} ${partiallySelected ? 'is-partial' : ''}`} onClick={() => toggleAllInScope(scope, items)} aria-label={`${allSelected ? 'Clear' : 'Select all'} ${title}`}>
            {allSelected ? <Check size={11} strokeWidth={3} /> : partiallySelected ? <Minus size={11} strokeWidth={3} /> : null}
          </button>}
        </div>
      </header>
      <div className="hbdr-scoped-resource-grid">
        {customLoading ? <div className="hbdr-scoped-resource-loading" role="status"><LoaderCircle size={18} className="animate-spin" /><strong>Discovering resource types</strong><span>Reading resources currently used by this namespace…</span></div> : items.map(item => {
          const selected = !custom || value[scope].includes(item.key);
          const details = [
            `Resource type: ${item.label}`,
            `API resource: ${item.detail || item.key}`,
            typeof item.count === 'number' ? `Available objects: ${item.count}` : '',
          ].filter(Boolean).join('\n');
          return <button type="button" key={item.key} className={selected ? 'is-selected' : ''} onClick={() => updateScope(scope, item.key)} title={details} aria-label={details}>
            <i className="hbdr-scoped-resource-check">{selected && <Check size={11} strokeWidth={3} />}</i><span><strong>{item.label}</strong></span>
          </button>;
        })}
        {!customLoading && items.length === 0 && <p>No resource types found in this scope.</p>}
      </div>
    </section>;
  };
  return <div className={`hbdr-scoped-resource-selector ${compact ? 'is-compact' : ''}`}>
    <div className="hbdr-scoped-resource-summary">
      <div className="hbdr-scoped-resource-summary-icon"><Layers3 size={17} /></div>
      <div><strong>{customLoading ? 'Loading custom resources' : custom ? `${selectedCount} resource types selected` : 'Default scope'}</strong><span>{customLoading ? 'The scope panels will update automatically.' : custom ? 'Only selected resource types will be processed.' : `All namespace-scoped resources are included; cluster-scoped resources are excluded.`}</span></div>
      <div className="hbdr-scoped-resource-mode">
        <button type="button" className={!custom ? 'is-active' : ''} onClick={setAll} disabled={customLoading || disabled}>Default</button>
        <button type="button" className={custom || customLoading ? 'is-active' : ''} onClick={() => void selectCustom()} disabled={customLoading || disabled} aria-busy={customLoading || disabled}>
          {customLoading && <LoaderCircle size={12} className="animate-spin" />}Custom
        </button>
      </div>
    </div>
    {(customLoading || (custom && customResourcesLoaded)) && <div className="hbdr-scoped-resource-scopes">
      {renderScope('Namespace-scoped resources', 'Application objects in the selected namespace', namespaceResources, 'namespaceScoped')}
      {renderScope('Cluster-scoped resources', purpose === 'restore' ? 'Shared across the target cluster. Changes may affect other applications.' : 'Shared infrastructure across the cluster', clusterResources, 'clusterScoped', purpose === 'restore')}
    </div>}
  </div>;
}
