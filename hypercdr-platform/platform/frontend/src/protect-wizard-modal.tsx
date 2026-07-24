import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  AlertCircle,
  Check,
  ChevronRight,
  Cloud,
  Database,
  FileCode,
  Filter,
  Grid3X3,
  HardDrive,
  PlusCircle,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  ShieldOff,
  Trash2,
  Upload,
  X,
} from 'lucide-react';

type ProtectWizardStep = 1 | 2 | 3 | 4 | 5 | 6;

type ExcludeRule = {
  group: string;
  resource: string;
  name: string;
  version: string;
  labels: string;
};

type ScriptFile = {
  name: string;
  size: number;
  lastModified?: number;
  content: string;
  source: 'upload' | 'manual';
  isEntry?: boolean;
};

type LabelOperator = 'Equals' | 'Not Equals';

type LabelCondition = {
  key: string;
  operator: LabelOperator;
  value: string;
};

type StorageRepo = {
  id: string;
  name: string;
  type: string;
  bucket: string;
  endpoint: string;
};

type PolicyOption = {
  id: string;
  name: string;
  type: string;
  schedule: string;
  retention: string;
  desc: string;
  status: string;
  hasRetention: boolean;
};

type TargetClusterOption = {
  id: string;
  name: string;
  region: string;
  version: string;
  nodes: number;
  applications: number;
  isCurrent: boolean;
};

type ProtectConfig = {
  scope: string;
  labels: string;
  labelConditions: LabelCondition[];
  storageType: string;
  storageId: string;
  policy: string;
  targetCluster: string;
  excludeRules: ExcludeRule[];
  preScripts: ScriptFile[];
  postScripts: ScriptFile[];
};

type Props = {
  open: boolean;
  step: ProtectWizardStep;
  setStep: (step: ProtectWizardStep) => void;
  onClose: () => void;
  onFinish: () => void;
  targetSummary: string;
  protectConfig: ProtectConfig;
  setProtectConfig: React.Dispatch<React.SetStateAction<ProtectConfig>>;
  showAddRuleForm: boolean;
  setShowAddRuleForm: (open: boolean) => void;
  newExcludeRule: ExcludeRule;
  setNewExcludeRule: React.Dispatch<React.SetStateAction<ExcludeRule>>;
  editingRuleIndex: number | null;
  resetExcludeRuleForm: () => void;
  saveExcludeRule: () => void;
  editExcludeRule: (rule: ExcludeRule, index: number) => void;
  storage: StorageRepo[];
  policyOptions: PolicyOption[];
  filteredPolicyOptions: PolicyOption[];
  paginatedPolicyOptions: PolicyOption[];
  wizardPolicySearchQuery: string;
  setWizardPolicySearchQuery: (value: string) => void;
  setWizardPolicyPage: React.Dispatch<React.SetStateAction<number>>;
  wizardPolicyPage: number;
  wizardPolicyTotalPages: number;
  wizardPolicyPageSize: number;
  targetClusterOptions: TargetClusterOption[];
  preScriptRef: React.RefObject<HTMLInputElement | null>;
  postScriptRef: React.RefObject<HTMLInputElement | null>;
  handleFileUpload: (type: 'preScripts' | 'postScripts', event: React.ChangeEvent<HTMLInputElement>) => void;
  saveScript: (type: 'preScripts' | 'postScripts', script: ScriptFile, index?: number | null) => void;
  removeScript: (type: 'preScripts' | 'postScripts', index: number) => void;
  setEntryScript: (type: 'preScripts' | 'postScripts', index: number) => void;
};

const STEP_NAMES = ['Scope', 'Target', 'Storage', 'Policy', 'Hooks', 'Review'];

const SCOPE_OPTIONS = [
  {
    id: 'namespace',
    icon: Grid3X3,
    title: 'Entire Namespace',
    desc: 'Protect workloads, services, config, secrets, and persistent volumes in this namespace.',
    note: 'Recommended for most application DR plans.',
  },
  {
    id: 'stateless',
    icon: ShieldOff,
    title: 'Stateless Resources',
    desc: 'Protect Kubernetes objects only and skip persistent volumes.',
    note: 'Best for services that rebuild state from external systems.',
  },
  {
    id: 'labels',
    icon: Filter,
    title: 'Label Selector',
    desc: 'Protect only resources matched by a Kubernetes label selector.',
    note: 'Use this when a namespace contains multiple logical apps.',
  },
];

const LABEL_KEYS = ['app', 'tier', 'environment', 'app.kubernetes.io/name', 'app.kubernetes.io/part-of'];
const LABEL_VALUES = ['frontend', 'backend', 'production', 'payments', 'mysql', 'web'];
const LABEL_OPERATORS: LabelOperator[] = ['Equals', 'Not Equals'];
const DEFAULT_HOOK_TEMPLATE = `#!/bin/sh
set -e

# Add application-specific hook commands here.
`;

function selectedPolicyName(policyOptions: PolicyOption[], policyId: string) {
  return policyOptions.find(policy => policy.id === policyId)?.name || 'Manual protection';
}

