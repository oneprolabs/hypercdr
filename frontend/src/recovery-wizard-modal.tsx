import React, { useEffect } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle,
  Check,
  Cloud,
  GitBranch,
  Play,
  RefreshCw,
  ShieldCheck,
  Target,
  X,
} from 'lucide-react';
import { ScopedResourceSelector, type ScopedResourceSelection } from './components/scoped-resource-selector';

export type RecoveryWizardMode = 'drill' | 'takeover';

export type RecoveryWizardConfig = {
  pointId: string;
  sourceType: 'snapshot' | 'export';
  targetMode: 'sandbox' | 'crossCluster' | 'inPlace';
  targetCluster: string;
  namespaceMode: 'generated' | 'original' | 'custom';
  targetNamespace: string;
  restoreMode: 'full' | 'dataOnly';
  artifactMode: 'all' | 'manifests' | 'volumes';
  conflictPolicy: 'skip' | 'overwrite' | 'replace';
  originalNamespaceConfirmed: boolean;
  includeClusterScoped: boolean;
  useTransforms: boolean;
  transformPreset: 'drill' | 'migration' | 'none';
  storageProfileMode: 'original' | 'alternate';
  alternateProfileId: string;
  preflightChecks: boolean;
  autoStartValidation: boolean;
  notes: string;
  forceProceed: boolean;
  includedResources: string[];
	excludedResources: string[];
	resourceSelection: ScopedResourceSelection;
  storageClassMappings: Record<string, string>;
  imageMappings: Record<string, string>;
  waitForWorkloads: boolean;
  runValidation: boolean;
  forceStart: boolean;
  contentCatalogLoaded: boolean;
  persistentDataExpected: boolean;
};

type RecoveryPoint = {
  id: string;
  title: string;
  time: string;
  type: string;
  status: string;
};

type ClusterOption = {
  id: string;
  name: string;
  region: string;
  version: string;
  isCurrent: boolean;
  storageClasses?: Array<{ name: string }>;
  apiResources?: Array<{ group?: string; version: string; resource: string; kind: string; namespaced: boolean }>;
};

export type BackupContentResource = {
  apiVersion: string;
  kind: string;
  namespace?: string;
  name: string;
  group?: string;
  resource?: string;
  clusterScoped: boolean;
  images?: string[];
  storageClasses?: string[];
};

type RepositoryOption = {
  id: string;
  name: string;
  type: string;
  endpoint: string;
  bucket: string;
};

type Props = {
  open: boolean;
  mode: RecoveryWizardMode;
  app: {
    name: string;
    namespace: string;
    storage?: string;
    targetCluster?: string;
  };
  profile: {
    uid: string;
  };
  currentClusterName: string;
  points: RecoveryPoint[];
  clusterOptions: ClusterOption[];
  repositoryOptions: RepositoryOption[];
  config: RecoveryWizardConfig;
  setConfig: React.Dispatch<React.SetStateAction<RecoveryWizardConfig | null>>;
  onClose: () => void;
  onSubmit: () => void;
  submitting?: boolean;
  readinessBlockers?: number;
  loadContents?: (restorePointId: string) => Promise<{ resources: BackupContentResource[]; truncated?: boolean }>;
};

function sourceMeta(type: RecoveryWizardConfig['sourceType']) {
  if (type === 'snapshot') {
    return {
      title: 'Local Snapshot',
      desc: 'Fast validation path from the source cluster snapshot chain.',
      icon: ShieldCheck,
    };
  }
  return {
    title: 'Remote Snapshot',
    desc: 'Repository based snapshot path that validates off-cluster recovery media.',
    icon: Cloud,
  };
}

