import React, { useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  Check,
  ChevronDown,
  Filter,
  Grid3X3,
  Layers3,
  Pencil,
  PlusCircle,
  Settings2,
  ShieldCheck,
  X,
} from 'lucide-react';
import { ScopedResourceSelector, type ScopedResourceOption, type ScopedResourceSelection } from './components/scoped-resource-selector';

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
  includedResources: string[];
  resourceSelection: ScopedResourceSelection;
  includeAllResources: boolean;
  labelSelector: {
    matchLabels: Record<string, string>;
    matchExpressions: Array<{ key: string; operator: string; values: string[] }>;
  };
  excludedResources: string[];
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
  submitting?: boolean;
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
  namespaceResourceOptions?: ScopedResourceOption[];
  customResourcesLoaded?: boolean;
  onRequestCustomResources?: () => Promise<boolean>;
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

const NAMESPACE_RESOURCE_OPTIONS = [
  'deployments.apps',
  'statefulsets.apps',
  'daemonsets.apps',
  'replicasets.apps',
  'jobs.batch',
  'cronjobs.batch',
  'services',
  'ingresses.networking.k8s.io',
  'networkpolicies.networking.k8s.io',
  'configmaps',
  'secrets',
  'serviceaccounts',
  'roles.rbac.authorization.k8s.io',
  'rolebindings.rbac.authorization.k8s.io',
  'persistentvolumeclaims',
];
const CLUSTER_RESOURCE_OPTIONS = [
  'persistentvolumes',
  'storageclasses.storage.k8s.io',
  'customresourcedefinitions.apiextensions.k8s.io',
  'clusterroles.rbac.authorization.k8s.io',
  'clusterrolebindings.rbac.authorization.k8s.io',
  'volumesnapshotclasses.snapshot.storage.k8s.io',
];
// Retained for legacy filter-plan rendering and migration summaries. New plans
// use NAMESPACE_RESOURCE_OPTIONS through the scoped selector below.
const RESOURCE_OPTIONS = NAMESPACE_RESOURCE_OPTIONS;
const RESOURCE_KIND_ALIASES: Record<string, string> = {
  'deployments.apps': 'deployment',
  'statefulsets.apps': 'statefulset',
  'daemonsets.apps': 'daemonset',
  'replicasets.apps': 'replicaset',
  'jobs.batch': 'job',
  'cronjobs.batch': 'cronjob',
  services: 'service',
  'ingresses.networking.k8s.io': 'ingress',
  'networkpolicies.networking.k8s.io': 'networkpolicy',
  configmaps: 'configmap',
  secrets: 'secret',
  serviceaccounts: 'serviceaccount',
  'roles.rbac.authorization.k8s.io': 'role',
  'rolebindings.rbac.authorization.k8s.io': 'rolebinding',
  persistentvolumeclaims: 'persistentvolumeclaim',
};
const LABEL_OPERATORS: LabelOperator[] = ['Equals', 'Not Equals'];
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
    submitting = false,
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
    namespaceResourceOptions,
    customResourcesLoaded,
    onRequestCustomResources,
    onCreateStorage,
    onRegisterCluster,
    onCreatePolicy,
  } = props;
  const [includeFilterOpen, setIncludeFilterOpen] = useState(false);
  const [editingIncludeIndex, setEditingIncludeIndex] = useState<number | null>(null);
  const [includeFilterType, setIncludeFilterType] = useState<'resource' | 'label'>('resource');
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
    if (!multiNamespaceFilterDisabled || protectConfig.resourceSelection.mode === 'all') return;
    setProtectConfig(prev => ({
      ...prev,
      scope: 'all',
      includeAllResources: true,
      includedResources: [],
      excludedResources: [],
      labelSelector: { matchLabels: {}, matchExpressions: [] },
      resourceSelection: { mode: 'all', namespaceScoped: [], clusterScoped: [] },
    }));
  }, [multiNamespaceFilterDisabled, protectConfig.resourceSelection.mode, setProtectConfig]);

  const resourceSelectionValid = true;
  const canSave = Boolean(resourceSelectionValid && protectConfig.storageId && (protectConfig.targetCluster || targetClusterOptions.length === 0));
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
  const selectedLabels = Object.entries(protectConfig.labelSelector.matchLabels || {}).map(([key, value]) => `${key}=${value}`);
  const toggleIncludedResource = (resource: string) => setProtectConfig(prev => {
    if (prev.includeAllResources) {
      return {
        ...prev,
        includeAllResources: false,
        includedResources: RESOURCE_OPTIONS.filter(item => item !== resource),
      };
    }
    return {
      ...prev,
      includedResources: prev.includedResources.includes(resource)
        ? prev.includedResources.filter(item => item !== resource)
        : [...prev.includedResources, resource],
    };
  });
  const toggleExcludedResource = (resource: string) => setProtectConfig(prev => ({
    ...prev,
    excludedResources: prev.excludedResources.includes(resource)
      ? prev.excludedResources.filter(item => item !== resource)
      : [...prev.excludedResources, resource],
  }));
  const toggleMatchLabel = (label: string) => {
    const [key, value] = label.split('=', 2);
    if (!key || !value) return;
    setProtectConfig(prev => {
      const matchLabels = { ...(prev.labelSelector.matchLabels || {}) };
      if (matchLabels[key] === value) delete matchLabels[key];
      else matchLabels[key] = value;
      return { ...prev, labelSelector: { ...prev.labelSelector, matchLabels } };
    });
  };
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

  const closeIncludeFilter = () => {
    setIncludeFilterOpen(false);
    setEditingIncludeIndex(null);
    setOpenMultiSelect(null);
    setNewIncludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
  };

  const openIncludeFilter = () => {
    setEditingIncludeIndex(null);
    setIncludeFilterType('resource');
    setNewIncludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
    setIncludeFilterOpen(true);
  };

  const editIncludeFilter = (rule: ExcludeRule, index: number) => {
    setNewIncludeRule(rule);
    setIncludeFilterType(rule.labels && !rule.resource ? 'label' : 'resource');
    setEditingIncludeIndex(index);
    setIncludeFilterOpen(true);
  };

  const saveIncludeRule = () => {
    const normalizedRule = {
      group: '',
      resource: includeFilterType === 'resource' ? newIncludeRule.resource : '',
      name: '',
      version: '',
      labels: includeFilterType === 'label' ? newIncludeRule.labels : '',
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
                          onChange={() => setProtectConfig(prev => ({
                            ...prev,
                            mergeNamespaces: true,
                            scope: 'all',
                            includeAllResources: true,
                            includedResources: [],
                            excludedResources: [],
                            labelSelector: { matchLabels: {}, matchExpressions: [] },
                            resourceSelection: { mode: 'all', namespaceScoped: [], clusterScoped: [] },
                          }))}
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
                      <strong>Resource selection</strong>
                    </div>
                  </div>
                  <div className="hbdr-config-inline-panel">
                    {multiNamespaceFilterDisabled ? <div className="hbdr-combined-scope-notice">
                      <ShieldCheck size={17} />
                      <div><strong>All application resources</strong><span>Multi-namespace protection always uses the complete resource scope. Custom selection is available only for a single namespace.</span></div>
                      <em>Full backup</em>
                    </div> : <ScopedResourceSelector
                      purpose="backup"
                      value={protectConfig.resourceSelection}
                      onChange={resourceSelection => setProtectConfig(prev => ({
                        ...prev,
                        scope: 'all',
                        includeAllResources: resourceSelection.mode === 'all',
                        includedResources: [],
                        excludedResources: [],
                        labelSelector: { matchLabels: {}, matchExpressions: [] },
                        resourceSelection,
                      }))}
                      namespaceResources={namespaceResourceOptions || []}
                      customResourcesLoaded={customResourcesLoaded}
                      onRequestCustomResources={onRequestCustomResources}
                    />}
                  </div>
                  {protectConfig.scope === 'filter' && (
                    <div className="hbdr-config-inline-panel">
                      <div className="hbdr-config-filter-head">
                        <div>
                          <strong>Velero Backup filters</strong>
                          <span>These values are written directly to the Backup spec.</span>
                        </div>
                        <em>{protectConfig.includeAllResources ? 'All' : protectConfig.includedResources.length} included · {protectConfig.excludedResources.length} excluded</em>
                      </div>
                      <div className="hbdr-config-filter-block hbdr-velero-filter-block">
                        <div className="hbdr-config-filter-title">
                          <div><strong>Included resources</strong><span>spec.includedResources · Empty means all resource types</span></div>
                        </div>
                        <div className="hbdr-velero-resource-grid">
                          <button type="button" className={`hbdr-velero-all-resources ${protectConfig.includeAllResources ? 'is-selected' : ''}`} onClick={() => setProtectConfig(prev => ({ ...prev, includeAllResources: !prev.includeAllResources, includedResources: [] }))}>
                            <span>All resource types</span><i>{protectConfig.includeAllResources && <Check size={11} />}</i>
                          </button>
                          {RESOURCE_OPTIONS.map(resource => {
                            const selected = protectConfig.includeAllResources || protectConfig.includedResources.includes(resource);
                            return <button type="button" key={`include-${resource}`} className={selected ? 'is-selected' : ''} onClick={() => toggleIncludedResource(resource)}><span>{resource}</span><i>{selected && <Check size={11} />}</i></button>;
                          })}
                        </div>
                        {!protectConfig.includeAllResources && protectConfig.includedResources.length === 0 && <p className="hbdr-velero-filter-validation">Select at least one resource type or choose All resource types.</p>}
                      </div>
                      <div className="hbdr-config-filter-block hbdr-velero-filter-block">
                        <div className="hbdr-config-filter-title">
                          <div><strong>Label selector</strong><span>spec.labelSelector · Conditions are combined with AND</span></div>
                          {selectedLabels.length > 0 && <button type="button" onClick={() => setProtectConfig(prev => ({ ...prev, labelSelector: { matchLabels: {}, matchExpressions: [] } }))}>Clear</button>}
                        </div>
                        <div className="hbdr-velero-resource-grid">
                          {knownLabelOptions.map(label => <button type="button" key={`label-${label}`} className={selectedLabels.includes(label) ? 'is-selected' : ''} onClick={() => toggleMatchLabel(label)}><span>{label}</span><i>{selectedLabels.includes(label) && <Check size={11} />}</i></button>)}
                          {knownLabelOptions.length === 0 && <p>No labels discovered in the selected namespace.</p>}
                        </div>
                      </div>
                      <div className="hbdr-config-filter-block hbdr-velero-filter-block">
                        <div className="hbdr-config-filter-title">
                          <div><strong>Excluded resources</strong><span>spec.excludedResources · Applied after include and label filters</span></div>
                          {protectConfig.excludedResources.length > 0 && <button type="button" onClick={() => setProtectConfig(prev => ({ ...prev, excludedResources: [] }))}>Clear</button>}
                        </div>
                        <div className="hbdr-velero-resource-grid">
                          {RESOURCE_OPTIONS.map(resource => {
                            const conflict = protectConfig.includedResources.includes(resource);
                            const selected = protectConfig.excludedResources.includes(resource);
                            return <button type="button" key={`exclude-${resource}`} disabled={conflict} title={conflict ? 'Remove this resource from Included resources before excluding it.' : undefined} className={selected ? 'is-selected is-excluded' : conflict ? 'is-conflict' : ''} onClick={() => toggleExcludedResource(resource)}><span>{resource}</span><i>{selected && <X size={11} />}</i></button>;
                          })}
                        </div>
                      </div>
                      <div className="hbdr-velero-spec-preview">
                        <strong>Backup spec preview</strong>
                        <pre>{JSON.stringify({
                          includedResources: protectConfig.includedResources,
                          labelSelector: protectConfig.labelSelector,
                          excludedResources: protectConfig.excludedResources,
                        }, null, 2)}</pre>
                      </div>
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
                        <button type="button" aria-label="Create new storage" onClick={() => navigateFromConfig(onCreateStorage)}>+ New</button>
                      </div>
                    </label>
                    <label className="hbdr-config-setting-row">
                      <span>Target cluster</span>
                      <div className="hbdr-config-select-action">
                        <select value={protectConfig.targetCluster} onChange={event => setProtectConfig(prev => ({ ...prev, targetCluster: event.target.value }))}>
                          <option value="">Select target cluster</option>
                          {targetClusterOptions.map(cluster => <option key={cluster.id} value={cluster.name}>{cluster.name}{cluster.isCurrent ? ' (source)' : ''}</option>)}
                        </select>
                        <button type="button" aria-label="Register new cluster" onClick={() => navigateFromConfig(onRegisterCluster)}>+ New</button>
                      </div>
                    </label>
                    <label className="hbdr-config-setting-row">
                      <span>Backup policy</span>
                      <div className="hbdr-config-select-action">
                        <select value={protectConfig.policy} onChange={event => setProtectConfig(prev => ({ ...prev, policy: event.target.value }))}>
                          <option value="">Select policy</option>
                          {policyOptions.map(policy => <option key={policy.id} value={policy.id}>{policy.name} - {policy.schedule} / {policy.retention}</option>)}
                        </select>
                        <button type="button" aria-label="Create new backup policy" onClick={() => navigateFromConfig(onCreatePolicy)}>+ New</button>
                      </div>
                    </label>
                  </div>
                </section>

                <section className="hbdr-config-advanced hbdr-config-hooks-unavailable" aria-disabled="true">
                  <div className="hbdr-config-advanced-toggle">
                    <span><Settings2 size={16} />Hooks</span>
                    <em>Not supported yet</em>
                  </div>
                  <p>Pre- and post-operation scripts are not available in this version.</p>
                </section>
              </main>
            </div>

            <footer className="hbdr-filter-drawer-actions hbdr-config-footer">
              <div className="hbdr-config-footer-status">
                <span className={canSave ? 'is-ready' : ''}>{canSave ? 'Ready' : !resourceSelectionValid ? 'Select at least one included resource' : 'Repository and target cluster required'}</span>
              </div>
              <button type="button" onClick={onClose}>Cancel</button>
              <button type="button" className="hbdr-config-save" disabled={!canSave || submitting} onClick={onFinish}>
                <Check size={15} />{submitting ? 'Saving...' : 'Save'}
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
                    <p>Choose one filter type, then select the values to include.</p>
                  </div>
                  <button type="button" onClick={closeIncludeFilter} aria-label="Close"><X size={16} /></button>
                </header>
                <div className="hbdr-config-filter-dialog-body">
                  <div className="hbdr-include-filter-types">
                    <button type="button" className={includeFilterType === 'resource' ? 'is-active' : ''} onClick={() => {
                      setIncludeFilterType('resource');
                      setNewIncludeRule(prev => ({ ...prev, labels: '' }));
                    }}>
                      <Grid3X3 size={17} />
                      <span><strong>Resource type</strong><em>Include selected Kubernetes resource kinds</em></span>
                      <i>{includeFilterType === 'resource' && <Check size={12} />}</i>
                    </button>
                    <button type="button" className={includeFilterType === 'label' ? 'is-active' : ''} onClick={() => {
                      setIncludeFilterType('label');
                      setNewIncludeRule(prev => ({ ...prev, resource: '' }));
                    }}>
                      <Filter size={17} />
                      <span><strong>Label</strong><em>Include resources matching selected labels</em></span>
                      <i>{includeFilterType === 'label' && <Check size={12} />}</i>
                    </button>
                  </div>
                  <div className="hbdr-include-filter-picker">
                    <div className="hbdr-include-filter-picker-head">
                      <strong>{includeFilterType === 'resource' ? 'Select resource types' : 'Select labels'}</strong>
                      <span>{includeFilterType === 'resource' ? includeRuleResources.length : includeRuleLabels.length} selected</span>
                    </div>
                    <div className="hbdr-include-filter-options">
                      {(includeFilterType === 'resource' ? RESOURCE_OPTIONS : knownLabelOptions).map(value => {
                        const selected = includeFilterType === 'resource' ? includeRuleResources.includes(value) : includeRuleLabels.includes(value);
                        return (
                          <button
                            type="button"
                            key={value}
                            className={selected ? 'is-selected' : ''}
                            onClick={() => setNewIncludeRule(prev => includeFilterType === 'resource'
                              ? { ...prev, resource: selected ? removeRuleValue(prev.resource, value) : addRuleValue(prev.resource, value) }
                              : { ...prev, labels: selected ? removeRuleValue(prev.labels, value) : addRuleValue(prev.labels, value) })}
                          >
                            <span>{value}</span>
                            <i>{selected && <Check size={11} />}</i>
                          </button>
                        );
                      })}
                      {includeFilterType === 'label' && knownLabelOptions.length === 0 && <p>No labels were discovered in the selected namespace.</p>}
                    </div>
                  </div>
                  {renderRulePreview(newIncludeRule, includeFilterType === 'label' ? includeRuleLabels : [], 'include')}
                </div>
                <footer>
                  <button type="button" onClick={closeIncludeFilter}>Cancel</button>
                  <button type="button" className="hbdr-config-filter-primary" disabled={includeFilterType === 'resource' ? includeRuleResources.length === 0 : includeRuleLabels.length === 0} onClick={saveIncludeRule}>{editingIncludeIndex === null ? 'Add Include' : 'Save Changes'}</button>
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