function labelConditionsToSelector(conditions: LabelCondition[]) {
  return conditions
    .filter(condition => condition.key)
    .map(condition => {
      if (!condition.value) return '';
      return condition.operator === 'Not Equals'
        ? `${condition.key}!=${condition.value}`
        : `${condition.key}=${condition.value}`;
    })
    .filter(Boolean)
    .join(',');
}

function summarizeExcludeRule(rule: ExcludeRule) {
  const values = [
    rule.group && `group:${rule.group}`,
    rule.version && `version:${rule.version}`,
    rule.resource && `kind:${rule.resource}`,
    rule.name && `name:${rule.name}`,
    rule.labels && `label:${rule.labels}`,
  ].filter(Boolean);

  return values.join(' / ');
}

function hasExcludeRuleContent(rule: ExcludeRule) {
  return Boolean(summarizeExcludeRule(rule));
}

function scriptRoleSummary(scripts: ScriptFile[]) {
  if (scripts.length === 0) return { entry: 'None', dependencies: '0 dependencies' };
  const entryScript = scripts.find((script, index) => script.isEntry ?? index === 0);
  const dependencyCount = scripts.filter((script, index) => !(script.isEntry ?? index === 0)).length;
  return {
    entry: entryScript?.name || 'None',
    dependencies: `${dependencyCount} ${dependencyCount === 1 ? 'dependency' : 'dependencies'}`,
  };
}

