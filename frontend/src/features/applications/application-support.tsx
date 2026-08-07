export function normalizeResourceCategories(app: AppItem): ResourceCategory[] {
  const summary = app.resourceSummary;
  if (summary?.categories?.length) {
    const grouped: Record<ResourceCategoryKey, ResourceKindSummary[]> = {
      workloads: [],
      network: [],
      storage: [],
      config: [],
      access: [],
      jobs: [],
      scaling: [],
      policy: [],
      other: [],
    };
    summary.categories.forEach(category => {
      (category.items || []).forEach(item => {
        grouped[resourceCategoryForKind(item.kind)].push(item);
      });
    });
    return resourceCategoryMeta.map(meta => {
      const items = mergeResourceItems(grouped[meta.key]).filter(item => item.count > 0);
      return {
        key: meta.key,
        label: meta.label,
        total: items.reduce((sum, item) => sum + item.count, 0),
        items,
      };
    });
  }
  return [
    buildResourceCategory('workloads', 'Workloads', [
      { kind: 'Deployment', count: summary?.deployments || app.workloadCount || 0 },
      { kind: 'StatefulSet', count: summary?.statefulsets || 0 },
      { kind: 'DaemonSet', count: summary?.daemonsets || 0 },
      { kind: 'Pod', count: 0 },
      { kind: 'ReplicaSet', count: 0 },
      { kind: 'ReplicationController', count: 0 },
    ], app.namespace),
    buildResourceCategory('network', 'Network', [
      { kind: 'Service', count: summary?.services || app.serviceCount || 0 },
      { kind: 'Ingress', count: summary?.ingresses || app.ingressCount || 0 },
      { kind: 'NetworkPolicy', count: summary?.networkPolicies || 0 },
    ], app.namespace),
    buildResourceCategory('storage', 'Storage', [
      { kind: 'PersistentVolumeClaim', count: summary?.pvcs || app.pvcCount || 0 },
    ], app.namespace),
    buildResourceCategory('config', 'Config', [
      { kind: 'ConfigMap', count: summary?.configmaps || app.configMapCount || 0 },
      { kind: 'Secret', count: summary?.secrets || app.secretCount || 0 },
    ], app.namespace),
    buildResourceCategory('access', 'Access', [
      { kind: 'ServiceAccount', count: summary?.serviceAccounts || 0 },
      { kind: 'Role', count: 0 },
      { kind: 'RoleBinding', count: 0 },
    ], app.namespace),
    buildResourceCategory('jobs', 'Jobs', [
      { kind: 'Job', count: summary?.jobs || 0 },
      { kind: 'CronJob', count: summary?.cronjobs || 0 },
    ], app.namespace),
    buildResourceCategory('scaling', 'Scaling', [
      { kind: 'HorizontalPodAutoscaler', count: 0 },
      { kind: 'PodDisruptionBudget', count: 0 },
    ], app.namespace),
    buildResourceCategory('policy', 'Policy', [
      { kind: 'ResourceQuota', count: 0 },
      { kind: 'LimitRange', count: 0 },
    ], app.namespace),
    buildResourceCategory('other', 'Other', [], app.namespace),
  ];
}

export function formatLabelOptionSummary(option: Pick<LabelSelectorOption, 'namespaceNames' | 'resources'>): string {
  const parts: string[] = [];
  if (option.namespaceNames.length > 0) {
    parts.push(`${option.namespaceNames.length} Namespace${option.namespaceNames.length === 1 ? '' : 's'}`);
  }
  const byKind = option.resources.reduce<Record<string, number>>((acc, resource) => {
    const kind = resource.kind || 'Resource';
    acc[kind] = (acc[kind] || 0) + 1;
    return acc;
  }, {});
  Object.entries(byKind)
    .sort(([left], [right]) => left.localeCompare(right))
    .slice(0, 4)
    .forEach(([kind, count]) => {
      parts.push(`${count} ${kind}${count === 1 ? '' : 's'}`);
    });
  if (Object.keys(byKind).length > 4) {
    parts.push(`+${Object.keys(byKind).length - 4} kinds`);
  }
  return parts.join(' · ') || 'No resources';
}

