import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle,
  Check,
  Cloud,
  GitBranch,
  Play,
  ShieldCheck,
  Target,
  X,
} from 'lucide-react';

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
): Pick<RecoveryWizardConfig, 'conflictPolicy' | 'useTransforms' | 'transformPreset'> {
  return {
    conflictPolicy: 'skip',
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
  } = props;

  const currentClusterOption = clusterOptions.find(item => item.name === currentClusterName) || clusterOptions.find(item => item.isCurrent);
  const currentTargetClusterName = currentClusterOption?.name || currentClusterName;
  const configuredTargetCluster = app.targetCluster || '';
  const source = sourceMeta(config.sourceType);
  const sourceNamespace = app.name;
  const generatedNamespace = mode === 'drill' ? `${sourceNamespace}-drill` : `${sourceNamespace}-restore`;
  const targetNamespace = namespaceValue(config, sourceNamespace);
  const restoresToOriginalNamespace = config.namespaceMode === 'original' || targetNamespace === sourceNamespace;
  const submitDisabled = !config.pointId || !config.targetCluster || !targetNamespace.trim() || (restoresToOriginalNamespace && !config.originalNamespaceConfirmed);
  const pointsBySource = {
    snapshot: points.filter(point => pointSourceType(point) === 'snapshot'),
    export: points.filter(point => pointSourceType(point) === 'export'),
  };
  const sourcePoints = pointsBySource[config.sourceType];

  const updateConfig = (patch: Partial<RecoveryWizardConfig>) => {
    setConfig(prev => (prev ? { ...prev, ...patch } : prev));
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

  const chooseTargetCluster = (targetCluster: string) => {
    const targetMode = inferTargetMode(targetCluster, config.namespaceMode, currentTargetClusterName);
    updateConfig({
      targetCluster,
      targetMode,
      originalNamespaceConfirmed: false,
      ...targetBehaviorPatch(targetMode),
    });
  };

  const chooseNamespaceMode = (namespaceMode: RecoveryWizardConfig['namespaceMode']) => {
    const targetMode = inferTargetMode(config.targetCluster, namespaceMode, currentTargetClusterName);
    updateConfig({
      namespaceMode,
      targetMode,
      targetNamespace: namespaceMode === 'original' ? sourceNamespace : namespaceMode === 'generated' ? generatedNamespace : config.targetNamespace,
      originalNamespaceConfirmed: false,
      ...targetBehaviorPatch(targetMode),
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
      ...targetBehaviorPatch(targetMode),
    });
  };

  const chooseCloneMode = () => {
    const targetMode = inferTargetMode(config.targetCluster, 'generated', currentTargetClusterName);
    updateConfig({
      namespaceMode: 'generated',
      targetMode,
      targetNamespace: generatedNamespace,
      restoreMode: 'full',
      conflictPolicy: 'skip',
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
            onClick={onClose}
          />
          <motion.div
            initial={{ opacity: 0, y: 18, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 18, scale: 0.98 }}
            className="hbdr-protect-dialog hbdr-protect-dialog-v2 hbdr-recovery-dialog"
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
                <button type="button" onClick={onClose} aria-label="Close recovery wizard"><X size={18} /></button>
              </div>
            </div>

            <div className="hbdr-protect-body hbdr-recovery-body">
              <div className="hbdr-recovery-single-page">
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Restore point</strong>
                  </div>

                  <div className="hbdr-recovery-point-picker">
                    <label>
                      <span>Snapshot type</span>
                      <div className="hbdr-recovery-source-tabs">
                        {(['snapshot', 'export'] as const).map(sourceType => {
                          const meta = sourceMeta(sourceType);
                          return (
                            <button
                              key={sourceType}
                              type="button"
                              className={config.sourceType === sourceType ? 'is-active' : ''}
                              onClick={() => chooseSourceType(sourceType)}
                            >
                              {meta.title}
                            </button>
                          );
                        })}
                      </div>
                    </label>
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
                            {point.time} / {point.type}
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
                              {cluster.name}{cluster.name === currentTargetClusterName ? ' / Current' : ''}{cluster.name === configuredTargetCluster ? ' / DR default' : ''}
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

              </div>
            </div>

            <div className="hbdr-protect-footer">
              <button type="button" onClick={onClose}>Cancel</button>
              <button
                type="button"
                className="hbdr-protect-primary"
                disabled={submitDisabled}
                onClick={() => {
                  if (submitDisabled) return;
                  onSubmit();
                }}
              >
                <Play size={15} />{submitLabel}
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