export function ProtectWizardModal(props: Props) {
  const {
    open,
    step,
    setStep,
    onClose,
    onFinish,
    targetSummary,
    protectConfig,
    setProtectConfig,
    showAddRuleForm,
    setShowAddRuleForm,
    newExcludeRule,
    setNewExcludeRule,
    editingRuleIndex,
    resetExcludeRuleForm,
    saveExcludeRule,
    editExcludeRule,
    storage,
    policyOptions,
    filteredPolicyOptions,
    paginatedPolicyOptions,
    wizardPolicySearchQuery,
    setWizardPolicySearchQuery,
    setWizardPolicyPage,
    wizardPolicyPage,
    wizardPolicyTotalPages,
    wizardPolicyPageSize,
    targetClusterOptions,
    preScriptRef,
    postScriptRef,
    handleFileUpload,
    saveScript,
    removeScript,
    setEntryScript,
  } = props;
  const [labelDialogOpen, setLabelDialogOpen] = React.useState(false);
  const [draftLabelCondition, setDraftLabelCondition] = React.useState<LabelCondition>({
    key: 'app',
    operator: 'Equals',
    value: 'frontend',
  });
  const [remoteStorageQuery, setRemoteStorageQuery] = React.useState('');
  const [scriptEditor, setScriptEditor] = React.useState<{
    open: boolean;
    type: 'preScripts' | 'postScripts';
    index: number | null;
    name: string;
    content: string;
    error: string;
  }>({
    open: false,
    type: 'preScripts',
    index: null,
    name: 'pre-backup-hook.sh',
    content: DEFAULT_HOOK_TEMPLATE,
    error: '',
  });

  const selectedStorage = protectConfig.storageType === 'local'
    ? 'Local CSI Snapshot'
    : storage.find(repo => repo.id === protectConfig.storageId)?.name || 'Remote repository required';
  const labelConditions = protectConfig.labelConditions.length > 0
    ? protectConfig.labelConditions
    : [{ key: 'app', operator: 'Equals' as LabelOperator, value: 'frontend' }];
  const selectedScopeTitle = SCOPE_OPTIONS.find(item => item.id === protectConfig.scope)?.title || protectConfig.scope;
  const selectedPolicy = policyOptions.find(policy => policy.id === protectConfig.policy);
  const selectedTargetCluster = protectConfig.targetCluster || 'No default target';
  const activeExcludeRules = protectConfig.excludeRules.filter(hasExcludeRuleContent);
  const labelSelectorSummary = protectConfig.scope === 'labels'
    ? labelConditions.map(condition => `${condition.key}${condition.operator === 'Not Equals' ? '!=' : '='}${condition.value}`).join(' AND ')
    : 'Not used';
  const preHookSummary = scriptRoleSummary(protectConfig.preScripts);
  const postHookSummary = scriptRoleSummary(protectConfig.postScripts);
  const totalHookScripts = protectConfig.preScripts.length + protectConfig.postScripts.length;
  const remoteStorageKeyword = remoteStorageQuery.trim().toLowerCase();
  const filteredStorage = storage.filter(repo => {
    if (!remoteStorageKeyword) return true;
    return [repo.name, repo.type, repo.bucket, repo.endpoint].some(value => value.toLowerCase().includes(remoteStorageKeyword));
  });
  const updateLabelConditions = (conditions: LabelCondition[]) => {
    setProtectConfig(prev => ({
      ...prev,
      labelConditions: conditions,
      labels: labelConditionsToSelector(conditions),
    }));
  };
  const addDraftLabelCondition = () => {
    if (!draftLabelCondition.key || !draftLabelCondition.value) return;
    updateLabelConditions([...labelConditions, draftLabelCondition]);
    setDraftLabelCondition({ key: 'tier', operator: 'Equals', value: 'backend' });
    setLabelDialogOpen(false);
  };
  const removeLabelCondition = (index: number) => {
    updateLabelConditions(labelConditions.filter((_, itemIndex) => itemIndex !== index));
  };
  const nextDisabled =
    (step === 1 && protectConfig.scope === 'labels' && labelConditions.some(condition => !condition.key || !condition.value)) ||
    (step === 3 && protectConfig.storageType === 'remote' && !protectConfig.storageId);
  const openScriptEditor = (
    type: 'preScripts' | 'postScripts',
    index: number | null = null
  ) => {
    const script = index === null ? null : protectConfig[type][index];
    const defaultName = type === 'preScripts' ? 'pre-backup-hook.sh' : 'post-backup-hook.sh';
    setScriptEditor({
      open: true,
      type,
      index,
      name: script?.name || defaultName,
      content: script?.content || DEFAULT_HOOK_TEMPLATE,
      error: '',
    });
  };
  const closeScriptEditor = () => {
    setScriptEditor(prev => ({ ...prev, open: false }));
  };
  const saveScriptEditor = () => {
    const normalizedName = scriptEditor.name.trim() || (scriptEditor.type === 'preScripts' ? 'pre-backup-hook.sh' : 'post-backup-hook.sh');
    const normalizedContent = scriptEditor.content.trimEnd();
    const duplicateName = protectConfig[scriptEditor.type].some((script, index) => (
      index !== scriptEditor.index && script.name.trim().toLowerCase() === normalizedName.toLowerCase()
    ));
    if (duplicateName) {
      setScriptEditor(prev => ({ ...prev, name: normalizedName, error: 'A hook script with this name already exists. Use a unique script name.' }));
      return;
    }
    saveScript(scriptEditor.type, {
      name: normalizedName,
      content: normalizedContent,
      size: new Blob([normalizedContent]).size,
      lastModified: Date.now(),
      source: 'manual',
    }, scriptEditor.index);
    closeScriptEditor();
  };

  return (
    <AnimatePresence>
      {open && (
        <div className="hbdr-protect-modal">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="hbdr-filter-drawer-backdrop hbdr-protect-backdrop"
            onClick={onClose}
          />
          <motion.div
            initial={{ opacity: 0, x: 32 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 32 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="hbdr-filter-drawer hbdr-protect-dialog hbdr-protect-dialog-v2 hbdr-protect-drawer"
          >
            <div className="hbdr-filter-drawer-head hbdr-protect-header">
              <div className="hbdr-protect-title-wrap">
                <div className="hbdr-protect-title-icon"><Settings2 size={18} /></div>
                <div>
                  <h3>DR Configuration</h3>
                  <span>{STEP_NAMES[step - 1]}</span>
                  <em className="hbdr-protect-target">{targetSummary}</em>
                </div>
              </div>
              <div className="hbdr-protect-header-actions">
                <div className="hbdr-protect-progress" aria-hidden="true">
                  {STEP_NAMES.map((name, index) => (
                    <i key={name} className={step >= index + 1 ? 'is-active' : ''} />
                  ))}
                </div>
                <button type="button" onClick={onClose} aria-label="Close"><X size={18} /></button>
              </div>
            </div>

            <div className="hbdr-protect-steps hbdr-protect-steps-v2" aria-label="Configuration steps">
              {STEP_NAMES.map((name, index) => {
                const itemStep = (index + 1) as ProtectWizardStep;
                return (
                  <button
                    key={name}
                    type="button"
                    onClick={() => setStep(itemStep)}
                    className={step === itemStep ? 'hbdr-protect-step-active' : ''}
                  >
                    <strong>{index + 1}</strong>
                    <span>{name}</span>
                  </button>
                );
              })}
            </div>

            <div className="hbdr-filter-drawer-body hbdr-protect-body">
              {step === 1 && (
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Choose what to protect</strong>
                    <span>Start from the namespace model, then add optional filters only when needed.</span>
                  </div>
                  <div className={`hbdr-protect-option-grid ${protectConfig.scope === 'labels' ? 'hbdr-protect-option-grid-tabbed' : ''}`}>
                    {SCOPE_OPTIONS.map(item => {
                      const isSelected = protectConfig.scope === item.id;
                      return (
                        <button
                          key={item.id}
                          type="button"
                          onClick={() => setProtectConfig(prev => ({ ...prev, scope: item.id }))}
                          className={isSelected ? 'hbdr-protect-option-active' : ''}
                          aria-pressed={isSelected}
                        >
                          <span className="hbdr-protect-option-check" aria-hidden="true">
                            {isSelected && <Check size={12} />}
                          </span>
                          <item.icon size={22} />
                          <strong>{item.title}</strong>
                          <span>{item.desc}</span>
                          <em>{item.note}</em>
                        </button>
                      );
                    })}
                  </div>

                  {protectConfig.scope === 'labels' && (
                    <div className="hbdr-protect-label-builder">
                      <div className="hbdr-protect-label-builder-head">
                        <div>
                          <strong>Label Selector Conditions</strong>
                          <span>These conditions only apply when Label Selector is the selected protection mode. All conditions must match.</span>
                        </div>
                        <button type="button" onClick={() => setLabelDialogOpen(true)}><PlusCircle size={14} />Add Condition</button>
                      </div>
                      <div className="hbdr-protect-label-condition-list">
                        {labelConditions.map((condition, index) => (
                          <React.Fragment key={`${condition.key}-${index}`}>
                            {index > 0 && <span className="hbdr-protect-rule-connector">AND</span>}
                            <div className="hbdr-protect-label-condition">
                              <span className="hbdr-protect-label-chip-key">{condition.key}</span>
                              <span className="hbdr-protect-label-chip-op">{condition.operator === 'Not Equals' ? '!=' : '='}</span>
                              <span className="hbdr-protect-label-chip-value">{condition.value}</span>
                              <button type="button" disabled={labelConditions.length <= 1} onClick={() => removeLabelCondition(index)} aria-label="Remove condition">
                                <Trash2 size={14} />
                              </button>
                            </div>
                          </React.Fragment>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="hbdr-protect-subpanel">
                    <div className="hbdr-protect-subpanel-head">
                      <div>
                        <strong>Exclude filters</strong>
                        <span>A resource matching any filter will be excluded.</span>
                      </div>
                      <button type="button" onClick={() => setShowAddRuleForm(true)}><PlusCircle size={14} />Add Exclude Filter</button>
                    </div>

                    {protectConfig.excludeRules.some(hasExcludeRuleContent) && (
                      <div className="hbdr-protect-rule-list">
                        {protectConfig.excludeRules.filter(hasExcludeRuleContent).map((rule, index) => (
                          <React.Fragment key={`${rule.group}-${rule.resource}-${index}`}>
                            {index > 0 && <span className="hbdr-protect-rule-connector">OR</span>}
                            <div className="hbdr-protect-rule">
                            <span className="hbdr-protect-rule-chip-main">{summarizeExcludeRule(rule)}</span>
                              <button type="button" onClick={() => setProtectConfig(prev => ({ ...prev, excludeRules: prev.excludeRules.filter((_, i) => i !== index) }))} aria-label="Delete">
                                <Trash2 size={14} />
                              </button>
                            </div>
                          </React.Fragment>
                        ))}
                      </div>
                    )}

                    {showAddRuleForm && (
                      <div className="hbdr-protect-rule-form-panel hbdr-protect-rule-popover">
                        <div className="hbdr-protect-rule-form-head hbdr-protect-rule-popover-head">
                          <strong>{editingRuleIndex === null ? 'New exclude filter' : 'Edit exclude filter'}</strong>
                          <button type="button" onClick={resetExcludeRuleForm} aria-label="Close"><X size={16} /></button>
                        </div>
                        <div className="hbdr-protect-rule-form-grid">
                          <label><span>API group</span>
                            <select value={newExcludeRule.group} onChange={e => setNewExcludeRule(prev => ({ ...prev, group: e.target.value }))}>
                              <option value="">Any API group</option>
                              {['core', 'apps', 'batch', 'networking.k8s.io'].map(item => <option key={item} value={item}>{item}</option>)}
                            </select>
                          </label>
                          <label><span>Version</span>
                            <select value={newExcludeRule.version} onChange={e => setNewExcludeRule(prev => ({ ...prev, version: e.target.value }))}>
                              <option value="">Any version</option>
                              {['v1', 'v1beta1', 'v1alpha1'].map(item => <option key={item} value={item}>{item}</option>)}
                            </select>
                          </label>
                          <label><span>Resource kind</span>
                            <select value={newExcludeRule.resource} onChange={e => setNewExcludeRule(prev => ({ ...prev, resource: e.target.value }))}>
                              <option value="">Any kind</option>
                              {['pods', 'deployments', 'statefulsets', 'services', 'jobs', 'configmaps', 'secrets'].map(item => <option key={item} value={item}>{item}</option>)}
                            </select>
                          </label>
                          <label><span>Resource name</span>
                            <select value={newExcludeRule.name} onChange={e => setNewExcludeRule(prev => ({ ...prev, name: e.target.value }))}>
                              <option value="">Any name</option>
                              {['debug-*', 'tmp-*', 'migration-job', 'canary'].map(item => <option key={item} value={item}>{item}</option>)}
                            </select>
                          </label>
                          <label className="hbdr-protect-rule-form-wide"><span>Labels</span>
                            <select value={newExcludeRule.labels} onChange={e => setNewExcludeRule(prev => ({ ...prev, labels: e.target.value }))}>
                              <option value="">No label filter</option>
                              {['backup=false', 'temporary=true', 'job-type=batch', 'dr.exclude=true'].map(item => <option key={item} value={item}>{item}</option>)}
                            </select>
                          </label>
                        </div>
                        <button type="button" className="hbdr-protect-rule-submit" onClick={saveExcludeRule}>
                          {editingRuleIndex === null ? 'Add Filter' : 'Update Filter'}
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {step === 2 && (
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Select recovery target</strong>
                    <span>This step is optional. If no default target is selected, operators must choose a target cluster during recovery.</span>
                  </div>
                  <div className="hbdr-protect-target-grid hbdr-protect-target-grid-tabbed">
                    <button
                      type="button"
                      onClick={() => setProtectConfig(prev => ({ ...prev, targetCluster: '' }))}
                      className={!protectConfig.targetCluster ? 'hbdr-protect-target-active hbdr-protect-target-empty' : 'hbdr-protect-target-empty'}
                    >
                      <Server size={20} />
                      <strong>No default target</strong>
                      <span>Require operators to select the target cluster when starting drill or takeover.</span>
                      <em>Recommended when the recovery site may vary.</em>
                      <i>{!protectConfig.targetCluster && <Check size={11} />}</i>
                    </button>
                    {targetClusterOptions.map(cluster => {
                      return (
                        <button
                          key={cluster.id}
                          type="button"
                          onClick={() => setProtectConfig(prev => ({ ...prev, targetCluster: cluster.name }))}
                          className={protectConfig.targetCluster === cluster.name ? 'hbdr-protect-target-active' : ''}
                        >
                          <Server size={18} />
                          <strong>{cluster.name}</strong>
                          <span>Kubernetes {cluster.version} - {cluster.nodes} nodes</span>
                          {cluster.isCurrent && <em>Current Workspace</em>}
                          <i>{protectConfig.targetCluster === cluster.name && <Check size={11} />}</i>
                        </button>
                      );
                    })}
                  </div>
                  <div className="hbdr-protect-note">
                    <AlertCircle size={18} />
                    <div>
                      <strong>{protectConfig.targetCluster ? 'Default target can be adjusted later' : 'No recovery target will be preselected'}</strong>
                      <span>{protectConfig.targetCluster ? 'Changing the target only affects future recovery workflows. Existing restore points remain available.' : 'Recovery workflows remain valid, but drill and takeover must choose a target cluster before execution.'}</span>
                    </div>
                  </div>
                </div>
              )}

              {step === 3 && (
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-section-title">
                    <strong>Choose backup storage</strong>
                    <span>Select a remote repository for recoverable off-cluster protection.</span>
                  </div>
                  <div className={`hbdr-protect-storage-choice ${protectConfig.storageType === 'remote' ? 'hbdr-protect-storage-choice-tabbed' : ''}`}>
                    <button type="button" disabled aria-disabled="true">
                      <HardDrive size={20} />
                      <strong>Local CSI Snapshot</strong>
                      <span>Not supported yet</span>
                    </button>
                    <button type="button" onClick={() => setProtectConfig(prev => ({ ...prev, storageType: 'remote', storageId: prev.storageId || storage[0]?.id || '' }))} className={protectConfig.storageType === 'remote' ? 'hbdr-protect-choice-active' : ''}>
                      <Cloud size={20} />
                      <strong>Remote Repository</strong>
                      <span>Recommended for drill, restore, failover, and takeover scenarios.</span>
                      {protectConfig.storageType === 'remote' && <i className="hbdr-protect-choice-check"><Check size={11} /></i>}
                    </button>
                  </div>
                  {protectConfig.storageType === 'local' ? (
                    <div className="hbdr-protect-note">
                      <AlertCircle size={18} />
                      <div>
                        <strong>Local protection selected</strong>
                        <span>The plan will use the cluster CSI snapshot capability. Select remote storage when cross-cluster recovery is required.</span>
                      </div>
                    </div>
                  ) : (
                    <div className="hbdr-protect-storage-panel">
                      <div className="hbdr-protect-storage-panel-head">
                        <div>
                          <strong>Remote repositories <em>{filteredStorage.length}</em></strong>
                          <span>Select one repository for cross-cluster restore, drill, and takeover workflows.</span>
                        </div>
                        <label className="hbdr-protect-storage-search">
                          <Search size={14} />
                          <input value={remoteStorageQuery} onChange={event => setRemoteStorageQuery(event.target.value)} placeholder="Search repository" />
                        </label>
                      </div>
                      <div className="hbdr-protect-storage-grid">
                        {filteredStorage.map(repo => (
                          <button key={repo.id} type="button" onClick={() => setProtectConfig(prev => ({ ...prev, storageId: repo.id }))} className={protectConfig.storageId === repo.id ? 'hbdr-protect-repo-active' : ''}>
                            <div>
                              <Database size={16} />
                              <strong>{repo.name}</strong>
                              <em>{repo.type}</em>
                            </div>
                            <span>Bucket: <b>{repo.bucket}</b></span>
                            <span>Endpoint: <b>{repo.endpoint}</b></span>
                            <i>{protectConfig.storageId === repo.id && <Check size={11} />}</i>
                          </button>
                        ))}
                        {filteredStorage.length === 0 && (
                          <div className="hbdr-protect-storage-empty">No repositories match the current search.</div>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {step === 4 && (
                <div className="hbdr-protect-section">
                  {protectConfig.policy === 'manual' && (
                    <div className="hbdr-protect-info-bar">
                      <AlertCircle size={16} />
                      <span>Manual mode is selected. No scheduled backup job will be created.</span>
                    </div>
                  )}
                  <div className="hbdr-protect-policy-toolbar">
                    <strong>Protection policies <em>{filteredPolicyOptions.length}</em></strong>
                    <label className="hbdr-protect-search">
                      <Search size={15} />
                      <input
                        value={wizardPolicySearchQuery}
                        onChange={event => { setWizardPolicySearchQuery(event.target.value); setWizardPolicyPage(1); }}
                        placeholder="Search by policy name, schedule, or type"
                      />
                    </label>
                  </div>
                  <div className="hbdr-protect-policy-list">
                    {paginatedPolicyOptions.map(policy => (
                      <button
                        key={policy.id}
                        type="button"
                        onClick={() => setProtectConfig(prev => ({ ...prev, policy: policy.id }))}
                        className={protectConfig.policy === policy.id ? 'hbdr-protect-policy-active' : ''}
                      >
                        <div className="hbdr-protect-policy-top">
                          <ShieldCheck size={17} />
                          <div>
                            <strong>{policy.name}</strong>
                            <span>{policy.desc}</span>
                          </div>
                          {protectConfig.policy === policy.id ? (
                            <i className="hbdr-protect-policy-check"><Check size={12} /></i>
                          ) : (
                            <i className="hbdr-protect-policy-radio" />
                          )}
                        </div>
                        <div className="hbdr-protect-policy-tags">
                          <em>{policy.schedule}</em>
                          <em className="is-retention">{policy.hasRetention ? `Retention: ${policy.retention}` : policy.retention}</em>
                          {policy.status !== 'Active' && <em>{policy.status}</em>}
                        </div>
                      </button>
                    ))}
                  </div>
                  {filteredPolicyOptions.length === 0 && (
                    <div className="hbdr-protect-empty">
                      <Search size={24} />
                      <strong>No matching policies</strong>
                      <span>Change the search keyword or create a new policy from Policy Management.</span>
                    </div>
                  )}
                  {filteredPolicyOptions.length > wizardPolicyPageSize && (
                    <div className="hbdr-protect-pager">
                      <button type="button" disabled={wizardPolicyPage <= 1} onClick={() => setWizardPolicyPage(prev => Math.max(1, prev - 1))}>Previous</button>
                      <span>{wizardPolicyPage} / {wizardPolicyTotalPages}</span>
                      <button type="button" disabled={wizardPolicyPage >= wizardPolicyTotalPages} onClick={() => setWizardPolicyPage(prev => Math.min(wizardPolicyTotalPages, prev + 1))}>Next</button>
                    </div>
                  )}
                </div>
              )}

              {step === 5 && (
                <div className="hbdr-protect-section hbdr-protect-hooks-unavailable">
                  <div className="hbdr-hooks-not-supported">
                    <strong>Execution Hooks</strong>
                    <span>Not supported yet</span>
                    <p>Pre- and post-backup scripts are not available in this version.</p>
                  </div>
                  <div className="hbdr-protect-hooks-intro">
                    <strong>Execution Hooks</strong>
                    <span>Entry is executed by the platform. Dependencies are called by Entry.</span>
                  </div>
                  <input ref={preScriptRef} type="file" className="hidden" onChange={event => handleFileUpload('preScripts', event)} />
                  <input ref={postScriptRef} type="file" className="hidden" onChange={event => handleFileUpload('postScripts', event)} />
                  {[
                    { key: 'preScripts' as const, title: 'Pre-backup Hook', timing: 'Runs before each backup job starts.', emptyTitle: 'No pre-backup hooks', emptyText: 'Add a script only when the application must be quiesced, flushed, or validated before backup.' },
                    { key: 'postScripts' as const, title: 'Post-backup Hook', timing: 'Runs after the backup job finishes.', emptyTitle: 'No post-backup hooks', emptyText: 'Add a script only when the application needs resume, cleanup, or notification after backup.' },
                  ].map(section => (
                    <div key={section.key} className="hbdr-protect-script-box">
                      <div className="hbdr-protect-script-head">
                        <div>
                          <FileCode size={18} />
                          <span>
                            <strong>{section.title}</strong>
                            <em>{section.timing}</em>
                          </span>
                        </div>
                        <div className="hbdr-protect-script-head-actions">
                          <button type="button" onClick={() => openScriptEditor(section.key)}>
                            <FileCode size={13} />Create Script
                          </button>
                          <button type="button" onClick={() => (section.key === 'preScripts' ? preScriptRef : postScriptRef).current?.click()}>
                            <Upload size={13} />Upload Script
                          </button>
                        </div>
                      </div>
                      <div className="hbdr-protect-script-list">
                        {protectConfig[section.key].length === 0 ? (
                          <div className="hbdr-protect-script-empty">
                            <span className="hbdr-protect-script-empty-icon"><FileCode size={20} /></span>
                            <strong>{section.emptyTitle}</strong>
                            <span>{section.emptyText}</span>
                          </div>
                        ) : protectConfig[section.key].map((script, index) => {
                          const isEntry = script.isEntry ?? index === 0;
                          return (
                          <div key={`${script.name}-${index}`}>
                            <span className={`hbdr-protect-script-role ${isEntry ? 'is-entry' : 'is-dependency'}`}>
                              {isEntry ? 'Entry' : 'Dependency'}
                            </span>
                            <span className="hbdr-protect-script-meta">
                              <strong>{script.name}</strong>
                              <em>
                                {isEntry ? 'Executed by platform' : 'Called by Entry'} - {script.source === 'manual' ? 'Saved' : 'Uploaded'} - {(script.size / 1024).toFixed(1)} KB
                              </em>
                            </span>
                            <div className="hbdr-protect-script-actions">
                              {!isEntry && <button type="button" onClick={() => setEntryScript(section.key, index)}>Set as entry</button>}
                              <button type="button" onClick={() => openScriptEditor(section.key, index)}>Edit</button>
                              <button type="button" onClick={() => removeScript(section.key, index)}><Trash2 size={14} /></button>
                            </div>
                          </div>
                        );
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {step === 6 && (
                <div className="hbdr-protect-section">
                  <div className="hbdr-protect-review-panel">
                    <div className="hbdr-protect-review-head">
                      <strong>Configuration Review</strong>
                      <span>Selected: {targetSummary} - Apply result: Move to Start DR</span>
                    </div>
                    <div className="hbdr-protect-review-layout hbdr-protect-review-step-list">
                      <div className="hbdr-protect-review-card is-primary">
                        <div className="hbdr-protect-review-card-head">
                          <span>1</span>
                          <strong>Scope</strong>
                          <ShieldCheck size={15} />
                        </div>
                        <dl>
                          <div><dt>Mode</dt><dd>{selectedScopeTitle}</dd></div>
                          {protectConfig.scope === 'labels' && <div><dt>Label selector</dt><dd>{labelSelectorSummary}</dd></div>}
                          {activeExcludeRules.length > 0 && (
                            <div className="hbdr-protect-review-filter-field">
                              <dt>Exclude filters</dt>
                              <dd className="hbdr-protect-review-filter-chips">
                                {activeExcludeRules.map((rule, index) => <span key={`${rule.group}-${rule.resource}-${index}`}>{summarizeExcludeRule(rule)}</span>)}
                              </dd>
                            </div>
                          )}
                        </dl>
                      </div>
                      <div className="hbdr-protect-review-card">
                        <div className="hbdr-protect-review-card-head">
                          <span>2</span>
                          <strong>Target</strong>
                          <Server size={15} />
                        </div>
                        <dl>
                          <div><dt>Default target</dt><dd>{selectedTargetCluster}</dd></div>
                        </dl>
                      </div>
                      <div className="hbdr-protect-review-card">
                        <div className="hbdr-protect-review-card-head">
                          <span>3</span>
                          <strong>Storage</strong>
                          <Database size={15} />
                        </div>
                        <dl>
                          <div><dt>Storage mode</dt><dd>{protectConfig.storageType === 'local' ? 'Local CSI Snapshot' : 'Remote Repository'}</dd></div>
                          {protectConfig.storageType === 'remote' && <div><dt>Repository</dt><dd>{selectedStorage}</dd></div>}
                        </dl>
                      </div>
                      <div className="hbdr-protect-review-card">
                        <div className="hbdr-protect-review-card-head">
                          <span>4</span>
                          <strong>Policy</strong>
                          <Settings2 size={15} />
                        </div>
                        <dl>
                          <div><dt>Policy</dt><dd>{selectedPolicyName(policyOptions, protectConfig.policy)}</dd></div>
                          <div><dt>{selectedPolicy?.schedule === 'Manual trigger' ? 'Trigger' : 'Schedule'}</dt><dd>{selectedPolicy?.schedule || 'Manual trigger'}</dd></div>
                          {selectedPolicy?.hasRetention && <div><dt>Retention</dt><dd>{selectedPolicy.retention}</dd></div>}
                        </dl>
                      </div>
                      <div className="hbdr-protect-review-card">
                        <div className="hbdr-protect-review-card-head">
                          <span>5</span>
                          <strong>Hooks</strong>
                          <FileCode size={15} />
                        </div>
                        {totalHookScripts === 0 ? (
                          <dl>
                            <div><dt>Hook execution</dt><dd>Not configured</dd></div>
                          </dl>
                        ) : (
                          <div className="hbdr-protect-review-hooks">
                            {protectConfig.preScripts.length > 0 && (
                              <div>
                                <span>Pre-backup entry</span>
                                <strong>{preHookSummary.entry}</strong>
                                <em>{preHookSummary.dependencies}</em>
                              </div>
                            )}
                            {protectConfig.postScripts.length > 0 && (
                              <div>
                                <span>Post-backup entry</span>
                                <strong>{postHookSummary.entry}</strong>
                                <em>{postHookSummary.dependencies}</em>
                              </div>
                            )}
                            <div>
                              <span>Total scripts</span>
                              <strong>{totalHookScripts}</strong>
                              <em>Only Entry scripts are executed</em>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                  <div className="hbdr-protect-review-notice">
                    <Check size={15} />
                    <span>Applying this configuration updates protection settings only. Existing restore points are retained, and synchronization can start from the Start DR stage.</span>
                  </div>
                </div>
              )}

              {scriptEditor.open && (
                <div className="hbdr-protect-script-editor">
                  <div className="hbdr-protect-script-editor-panel">
                    <div className="hbdr-protect-script-editor-head">
                      <div>
                        <strong>{scriptEditor.index === null ? 'Create Hook Script' : 'Edit Hook Script'}</strong>
                        <span>{scriptEditor.type === 'preScripts' ? 'Pre-backup hook' : 'Post-backup hook'}</span>
                      </div>
                      <button type="button" onClick={closeScriptEditor} aria-label="Close"><X size={15} /></button>
                    </div>
                    <label className="hbdr-protect-script-name-field">
                      <span>Script name</span>
                      <input
                        value={scriptEditor.name}
                        className={scriptEditor.error ? 'is-error' : ''}
                        onChange={event => setScriptEditor(prev => ({ ...prev, name: event.target.value, error: '' }))}
                      />
                      {scriptEditor.error && (
                        <div className="hbdr-protect-script-error">
                          <AlertCircle size={14} />
                          <span>{scriptEditor.error}</span>
                        </div>
                      )}
                    </label>
                    <label className="hbdr-protect-script-code-field">
                      <span>Script content</span>
                      <textarea
                        value={scriptEditor.content}
                        spellCheck={false}
                        onChange={event => setScriptEditor(prev => ({ ...prev, content: event.target.value }))}
                      />
                    </label>
                    <div className="hbdr-protect-script-editor-actions">
                      <button type="button" onClick={closeScriptEditor}>Cancel</button>
                      <button type="button" className="hbdr-protect-script-editor-primary" onClick={saveScriptEditor}>Save Script</button>
                    </div>
                  </div>
                </div>
              )}
            </div>

            {labelDialogOpen && (
              <div className="hbdr-protect-label-popover-layer">
                <div className="hbdr-protect-label-popover" role="dialog" aria-modal="true" aria-label="Add label condition">
                  <div className="hbdr-protect-label-popover-head">
                    <div>
                      <strong>Add label condition</strong>
                      <span>Select one rule. It will be added to the condition list below.</span>
                    </div>
                    <button type="button" onClick={() => setLabelDialogOpen(false)} aria-label="Close"><X size={14} /></button>
                  </div>
                  <div className="hbdr-protect-label-popover-grid">
                    <label>
                      <span className="hbdr-protect-label-popover-kicker">Label key</span>
                      <select value={draftLabelCondition.key} onChange={event => setDraftLabelCondition(prev => ({ ...prev, key: event.target.value }))}>
                        {LABEL_KEYS.map(key => <option key={key} value={key}>{key}</option>)}
                      </select>
                    </label>
                    <label>
                      <span className="hbdr-protect-label-popover-kicker">Match rule</span>
                      <select value={draftLabelCondition.operator} onChange={event => setDraftLabelCondition(prev => ({ ...prev, operator: event.target.value as LabelOperator }))}>
                        {LABEL_OPERATORS.map(operator => <option key={operator} value={operator}>{operator}</option>)}
                      </select>
                    </label>
                    <label>
                      <span className="hbdr-protect-label-popover-kicker">Label value</span>
                      <select value={draftLabelCondition.value} onChange={event => setDraftLabelCondition(prev => ({ ...prev, value: event.target.value }))}>
                        {LABEL_VALUES.map(value => <option key={value} value={value}>{value}</option>)}
                      </select>
                    </label>
                  </div>
                  <div className="hbdr-protect-label-popover-actions">
                    <button type="button" onClick={() => setLabelDialogOpen(false)}>Cancel</button>
                    <button type="button" className="hbdr-protect-label-popover-primary" onClick={addDraftLabelCondition}>Add</button>
                  </div>
                </div>
              </div>
            )}

            <div className="hbdr-filter-drawer-actions hbdr-protect-footer">
              <button type="button" onClick={() => step > 1 ? setStep((step - 1) as ProtectWizardStep) : onClose()}>
                {step === 1 ? 'Cancel' : 'Back'}
              </button>
              <button
                type="button"
                className="hbdr-protect-primary"
                disabled={nextDisabled}
                onClick={() => step < 6 ? setStep((step + 1) as ProtectWizardStep) : onFinish()}
              >
                {step < 6 ? <>Next <ChevronRight size={15} /></> : <><Check size={15} />Apply Configuration</>}
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
