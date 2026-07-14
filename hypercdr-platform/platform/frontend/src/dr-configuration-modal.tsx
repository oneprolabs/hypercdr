import React, { useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  Check,
  ChevronDown,
  FileCode,
  Filter,
  Grid3X3,
  Layers3,
  Pencil,
  PlusCircle,
  Settings2,
  ShieldCheck,
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

type LabelResourceMatch = {
  id: string;
  name: string;
  namespace: string;
  kind: string;
  category: 'workloads' | 'network' | 'storage' | 'config' | 'access' | 'jobs' | 'scaling' | 'policy' | 'other' | 'namespace';
  labels: Record<string, string>;
};

type LabelSelectorOption = {
  key: string;
  value: string;
  namespaceNames: string[];
  resources: LabelResourceMatch[];
  summary: string;
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
  includeRules: ExcludeRule[];
  storageType: string;
  storageId: string;
  policy: string;
  targetCluster: string;
  mergeNamespaces?: boolean;
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
  targetCount: number;
  targetNames: string[];
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
  labelOptions: LabelSelectorOption[];
  preScriptRef: React.RefObject<HTMLInputElement | null>;
  postScriptRef: React.RefObject<HTMLInputElement | null>;
  handleFileUpload: (type: 'preScripts' | 'postScripts', event: React.ChangeEvent<HTMLInputElement>) => void;
  saveScript: (type: 'preScripts' | 'postScripts', script: ScriptFile, index?: number | null) => void;
  removeScript: (type: 'preScripts' | 'postScripts', index: number) => void;
  setEntryScript: (type: 'preScripts' | 'postScripts', index: number) => void;
  onCreateStorage?: () => void;
  onRegisterCluster?: () => void;
  onCreatePolicy?: () => void;
};

const SCOPE_OPTIONS = [
  { id: 'all', title: 'All resources', desc: 'Full namespace scope', icon: Grid3X3 },
  { id: 'filter', title: 'Filtered resources', desc: 'Include / exclude rules', icon: Filter },
];

const RESOURCE_OPTIONS = [
  'deployments',
  'statefulsets',
  'daemonsets',
  'replicasets',
  'jobs',
  'cronjobs',
  'services',
  'ingresses',
  'networkpolicies',
  'configmaps',
  'secrets',
  'serviceaccounts',
  'roles',
  'rolebindings',
  'persistentvolumeclaims',
];
const RESOURCE_KIND_ALIASES: Record<string, string> = {
  deployments: 'deployment',
  statefulsets: 'statefulset',
  daemonsets: 'daemonset',
  replicasets: 'replicaset',
  jobs: 'job',
  cronjobs: 'cronjob',
  services: 'service',
  ingresses: 'ingress',
  networkpolicies: 'networkpolicy',
  configmaps: 'configmap',
  secrets: 'secret',
  serviceaccounts: 'serviceaccount',
  roles: 'role',
  rolebindings: 'rolebinding',
  persistentvolumeclaims: 'persistentvolumeclaim',
};
const LABEL_OPERATORS: LabelOperator[] = ['Equals', 'Not Equals'];
const DEFAULT_HOOK_TEMPLATE = `#!/bin/sh
set -e

# Add application-specific hook commands here.
`;

function labelConditionsToSelector(conditions: LabelCondition[]) {
  return conditions
    .filter(condition => condition.key && condition.value)
    .map(condition => condition.operator === 'Not Equals'
      ? `${condition.key}!=${condition.value}`
      : `${condition.key}=${condition.value}`)
    .join(',');
}

function summarizeRule(rule: ExcludeRule) {
  const resources = splitRuleValues(rule.resource);
  const labels = splitRuleValues(rule.labels);
  return [
    resources.length > 0 && `resources:${resources.join(',')}`,
    labels.length > 0 && `labels:${labels.join(',')}`,
  ].filter(Boolean).join(' / ');
}

function hasRuleContent(rule: ExcludeRule) {
  return Boolean(rule.resource || rule.labels);
}

function splitRuleValues(value: string) {
  return value.split(',').map(item => item.trim()).filter(Boolean);
}

function addRuleValue(current: string, value: string) {
  const next = value.trim();
  if (!next) return current;
  return uniqueSorted([...splitRuleValues(current), next]).join(',');
}

function removeRuleValue(current: string, value: string) {
  return splitRuleValues(current).filter(item => item !== value).join(',');
}

function resourceKindKey(value: string) {
  const lower = value.toLowerCase().replace(/[^a-z0-9]/g, '');
  return RESOURCE_KIND_ALIASES[lower] || lower;
}

function matchesResourceTypes(resource: LabelResourceMatch, types: string[]) {
  if (types.length === 0) return true;
  const kind = resourceKindKey(resource.kind);
  return types.some(type => resourceKindKey(type) === kind);
}

function buildLabelValueMap(options: LabelSelectorOption[]) {
  return options.reduce<Record<string, string[]>>((acc, option) => {
    const values = acc[option.key] || [];
    if (!values.includes(option.value)) {
      acc[option.key] = [...values, option.value].sort((left, right) => left.localeCompare(right));
    }
    return acc;
  }, {});
}

function optionForCondition(options: LabelSelectorOption[], condition: LabelCondition) {
  return options.find(option => option.key === condition.key && option.value === condition.value);
}

function matchesLabelConditions(resource: LabelResourceMatch, conditions: LabelCondition[]) {
  return conditions.every(condition => {
    const actual = resource.labels?.[condition.key];
    if (condition.operator === 'Not Equals') return actual !== condition.value;
    return actual === condition.value;
  });
}

function summarizeMatches(resources: LabelResourceMatch[]) {
  const byKind = resources.reduce<Record<string, LabelResourceMatch[]>>((acc, resource) => {
    const kind = resource.kind || 'Resource';
    acc[kind] = [...(acc[kind] || []), resource];
    return acc;
  }, {});
  return Object.entries(byKind)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([kind, items]) => ({ kind, items }));
}