export function buildLabelSelectorOptions(targetApps: AppItem[]): LabelSelectorOption[] {
  const optionMap = new Map<string, LabelSelectorOption>();
  const ensureOption = (key: string, value: string): LabelSelectorOption => {
    const id = `${key}\u0000${value}`;
    let option = optionMap.get(id);
    if (!option) {
      option = { key, value, namespaceNames: [], resources: [], summary: '' };
      optionMap.set(id, option);
    }
    return option;
  };

  targetApps.forEach(app => {
    Object.entries(app.labels || {}).forEach(([key, value]) => {
      if (!key || !value) return;
      const option = ensureOption(key, value);
      if (!option.namespaceNames.includes(app.namespace)) {
        option.namespaceNames.push(app.namespace);
      }
    });

    normalizeResourceCategories(app).forEach(category => {
      category.items.forEach(item => {
        item.resources?.forEach(resource => {
          Object.entries(resource.labels || {}).forEach(([key, value]) => {
            if (!key || !value) return;
            const option = ensureOption(key, value);
            const kind = resource.kind || item.kind;
            const namespace = resource.namespace || app.namespace;
            const id = `${namespace}/${kind}/${resource.name}`;
            const categoryKey = resourceCategoryKeys.has(category.key as ResourceCategoryKey)
              ? category.key as ResourceCategoryKey
              : 'other';
            if (!option.resources.some(existing => existing.id === id)) {
              option.resources.push({
                id,
                name: resource.name,
                namespace,
                kind,
                category: categoryKey,
                labels: resource.labels || {},
              });
            }
          });
        });
      });
    });
  });

  return Array.from(optionMap.values())
    .map(option => ({
      ...option,
      namespaceNames: [...option.namespaceNames].sort((a, b) => a.localeCompare(b)),
      resources: [...option.resources].sort((a, b) => `${a.kind}/${a.name}`.localeCompare(`${b.kind}/${b.name}`)),
      summary: formatLabelOptionSummary(option),
    }))
    .sort((a, b) => `${a.key}=${a.value}`.localeCompare(`${b.key}=${b.value}`));
}

export function stringArrayFromAny(value: any): string[] {
  if (Array.isArray(value)) return value.map(item => String(item)).filter(Boolean);
  if (typeof value === 'string' && value.trim()) return [value.trim()];
  return [];
}

export function namespacesFromPayload(payload: any): string[] {
  const values = [
    ...stringArrayFromAny(payload?.includedNamespaces),
    ...stringArrayFromAny(payload?.sourceNamespaces),
    ...stringArrayFromAny(payload?.velero?.includedNamespaces),
    ...stringArrayFromAny(payload?.velero?.manifest?.spec?.includedNamespaces),
  ];
  if (payload?.sourceNamespace) values.push(String(payload.sourceNamespace));
  return Array.from(new Set(values.filter(Boolean)));
}

export function taskRestorePointId(task: ApiTask): string {
  return task.restorePointId || String(task.payload?.restorePointId || '');
}

export function appOverrideKey(app: Pick<AppItem, 'clusterId' | 'namespace' | 'name'>): string {
  return `${app.clusterId || 'unknown'}::${app.namespace || app.name}`;
}