function targetMeta(type: RecoveryWizardConfig['targetMode'], mode: RecoveryWizardMode) {
  if (type === 'sandbox') {
    return {
      title: 'Sandbox Drill',
      desc: 'Recover into an isolated validation namespace without changing service routing.',
      risk: 'Production-safe',
      namespaceHint: 'Generated drill namespace',
      icon: ShieldCheck,
    };
  }
  if (type === 'crossCluster') {
    return {
      title: mode === 'drill' ? 'Cross-cluster Drill' : 'Cross-cluster Takeover',
      desc: 'Recover into the prepared DR cluster and validate cluster-to-cluster readiness.',
      risk: mode === 'drill' ? 'DR readiness check' : 'Routing cutover required',
      namespaceHint: 'Target cluster namespace',
      icon: Target,
    };
  }
  return {
    title: 'Restore In Place',
    desc: 'Recover into the original namespace. Use only for controlled rollback.',
    risk: 'Overwrite risk',
    namespaceHint: 'Original namespace',
    icon: GitBranch,
  };
}

function namespaceValue(config: RecoveryWizardConfig, sourceNamespace: string) {
  return config.namespaceMode === 'original' ? sourceNamespace : config.targetNamespace;
}

function pointSourceType(point: RecoveryPoint): RecoveryWizardConfig['sourceType'] {
  return point.type.toLowerCase().includes('local') ? 'snapshot' : 'export';
}

function inferTargetMode(
  targetCluster: string,
  namespaceMode: RecoveryWizardConfig['namespaceMode'],
  currentClusterName: string,
): RecoveryWizardConfig['targetMode'] {
  if (targetCluster !== currentClusterName) return 'crossCluster';
  return namespaceMode === 'original' ? 'inPlace' : 'sandbox';
}

function targetBehaviorPatch(
  targetMode: RecoveryWizardConfig['targetMode'],
  namespaceMode: RecoveryWizardConfig['namespaceMode'],
): Pick<RecoveryWizardConfig, 'conflictPolicy' | 'useTransforms' | 'transformPreset'> {
  return {
    conflictPolicy: namespaceMode === 'generated' ? 'replace' : 'skip',
    useTransforms: targetMode !== 'inPlace',
    transformPreset: targetMode === 'crossCluster' ? 'migration' : targetMode === 'sandbox' ? 'drill' : 'none',
  };
}