function uniqueSorted(values: string[]) {
  return values
    .map(value => value.trim())
    .filter(Boolean)
    .filter((value, index, list) => list.indexOf(value) === index)
    .sort((left, right) => left.localeCompare(right));
}

export function DrConfigurationModal(props: Props) {
  const {
    open,
    setStep,
    onClose,
    onFinish,
    targetSummary,
    targetCount,
    targetNames,
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
    targetClusterOptions,
    labelOptions,
    preScriptRef,
    postScriptRef,
    handleFileUpload,
    saveScript,
    removeScript,
    setEntryScript,
    onCreateStorage,
    onRegisterCluster,
    onCreatePolicy,
  } = props;
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [includeFilterOpen, setIncludeFilterOpen] = useState(false);
  const [editingIncludeIndex, setEditingIncludeIndex] = useState<number | null>(null);
  const [newIncludeRule, setNewIncludeRule] = useState<ExcludeRule>({ group: '', resource: '', name: '', version: '', labels: '' });
  const headerSummary = targetCount === 1
    ? `Namespace: ${targetNames[0] || targetSummary}`
    : `${targetCount} namespaces selected`;
  const multiNamespaceFilterDisabled = targetCount > 1;
  const labelValueMap = useMemo(() => buildLabelValueMap(labelOptions), [labelOptions]);
  const labelKeys = useMemo(() => Object.keys(labelValueMap).sort(), [labelValueMap]);
  const firstLabelKey = labelKeys[0] || '';
  const firstLabelValue = firstLabelKey ? (labelValueMap[firstLabelKey]?.[0] || '') : '';
  const [draftLabelCondition, setDraftLabelCondition] = useState<LabelCondition>({ key: firstLabelKey, operator: 'Equals', value: firstLabelValue });
  React.useEffect(() => {
    setDraftLabelCondition(current => {
      if (current.key && labelValueMap[current.key]?.includes(current.value)) return current;
      return { key: firstLabelKey, operator: 'Equals', value: firstLabelValue };
    });
  }, [firstLabelKey, firstLabelValue, labelValueMap]);
  React.useEffect(() => {
    if (!multiNamespaceFilterDisabled || protectConfig.scope !== 'filter') return;
    setProtectConfig(prev => ({ ...prev, scope: 'all' }));
  }, [multiNamespaceFilterDisabled, protectConfig.scope, setProtectConfig]);

  const canSave = Boolean(protectConfig.storageId && (protectConfig.targetCluster || targetClusterOptions.length === 0));
  void filteredPolicyOptions;
  void paginatedPolicyOptions;
  void wizardPolicySearchQuery;
  void setWizardPolicySearchQuery;
  void setWizardPolicyPage;
  void wizardPolicyPage;
  void wizardPolicyTotalPages;
  const allLabelResources = useMemo(() => {
    const map = new Map<string, LabelResourceMatch>();
    labelOptions.forEach(option => {
      option.resources.forEach(resource => map.set(resource.id, resource));
    });
    return Array.from(map.values());
  }, [labelOptions]);
  const knownLabelOptions = useMemo(
    () => uniqueSorted(labelOptions.map(option => `${option.key}=${option.value}`)),
    [labelOptions]
  );
  const includeRuleResources = splitRuleValues(newIncludeRule.resource);
  const excludeRuleResources = splitRuleValues(newExcludeRule.resource);
  const includeRuleLabels = splitRuleValues(newIncludeRule.labels);
  const renderRulePreview = (rule: ExcludeRule, labels: string[], mode: 'include' | 'exclude') => {
    const resources = splitRuleValues(rule.resource);
    const criteria = [
      resources.length > 0 && { label: 'resource type is', values: resources },
    ].filter(Boolean) as Array<{ label: string; values: string[] }>;
    const conditions: Array<{ label: string; values: string[] }> = [
      ...criteria,
      ...(labels.length > 0 ? [{ label: 'labels include', values: labels }] : []),
    ];
    const empty = criteria.length === 0 && labels.length === 0;
    return (
      <div className="hbdr-config-filter-preview">
        <ChevronDown size={14} />
        {empty ? (
          <span className="hbdr-config-filter-preview-empty">Select at least one criterion.</span>
        ) : (
          <div className="hbdr-config-filter-preview-content">
            <span>{mode === 'include' ? 'Apply to resources where' : 'Exclude resources where'}</span>
            <div className="hbdr-config-filter-preview-sentence">
              {conditions.map((condition, index) => (
                <React.Fragment key={`${condition.label}-${condition.values.join('|')}`}>
                  {index > 0 && <strong className="hbdr-config-filter-preview-and-word">and</strong>}
                  <strong>{condition.label}</strong>
                  {condition.values.map(value => <em key={value}>{value}</em>)}
                </React.Fragment>
              ))}
            </div>
          </div>
        )}
      </div>
    );
  };
  const [openMultiSelect, setOpenMultiSelect] = React.useState<string | null>(null);
  const renderMultiSelect = (
    id: string,
    label: string,
    placeholder: string,
    options: string[],
    selected: string[],
    onToggle: (value: string) => void,
  ) => {
    const open = openMultiSelect === id;
    return (
      <div className="hbdr-config-multiselect">
        <div className="hbdr-config-multiselect-head">
          <strong>{label}</strong>
        </div>
        <div
          className={`hbdr-config-multiselect-control ${open ? 'is-open' : ''}`}
          onClick={() => setOpenMultiSelect(current => current === id ? null : id)}
        >
          <div className="hbdr-config-multiselect-values">
            {selected.length === 0 ? (
              <span className="hbdr-config-multiselect-placeholder">{placeholder}</span>
            ) : selected.map(value => (
              <span key={value} className="hbdr-config-multiselect-chip">
                {value}
                <X
                  size={11}
                  onClick={event => {
                    event.stopPropagation();
                    onToggle(value);
                  }}
                />
              </span>
            ))}
          </div>
          <ChevronDown size={14} />
        </div>
        {open && (
          <div className="hbdr-config-multiselect-menu">
            {options.length === 0 ? (
              <span className="hbdr-config-multiselect-empty">No options available</span>
            ) : options.map(option => {
              const checked = selected.includes(option);
              return (
                <label
                  key={option}
                  className={checked ? 'is-selected' : ''}
                  onClick={event => event.stopPropagation()}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => onToggle(option)}
                  />
                  <span>{option}</span>
                </label>
              );
            })}
          </div>
        )}
      </div>
    );
  };
  const includeLabelConditions = useMemo(() => {
    const fromRules = (protectConfig.includeRules || [])
      .flatMap(rule => rule.labels.split(','))
      .map(value => value.trim())
      .map(value => {
        const separator = value.indexOf('=');
        if (separator <= 0 || separator === value.length - 1) return null;
        return { key: value.slice(0, separator).trim(), operator: 'Equals' as LabelOperator, value: value.slice(separator + 1).trim() };
      })
      .filter((condition): condition is LabelCondition => Boolean(condition));
    return fromRules.length > 0 ? fromRules : protectConfig.labelConditions;
  }, [protectConfig.includeRules, protectConfig.labelConditions]);
  const matchedResources = useMemo(() => {
    const includeResources = (protectConfig.includeRules || []).flatMap(rule => splitRuleValues(rule.resource));
    const excludeResources = (protectConfig.excludeRules || []).flatMap(rule => splitRuleValues(rule.resource));
    if (includeLabelConditions.length === 0 && includeResources.length === 0 && excludeResources.length === 0) return [];
    return allLabelResources
      .filter(resource => matchesResourceTypes(resource, includeResources))
      .filter(resource => matchesLabelConditions(resource, includeLabelConditions))
      .filter(resource => excludeResources.length === 0 || !matchesResourceTypes(resource, excludeResources));
  }, [allLabelResources, includeLabelConditions, protectConfig.excludeRules, protectConfig.includeRules]);
  const selectedLabelOptions = useMemo(
    () => includeLabelConditions
      .map(condition => optionForCondition(labelOptions, condition))
      .filter((option): option is LabelSelectorOption => Boolean(option)),
    [labelOptions, includeLabelConditions]
  );
  const matchedGroups = useMemo(() => summarizeMatches(matchedResources), [matchedResources]);
  const hasResourceFilters = useMemo(() => {
    const includeResources = (protectConfig.includeRules || []).some(rule => splitRuleValues(rule.resource).length > 0);
    const excludeResources = (protectConfig.excludeRules || []).some(rule => splitRuleValues(rule.resource).length > 0);
    return includeResources || excludeResources || includeLabelConditions.length > 0;
  }, [includeLabelConditions.length, protectConfig.excludeRules, protectConfig.includeRules]);

  const updateScope = (scope: string) => {
    if (scope === 'filter' && multiNamespaceFilterDisabled) return;
    setStep(1);
    setProtectConfig(prev => ({ ...prev, scope }));
  };

  const closeIncludeFilter = () => {
    setIncludeFilterOpen(false);
    setEditingIncludeIndex(null);
    setOpenMultiSelect(null);
    setNewIncludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
  };

  const openIncludeFilter = () => {
    setEditingIncludeIndex(null);
    setNewIncludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
    setIncludeFilterOpen(true);
  };

  const editIncludeFilter = (rule: ExcludeRule, index: number) => {
    setNewIncludeRule(rule);
    setEditingIncludeIndex(index);
    setIncludeFilterOpen(true);
  };

  const saveIncludeRule = () => {
    const normalizedRule = {
      group: '',
      resource: newIncludeRule.resource,
      name: '',
      version: '',
      labels: newIncludeRule.labels,
    };
    if (!hasRuleContent(normalizedRule)) {
      closeIncludeFilter();
      return;
    }
    setProtectConfig(prev => ({
      ...prev,
      includeRules: editingIncludeIndex === null
        ? [...(prev.includeRules || []), normalizedRule]
        : (prev.includeRules || []).map((rule, index) => index === editingIncludeIndex ? normalizedRule : rule),
    }));
    closeIncludeFilter();
  };
  const closeExcludeFilter = () => {
    resetExcludeRuleForm();
  };
  const handleSaveExcludeRule = () => {
    saveExcludeRule();
  };
  const createManualScript = (type: 'preScripts' | 'postScripts') => {
    const name = `${type === 'preScripts' ? 'pre' : 'post'}-hook-${Date.now()}.sh`;
    saveScript(type, {
      name,
      size: DEFAULT_HOOK_TEMPLATE.length,
      content: DEFAULT_HOOK_TEMPLATE,
      source: 'manual',
    });
  };
  const navigateFromConfig = (handler?: () => void) => {
    if (!handler) return;
    onClose();
    handler();
  };

  return (
    <AnimatePresence>
      {open && (
        <div className="hbdr-config-modal">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="hbdr-filter-drawer-backdrop hbdr-config-backdrop"
            onClick={onClose}
          />
          <motion.div
            initial={{ opacity: 0, x: 32 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 32 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="hbdr-filter-drawer hbdr-config-dialog hbdr-config-drawer"
          >
            <header className="hbdr-filter-drawer-head hbdr-config-header">
              <div>
                <h3>DR Configuration</h3>
                <p>{headerSummary}</p>
              </div>
              <button type="button" onClick={onClose} aria-label="Close"><X size={18} /></button>
            </header>

            <div className="hbdr-filter-drawer-body hbdr-config-body">
              <main className="hbdr-config-main">
                {targetCount > 1 && (
                  <section className="hbdr-config-selected-namespaces">
                    <div className="hbdr-config-selected-head">
                      <div className="hbdr-config-selected-title">
                        <Layers3 size={17} />
                        <strong>Namespaces</strong>
                      </div>
                      <em>{targetCount}</em>
                    </div>
                    <div className="hbdr-config-selected-tags">
                      {targetNames.map(name => <span key={name}>{name}</span>)}
                    </div>
                    <div className="hbdr-config-plan-checks" role="group" aria-label="DR plan mode">
                      <label>
                        <input
                          type="checkbox"
                          checked={!protectConfig.mergeNamespaces}
                          onChange={() => setProtectConfig(prev => ({ ...prev, mergeNamespaces: false }))}
                        />
                        <span>Independent DR</span>
                      </label>
                      <label>
                        <input
                          type="checkbox"
                          checked={Boolean(protectConfig.mergeNamespaces)}
                          onChange={() => setProtectConfig(prev => ({ ...prev, mergeNamespaces: true, scope: prev.scope === 'filter' ? 'all' : prev.scope }))}
                        />
                        <span>Merge DR</span>
                      </label>
                    </div>
                  </section>
                )}
                <section className="hbdr-config-section">
                  <div className="hbdr-config-section-head">
                    <ShieldCheck size={17} />
                    <div>
                      <strong>Protection scope</strong>
                    </div>
                  </div>
                  <div className="hbdr-config-scope-grid">
                    {SCOPE_OPTIONS.map(option => {
                      const Icon = option.icon;
                      const active = protectConfig.scope === option.id;
                      const disabled = option.id === 'filter' && multiNamespaceFilterDisabled;
                      return (
                        <button
                          key={option.id}
                          type="button"
                          className={`${active ? 'is-active' : ''} ${disabled ? 'is-disabled' : ''}`}
                          disabled={disabled}
                          title={disabled ? 'Filtered resources are not available when multiple namespaces are selected.' : undefined}
                          onClick={() => updateScope(option.id)}
                        >
                          <Icon size={16} />
                          <strong>{option.title}</strong>
                          <span>{disabled ? 'Not available for multiple namespaces' : option.desc}</span>
                        </button>
                      );
                    })}
                  </div>
                  {protectConfig.scope === 'filter' && (
                    <div className="hbdr-config-inline-panel">
                      <div className="hbdr-config-filter-head">
                        <div>
                          <strong>Resource filters</strong>
                        </div>
                        <em>{(protectConfig.includeRules || []).filter(hasRuleContent).length} include · {protectConfig.excludeRules.filter(hasRuleContent).length} exclude</em>
                      </div>
                      <div className="hbdr-config-filter-block">
                        <div className="hbdr-config-filter-title">
                          <strong>Include filters</strong>
                          <button type="button" onClick={openIncludeFilter}><PlusCircle size={14} />Add include</button>
                        </div>
                        <div className="hbdr-config-filter-list">
                          {(protectConfig.includeRules || []).filter(hasRuleContent).length === 0 ? <span>All resources are included before excludes.</span> : (protectConfig.includeRules || []).filter(hasRuleContent).map((rule, index) => (
                            <div key={`${summarizeRule(rule)}-${index}`} className="hbdr-config-filter-row">
                              <dl>
                                {rule.resource && <div><dt>Resources</dt><dd>{splitRuleValues(rule.resource).join(', ')}</dd></div>}
                                {rule.labels && <div><dt>Labels</dt><dd>{rule.labels}</dd></div>}
                              </dl>
                              <div className="hbdr-config-filter-actions">
                                <button type="button" aria-label="Edit include filter" onClick={() => editIncludeFilter(rule, index)}><Pencil size={13} /></button>
                                <button
                                  type="button"
                                  aria-label="Remove include filter"
                                  onClick={() => setProtectConfig(prev => ({ ...prev, includeRules: (prev.includeRules || []).filter((_, itemIndex) => itemIndex !== index) }))}
                                >
                                  <Trash2 size={13} />
                                </button>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                      <div className="hbdr-config-filter-block">
                        <div className="hbdr-config-filter-title">
                          <strong>Exclude filters</strong>
                          <button type="button" onClick={() => setShowAddRuleForm(true)}><PlusCircle size={14} />Add exclude</button>
                        </div>
                        <div className="hbdr-config-filter-list">
                          {protectConfig.excludeRules.filter(hasRuleContent).length === 0 ? <span>No exclude filters</span> : protectConfig.excludeRules.filter(hasRuleContent).map((rule, index) => (
                            <div key={`${summarizeRule(rule)}-${index}`} className="hbdr-config-filter-row">
                              <dl>
                                {rule.resource && <div><dt>Resources</dt><dd>{splitRuleValues(rule.resource).join(', ')}</dd></div>}
                              </dl>
                              <div className="hbdr-config-filter-actions">
                                <button type="button" aria-label="Edit exclude filter" onClick={() => editExcludeRule(rule, index)}><Pencil size={13} /></button>
                                <button type="button" aria-label="Remove exclude filter" onClick={() => setProtectConfig(prev => ({ ...prev, excludeRules: prev.excludeRules.filter((_, itemIndex) => itemIndex !== index) }))}><Trash2 size={13} /></button>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                      {hasResourceFilters && (
                        <div className="hbdr-config-label-preview">
                          <div className="hbdr-config-label-preview-head">
                            <strong>Matched resources</strong>
                            <span>{matchedResources.length > 0 ? `${matchedResources.length} resources` : 'No resource match'}</span>
                          </div>
                          {matchedResources.length > 0 ? (
                            <div className="hbdr-config-label-preview-grid">
                              {matchedGroups.slice(0, 6).map(group => (
                                <div key={group.kind}>
                                  <strong>{group.kind}</strong>
                                  <span>{group.items.slice(0, 4).map(item => item.name).join(', ')}</span>
                                  {group.items.length > 4 && <em>+{group.items.length - 4} more</em>}
                                </div>
                              ))}
                            </div>
                          ) : (
                            <p>No namespaced resources currently match this selector. Saving it may create backups with no application resources.</p>
                          )}
                          {selectedLabelOptions.some(option => option.namespaceNames.length > 0) && (
                            <p>
                              Namespace labels matched: {selectedLabelOptions
                                .flatMap(option => option.namespaceNames)
                                .filter((name, index, names) => names.indexOf(name) === index)
                                .join(', ')}
                            </p>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </section>

                <section className="hbdr-config-section">
                  <div className="hbdr-config-section-head">
                    <Settings2 size={17} />
                    <div>
                      <strong>Basic settings</strong>
                    </div>
                  </div>
                  <div className="hbdr-config-settings-list">
                    <label className="hbdr-config-setting-row">
                      <span>Backup repository</span>
                      <div className="hbdr-config-select-action">
                        <select value={protectConfig.storageId} onChange={event => setProtectConfig(prev => ({ ...prev, storageId: event.target.value }))}>
                          <option value="">Select repository</option>
                          {storage.map(repo => <option key={repo.id} value={repo.id}>{repo.name} ({repo.type})</option>)}
                        </select>
                        <button type="button" onClick={() => navigateFromConfig(onCreateStorage)}>New storage</button>
                      </div>
                    </label>
                    <label className="hbdr-config-setting-row">
                      <span>Target cluster</span>
                      <div className="hbdr-config-select-action">
                        <select value={protectConfig.targetCluster} onChange={event => setProtectConfig(prev => ({ ...prev, targetCluster: event.target.value }))}>
                          <option value="">Select target cluster</option>
                          {targetClusterOptions.map(cluster => <option key={cluster.id} value={cluster.name}>{cluster.name}{cluster.isCurrent ? ' (source)' : ''}</option>)}
                        </select>
                        <button type="button" onClick={() => navigateFromConfig(onRegisterCluster)}>Register cluster</button>
                      </div>
                    </label>
                    <label className="hbdr-config-setting-row">
                      <span>Backup policy</span>
                      <div className="hbdr-config-select-action">
                        <select value={protectConfig.policy} onChange={event => setProtectConfig(prev => ({ ...prev, policy: event.target.value }))}>
                          <option value="">Select policy</option>
                          {policyOptions.map(policy => <option key={policy.id} value={policy.id}>{policy.name} - {policy.schedule} / {policy.retention}</option>)}
                        </select>
                        <button type="button" onClick={() => navigateFromConfig(onCreatePolicy)}>Create policy</button>
                      </div>
                    </label>
                  </div>
                </section>

                <section className={`hbdr-config-advanced ${advancedOpen ? 'is-open' : ''}`}>
                  <button type="button" className="hbdr-config-advanced-toggle" onClick={() => setAdvancedOpen(prev => !prev)}>
                    <span><Settings2 size={16} />Hooks</span>
                    <ChevronDown size={16} />
                  </button>
                  {advancedOpen && (
                    <div className="hbdr-config-advanced-body">
                      {(['preScripts', 'postScripts'] as const).map(type => {
                        const list = protectConfig[type];
                        return (
                          <div key={type} className="hbdr-config-advanced-block">
                            <div className="hbdr-config-advanced-head">
                              <strong>{type === 'preScripts' ? 'Pre hooks' : 'Post hooks'}</strong>
                              <div>
                                <input ref={type === 'preScripts' ? preScriptRef : postScriptRef} type="file" accept=".sh,.bash,.txt" onChange={event => handleFileUpload(type, event)} hidden />
                                <button type="button" onClick={() => (type === 'preScripts' ? preScriptRef : postScriptRef).current?.click()}><Upload size={14} />Upload</button>
                                <button type="button" onClick={() => createManualScript(type)}><FileCode size={14} />Manual</button>
                              </div>
                            </div>
                            <div className="hbdr-config-script-list">
                              {list.length === 0 ? <span>No hooks configured</span> : list.map((script, index) => {
                                const isEntry = script.isEntry ?? index === 0;
                                return (
                                  <div key={`${script.name}-${index}`}>
                                    <span>{script.name}</span>
                                    <em>{isEntry ? 'Entry' : 'Dependency'}</em>
                                    {!isEntry && <button type="button" onClick={() => setEntryScript(type, index)}>Set entry</button>}
                                    <button type="button" onClick={() => removeScript(type, index)}><Trash2 size={13} /></button>
                                  </div>
                                );
                              })}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </section>
              </main>
            </div>

            <footer className="hbdr-filter-drawer-actions hbdr-config-footer">
              <div className="hbdr-config-footer-status">
                <span className={canSave ? 'is-ready' : ''}>{canSave ? 'Ready' : 'Repository and target cluster required'}</span>
              </div>
              <button type="button" onClick={onClose}>Cancel</button>
              <button type="button" className="hbdr-config-save" disabled={!canSave} onClick={onFinish}>
                <Check size={15} />Save
              </button>
            </footer>
          </motion.div>
          {includeFilterOpen && (
            <div className="hbdr-config-filter-modal">
              <div className="hbdr-config-filter-modal-backdrop" role="presentation" onClick={closeIncludeFilter} />
              <div className="hbdr-config-filter-dialog" onMouseDown={event => {
                if ((event.target as HTMLElement).closest('.hbdr-config-multiselect')) return;
                setOpenMultiSelect(null);
              }}>
                <header>
                  <div>
                    <h4>{editingIncludeIndex === null ? 'Add Include Filter' : 'Edit Include Filter'}</h4>
                  </div>
                  <button type="button" onClick={closeIncludeFilter} aria-label="Close"><X size={16} /></button>
                </header>
                <div className="hbdr-config-filter-dialog-body">
                  <div className="hbdr-config-rule-form">
                    {renderMultiSelect(
                      'include-resources',
                      'Resource types',
                      'Select resource types',
                      RESOURCE_OPTIONS,
                      includeRuleResources,
                      value => setNewIncludeRule(prev => ({
                        ...prev,
                        resource: includeRuleResources.includes(value)
                          ? removeRuleValue(prev.resource, value)
                          : addRuleValue(prev.resource, value),
                      })),
                    )}
                    {renderMultiSelect(
                      'include-labels',
                      'Labels',
                      'Select labels',
                      knownLabelOptions,
                      includeRuleLabels,
                      value => setNewIncludeRule(prev => ({
                        ...prev,
                        labels: includeRuleLabels.includes(value)
                          ? removeRuleValue(prev.labels, value)
                          : addRuleValue(prev.labels, value),
                      })),
                    )}
                  </div>
                  {renderRulePreview(newIncludeRule, includeRuleLabels, 'include')}
                </div>
                <footer>
                  <button type="button" onClick={closeIncludeFilter}>Cancel</button>
                  <button type="button" className="hbdr-config-filter-primary" disabled={!hasRuleContent(newIncludeRule)} onClick={saveIncludeRule}>{editingIncludeIndex === null ? 'Add Filter' : 'Save Filter'}</button>
                </footer>
              </div>
            </div>
          )}
          {showAddRuleForm && (
            <div className="hbdr-config-filter-modal">
              <div className="hbdr-config-filter-modal-backdrop" role="presentation" onClick={closeExcludeFilter} />
              <div className="hbdr-config-filter-dialog" onMouseDown={event => {
                if ((event.target as HTMLElement).closest('.hbdr-config-multiselect')) return;
                setOpenMultiSelect(null);
              }}>
                <header>
                  <div>
                    <h4>{editingRuleIndex === null ? 'Add Exclude Filter' : 'Edit Exclude Filter'}</h4>
                  </div>
                  <button type="button" onClick={closeExcludeFilter} aria-label="Close"><X size={16} /></button>
                </header>
                <div className="hbdr-config-filter-dialog-body">
                  <div className="hbdr-config-rule-form">
                    {renderMultiSelect(
                      'exclude-resources',
                      'Resource types',
                      'Select resource types',
                      RESOURCE_OPTIONS,
                      excludeRuleResources,
                      value => setNewExcludeRule(prev => ({
                        ...prev,
                        resource: excludeRuleResources.includes(value)
                          ? removeRuleValue(prev.resource, value)
                          : addRuleValue(prev.resource, value),
                      })),
                    )}
                  </div>
                  {renderRulePreview(newExcludeRule, [], 'exclude')}
                </div>
                <footer>
                  <button type="button" onClick={closeExcludeFilter}>Cancel</button>
                  <button type="button" className="hbdr-config-filter-primary" disabled={!hasRuleContent(newExcludeRule)} onClick={handleSaveExcludeRule}>{editingRuleIndex === null ? 'Add Filter' : 'Save Filter'}</button>
                </footer>
              </div>
            </div>
          )}
        </div>
      )}
    </AnimatePresence>
  );
}