export function formatNextSyncTime(value?: string) {
  const date = parseUTCInstant(value);
  if (!date || date.getUTCFullYear() <= 1) return '';
  const dayKey = (input: Date) => new Intl.DateTimeFormat('en-CA', {
    timeZone: getUserTimeZone(),
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(input);
  const sameDay = dayKey(date) === dayKey(new Date());
  const options: Intl.DateTimeFormatOptions = sameDay
    ? { hour: '2-digit', minute: '2-digit', hour12: false }
    : { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false };
  return date.toLocaleString(undefined, { ...options, timeZone: getUserTimeZone() });
}

export function recoveryCompletedTargetTitle(restorePointLabel: string, completedAt: string | undefined, clusterName: string, namespace: string, actionLabel: string) {
  const point = restorePointLabel || 'restore point';
  const time = formatLocalDateTime(completedAt) || 'completed';
  const target = [clusterName || 'target cluster', namespace || 'namespace'].filter(Boolean).join(' / ');
  return `${point} ${actionLabel.toLowerCase()} ${time} to ${target}`;
}

export function recoveryCompletedTargetLabel(clusterName: string, namespace: string) {
  return [clusterName || 'target', namespace || 'namespace'].filter(Boolean).join(' / ');
}

export function restorePointDisplayLabel(point?: {
  id?: string;
  taskCreatedAt?: string;
  createdAt?: string;
  title?: string;
  veleroBackupName?: string;
} | null) {
  if (!point) return '';
  const timestamp = formatLocalDateTime(point.taskCreatedAt || point.createdAt);
  if (timestamp) return `RP-${timestamp}`;
  const label = point.title || point.veleroBackupName || (point.id ? point.id.slice(0, 8) : '');
  return label ? `RP-${label}` : '';
}

export function scriptPayload(script: { name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }) {
  return {
    name: script.name,
    size: script.size,
    source: script.source,
    isEntry: Boolean(script.isEntry),
    content: script.content,
  };
}

export function formatPolicySchedule(policy: Pick<PolicyItem, 'composition' | 'type' | 'intervalValue' | 'intervalUnit' | 'hour' | 'minute' | 'weekDay' | 'monthDay'>) {
  if (policy.composition === 'manual') return 'Manual trigger';
  if (policy.composition === 'retention') return 'Not scheduled';
  if (policy.type === 'interval') return `Every ${policy.intervalValue} ${policy.intervalUnit === 'minutes' ? 'minutes' : 'hours'}`;
  if (policy.type === 'daily') return `Every day ${formatTime(policy.hour, policy.minute)}`;
  if (policy.type === 'weekly') return `Every week ${weekdays[policy.weekDay]} ${formatTime(policy.hour, policy.minute)}`;
  return `Every month ${policy.monthDay} Day ${formatTime(policy.hour, policy.minute)}`;
}

export function formatPolicyComposition(composition: PolicyComposition) {
  if (composition === 'manual') return 'Manual';
  if (composition === 'schedule') return 'Schedule Only';
  if (composition === 'retention') return 'Retention Only';
  return 'Schedule + Retention';
}

export function formatPolicyRetention(policy: Pick<PolicyItem, 'composition' | 'retention'>) {
  if (policy.composition === 'manual') return 'Not defined';
  if (policy.composition === 'schedule') return 'Platform default';
  return `${policy.retention ?? 0} copies`;
}

export function formatScopeLabel(scope: string | undefined) {
  const value = (scope || '').toLowerCase();
  if (value === 'all') return 'All';
  if (value === 'custom') return 'Custom';
  if (value === 'filtered') return 'Filtered';
  return scope || 'Not configured';
}

export type ListToolbarChoice = {
  value: string;
  label: string;
  count?: number;
};

export type ListToolbarColumn = {
  value: string;
  label: string;
  locked?: boolean;
};

export function listToolbarQueryFields(
  fixedFields: ListToolbarChoice[],
  columns: ListToolbarColumn[],
  visibleColumns: string[],
) {
  const seen = new Set<string>();
  const fields: ListToolbarChoice[] = [];
  const append = (field: ListToolbarChoice) => {
    if (seen.has(field.value)) return;
    seen.add(field.value);
    fields.push(field);
  };
  fixedFields.forEach(append);
  columns
    .filter(column => visibleColumns.includes(column.value))
    .forEach(column => append({ value: column.value, label: column.label }));
  return fields;
}

export const COLUMN_FILTER_PREFIX = 'columnFilter:';

export function parseColumnFilterToken(token: string): { field: string; value: string } | null {
  if (!token.startsWith(COLUMN_FILTER_PREFIX)) return null;
  const body = token.slice(COLUMN_FILTER_PREFIX.length);
  const separator = body.indexOf(':');
  if (separator < 0) return null;
  const field = decodeURIComponent(body.slice(0, separator));
  const value = decodeURIComponent(body.slice(separator + 1)).trim();
  if (!field || !value) return null;
  return { field, value };
}

export function matchesColumnFilterToken(token: string, valueForField: (field: string) => string) {
  const parsed = parseColumnFilterToken(token);
  if (!parsed) return false;
  return valueForField(parsed.field).toLowerCase().includes(parsed.value.toLowerCase());
}

export function ErrorDetailModalFrame({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0 bg-slate-950/18" onClick={onClose} />
      <motion.aside initial={{ opacity: 0, x: 40 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 40 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-error-detail-drawer" role="dialog" aria-modal="true" aria-label={title}>
        <div className="flex items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
          <h3 className="text-sm font-black text-slate-900">{title}</h3>
          <button onClick={onClose} className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700" aria-label="Close"><X size={16} /></button>
        </div>
        <div className="p-4">{children}</div>
      </motion.aside>
    </div>
  );
}
import React from 'react';
import { X } from 'lucide-react';
import { motion } from 'motion/react';
import { formatLocalDateTime, getUserTimeZone, parseUTCInstant } from '../../lib/date-time';
import type { AppItem, LabelSelectorOption, ResourceCategory, ResourceCategoryKey, ResourceKindSummary } from '../clusters/types';
import type { ApiTask, PolicyComposition, PolicyItem } from '../recovery/types';
import { mergeResourceItems, resourceCategoryForKind, resourceCategoryKeys, resourceCategoryMeta } from '../recovery/task-ui';
import { buildResourceCategory, formatTime, weekdays } from './application-primitives';