export function RecoveryWizardModal(props: Props) {
  const {
    open,
    mode,
    app,
    profile,
    currentClusterName,
    points,
    clusterOptions,
    config,
    setConfig,
    onClose,
    onSubmit,
    submitting = false,
	readinessBlockers = 0,
    loadContents,
  } = props;

  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const [contents, setContents] = React.useState<BackupContentResource[]>([]);
  const [contentsLoading, setContentsLoading] = React.useState(false);
  const [contentsError, setContentsError] = React.useState('');
  const [contentsReload, setContentsReload] = React.useState(0);

  const currentClusterOption = clusterOptions.find(item => item.name === currentClusterName) || clusterOptions.find(item => item.isCurrent);
  const currentTargetClusterName = currentClusterOption?.name || currentClusterName;
  const configuredTargetCluster = app.targetCluster || '';
  const source = sourceMeta(config.sourceType);
  const sourceNamespace = app.name;
  const generatedNamespace = mode === 'drill' ? `${sourceNamespace}-drill` : `${sourceNamespace}-restore`;
  const targetNamespace = namespaceValue(config, sourceNamespace);
  const restoresToOriginalNamespace = config.namespaceMode === 'original' || targetNamespace === sourceNamespace;
  const targetClusterOption = clusterOptions.find(item => item.name === config.targetCluster);
  const backupStorageClasses = Array.from(new Set(contents.flatMap(item => item.storageClasses || []))).sort();
  const backupImages = Array.from(new Set(contents.flatMap(item => item.images || []))).sort();
  const restorePointUnavailable = Boolean(contentsError && /restore.?point.?not.?found|no longer available/i.test(contentsError));
  const submitDisabled = !config.pointId || !config.targetCluster || !targetNamespace.trim() || restorePointUnavailable || (restoresToOriginalNamespace && !config.originalNamespaceConfirmed) || (readinessBlockers > 0 && !config.forceProceed);
  const pointsBySource = {
    snapshot: points.filter(point => pointSourceType(point) === 'snapshot'),
    export: points.filter(point => pointSourceType(point) === 'export'),
  };
  const sourcePoints = pointsBySource[config.sourceType];

  const updateConfig = (patch: Partial<RecoveryWizardConfig>) => {
    setConfig(prev => (prev ? { ...prev, ...patch } : prev));
  };

  useEffect(() => {
    let cancelled = false;
    if (!config.pointId || !loadContents) { setContents([]); return; }
    setContentsLoading(true); setContentsError('');
    updateConfig({ contentCatalogLoaded: false, persistentDataExpected: false, forceStart: false });
    loadContents(config.pointId).then(result => {
      if (!cancelled) {
        const resources = result.resources || [];
        setContents(resources);
        updateConfig({ contentCatalogLoaded: true, persistentDataExpected: resources.some(item => ['PersistentVolumeClaim', 'PersistentVolume', 'VolumeSnapshot'].includes(item.kind)) });
      }
    }).catch(error => {
      if (!cancelled) { setContents([]); updateConfig({ contentCatalogLoaded: false }); setContentsError(error instanceof Error ? error.message : 'Restore point contents could not be loaded.'); }
    }).finally(() => { if (!cancelled) setContentsLoading(false); });
    return () => { cancelled = true; };
  }, [config.pointId, contentsReload]);

  const uniqueContents = Array.from(contents.reduce((items, item) => {
    const identity = [item.apiVersion, item.kind, item.namespace || '', item.name].join('|');
    if (!items.has(identity)) items.set(identity, item);
    return items;
  }, new Map<string, BackupContentResource>()).values());
  const resourceGroups = Array.from(uniqueContents.reduce((groups, item) => {
    const resourceKey = item.resource ? `${item.resource}${item.group ? `.${item.group}` : ''}` : item.kind;
    const key = `${item.apiVersion}|${resourceKey}|${item.clusterScoped ? 'cluster' : 'namespace'}`;
    const current = groups.get(key) || { key, resourceKey, apiVersion: item.apiVersion, kind: item.kind, clusterScoped: item.clusterScoped, count: 0 };
    current.count += 1; groups.set(key, current); return groups;
  }, new Map<string, { key: string; resourceKey: string; apiVersion: string; kind: string; clusterScoped: boolean; count: number }>()).values()).sort((a, b) => a.kind.localeCompare(b.kind));
  const restoreNamespaceOptions = resourceGroups
    .filter(group => !group.clusterScoped)
    .map(group => ({ key: group.resourceKey, label: group.kind, detail: group.apiVersion, count: group.count }));

  const restoreSelection: ScopedResourceSelection = config.resourceSelection || { mode: 'all', namespaceScoped: [], clusterScoped: [] };
  const updateRestoreSelection = (selection: ScopedResourceSelection) => {
    const excluded = selection.mode === 'exclude' ? selection.namespaceScoped : [];
    updateConfig({
      resourceSelection: selection,
      // Drill custom mode is exclusion-based: checked resource types map
      // directly to Velero excludedResources. An empty custom selection is
      // therefore equivalent to Default and preserves Velero's resource
      // closure for filesystem volume restores.
      includedResources: [],
      excludedResources: excluded,
      includeClusterScoped: false,
    });
  };

  const updateMapping = (field: 'storageClassMappings' | 'imageMappings', source: string, target: string) => {
    const next = { ...(config[field] || {}) };
    if (target.trim() && target.trim() !== source) next[source] = target.trim(); else delete next[source];
    updateConfig({ [field]: next } as Partial<RecoveryWizardConfig>);
  };

  const choosePoint = (point: RecoveryPoint) => {
    updateConfig({
      pointId: point.id,
      sourceType: pointSourceType(point),
      storageProfileMode: pointSourceType(point) === 'snapshot' ? 'original' : config.storageProfileMode,
      alternateProfileId: pointSourceType(point) === 'snapshot' ? '' : config.alternateProfileId,
    });
  };

  const chooseSourceType = (sourceType: RecoveryWizardConfig['sourceType']) => {
    const nextPoint = pointsBySource[sourceType][0];
    updateConfig({
      sourceType,
      pointId: nextPoint?.id || '',
      storageProfileMode: sourceType === 'snapshot' ? 'original' : config.storageProfileMode,
      alternateProfileId: sourceType === 'snapshot' ? '' : config.alternateProfileId,
    });
  };

  useEffect(() => {
    if (mode !== 'drill' || config.sourceType !== 'snapshot') return;
    const nextPoint = points.find(point => pointSourceType(point) === 'export');
    updateConfig({
      sourceType: 'export',
      pointId: nextPoint?.id || '',
    });
  }, [config.sourceType, mode, points]);

  const chooseTargetCluster = (targetCluster: string) => {
    const targetMode = inferTargetMode(targetCluster, config.namespaceMode, currentTargetClusterName);
    updateConfig({
      targetCluster,
      targetMode,
      originalNamespaceConfirmed: false,
      ...targetBehaviorPatch(targetMode, config.namespaceMode),
    });
  };

  const chooseNamespaceMode = (namespaceMode: RecoveryWizardConfig['namespaceMode']) => {
    const targetMode = inferTargetMode(config.targetCluster, namespaceMode, currentTargetClusterName);
    updateConfig({
      namespaceMode,
      targetMode,
      targetNamespace: namespaceMode === 'original' ? sourceNamespace : namespaceMode === 'generated' ? generatedNamespace : config.targetNamespace,
      originalNamespaceConfirmed: false,
      ...targetBehaviorPatch(targetMode, namespaceMode),
    });
  };

  const updateTargetNamespace = (targetNamespace: string) => {
    const namespaceMode: RecoveryWizardConfig['namespaceMode'] = 'custom';
    const targetMode = inferTargetMode(config.targetCluster, namespaceMode, currentTargetClusterName);
    updateConfig({
      namespaceMode,
      targetNamespace,
      targetMode,
      originalNamespaceConfirmed: false,
      ...targetBehaviorPatch(targetMode, namespaceMode),
    });
  };

  const chooseCloneMode = () => {
    const targetMode = inferTargetMode(config.targetCluster, 'generated', currentTargetClusterName);
    updateConfig({
      namespaceMode: 'generated',
      targetMode,
      targetNamespace: generatedNamespace,
      restoreMode: 'full',
      conflictPolicy: 'replace',
      originalNamespaceConfirmed: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : 'drill',
    });
  };

  const chooseFullOriginalMode = () => {
    const targetMode = inferTargetMode(config.targetCluster, 'original', currentTargetClusterName);
    updateConfig({
      namespaceMode: 'original',
      targetMode,
      targetNamespace: sourceNamespace,
      restoreMode: 'full',
      conflictPolicy: 'replace',
      originalNamespaceConfirmed: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : 'none',
    });
  };

  const submitLabel = mode === 'drill' ? 'Start Drill' : 'Start Takeover';
  const title = mode === 'drill' ? 'DR Drill' : 'DR Takeover';
  const HeaderIcon = mode === 'drill' ? ShieldCheck : Target;

  return (
    <AnimatePresence>
      {open && (
        <div className="hbdr-protect-modal">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="hbdr-protect-backdrop"
            onClick={() => { if (!submitting) onClose(); }}
          />
          <motion.div
            initial={{ opacity: 0, x: 32 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 32 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="hbdr-filter-drawer hbdr-protect-dialog hbdr-protect-dialog-v2 hbdr-protect-drawer hbdr-recovery-dialog"
          >
            <div className="hbdr-protect-header">
              <div className="hbdr-protect-title-wrap">
                <div className="hbdr-protect-title-icon"><HeaderIcon size={22} /></div>
                <div>
                  <h3>{title}</h3>
                  <span><em className="hbdr-protect-target">{app.name}</em></span>
                </div>
              </div>
              <div className="hbdr-protect-header-actions">
                <button type="button" onClick={onClose} disabled={submitting} aria-label="Close recovery wizard"><X size={18} /></button>
              </div>
            </div>

            <div className="hbdr-protect-body hbdr-recovery-body">
              <div className="hbdr-recovery-single-page">
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Restore point</strong>
                  </div>

                  <div className="hbdr-recovery-point-picker">
                    {mode !== 'drill' && <label>
                      <span>Snapshot type</span>
                      <div className="hbdr-recovery-source-tabs">
                        {(['export', 'snapshot'] as const).map(sourceType => {
                          const meta = sourceMeta(sourceType);
                          return (
                            <button
                              key={sourceType}
                              type="button"
                              className={config.sourceType === sourceType ? 'is-active' : ''}
                              aria-label={meta.title}
                              onClick={() => chooseSourceType(sourceType)}
                            >
                              <span>{meta.title}</span>
                            </button>
                          );
                        })}
                      </div>
                    </label>}
                    <label>
                      <select
                        value={config.pointId}
                        onChange={event => {
                          const point = sourcePoints.find(item => item.id === event.target.value);
                          if (point) choosePoint(point);
                        }}
                        disabled={sourcePoints.length === 0}
                      >
                        {sourcePoints.length === 0 && <option value="">No {source.title} available</option>}
                        {sourcePoints.map(point => (
                          <option key={point.id} value={point.id}>
                            {point.time}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                </div>

                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Target</strong>
                  </div>

                  <div className="hbdr-recovery-target-layout">
                    <div className="hbdr-recovery-target-main">
                      <div className="hbdr-recovery-target-section">
                        <label className="hbdr-recovery-plain-field">
                          <span>Target cluster</span>
                        <select
                          value={config.targetCluster}
                          onChange={event => chooseTargetCluster(event.target.value)}
                        >
                          {clusterOptions.map(cluster => (
                            <option key={cluster.id} value={cluster.name}>
                              {cluster.name}{cluster.name === currentTargetClusterName ? ' / Current' : ''}{cluster.name === configuredTargetCluster ? ' / Configured target' : ''}
                            </option>
                          ))}
                        </select>
                        </label>
                      </div>

                      <div className="hbdr-recovery-target-section">
                        <div className="hbdr-recovery-target-section-head">
                          <strong>Recovery mode</strong>
                        </div>
                        <div className="hbdr-recovery-namespace-options">
                          <button type="button" onClick={chooseCloneMode} className={config.namespaceMode !== 'original' ? 'is-active' : ''}>
                            <ShieldCheck size={16} />
                            <span>
                              <strong>Clone to new namespace</strong>
                              <em>{config.namespaceMode === 'original' ? generatedNamespace : targetNamespace || generatedNamespace}</em>
                            </span>
                            <i>{config.namespaceMode !== 'original' && <Check size={11} />}</i>
                          </button>
                          <button type="button" onClick={chooseFullOriginalMode} className={config.namespaceMode === 'original' && config.restoreMode === 'full' ? 'is-active' : ''}>
                            <GitBranch size={16} />
                            <span>
                              <strong>Full restore to original namespace</strong>
                            </span>
                            <i>{config.namespaceMode === 'original' && config.restoreMode === 'full' && <Check size={11} />}</i>
                          </button>
                        </div>
                        {config.namespaceMode !== 'original' && (
                          <input
                            className="hbdr-recovery-namespace-input"
                            value={targetNamespace}
                            onChange={event => updateTargetNamespace(event.target.value)}
                            placeholder="Target namespace"
                          />
                        )}
                        {restoresToOriginalNamespace && (
                          <div className="hbdr-recovery-danger-zone">
                            <AlertCircle size={17} />
                            <div>
                              <strong>Original namespace restore</strong>
                              <label className="hbdr-recovery-confirm-check">
                                <input
                                  type="checkbox"
                                  checked={config.originalNamespaceConfirmed}
                                  onChange={event => updateConfig({ originalNamespaceConfirmed: event.target.checked })}
                                />
                                <span>I understand this restore targets the original namespace and may affect existing resources.</span>
                              </label>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                </div>

                <div className="hbdr-protect-section hbdr-recovery-scope-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Restore scope</strong>
                    <span>Choose the Kubernetes resource types restored from this point.</span>
                  </div>
                  <ScopedResourceSelector
                    purpose="restore"
                    value={restoreSelection}
                    onChange={updateRestoreSelection}
                    disabled={contentsLoading || Boolean(contentsError)}
                    namespaceResources={restoreNamespaceOptions}
                  />
                  {contentsLoading && <p className="hbdr-recovery-inline-status"><RefreshCw size={13} className="animate-spin" /> Reading the selected restore point…</p>}
                  {contentsError && (
                    <div className="hbdr-recovery-content-failure">
                      <p className="hbdr-recovery-inline-error">{restorePointUnavailable
                        ? 'Selected restore point is no longer available. Refresh the restore point list and select another restore point.'
                        : `Restore scope unavailable: ${contentsError}.`}</p>
                      {!restorePointUnavailable && <button type="button" onClick={() => setContentsReload(value => value + 1)}><RefreshCw size={12} />Retry content inspection</button>}
                    </div>
                  )}
                </div>

                <div className="hbdr-protect-section hbdr-recovery-advanced">
                  <button type="button" className="hbdr-recovery-advanced-toggle" onClick={() => setAdvancedOpen(value => !value)}>
                    <span><strong>Advanced options</strong><em>Mappings and conflict handling</em></span>
                    <b>{advancedOpen ? 'Hide' : 'Show'}</b>
                  </button>
                  {advancedOpen && (
                    <div className="hbdr-recovery-advanced-content">
                      <section>
                        <header><strong>Environment mappings</strong><span>Leave blank to preserve the value stored in the backup.</span></header>
                        {backupStorageClasses.map(source => <label className="hbdr-recovery-mapping" key={`sc-${source}`}><span>StorageClass <b>{source}</b></span><select value={config.storageClassMappings?.[source] || ''} onChange={event => updateMapping('storageClassMappings', source, event.target.value)}><option value="">Keep original</option>{(targetClusterOption?.storageClasses || []).map(item => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label>)}
                        {backupImages.map(source => <label className="hbdr-recovery-image-mapping" key={`image-${source}`}>
                          <span><em>Source image</em><b title={source}>{source}</b></span>
                          <span><em>Target image</em><input title={config.imageMappings?.[source] || ''} value={config.imageMappings?.[source] || ''} onChange={event => updateMapping('imageMappings', source, event.target.value)} placeholder="Keep original image" /></span>
                        </label>)}
                        {config.contentCatalogLoaded && backupStorageClasses.length === 0 && backupImages.length === 0 && <p className="hbdr-recovery-muted">No StorageClass or container image references were found in the inspected backup.</p>}
                        {!config.contentCatalogLoaded && <p className="hbdr-recovery-muted">Mappings are unavailable until restore point content inspection succeeds.</p>}
                      </section>
                      {mode !== 'drill' && <section>
                        <header><strong>Validation</strong><span>These checks run after Kubernetes resources and persistent data are restored.</span></header>
                        <label className="hbdr-recovery-confirm-check"><input type="checkbox" checked={config.waitForWorkloads} onChange={event => updateConfig({ waitForWorkloads: event.target.checked })} /><span>Wait for workloads and pods to become ready</span></label>
                        <label className="hbdr-recovery-confirm-check"><input type="checkbox" checked={config.runValidation} onChange={event => updateConfig({ runValidation: event.target.checked })} /><span>Run application validation</span></label>
                      </section>}
                    </div>
                  )}
                </div>

              </div>
            </div>

            <div className="hbdr-protect-footer">
			  {readinessBlockers > 0 && (
				<label className="hbdr-recovery-confirm-check" title={`${readinessBlockers} blocking readiness finding${readinessBlockers === 1 ? '' : 's'}`}>
				  <input type="checkbox" checked={config.forceProceed} onChange={event => updateConfig({ forceProceed: event.target.checked })} />
				  <span>I understand the identified risks and want to proceed anyway.</span>
				</label>
			  )}
              <button type="button" onClick={onClose} disabled={submitting}>Cancel</button>
              <button
                type="button"
                className="hbdr-protect-primary"
                disabled={submitDisabled || submitting}
                onClick={() => {
                  if (submitDisabled || submitting) return;
                  onSubmit();
                }}
              >
                <Play size={15} />{submitting ? 'Submitting…' : submitLabel}
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
