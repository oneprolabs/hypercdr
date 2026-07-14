import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { DrConfigurationModal } from './dr-configuration-modal';
import { RecoveryWizardModal, type RecoveryWizardConfig } from './recovery-wizard-modal';
import { HyperTable, type HyperTableColumn } from './components/table';
import {
  Activity,
  AlertCircle,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Archive,
  Bell,
  Boxes,
  Calendar,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ClipboardList,
  Clock,
  Cloud,
  Database,
  Edit2,
  Eye,
  FileCode,
  FileCog,
  Filter,
  Gauge,
  Grid3X3,
  HardDrive,
  History,
  KeyRound,
  Layers,
  Lock,
  Languages,
  MoreVertical,
  Network,
  Play,
  Plus,
  PlusCircle,
  RefreshCw,
  Search,
  Server,
  Settings,
  Settings2,
  ShieldOff,
  ShieldCheck,
  Star,
  Sun,
  Terminal,
  Trash2,
  Upload,
  User,
  X,
  Zap,
} from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';

type View =
  | 'login'
  | 'dashboard'
  | 'applications'
  | 'dr_tasks'
  | 'failback'
  | 'clusters'
  | 'storage'
  | 'policies'
  | 'restore_points'
  | 'operations'
  | 'tags'
  | 'alerts'
  | 'settings'
  | 'tenants';

type TopModule = 'overview' | 'dr' | 'config' | 'ops' | 'monitor' | 'settings';

type ClusterStatus = 'healthy' | 'warning' | 'syncing';

type PolicyComposition = 'manual' | 'combined' | 'schedule' | 'retention';
type PolicyScheduleType = 'interval' | 'daily' | 'weekly' | 'monthly';
type PolicyItem = {
  id: string;
  name: string;
  composition: PolicyComposition;
  type: PolicyScheduleType;
  intervalValue: number;
  intervalUnit: 'minutes' | 'hours';
  hour: number;
  minute: number;
  weekDay: number;
  monthDay: number;
  retention?: number;
  status: 'Active' | 'Disabled';
  bound: number;
};

type LocaleCode = 'en';

const copyTextToClipboard = async (text: string, textarea?: HTMLTextAreaElement | null): Promise<boolean> => {
  if (!text) return false;

  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall back to selection-based copy below.
    }
  }

  const selectAndCopy = (target: HTMLTextAreaElement): boolean => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    target.focus();
    target.select();
    target.setSelectionRange(0, target.value.length);
    let copied = false;
    try {
      copied = document.execCommand('copy');
    } catch {
      copied = false;
    }
    if (previousFocus && previousFocus !== target) {
      previousFocus.focus();
    }
    return copied;
  };

  if (textarea) {
    textarea.value = text;
    if (selectAndCopy(textarea)) return true;
  }

  const scratch = document.createElement('textarea');
  scratch.value = text;
  scratch.setAttribute('readonly', 'true');
  scratch.style.position = 'fixed';
  scratch.style.left = '-9999px';
  scratch.style.top = '0';
  document.body.appendChild(scratch);
  const copied = selectAndCopy(scratch);
  document.body.removeChild(scratch);
  return copied;
};

const locales: Record<LocaleCode, {
  code: LocaleCode;
  name: string;
  languageLabel: string;
  topNav: Record<TopModule, string>;
  secondaryTitles: Record<TopModule, string>;
  secondaryMeta: Record<View, [string, string]>;
}> = {
  en: {
    code: 'en',
    name: 'English',
    languageLabel: 'Language',
    topNav: {
      overview: 'Dashboard',
      dr: 'DR',
      config: 'Configuration',
      ops: 'Operations',
      monitor: 'Monitor & Alerts',
      settings: 'Settings',
    },
    secondaryTitles: {
      overview: '',
      dr: 'DR',
      config: 'Configuration',
      ops: 'Operations',
      monitor: 'Monitor & Alerts',
      settings: 'Settings',
    },
    secondaryMeta: {
      applications: ['Application DR', 'Select applications, configure policies, and start DR'],
      dr_tasks: ['Backup & Recovery Tasks', 'Audit backup and recovery task records'],
      failback: ['Failback', 'Fail back taken-over applications to the production cluster'],
      clusters: ['Clusters', 'Registration, default cluster, and agent status'],
      storage: ['Storage', 'Maintain shared restore-point repositories across clusters'],
      policies: ['Policies', 'Maintain application protection plans and recovery targets'],
      restore_points: ['Restore Points', 'View, drill, and take over restore points'],
      operations: ['History', 'Backup, restore, and takeover audit'],
      tags: ['Tag Management', 'Create and maintain reusable application tags'],
      alerts: ['Alerts', 'Cluster DR risks and event alerts'],
      settings: ['System', 'Platform parameters and security policies'],
      tenants: ['Tenants', 'Tenants and administrator accounts'],
      login: ['', ''],
      dashboard: ['', ''],
    },
  },
};

interface AppItem {
  apiId?: string;
  clusterId?: string;
  name: string;
  namespace: string;
  status: 'Active' | 'Running' | 'Terminating' | 'Unknown' | 'Protected';
  namespaceStatus?: string;
  policy?: string;
  storage?: string;
  targetCluster?: string;
  isProtected: boolean;
  lastBackup?: string;
  labels?: Record<string, string>;
  tags?: string[];
  workloadCount?: number;
  serviceCount?: number;
  ingressCount?: number;
  configMapCount?: number;
  secretCount?: number;
  pvcCount?: number;
  pvCapacityBytes?: number;
  resourceSummary?: ResourceSummary;
  protectionStatus?: string;
  protectionPlanId?: string;
  protectionPlanCreatedAt?: string;
  stage?: ApplicationStage;
  memberApps?: AppItem[];
  isMergedPlan?: boolean;
}

type ResourceCategoryKey = 'workloads' | 'network' | 'storage' | 'config' | 'access' | 'jobs' | 'scaling' | 'policy' | 'other';

type ResourceRef = {
  name: string;
  namespace?: string;
  kind?: string;
  apiVersion?: string;
  labels?: Record<string, string>;
  ready?: string;
  desiredReplicas?: number;
  readyReplicas?: number;
  updatedReplicas?: number;
  availableReplicas?: number;
  ageSeconds?: number;
  containers?: string[];
  images?: string[];
  selector?: string;
  fields?: Record<string, string>;
};

type LabelResourceMatch = {
  id: string;
  name: string;
  namespace: string;
  kind: string;
  category: ResourceCategoryKey | 'namespace';
  labels: Record<string, string>;
};

type LabelSelectorOption = {
  key: string;
  value: string;
  namespaceNames: string[];
  resources: LabelResourceMatch[];
  summary: string;
};

type ResourceKindSummary = {
  kind: string;
  shortName?: string;
  count: number;
  resources?: ResourceRef[];
};

type ResourceCategory = {
  key: ResourceCategoryKey | string;
  label: string;
  total: number;
  items?: ResourceKindSummary[];
};

type ResourceSummary = {
  deployments?: number;
  statefulsets?: number;
  daemonsets?: number;
  jobs?: number;
  cronjobs?: number;
  services?: number;
  ingresses?: number;
  networkPolicies?: number;
  configmaps?: number;
  secrets?: number;
  serviceAccounts?: number;
  pvcs?: number;
  pvCapacityBytes?: number;
  drSupport?: DRSupportSummary;
  ageSeconds?: number;
  categories?: ResourceCategory[];
};

type DRSupportSummary = {
  status?: 'supported' | 'unsupported' | 'warning' | string;
  reason?: string;
  checks?: DRSupportCheck[];
};

type DRSupportCheck = {
  kind?: string;
  name?: string;
  status?: string;
  reason?: string;
  storageClass?: string;
  provisioner?: string;
  volume?: string;
  volumeType?: string;
};

interface TagItem {
  id: string;
  name: string;
  createdAt: string;
}

type SyncTaskState = {
  status: 'syncing' | 'stopped' | 'completed' | 'failed';
  progress: number;
  error?: string;
  detail?: string;
};

type RecoveryTaskState = {
  mode: 'drill' | 'takeover';
  status: 'running' | 'completed' | 'failed';
  progress: number;
  targetCluster: string;
  targetNamespace: string;
  pointId: string;
  message: string;
};

interface Cluster {
  id: string;
  name: string;
  region: string;
  version: string;
  status: ClusterStatus;
  connectionStatus?: string;
  compliance: number;
  nodes: number;
  nodeDetails?: ClusterNode[];
  storageClasses?: ClusterStorageClass[];
  namespaces: number;
  applications: number;
  agentVersion: string;
  latestAgentVersion: string;
  agentImage?: string;
  agentImageDigest?: string;
  latestAgentImage?: string;
  latestAgentImageDigest?: string;
  agentUpgradeAvailable?: boolean;
  agentUpgradeStatus?: string;
  veleroStatus?: string;
  lastSeenAt?: string;
  role?: 'source' | 'target' | 'both';
  isDefault?: boolean;
  apps: AppItem[];
}

type ClusterNode = {
  name: string;
  status: string;
  roles?: string;
  ageSeconds?: number;
  kubeletVersion?: string;
  capacity?: Record<string, string>;
};

type ClusterStorageClass = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  allowVolumeExpansion: string;
  default?: boolean;
  ageSeconds?: number;
};

type ClusterNamespaceRow = {
  name: string;
  status: string;
  age: string;
};

type ClusterNodeRow = {
  name: string;
  status: string;
  roles: string;
  age: string;
  version: string;
};

type ClusterStorageClassRow = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  allowVolumeExpansion: string;
  age: string;
};

interface StorageRepo {
  id: string;
  name: string;
  type: string;
  endpoint: string;
  bucket: string;
  region: string;
  useTls: boolean;
  status: 'connected' | 'warning' | 'unknown';
  updatedAt: string;
  lastValidatedAt?: string;
  config?: Record<string, string | boolean>;
  urlStyle?: string;
}

const initialClusters: Cluster[] = [];

const initialStorage: StorageRepo[] = [];

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

const initialPolicies: PolicyItem[] = [];

const initialTags: TagItem[] = [];

type ApiList<T> = { items: T[] };

function listItems<T>(response: ApiList<T>): T[] {
  return Array.isArray(response.items) ? response.items : [];
}

type ApiCluster = {
  id: string;
  name: string;
  kubeVersion: string;
  status: string;
  connectionStatus: string;
  nodeCount: number;
  nodes?: ClusterNode[];
  storageClasses?: ClusterStorageClass[];
  namespaceCount: number;
  applicationCount: number;
  complianceScore?: number;
  agentVersion: string;
  agentImage?: string;
  agentImageId?: string;
  agentImageDigest?: string;
  latestAgentVersion?: string;
  latestAgentImage?: string;
  latestAgentImageDigest?: string;
  agentUpgradeAvailable?: boolean;
  agentUpgradeStatus?: string;
  veleroVersion?: string;
  veleroStatus: string;
  role?: 'source' | 'target' | 'both';
  isDefault?: boolean;
  registeredAt?: string;
  lastSeenAt?: string;
};
type ApiApplication = {
  id: string;
  clusterId: string;
  namespace: string;
  name: string;
  status: string;
  labels?: Record<string, string>;
  workloadCount: number;
  serviceCount: number;
  ingressCount: number;
  configMapCount: number;
  secretCount: number;
  pvcCount: number;
  pvCapacityBytes: number;
  resourceSummary?: ResourceSummary;
  protectionStatus?: string;
};
type ApiStorageRepo = {
  id: string;
  name: string;
  type: string;
  endpoint?: string;
  bucket?: string;
  region?: string;
  tlsEnabled: boolean;
  status: string;
  updatedAt?: string;
  createdAt?: string;
  lastValidatedAt?: string;
  config?: Record<string, unknown>;
};

type StorageRepositoryInput = {
  name: string;
  type: string;
  endpoint: string;
  bucket: string;
  region: string;
  tlsEnabled: boolean;
  config: Record<string, string | boolean>;
  accessKey?: string;
  secretKey?: string;
};
type ApiPolicy = {
  id: string;
  name: string;
  composition: string;
  scheduleType: string;
  intervalValue?: number;
  intervalUnit?: string;
  hour?: number;
  minute?: number;
  weekDay?: number;
  monthDay?: number;
  retentionCount?: number;
  status: string;
  boundCount: number;
};
type ApiProtectionPlan = {
  id: string;
  appId: string;
  appIds?: string[];
  sourceClusterId?: string;
  policyId?: string;
  storageRepoId?: string;
  targetClusterId?: string;
  labelSelector?: string;
  status?: string;
  warning?: string;
  activationTask?: ApiTask;
  planStorageSize?: Record<string, any>;
  nextFireAt?: string;
  scheduleEnabled?: boolean;
  createdAt?: string;
  updatedAt?: string;
};
type ApiAgentToken = {
  id: string;
  token: string;
  expiresAt: string;
  prepareNodeCommand?: string;
  installCommand: string;
};
type ApiTask = {
  id: string;
  clusterId: string;
  appId?: string;
  protectionPlanId?: string;
  restorePointId?: string;
  type: string;
  status: string;
  progress: number;
  commandId?: string;
  errorCode?: string;
  errorMessage?: string;
  payload?: Record<string, any>;
  createdAt?: string;
  completedAt?: string;
};
type ApiTaskEvent = {
  id: string;
  taskId: string;
  level: string;
  reason: string;
  message: string;
  payload?: Record<string, any>;
  createdAt?: string;
};
type VolumeProgressInfo = {
  operation?: string;
  bytesDone: number;
  totalBytes: number;
  knownTotal: boolean;
  allTotalsKnown?: boolean;
  percent: number;
  speedBytesPerSecond: number;
  etaSeconds: number;
  itemCount?: number;
  runningCount?: number;
  completedCount?: number;
  failedCount?: number;
};
type ApiTaskResponse = ApiTask | {
  task: ApiTask;
  warning?: string;
};
type ApiTaskCancelResponse = {
  task: ApiTask;
  cancelTask?: ApiTask;
  warning?: string;
};
type ApiCaptcha = {
  id: string;
  image: string;
  expiresAt: string;
};
type ApiLoginResponse = {
  user: {
    id: string;
    email: string;
    role: string;
    status: string;
  };
  session: {
    token: string;
    expiresAt: string;
  };
};
type AuthSession = ApiLoginResponse & {
  signedInAt: string;
};

type ClusterTaskLog = {
  task: ApiTask;
  events: ApiTaskEvent[];
  loading: boolean;
};

type ApiRestorePoint = {
  id: string;
  sourceClusterId: string;
  protectionPlanId?: string;
  appId?: string;
  storageRepoId?: string;
  veleroBackupName: string;
  pointType: string;
  status: string;
  sizeBytes?: number;
  completedAt?: string;
  sourceNamespace?: string;
  backupStorageName?: string;
  metadata?: Record<string, any>;
  createdAt: string;
};

const AUTH_SESSION_KEY = 'hypercdr.auth.session';
const NAV_VIEW_KEY = 'hypercdr.nav.view';
const SELECTED_CLUSTER_KEY = 'hypercdr.selectedClusterId';
const DEFAULT_CLUSTER_KEY = 'hypercdr.defaultClusterId';
const RESTORABLE_VIEWS = new Set<View>([
  'dashboard',
  'applications',
  'dr_tasks',
  'failback',
  'clusters',
  'storage',
  'policies',
  'restore_points',
  'operations',
  'tags',
  'alerts',
  'settings',
  'tenants',
]);

function readStoredView(): View | null {
  try {
    const value = localStorage.getItem(NAV_VIEW_KEY) as View | null;
    return value && RESTORABLE_VIEWS.has(value) ? value : null;
  } catch {
    return null;
  }
}

function writeStoredView(view: View) {
  if (!RESTORABLE_VIEWS.has(view)) return;
  try {
    localStorage.setItem(NAV_VIEW_KEY, view);
  } catch {
    // Navigation still works in the current tab.
  }
}

function clearStoredView() {
  try {
    localStorage.removeItem(NAV_VIEW_KEY);
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
}

function readStoredAuthSession(): AuthSession | null {
  try {
    const raw = localStorage.getItem(AUTH_SESSION_KEY);
    if (!raw) return null;
    const session = JSON.parse(raw) as AuthSession;
    if (!session?.session?.token || !session?.session?.expiresAt || !session?.user?.email) {
      localStorage.removeItem(AUTH_SESSION_KEY);
      return null;
    }
    if (Date.parse(session.session.expiresAt) <= Date.now()) {
      localStorage.removeItem(AUTH_SESSION_KEY);
      return null;
    }
    return session;
  } catch {
    try {
      localStorage.removeItem(AUTH_SESSION_KEY);
    } catch {
      // localStorage can be unavailable in restricted browser contexts.
    }
    return null;
  }
}

function writeStoredAuthSession(session: AuthSession) {
  try {
    localStorage.setItem(AUTH_SESSION_KEY, JSON.stringify(session));
  } catch {
    // The in-memory session still keeps the current tab signed in.
  }
}

function clearStoredAuthSession() {
  try {
    localStorage.removeItem(AUTH_SESSION_KEY);
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
}

async function readApiError(response: Response): Promise<Error> {
  try {
    const body = await response.json();
    const message = body?.message || body?.error || `${response.status} ${response.statusText}`;
    return new Error(String(message));
  } catch {
    return new Error(`${response.status} ${response.statusText}`);
  }
}

async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(path);
  if (!response.ok) throw await readApiError(response);
  return response.json() as Promise<T>;
}

async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await readApiError(response);
  return response.json() as Promise<T>;
}

function isAgentTokenUsable(token: ApiAgentToken | null) {
  if (!token?.installCommand) return false;
  const expiresAt = Date.parse(token.expiresAt);
  return Number.isNaN(expiresAt) || expiresAt > Date.now() + 60_000;
}

async function apiPatch<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await readApiError(response);
  return response.json() as Promise<T>;
}

async function apiDelete<T>(path: string): Promise<T> {
  const response = await fetch(path, { method: 'DELETE' });
  if (!response.ok) throw await readApiError(response);
  return response.json() as Promise<T>;
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const decimals = value >= 100 || unitIndex === 0 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(decimals)} ${units[unitIndex]}`;
}

function shortDigest(digest?: string): string {
  if (!digest) return '';
  const cleaned = digest.replace(/^sha256:/, '');
  return cleaned.length > 12 ? cleaned.slice(0, 12) : cleaned;
}

function numberFromUnknown(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number(value.trim());
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function recordFromUnknown(value: unknown): Record<string, any> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, any> : {};
}

function restorePointOriginalSize(point: ApiRestorePoint): { label: string; title: string; bytes: number } {
  const metadata = recordFromUnknown(point.metadata);
  const restorePointSize = recordFromUnknown(metadata.restorePointSize);
  const directSize = recordFromUnknown(metadata.size);
  const velero = recordFromUnknown(metadata.velero);
  const veleroRestorePointSize = recordFromUnknown(velero.restorePointSize);
  const veleroSize = recordFromUnknown(velero.size);
  const size = Object.keys(restorePointSize).length > 0
    ? restorePointSize
    : Object.keys(directSize).length > 0
      ? directSize
      : Object.keys(veleroRestorePointSize).length > 0
        ? veleroRestorePointSize
        : veleroSize;
  const metadataBytes = numberFromUnknown(size.metadataBytes);
  const volumeBytes = numberFromUnknown(size.volumeBytes);
  const uploadedBytes = numberFromUnknown(size.uploadedBytes);
  const uploadedVolumeBytes = numberFromUnknown(size.uploadedVolumeBytes);
  const totalBytes = numberFromUnknown(size.totalBytes) || metadataBytes + volumeBytes || numberFromUnknown(point.sizeBytes);
  const sizeStatus = String(metadata.sizeStatus || velero.sizeStatus || size.sizeStatus || '').trim();

  if (!totalBytes) {
    return { label: 'Unknown', title: 'Total size unavailable', bytes: 0 };
  }

  const label = formatBytes(totalBytes);
  const parts = [
    `Total: ${label}`,
    `Metadata: ${metadataBytes > 0 ? formatBytes(metadataBytes) : 'Unknown'}`,
    `Volume: ${volumeBytes > 0 ? formatBytes(volumeBytes) : 'Unknown'}`,
    uploadedBytes > 0 ? `Uploaded: ${formatBytes(uploadedBytes)}` : '',
    uploadedVolumeBytes > 0 ? `Uploaded volume: ${formatBytes(uploadedVolumeBytes)}` : '',
    sizeStatus ? `Status: ${sizeStatus}` : '',
  ].filter(Boolean);
  return { label, title: parts.join('; '), bytes: totalBytes };
}

function formatSignedBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const prefix = bytes < 0 ? '-' : '';
  return `${prefix}${formatBytes(Math.abs(bytes))}`;
}

function restorePointStorageSize(point: ApiRestorePoint): { label: string; title: string; bytes: number } {
  const metadata = recordFromUnknown(point.metadata);
  const increment = recordFromUnknown(metadata.storageIncrementSize);
  const bytes = numberFromUnknown(increment.bytes);
  const planTotalBytes = numberFromUnknown(increment.planTotalBytes);
  const previousTotalBytes = numberFromUnknown(increment.previousTotalBytes);
  const hasPrevious = Boolean(increment.hasPrevious);
  if (!bytes && !planTotalBytes) {
    return { label: 'Unknown', title: 'Storage increment unavailable', bytes: 0 };
  }
  const label = formatSignedBytes(bytes || planTotalBytes);
  const parts = [
    hasPrevious ? `Net change: ${label}` : `Initial stored size: ${label}`,
    planTotalBytes ? `Plan total: ${formatBytes(planTotalBytes)}` : '',
    hasPrevious ? `Previous total: ${formatBytes(previousTotalBytes)}` : '',
  ].filter(Boolean);
  return { label, title: parts.join('; '), bytes: bytes || planTotalBytes };
}

function formatBytesPerSecond(bytes: number): string {
  if (!bytes || bytes < 0) return '';
  return `${formatBytes(bytes)}/s`;
}

function formatEta(seconds: number): string {
  if (!seconds || seconds < 1) return '';
  if (seconds < 60) return `${Math.round(seconds)}s left`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${Math.round(seconds % 60)}s left`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m left`;
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '0.00';
  return Math.max(0, Math.min(100, value)).toFixed(2);
}

function latestVolumeProgress(events?: ApiTaskEvent[]): VolumeProgressInfo | null {
  if (!events?.length) return null;
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const progress = events[index]?.payload?.velero?.volumeProgress;
    if (!progress || typeof progress !== 'object') continue;
    return {
      operation: typeof progress.operation === 'string' ? progress.operation : undefined,
      bytesDone: Number(progress.bytesDone || 0),
      totalBytes: Number(progress.totalBytes || 0),
      knownTotal: Boolean(progress.knownTotal),
      allTotalsKnown: Boolean(progress.allTotalsKnown),
      percent: Number(progress.percent || 0),
      speedBytesPerSecond: Number(progress.speedBytesPerSecond || 0),
      etaSeconds: Number(progress.etaSeconds || 0),
      itemCount: Number(progress.itemCount || 0),
      runningCount: Number(progress.runningCount || 0),
      completedCount: Number(progress.completedCount || 0),
      failedCount: Number(progress.failedCount || 0),
    };
  }
  return null;
}

function taskProgressInfo(task: ApiTask, events?: ApiTaskEvent[]): VolumeProgressInfo | null {
  const metrics = task.payload?.progressMetrics && typeof task.payload.progressMetrics === 'object'
    ? task.payload.progressMetrics
    : task.payload || {};
  const totalBytes = Number(metrics.totalBytes || 0);
  const syncedBytes = Number(metrics.syncedBytes || 0);
  if (totalBytes > 0) {
    return {
      bytesDone: Math.max(0, syncedBytes),
      totalBytes,
      knownTotal: true,
      allTotalsKnown: true,
      percent: Number(metrics.percent || (syncedBytes > 0 ? (syncedBytes * 100) / totalBytes : 0)),
      speedBytesPerSecond: Number(metrics.speedBytesPerSecond || 0),
      etaSeconds: Number(metrics.etaSeconds || 0),
    };
  }
  const volume = latestVolumeProgress(events);
  if (!volume || !volume.knownTotal || !volume.allTotalsKnown || volume.totalBytes <= 0) {
    return null;
  }
  return volume;
}

function hasTaskEventReason(events: ApiTaskEvent[] | undefined, reasons: string[]): boolean {
  if (!events?.length) return false;
  const allowed = new Set(reasons);
  return events.some(event => allowed.has(event.reason));
}

function latestTaskMessage(events: ApiTaskEvent[] | undefined, fallback: string): string {
  return events?.at(-1)?.message || fallback;
}

function eventRestoreResultErrors(event?: ApiTaskEvent | null): string[] {
  const restoreResults = event?.payload?.velero?.restoreResults || event?.payload?.restoreResults;
  const errors = restoreResults?.errors;
  return Array.isArray(errors) ? errors.map(error => String(error)).filter(Boolean) : [];
}

function taskFailureDetails(task: ApiTask, events?: ApiTaskEvent[]): string[] {
  const details: string[] = [];
  if (task.errorMessage) details.push(task.errorMessage);
  volumeFailureDetails(task.payload).forEach(detail => details.push(detail));
  const seen = new Set(details);
  (events || []).forEach(event => {
    volumeFailureDetails(event.payload).forEach(detail => {
      if (!seen.has(detail)) {
        details.push(detail);
        seen.add(detail);
      }
    });
    eventRestoreResultErrors(event).forEach(error => {
      if (!seen.has(error)) {
        details.push(error);
        seen.add(error);
      }
    });
  });
  if (details.length === 0 && task.errorCode) details.push(task.errorCode);
  return details;
}

type ErrorMessageDefinition = {
  code: string;
  aliases: string[];
  title: string;
  description: string;
  detail: string;
  match?: (message: string) => boolean;
};

const ERROR_MESSAGE_CATALOG: ErrorMessageDefinition[] = [
  {
    code: '100000',
    aliases: ['TASK_FAILED', 'UNKNOWN_ERROR'],
    title: 'Task failed',
    description: 'The task failed. Open details to review the reported error.',
    detail: 'The platform did not match this failure to a known error type. Review the original error, task events, and technical payload.',
  },
  {
    code: '110001',
    aliases: ['AGENT_OFFLINE', 'DISPATCH_WAITING_AGENT'],
    title: 'Cluster is offline',
    description: 'Reconnecting to source cluster agent. Pending tasks will resume automatically.',
    detail: 'The source cluster agent is not connected to the platform. The task can continue after the WebSocket connection is restored.',
  },
  {
    code: '110002',
    aliases: ['DISPATCH_FAILED'],
    title: 'Task dispatch failed',
    description: 'The platform could not dispatch the task to the cluster agent.',
    detail: 'The platform failed to send the task command to the agent. Check agent connectivity and platform task dispatch events.',
  },
  {
    code: '110003',
    aliases: ['LOCAL_LEDGER_WRITE_FAILED'],
    title: 'Agent state storage is not writable',
    description: 'The cluster agent could not write its local task ledger. Sync cannot start until agent state storage is writable.',
    detail: 'The agent failed to persist task or event state under /var/lib/hypercdr-agent. Check the agent state PVC, storage backend health, mount mode, and node storage status before retrying.',
    match: message => message.includes('/var/lib/hypercdr-agent') && message.includes('read-only file system'),
  },
  {
    code: '120001',
    aliases: ['BACKUP_REPOSITORY_CONNECTION_FAILED'],
    title: 'Backup repository connection failed',
    description: 'The cluster agent could not connect to the backup repository.',
    detail: 'The cluster agent could not connect to the configured backup repository. Check endpoint, credentials, bucket, TLS settings, and network access.',
    match: message => message.includes('backup repository connection'),
  },
  {
    code: '120002',
    aliases: ['KOPIA_REPOSITORY_NOT_INITIALIZED'],
    title: 'Kopia repository is not initialized',
    description: 'Kopia repository is missing or not initialized for this storage location.',
    detail: 'The Kopia repository required for file-system volume backup is missing or not initialized. Reconfigure or retry the BackupStorageLocation for this cluster, then run sync again.',
    match: message => message.includes('repository not initialized in the provided storage'),
  },
  {
    code: '120003',
    aliases: ['STORAGE_PREFLIGHT_FAILED'],
    title: 'Storage preflight failed',
    description: 'The storage preflight check failed before the task was dispatched.',
    detail: 'The platform could not verify storage readiness before dispatching the task. Check BackupStorageLocation configuration and retry storage setup.',
  },
  {
    code: '120004',
    aliases: ['STORAGE_FAILED'],
    title: 'Storage configuration failed',
    description: 'Backup storage configuration failed for the cluster.',
    detail: 'The cluster could not complete BackupStorageLocation setup. Check Velero namespace resources, repository credentials, and object storage connectivity.',
  },
  {
    code: '130002',
    aliases: ['VOLUME_BACKUP_FAILED', 'BACKUP_FAILED', 'VELERO_BACKUP_FAILED'],
    title: 'Volume data backup failed',
    description: 'One or more persistent volume backups failed during data transfer.',
    detail: 'Velero reported a data path backup failure. Check pod volume backup details, repository state, and volume backup task events.',
    match: message => message.includes('data path backup failed'),
  },
  {
    code: '130003',
    aliases: ['BACKUP_PARTIALLY_FAILED'],
    title: 'Backup partially failed',
    description: 'Velero completed with partial failures. Review task details for affected resources.',
    detail: 'The backup reached a partially failed state. Some resources or volume data may not be protected by this restore point.',
    match: message => message.includes('PartiallyFailed'),
  },
  {
    code: '130004',
    aliases: ['SYNC_FAILED'],
    title: 'Sync failed',
    description: 'The sync task failed before a usable restore point was created.',
    detail: 'The backup or restore point indexing workflow failed. Review Velero backup status, task events, and repository state.',
  },
  {
    code: '130005',
    aliases: ['SYNC_FORCE_STOPPED'],
    title: 'Sync canceled',
    description: 'The sync task was canceled by user.',
    detail: 'The platform requested Velero to delete the running backup. No restore point is created for this canceled sync task.',
  },
  {
    code: '130006',
    aliases: ['SYNC_FORCE_STOP_FAILED', 'BACKUP_CANCEL_DELETE_FAILED', 'BACKUP_CANCEL_SUBMIT_FAILED'],
    title: 'Cancel sync failed',
    description: 'The running sync task could not be canceled.',
    detail: 'The platform could not complete the Velero backup delete request. Check the source cluster agent, Velero DeleteBackupRequest, and Backup CR status.',
  },
  {
    code: '140001',
    aliases: ['RESTORE_FAILED'],
    title: 'Restore failed',
    description: 'The restore task failed before the target namespace became usable.',
    detail: 'Velero restore failed. Review restore result errors, namespace conflicts, transforms, and target cluster resources.',
  },
  {
    code: '140002',
    aliases: ['DRILL_FAILED'],
    title: 'Drill failed',
    description: 'The drill task failed before validation could complete.',
    detail: 'The drill restore did not complete successfully. Review restore result errors and target namespace resources.',
  },
  {
    code: '140003',
    aliases: ['TAKEOVER_FAILED'],
    title: 'Takeover failed',
    description: 'The takeover task failed before the target namespace became active.',
    detail: 'The takeover restore did not complete successfully. Review restore errors and target cluster readiness before retrying.',
  },
  {
    code: '140004',
    aliases: ['RECOVERY_TASK_FAILED'],
    title: 'Recovery task failed',
    description: 'The recovery task failed. Review task details for the root cause.',
    detail: 'The recovery workflow failed. Review the original error, task events, and technical payload.',
  },
];

const ERROR_MESSAGE_BY_CODE = new Map(ERROR_MESSAGE_CATALOG.map(item => [item.code, item]));
const ERROR_CODE_BY_ALIAS = new Map(ERROR_MESSAGE_CATALOG.flatMap(item => item.aliases.map(alias => [alias, item.code] as const)));

function taskFailureText(task: ApiTask, events?: ApiTaskEvent[]): string {
  const details = taskFailureDetails(task, events);
  return details.length > 0 ? summarizeTaskFailure(details[0]) : 'View details';
}

function taskFailureSummary(task: ApiTask, events?: ApiTaskEvent[]): { code: string; title: string; description: string; fullText: string } {
  const details = taskFailureDetails(task, events);
  const firstDetail = details[0] || task.errorMessage || task.errorCode || 'Task failed';
  const code = resolveErrorCode(task.errorCode, firstDetail);
  const definition = errorMessageDefinition(code);
  const originalCode = String(task.errorCode || '').trim();
  const fullText = [
    originalCode && !/^\d{6}$/.test(originalCode) ? `Original error code: ${originalCode}` : '',
    definition.detail,
    ...(details.length > 0 ? details : [firstDetail]),
  ].filter(Boolean).join('\n');
  return {
    code,
    title: definition.title,
    description: definition.description,
    fullText,
  };
}

function normalizeErrorCode(code?: string): string {
  const raw = String(code || '').trim();
  if (/^\d{6}$/.test(raw)) return raw;
  const normalized = raw.replace(/[^a-zA-Z0-9_:-]+/g, '_').replace(/^_+|_+$/g, '').toUpperCase();
  const mapped = ERROR_CODE_BY_ALIAS.get(normalized);
  return mapped || '100000';
}

function resolveErrorCode(rawCode: string | undefined, message: string): string {
  const normalized = normalizeErrorCode(rawCode);
  if (rawCode && normalized !== '100000') return normalized;
  const matched = ERROR_MESSAGE_CATALOG.find(item => item.match?.(message));
  return matched?.code || normalized;
}

function errorMessageDefinition(code: string): ErrorMessageDefinition {
  return ERROR_MESSAGE_BY_CODE.get(code) || ERROR_MESSAGE_BY_CODE.get('100000')!;
}

function volumeFailureDetails(payload: any): string[] {
  const velero = recordFromUnknown(payload?.velero) || {};
  const volumeProgress = recordFromUnknown(payload?.volumeProgress) || recordFromUnknown(velero.volumeProgress) || {};
  const items = Array.isArray(volumeProgress.items) ? volumeProgress.items : [];
  return items
    .map(item => {
      const record = recordFromUnknown(item) || {};
      const message = String(record.message || '').trim();
      if (!message) return '';
      const name = String(record.name || '').trim();
      const phase = String(record.phase || '').trim();
      const prefix = ['Volume backup', name, phase].filter(Boolean).join(' ');
      return `${prefix}: ${humanizeBackupFailureMessage(message)}`;
    })
    .filter(Boolean);
}

function humanizeBackupFailureMessage(message: string): string {
  if (message.includes('repository not initialized in the provided storage')) {
    return `${message}. Kopia repository is missing or not initialized. Reconfigure or retry the BackupStorageLocation for this cluster, then run sync again. If the object storage kopia directory was deleted manually, the repository must be initialized again first.`;
  }
  return message;
}

function summarizeTaskFailure(message: string): string {
  if (!message) return 'View details';
  if (message.includes('repository not initialized in the provided storage')) return 'Kopia repository is not initialized';
  if (message.includes('backup repository connection')) return 'Backup repository connection failed';
  if (message.includes('data path backup failed')) return 'Volume data backup failed';
  if (message.includes('PartiallyFailed')) return 'Backup partially failed';
  if (message.length > 96) return `${message.slice(0, 93).trim()}...`;
  return message;
}

function TaskErrorStatus({
  code,
  title,
  description,
  detail,
  onClick,
}: {
  code?: string;
  title: string;
  description?: string;
  detail?: string;
  onClick?: (event: React.MouseEvent<HTMLElement>) => void;
}) {
  const errorCode = normalizeErrorCode(code);
  const summary = description && description.trim() ? description.trim() : detail && detail.trim() ? detail.trim() : 'Open task details to view the complete error information.';
  const stopTableEvent = (event: React.SyntheticEvent<HTMLElement>) => {
    event.stopPropagation();
  };
  const content = (
    <>
      <span className="hbdr-dr-task-error-title">
        <strong><b>[{errorCode}]</b> {title}</strong>
      </span>
      <em>{summary}</em>
    </>
  );
  if (onClick) {
    return (
      <button
        type="button"
        className="hbdr-dr-task-error"
        onPointerDown={stopTableEvent}
        onMouseDown={stopTableEvent}
        onClick={event => {
          event.stopPropagation();
          onClick(event);
        }}
      >
        {content}
      </button>
    );
  }
  return (
    <span className="hbdr-dr-task-error">
      {content}
    </span>
  );
}

function TaskErrorDetailBlock({
  failure,
  details,
}: {
  failure: { code: string; title: string; description: string; fullText: string };
  details?: string[];
}) {
  const fullDetails = details && details.length > 0 ? details : failure.fullText ? [failure.fullText] : [];
  return (
    <div className="hbdr-task-detail-error">
      <div>
        <strong>[{failure.code}] {failure.title}</strong>
        <span>{failure.description}</span>
        <section>
          <b>Detail</b>
          {fullDetails.length > 0 ? (
            fullDetails.map((detail, index) => <p key={`${index}-${detail}`}>{detail}</p>)
          ) : (
            <p>No detailed error was reported by the task.</p>
          )}
        </section>
      </div>
    </div>
  );
}

function syncPreparingMessage(events: ApiTaskEvent[] | undefined): string {
  const storageStarted = hasTaskEventReason(events, ['storage_preflight_started']);
  const storageDoneOrSkipped = hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
  if (storageStarted && !storageDoneOrSkipped) {
    return 'Configuring storage...';
  }
  return 'Dispatching sync task...';
}

function recoveryActionText(taskType: string): { label: string; dispatching: string; running: string; complete: string } {
  if (taskType === 'drill') {
    return {
      label: 'Drill',
      dispatching: 'Dispatching drill task...',
      running: 'Drilling...',
      complete: 'Drill complete',
    };
  }
  if (taskType === 'takeover') {
    return {
      label: 'Takeover',
      dispatching: 'Dispatching takeover task...',
      running: 'Taking over...',
      complete: 'Takeover complete',
    };
  }
  return {
    label: 'Restore',
    dispatching: 'Dispatching restore task...',
    running: 'Restoring...',
    complete: 'Restore complete',
  };
}

function taskDetailLabel(taskType: string): string {
  const normalized = (taskType || '').toLowerCase();
  if (normalized === 'drill') return 'Drill';
  if (normalized === 'takeover') return 'Takeover';
  if (normalized === 'restore') return 'Restore';
  if (normalized.includes('backup') || normalized.includes('sync')) return 'Sync';
  return 'Task';
}

function recoveryPreparingMessage(events: ApiTaskEvent[] | undefined, taskType: string): string {
  const text = recoveryActionText(taskType);
  const storageStarted = hasTaskEventReason(events, ['storage_preflight_started']);
  const storageDoneOrSkipped = hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
  if (storageStarted && !storageDoneOrSkipped) {
    return 'Configuring storage...';
  }
  return text.dispatching;
}

function formatAge(seconds?: number): string {
  if (!seconds || seconds < 0) return '-';
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  if (days > 0) return `${days}d`;
  if (hours > 0) return `${hours}h`;
  if (minutes > 0) return `${minutes}m`;
  return `${Math.floor(seconds)}s`;
}

function compactList(values?: string[], empty = '-'): string {
  if (!values || values.length === 0) return empty;
  return values.join(', ');
}

function kubectlColumnsForKind(kind?: string): string[] {
  switch ((kind || '').toLowerCase()) {
    case 'deployment':
      return ['NAME', 'READY', 'UP-TO-DATE', 'AVAILABLE', 'AGE', 'CONTAINERS', 'IMAGES', 'SELECTOR'];
    case 'statefulset':
      return ['NAME', 'READY', 'AGE'];
    case 'daemonset':
      return ['NAME', 'DESIRED', 'CURRENT', 'READY', 'UP-TO-DATE', 'AVAILABLE', 'NODE SELECTOR', 'AGE'];
    case 'job':
      return ['NAME', 'COMPLETIONS', 'DURATION', 'AGE'];
    case 'cronjob':
      return ['NAME', 'SCHEDULE', 'TIMEZONE', 'SUSPEND', 'ACTIVE', 'LAST SCHEDULE', 'AGE'];
    case 'service':
      return ['NAME', 'TYPE', 'CLUSTER-IP', 'EXTERNAL-IP', 'PORT(S)', 'AGE'];
    case 'ingress':
      return ['NAME', 'CLASS', 'HOSTS', 'ADDRESS', 'PORTS', 'AGE'];
    case 'networkpolicy':
      return ['NAME', 'POD-SELECTOR', 'AGE'];
    case 'persistentvolumeclaim':
      return ['NAME', 'STATUS', 'VOLUME', 'CAPACITY', 'ACCESS MODES', 'STORAGECLASS', 'VOLUMEATTRIBUTESCLASS', 'AGE'];
    case 'configmap':
      return ['NAME', 'DATA', 'AGE'];
    case 'secret':
      return ['NAME', 'TYPE', 'DATA', 'AGE'];
    case 'serviceaccount':
      return ['NAME', 'SECRETS', 'AGE'];
    case 'role':
      return ['NAME', 'AGE'];
    case 'rolebinding':
      return ['NAME', 'ROLE', 'AGE'];
    case 'horizontalpodautoscaler':
      return ['NAME', 'REFERENCE', 'TARGETS', 'MINPODS', 'MAXPODS', 'REPLICAS', 'AGE'];
    case 'poddisruptionbudget':
      return ['NAME', 'MIN AVAILABLE', 'MAX UNAVAILABLE', 'ALLOWED DISRUPTIONS', 'AGE'];
    case 'resourcequota':
    case 'limitrange':
      return ['NAME', 'AGE'];
    default:
      return ['KIND', 'NAME', 'NAMESPACE', 'API VERSION'];
  }
}

function resourceColumnValue(resource: ResourceRef, item: ResourceKindSummary, column: string, namespace: string): string {
  if (column === 'KIND') return item.kind;
  if (column === 'NAME') return resource.name;
  if (column === 'NAMESPACE') return resource.namespace || namespace;
  if (column === 'API VERSION') return resource.apiVersion || '-';
  if (resource.fields?.[column]) return resource.fields[column];
  switch (column) {
    case 'READY':
      return resource.ready || (resource.desiredReplicas !== undefined ? `${resource.readyReplicas || 0}/${resource.desiredReplicas}` : '-');
    case 'UP-TO-DATE':
      return resource.updatedReplicas !== undefined ? String(resource.updatedReplicas) : '-';
    case 'AVAILABLE':
      return resource.availableReplicas !== undefined ? String(resource.availableReplicas) : '-';
    case 'AGE':
      return formatAge(resource.ageSeconds);
    case 'CONTAINERS':
      return compactList(resource.containers);
    case 'IMAGES':
      return compactList(resource.images);
    case 'SELECTOR':
      return resource.selector || '-';
    default:
      return '-';
  }
}

function resourcePrimaryStatus(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
  const columns = kubectlColumnsForKind(item.kind);
  const preferred = ['READY', 'STATUS', 'TYPE', 'COMPLETIONS', 'SUSPEND'];
  for (const column of preferred) {
    if (columns.includes(column)) {
      const value = resourceColumnValue(resource, item, column, namespace);
      if (value && value !== '-') return value;
    }
  }
  return resource.ready || '-';
}

function resourceInventoryDetails(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string[] {
  const columns = kubectlColumnsForKind(item.kind);
  const skip = new Set(['KIND', 'NAME', 'NAMESPACE', 'READY', 'STATUS', 'TYPE', 'COMPLETIONS', 'SUSPEND', 'AGE']);
  const details = columns
    .filter(column => !skip.has(column))
    .map(column => {
      const value = resourceColumnValue(resource, item, column, namespace);
      return value && value !== '-' ? `${column}: ${value}` : '';
    })
    .filter(Boolean);
  if (details.length > 0) return details.slice(0, 4);
  if (resource.apiVersion) return [`API: ${resource.apiVersion}`];
  return [];
}

function resourceInventoryTitle(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
  const lines = [
    `${item.kind}/${resource.name}`,
    `Namespace: ${resource.namespace || namespace}`,
    `Status: ${resourcePrimaryStatus(resource, item, namespace)}`,
    ...resourceInventoryDetails(resource, item, namespace),
    `Age: ${formatAge(resource.ageSeconds)}`,
  ];
  return lines.filter(Boolean).join('\n');
}

function resourceInventoryDetailLimit(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string[] {
  const details = resourceInventoryDetails(resource, item, namespace);
  const kind = (item.kind || '').toLowerCase();
  if (kind === 'persistentvolumeclaim') return details.slice(0, 4);
  if (kind === 'deployment' || kind === 'daemonset' || kind === 'statefulset') return details.slice(0, 3);
  return details.slice(0, 3);
}

function resourceFactLabel(label: string): string {
  const normalized = label.trim().toUpperCase();
  const labels: Record<string, string> = {
    'UP-TO-DATE': 'Updated',
    AVAILABLE: 'Available',
    CONTAINERS: 'Containers',
    IMAGES: 'Images',
    DESIRED: 'Desired',
    CURRENT: 'Current',
    'CLUSTER-IP': 'Cluster IP',
    'EXTERNAL-IP': 'External IP',
    'PORT(S)': 'Ports',
    VOLUME: 'Volume',
    CAPACITY: 'Capacity',
    'ACCESS MODES': 'Access',
    STORAGECLASS: 'StorageClass',
    DATA: 'Data',
    SECRETS: 'Secrets',
    API: 'API',
    SELECTOR: 'Selector',
    'POD-SELECTOR': 'Pod Selector',
  };
  return labels[normalized] || label.toLowerCase().replace(/\b\w/g, char => char.toUpperCase());
}

function resourceInventoryFacts(resource: ResourceRef, item: ResourceKindSummary, namespace: string): Array<{ label: string; value: string }> {
  return resourceInventoryDetailLimit(resource, item, namespace).map(detail => {
    const separator = detail.indexOf(':');
    if (separator === -1) return { label: 'Info', value: detail };
    return {
      label: resourceFactLabel(detail.slice(0, separator)),
      value: detail.slice(separator + 1).trim(),
    };
  });
}

function resourceInventoryDetailText(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
  const facts = resourceInventoryFacts(resource, item, namespace);
  const factValue = (label: string) => facts.find(fact => fact.label === label)?.value;
  const status = resourcePrimaryStatus(resource, item, namespace);
  const kind = (item.kind || '').toLowerCase();
  let parts: string[] = [];
  if (kind === 'deployment' || kind === 'daemonset' || kind === 'statefulset') {
    parts = [
      status !== '-' ? `Ready ${status}` : '',
      factValue('Available') ? `Available ${factValue('Available')}` : '',
      factValue('Containers') ? `Containers ${factValue('Containers')}` : '',
    ];
  } else if (kind === 'persistentvolumeclaim') {
    parts = [
      status !== '-' ? status : '',
      factValue('Capacity') || '',
      factValue('StorageClass') || '',
    ];
  } else if (kind === 'service') {
    parts = [
      status !== '-' ? status : '',
      factValue('Cluster IP') ? `IP ${factValue('Cluster IP')}` : '',
      factValue('Ports') || '',
    ];
  } else if (kind === 'configmap' || kind === 'secret' || kind === 'serviceaccount') {
    parts = facts.slice(0, 2).map(fact => `${fact.label} ${fact.value}`);
  } else {
    parts = [
      status !== '-' ? status : '',
      ...facts.slice(0, 2).map(fact => `${fact.label} ${fact.value}`),
    ];
  }
  parts = parts.filter(Boolean);
  return parts.length > 0 ? parts.join(' · ') : '-';
}

function resourceGridTemplate(columns: string[]): string {
  return columns.map(column => {
    if (column === 'NAME') return 'minmax(160px, 1.2fr)';
    if (column === 'IMAGES') return 'minmax(280px, 2fr)';
    if (column === 'SELECTOR' || column === 'VOLUME') return 'minmax(220px, 1.6fr)';
    if (column === 'PORT(S)' || column === 'HOSTS') return 'minmax(150px, 1fr)';
    if (column === 'AGE' || column === 'DATA' || column === 'READY' || column === 'TYPE' || column === 'STATUS') return 'minmax(74px, max-content)';
    return 'minmax(118px, max-content)';
  }).join(' ');
}

function mapApplicationStatus(status: string | undefined, isProtected: boolean): AppItem['status'] {
  if (isProtected) return 'Protected';
  const normalized = (status || '').trim().toLowerCase();
  if (normalized === 'active' || normalized === 'running') return 'Active';
  if (normalized === 'terminating') return 'Terminating';
  return 'Unknown';
}

function normalizeNodeStatus(status: string | undefined): string {
  const normalized = (status || '').trim().toLowerCase();
  if (normalized === 'ready') return 'Ready';
  if (normalized === 'notready' || normalized === 'not-ready') return 'NotReady';
  return status || 'Unknown';
}

type ApplicationStage = 'select' | 'config' | 'run';
function stageOfApp(protectionStatus: string | undefined, isProtected: boolean): ApplicationStage {
  const ps = (protectionStatus || '').toLowerCase();
  if (ps === 'protected' || isProtected) return 'run';
  if (ps === 'pending_protection') return 'config';
  return 'select';
}

function drStatusForPlan(status: string | undefined): { label: string; tone: 'ok' | 'progress' | 'warn' | 'muted'; title: string } {
  const normalized = (status || '').trim().toLowerCase();
  switch (normalized) {
    case 'active':
      return { label: 'Ready', tone: 'ok', title: 'Storage location and backup schedule are active' };
    case 'active_with_warning':
      return { label: 'Ready', tone: 'warn', title: 'Source backup schedule is active. Target cluster storage needs attention; restore, drill, and takeover may be unavailable until BSL is reconfigured.' };
    case 'activating_storage':
      return { label: 'Configuring storage', tone: 'progress', title: 'BackupStorageLocation is being configured. Failed attempts are retried automatically up to 3 times.' };
    case 'storage_failed':
      return { label: 'Storage failed', tone: 'warn', title: 'BackupStorageLocation configuration failed' };
    case 'activating_schedule':
      return { label: 'Configuring schedule', tone: 'progress', title: 'Velero backup schedule is being configured' };
    case 'schedule_failed':
      return { label: 'Schedule failed', tone: 'warn', title: 'Velero backup schedule configuration failed' };
    case 'cleanup_running':
      return { label: 'Cleaning...', tone: 'progress', title: 'Protection resources are being cleaned. Restore points, task records, schedule, and backup data are being removed.' };
    case 'cleanup_failed':
      return { label: 'Cleanup failed', tone: 'warn', title: 'Protection resource cleanup failed. Check the latest cleanup task error and retry cleanup.' };
    case 'disabled':
      return { label: 'Disabled', tone: 'muted', title: 'Protection plan is disabled' };
    case 'pending_activation':
    case '':
      return { label: 'Pending activation', tone: 'muted', title: 'Protection plan is saved but not active yet' };
    default:
      return { label: status || 'Pending activation', tone: 'muted', title: status || 'Pending activation' };
  }
}

function isProtectionPlanReady(status: string | undefined): boolean {
  return ['active', 'active_with_warning'].includes((status || '').trim().toLowerCase());
}

function canRetryDrActivation(status: string | undefined): boolean {
  return ['pending_activation', 'schedule_failed'].includes((status || '').trim().toLowerCase());
}

function isProtectionPlanCleaning(status: string | undefined): boolean {
  return (status || '').trim().toLowerCase() === 'cleanup_running';
}

const resourceCategoryMeta: Array<{ key: ResourceCategoryKey; label: string }> = [
  { key: 'workloads', label: 'Workloads' },
  { key: 'network', label: 'Network' },
  { key: 'storage', label: 'Storage' },
  { key: 'config', label: 'Config' },
  { key: 'access', label: 'Access' },
  { key: 'jobs', label: 'Jobs' },
  { key: 'scaling', label: 'Scaling' },
  { key: 'policy', label: 'Policy' },
  { key: 'other', label: 'Other' },
];
const resourceCategoryKeys = new Set<ResourceCategoryKey>(resourceCategoryMeta.map(item => item.key));
const resourceCategoryIconMap: Record<ResourceCategoryKey, React.ComponentType<{ size?: number; className?: string }>> = {
  workloads: Boxes,
  network: Network,
  storage: HardDrive,
  config: FileCog,
  access: KeyRound,
  jobs: Clock,
  scaling: Gauge,
  policy: ClipboardList,
  other: MoreVertical,
};

function shortResourceKind(kind: string): string {
  const map: Record<string, string> = {
    Deployment: 'DEP',
    StatefulSet: 'STS',
    DaemonSet: 'DS',
    Job: 'JOB',
    CronJob: 'CJ',
    PersistentVolumeClaim: 'PVC',
    Service: 'SVC',
    Ingress: 'ING',
    NetworkPolicy: 'NP',
    ConfigMap: 'CM',
    Secret: 'SEC',
    ServiceAccount: 'SA',
    Role: 'ROLE',
    RoleBinding: 'RB',
    HorizontalPodAutoscaler: 'HPA',
    PodDisruptionBudget: 'PDB',
    ResourceQuota: 'RQ',
    LimitRange: 'LIMIT',
  };
  return map[kind] || kind.toUpperCase();
}

function resourceCategoryForKind(kind: string): ResourceCategoryKey {
  const normalized = kind.toLowerCase();
  if (['pod', 'deployment', 'statefulset', 'daemonset', 'replicaset', 'replicationcontroller'].includes(normalized)) return 'workloads';
  if (['service', 'ingress', 'gateway', 'httproute', 'endpoints', 'endpointslice', 'networkpolicy'].includes(normalized)) return 'network';
  if (normalized === 'persistentvolumeclaim') return 'storage';
  if (['configmap', 'secret'].includes(normalized)) return 'config';
  if (['serviceaccount', 'role', 'rolebinding'].includes(normalized)) return 'access';
  if (['job', 'cronjob'].includes(normalized)) return 'jobs';
  if (['horizontalpodautoscaler', 'poddisruptionbudget'].includes(normalized)) return 'scaling';
  if (['resourcequota', 'limitrange'].includes(normalized)) return 'policy';
  return 'other';
}

function mergeResourceItems(items: ResourceKindSummary[]): ResourceKindSummary[] {
  const byKind = new Map<string, ResourceKindSummary>();
  items.forEach(item => {
    if (!item.kind) return;
    const existing = byKind.get(item.kind);
    if (existing) {
      existing.count += item.count || 0;
      existing.resources = [...(existing.resources || []), ...(item.resources || [])];
    } else {
      byKind.set(item.kind, {
        ...item,
        shortName: item.shortName || shortResourceKind(item.kind),
        count: item.count || 0,
        resources: item.resources || [],
      });
    }
  });
  return Array.from(byKind.values()).sort((a, b) => a.kind.localeCompare(b.kind));
}

function fallbackResourceRefs(kind: string, count: number, namespace: string): ResourceRef[] {
  void kind;
  void count;
  void namespace;
  return [];
}

function buildResourceCategory(key: ResourceCategoryKey, label: string, items: Array<{ kind: string; count: number }>, namespace: string): ResourceCategory {
  const visibleItems = items.filter(item => item.count > 0).map(item => ({
    kind: item.kind,
    shortName: shortResourceKind(item.kind),
    count: item.count,
    resources: fallbackResourceRefs(item.kind, item.count, namespace),
  }));
  return {
    key,
    label,
    total: visibleItems.reduce((sum, item) => sum + item.count, 0),
    items: visibleItems,
  };
}

function normalizeResourceCategories(app: AppItem): ResourceCategory[] {
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

function formatLabelOptionSummary(option: Pick<LabelSelectorOption, 'namespaceNames' | 'resources'>): string {
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

function buildLabelSelectorOptions(targetApps: AppItem[]): LabelSelectorOption[] {
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

function mapClusterStatus(status: string, connectionStatus: string): ClusterStatus {
  if (connectionStatus === 'online' && status !== 'warning') return 'healthy';
  if (status === 'syncing') return 'syncing';
  return 'warning';
}


type ApiRestorePointView = {
  id: string;
  sourceClusterId: string;
  protectionPlanId?: string;
  appId?: string;
  backupTaskId?: string;
  sourceNamespace: string;
  title: string;
  time: string;
  pointType: 'local' | 'remote';
  status: string;
  veleroBackupName: string;
  includedNamespaces?: string[];
};

function stringArrayFromAny(value: any): string[] {
  if (Array.isArray(value)) return value.map(item => String(item)).filter(Boolean);
  if (typeof value === 'string' && value.trim()) return [value.trim()];
  return [];
}

function namespacesFromPayload(payload: any): string[] {
  const values = [
    ...stringArrayFromAny(payload?.includedNamespaces),
    ...stringArrayFromAny(payload?.sourceNamespaces),
    ...stringArrayFromAny(payload?.velero?.includedNamespaces),
    ...stringArrayFromAny(payload?.velero?.manifest?.spec?.includedNamespaces),
  ];
  if (payload?.sourceNamespace) values.push(String(payload.sourceNamespace));
  return Array.from(new Set(values.filter(Boolean)));
}

function restorePointIsScheduled(point: Pick<ApiRestorePoint, 'metadata'>) {
  const scheduled = point.metadata?.scheduled;
  return scheduled === true || scheduled === 'true';
}

function restorePointListStatus(point: ApiRestorePoint) {
  const retentionState = typeof point.metadata?.retentionState === 'string' ? point.metadata.retentionState : '';
  if (retentionState === 'deleting' || retentionState === 'pending_delete') return 'deleting';
  if (retentionState === 'delete_failed') return 'delete_failed';
  return point.status || 'available';
}

function mapRestorePoint(raw: any): ApiRestorePointView {
  const ns: string = raw?.sourceNamespace || raw?.metadata?.sourceNamespace || '';
  const includedNamespaces = namespacesFromPayload({ ...raw?.metadata, sourceNamespace: ns });
  const storageName: string = raw?.backupStorageName || raw?.metadata?.backupStorageName || 'default';
  const time: string = raw?.completedAt || raw?.startedAt || raw?.createdAt || '';
  const pointType: 'local' | 'remote' = (raw?.pointType || 'remote').toLowerCase().includes('local') ? 'local' : 'remote';
  return {
    id: raw?.id,
    sourceClusterId: raw?.sourceClusterId,
    protectionPlanId: raw?.protectionPlanId,
    appId: raw?.appId,
    backupTaskId: raw?.backupTaskId || raw?.metadata?.backupTaskId || '',
    sourceNamespace: ns,
    title: `${storageName} · ${raw?.veleroBackupName || raw?.id?.slice(0, 8) || 'restore point'}`,
    time,
    pointType,
    status: raw?.status || 'available',
    veleroBackupName: raw?.veleroBackupName || '',
    includedNamespaces,
  };
}

function restorePointNamespaces(point: { sourceNamespace?: string; includedNamespaces?: string[]; metadata?: Record<string, any> }): string[] {
  const includedNamespaces = 'includedNamespaces' in point && Array.isArray(point.includedNamespaces) ? point.includedNamespaces : [];
  if (includedNamespaces.length) return includedNamespaces;
  const metadataNamespaces = namespacesFromPayload({ ...point.metadata, sourceNamespace: point.sourceNamespace || point.metadata?.sourceNamespace });
  return metadataNamespaces.length ? metadataNamespaces : [point.sourceNamespace].filter(Boolean);
}

function taskPlanId(task: ApiTask): string {
  return task.protectionPlanId || String(task.payload?.protectionPlanId || '');
}

function taskRestorePointId(task: ApiTask): string {
  return task.restorePointId || String(task.payload?.restorePointId || '');
}

function recoveryTaskMatchesApp(task: ApiTask, app: ApiApplication, plans: ApiProtectionPlan[], restorePoints: ApiRestorePointView[]): boolean {
  const appPlan = plans.find(item => planIncludesApp(item, app.id));
  const planID = taskPlanId(task);
  const restorePointID = taskRestorePointId(task);
  const namespace = app.namespace || app.name;
  if (!appPlan?.id || !planID || !restorePointID || !namespace) return false;
  if (planID !== appPlan.id) return false;
  const taskNamespaces = namespacesFromPayload(task.payload);
  if (!taskNamespaces.includes(namespace)) return false;
  const point = restorePoints.find(item => item.id === restorePointID);
  if (point) {
    return point.protectionPlanId === appPlan.id && restorePointNamespaces(point).includes(namespace);
  }
  return true;
}

function buildAppTaskMap(tasks: ApiTask[], apps: ApiApplication[], taskTypes?: string[], restorePoints: ApiRestorePointView[] = [], plans: ApiProtectionPlan[] = []): Record<string, ApiTask> {
  const byNamespace: Record<string, ApiTask> = {};
  const sorted = [...tasks].sort((a, b) => {
    const activeDelta = Number(isActiveTaskStatus(b.status)) - Number(isActiveTaskStatus(a.status));
    if (activeDelta !== 0) return activeDelta;
    return (b.createdAt || '').localeCompare(a.createdAt || '');
  });
  const allowedTypes = taskTypes ? new Set(taskTypes) : null;
  for (const app of apps) {
    const match = sorted.find(t => {
      if (allowedTypes && !allowedTypes.has(t.type)) return false;
      if (t.payload?.archivedClusterId || t.payload?.archivedAppId || t.payload?.archivedProtectionPlanId) return false;
      if (['restore', 'drill', 'takeover'].includes(t.type)) return recoveryTaskMatchesApp(t, app, plans, restorePoints);
      if (!t.clusterId || t.clusterId !== app.clusterId) return false;
      const taskNamespaces = namespacesFromPayload(t.payload);
      if (t.protectionPlanId && taskNamespaces.length > 0) {
        return taskNamespaces.includes(app.namespace);
      }
      if (t.appId === app.id) return true;
      if (t.restorePointId) {
        const point = restorePoints.find(item => item.id === t.restorePointId);
        if (point) {
          if (point.sourceClusterId && point.sourceClusterId !== app.clusterId) return false;
          if (point.appId) return point.appId === app.id;
          return point.sourceClusterId === app.clusterId && point.sourceNamespace === app.namespace;
        }
      }
      return t.payload && t.payload.sourceNamespace === app.namespace;
    });
    if (match) byNamespace[app.namespace] = match;
  }
  return byNamespace;
}

function taskMatchesRestorePoint(task: ApiTask, restorePointId: string): boolean {
  return task.restorePointId === restorePointId
    || String(task.payload?.restorePointId || '') === restorePointId
    || String(task.payload?.archivedRestorePointId || '') === restorePointId;
}

function latestTaskForRestorePoint(tasks: ApiTask[], restorePointId: string): ApiTask | undefined {
  return [...tasks]
    .filter(item => taskMatchesRestorePoint(item, restorePointId) && ['restore', 'drill', 'takeover'].includes(item.type))
    .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''))[0];
}

function isActiveTaskStatus(status: string | undefined): boolean {
  return ['queued', 'dispatched', 'accepted', 'running', 'syncing', 'finalizing', 'canceling'].includes((status || '').toLowerCase());
}

function isCompletedTaskStatus(status: string | undefined): boolean {
  return ['succeeded', 'completed', 'success'].includes((status || '').toLowerCase());
}

function appOverrideKey(app: Pick<AppItem, 'clusterId' | 'namespace' | 'name'>): string {
  return `${app.clusterId || 'unknown'}::${app.namespace || app.name}`;
}

function planIncludesApp(plan: ApiProtectionPlan, appId: string): boolean {
  return plan.appId === appId || Boolean(plan.appIds?.includes(appId));
}

function mapApps(apps: ApiApplication[], plans: ApiProtectionPlan[], policies: ApiPolicy[], storage: ApiStorageRepo[], clusters: ApiCluster[]): AppItem[] {
  return apps.map(app => {
    const plan = plans.find(item => planIncludesApp(item, app.id));
    const protectedByState = app.protectionStatus === 'protected';
    const isProtected = Boolean(plan) || protectedByState;
    const policy = policies.find(item => item.id === plan?.policyId);
    const repo = storage.find(item => item.id === plan?.storageRepoId);
    const target = clusters.find(item => item.id === plan?.targetClusterId);
    return {
      apiId: app.id,
      clusterId: app.clusterId,
      name: app.namespace || app.name,
      namespace: app.namespace || app.name,
      status: mapApplicationStatus(app.status, isProtected),
      namespaceStatus: app.status || 'unknown',
      workloadCount: app.workloadCount || 0,
      serviceCount: app.serviceCount || 0,
      ingressCount: app.ingressCount || 0,
      configMapCount: app.configMapCount || 0,
      secretCount: app.secretCount || 0,
      pvcCount: app.pvcCount || 0,
      pvCapacityBytes: app.pvCapacityBytes || 0,
      resourceSummary: app.resourceSummary,
      labels: app.labels || {},
      protectionStatus: app.protectionStatus,
      protectionPlanId: plan?.id,
      protectionPlanCreatedAt: plan?.createdAt,
      stage: stageOfApp(app.protectionStatus, isProtected),
      policy: policy?.name,
      storage: repo?.name,
      targetCluster: target?.name,
      isProtected,
      lastBackup: isProtected ? 'synced recently' : undefined,
      tags: [],
    };
  });
}

function mapCluster(cluster: ApiCluster, apps: AppItem[] = []): Cluster {
  const appCount = cluster.applicationCount || cluster.namespaceCount || apps.length;
  return {
    id: cluster.id,
    name: cluster.name || 'unknown-cluster',
    region: cluster.connectionStatus === 'online' ? 'connected' : 'disconnected',
    version: cluster.kubeVersion || 'unknown',
    status: mapClusterStatus(cluster.status, cluster.connectionStatus),
    connectionStatus: cluster.connectionStatus || 'unknown',
    compliance: cluster.complianceScore ?? 0,
    nodes: cluster.nodeCount,
    nodeDetails: cluster.nodes || [],
    storageClasses: cluster.storageClasses || [],
    namespaces: cluster.namespaceCount || appCount,
    applications: appCount,
    agentVersion: cluster.agentVersion || 'pending',
    latestAgentVersion: cluster.latestAgentVersion || cluster.agentVersion || 'pending',
    agentImage: cluster.agentImage,
    agentImageDigest: cluster.agentImageDigest,
    latestAgentImage: cluster.latestAgentImage,
    latestAgentImageDigest: cluster.latestAgentImageDigest,
    agentUpgradeAvailable: Boolean(cluster.agentUpgradeAvailable),
    agentUpgradeStatus: cluster.agentUpgradeStatus,
    veleroStatus: cluster.veleroStatus || 'unknown',
    lastSeenAt: cluster.lastSeenAt,
    role: cluster.role || 'both',
    isDefault: Boolean(cluster.isDefault),
    apps,
  };
}

function mapStorageRepo(repo: ApiStorageRepo): StorageRepo {
  const raw = (repo.status || '').toLowerCase();
  const status: StorageRepo['status'] = ['connected', 'ready', 'active'].includes(raw)
    ? 'connected'
    : raw === 'warning'
      ? 'warning'
      : 'unknown';
  const cfg = (repo.config || {}) as Record<string, unknown>;
  const urlStyle = typeof cfg.urlStyle === 'string' ? (cfg.urlStyle as string) : 'path';
  return {
    id: repo.id,
    name: repo.name,
    type: repo.type || 'S3',
    endpoint: repo.endpoint || '',
    bucket: repo.bucket || '',
    region: repo.region || 'N/A',
    useTls: repo.tlsEnabled,
    status,
    updatedAt: repo.updatedAt || repo.createdAt || '',
    lastValidatedAt: repo.lastValidatedAt && repo.lastValidatedAt !== '0001-01-01T00:00:00Z' ? repo.lastValidatedAt : undefined,
    urlStyle,
  };
}

function formatLastSeen(value?: string) {
  if (!value) return 'unknown';
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return 'unknown';
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function formatDateTime(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function formatLocalDateTime(value?: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const parts = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value || '';
  return `${part('year')}-${part('month')}-${part('day')} ${part('hour')}:${part('minute')}:${part('second')}`;
}

function formatNextSyncTime(value?: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return '';
  const now = new Date();
  const sameDay = date.getFullYear() === now.getFullYear()
    && date.getMonth() === now.getMonth()
    && date.getDate() === now.getDate();
  const options: Intl.DateTimeFormatOptions = sameDay
    ? { hour: '2-digit', minute: '2-digit', hour12: false }
    : { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false };
  return date.toLocaleString(undefined, options);
}

function currentTimeZoneLabel() {
  const offsetMinutes = -new Date().getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? '+' : '-';
  const abs = Math.abs(offsetMinutes);
  const hours = String(Math.floor(abs / 60)).padStart(2, '0');
  const minutes = String(abs % 60).padStart(2, '0');
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return `My Time Zone: GMT${sign}${hours}:${minutes}${zone ? ` (${zone})` : ''}`;
}

function recoveryCompletedTargetTitle(restorePointLabel: string, completedAt: string | undefined, clusterName: string, namespace: string, actionLabel: string) {
  const point = restorePointLabel || 'restore point';
  const time = formatLocalDateTime(completedAt) || 'completed';
  const target = [clusterName || 'target cluster', namespace || 'namespace'].filter(Boolean).join(' / ');
  return `${point} ${actionLabel.toLowerCase()} ${time} to ${target}`;
}

function recoveryCompletedTargetLabel(clusterName: string, namespace: string) {
  return [clusterName || 'target', namespace || 'namespace'].filter(Boolean).join(' / ');
}

function restorePointDisplayLabel(point?: {
  id?: string;
  time?: string;
  title?: string;
  veleroBackupName?: string;
  completedAt?: string;
  startedAt?: string;
  createdAt?: string;
} | null) {
  if (!point) return '';
  const rawTime = point.time || point.completedAt || point.startedAt || point.createdAt || '';
  const localTime = formatLocalDateTime(rawTime);
  const label = localTime || point.title || point.veleroBackupName || (point.id ? point.id.slice(0, 8) : '');
  return label ? `RP-${label}` : '';
}

function taskStatusLabel(status?: string) {
  if (isSucceededStatus(status)) return 'Succeeded';
  if (isFailedStatus(status)) return 'Failed';
  if (isActiveTaskStatus(status)) return 'Running';
  return status || 'Unknown';
}

function taskStatusClass(status?: string) {
  if (status === 'succeeded') return 'text-emerald-600';
  if (status === 'failed') return 'text-rose-600';
  if (['running', 'accepted', 'dispatched', 'queued'].includes(status || '')) return 'text-blue-600';
  return 'text-slate-500';
}

function agentReadiness(cluster: Cluster) {
  if (cluster.connectionStatus !== 'online') {
    return { label: 'Offline', className: 'text-slate-500' };
  }
  if (cluster.status === 'healthy') {
    return { label: 'Ready', className: 'text-emerald-600' };
  }
  if (cluster.status === 'syncing') {
    return { label: 'Syncing', className: 'text-blue-600' };
  }
  return { label: 'Degraded', className: 'text-amber-600' };
}

function isS3CompatibleType(type: string) {
  return ['s3-compatible', 's3 compatible'].includes(type.toLowerCase());
}

function buildStorageRepositoryInput(repo: StorageRepo): StorageRepositoryInput {
  const config = repo.config || {};
  const isCompatible = isS3CompatibleType(repo.type);
  const endpoint = String(config.endpoint || repo.endpoint || '');
  const bucket = String(config.bucket || repo.bucket || '');
  const region = String(config.region || repo.region || '');
  const accessKey = String(config.accessKey || '');
  const secretKey = String(config.secretKey || '');
  const payloadConfig: Record<string, string | boolean> = {};
  if (config.urlStyle) payloadConfig.urlStyle = String(config.urlStyle);
  if (config.prefix) payloadConfig.prefix = String(config.prefix);
  return {
    name: repo.name,
    type: repo.type,
    endpoint,
    bucket,
    region,
    tlsEnabled: Boolean(config.useSsl ?? repo.useTls),
    config: payloadConfig,
    accessKey,
    secretKey,
  };
}

function mapPolicy(policy: ApiPolicy): PolicyItem {
  const scheduleType = ['daily', 'weekly', 'monthly'].includes(policy.scheduleType) ? policy.scheduleType as PolicyScheduleType : 'interval';
  return {
    id: policy.id,
    name: policy.name,
    composition: policy.composition === 'manual' ? 'manual' : 'combined',
    type: scheduleType,
    intervalValue: policy.intervalValue || 1,
    intervalUnit: policy.intervalUnit === 'minute' || policy.intervalUnit === 'minutes' ? 'minutes' : 'hours',
    hour: policy.hour || 0,
    minute: policy.minute || 0,
    weekDay: policy.weekDay || 0,
    monthDay: policy.monthDay || 1,
    retention: policy.retentionCount || 0,
    status: policy.status === 'disabled' ? 'Disabled' : 'Active',
    bound: policy.boundCount || 0,
  };
}

function labelConditionsToSelector(conditions: Array<{ key: string; operator: 'Equals' | 'Not Equals'; value: string }>) {
  return conditions
    .filter(condition => condition.key && condition.value)
    .map(condition => condition.operator === 'Not Equals' ? `${condition.key}!=${condition.value}` : `${condition.key}=${condition.value}`)
    .join(',');
}

function scriptPayload(script: { name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }) {
  return {
    name: script.name,
    size: script.size,
    source: script.source,
    isEntry: Boolean(script.isEntry),
    content: script.content,
  };
}

const topNav: Array<{ key: TopModule; view: View }> = [
  { key: 'overview', view: 'dashboard' },
  { key: 'dr', view: 'applications' },
  { key: 'config', view: 'clusters' },
  { key: 'ops', view: 'operations' },
  { key: 'monitor', view: 'alerts' },
  { key: 'settings', view: 'settings' },
];

function moduleForView(view: View): TopModule {
  if (view === 'dashboard') return 'overview';
  if (view === 'applications' || view === 'restore_points' || view === 'dr_tasks' || view === 'failback') return 'dr';
  if (view === 'clusters' || view === 'storage' || view === 'policies') return 'config';
  if (view === 'operations' || view === 'tags') return 'ops';
  if (view === 'alerts') return 'monitor';
  return 'settings';
}

function statusText(status: ClusterStatus) {
  if (status === 'healthy') return 'Healthy';
  if (status === 'syncing') return 'Syncing';
  return 'Alert';
}

const formatTime = (hour: number, minute: number) => `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;

function formatPolicySchedule(policy: Pick<PolicyItem, 'composition' | 'type' | 'intervalValue' | 'intervalUnit' | 'hour' | 'minute' | 'weekDay' | 'monthDay'>) {
  if (policy.composition === 'manual') return 'Manual trigger';
  if (policy.composition === 'retention') return 'Not scheduled';
  if (policy.type === 'interval') return `Every ${policy.intervalValue} ${policy.intervalUnit === 'minutes' ? 'minutes' : 'hours'}`;
  if (policy.type === 'daily') return `Every day ${formatTime(policy.hour, policy.minute)}`;
  if (policy.type === 'weekly') return `Every week ${weekdays[policy.weekDay]} ${formatTime(policy.hour, policy.minute)}`;
  return `Every month ${policy.monthDay} Day ${formatTime(policy.hour, policy.minute)}`;
}

function formatPolicyType(type: PolicyScheduleType) {
  if (type === 'interval') return 'Interval';
  if (type === 'daily') return 'Daily Backup';
  if (type === 'weekly') return 'Weekly Backup';
  return 'Monthly Backup';
}

function formatPolicyComposition(composition: PolicyComposition) {
  if (composition === 'manual') return 'Manual';
  if (composition === 'schedule') return 'Schedule Only';
  if (composition === 'retention') return 'Retention Only';
  return 'Schedule + Retention';
}

function formatPolicyRetention(policy: Pick<PolicyItem, 'composition' | 'retention'>) {
  if (policy.composition === 'manual') return 'Not defined';
  if (policy.composition === 'schedule') return 'Platform default';
  return `${policy.retention ?? 0} copies`;
}

function formatScopeLabel(scope: string | undefined) {
  const value = (scope || '').toLowerCase();
  if (value === 'namespace' || value === 'all') return 'All resources';
  if (value === 'label-selector' || value === 'labels' || value === 'filter' || value === 'filtered') return 'Filter resources';
  if (value === 'stateless only' || value === 'stateless') return 'Filter resources';
  return scope || 'All resources';
}

function clusterStatusMeta(status: ClusterStatus) {
  if (status === 'healthy') {
    return { label: 'Healthy' };
  }
  if (status === 'syncing') {
    return { label: 'Syncing' };
  }
  return { label: 'Alert' };
}

function LanguageSwitcher({
  locale,
  setLocale,
  compact = false,
}: {
  locale: LocaleCode;
  setLocale: (locale: LocaleCode) => void;
  compact?: boolean;
}) {
  const language = locales[locale];
  const switchToEnglish = () => setLocale('en');

  return (
    <button
      type="button"
      onClick={switchToEnglish}
      className={compact ? 'hbdr-language-switch hbdr-language-switch-compact' : 'hbdr-language-switch'}
      aria-label={`${language.languageLabel}: ${language.name}`}
    >
      <Languages size={14} />
      <span>{compact ? 'EN' : language.languageLabel}</span>
      <strong>{language.name}</strong>
      <ChevronDown size={13} />
    </button>
  );
}

type ListToolbarChoice = {
  value: string;
  label: string;
  count?: number;
};

type ListToolbarColumn = {
  value: string;
  label: string;
  locked?: boolean;
};

function toggleListValue(values: string[], value: string) {
  return values.includes(value) ? values.filter(item => item !== value) : [...values, value];
}

function listToolbarQueryFields(
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

const COLUMN_FILTER_PREFIX = 'columnFilter:';

function makeColumnFilterToken(field: string, value: string) {
  return `${COLUMN_FILTER_PREFIX}${encodeURIComponent(field)}:${encodeURIComponent(value.trim())}`;
}

function parseColumnFilterToken(token: string): { field: string; value: string } | null {
  if (!token.startsWith(COLUMN_FILTER_PREFIX)) return null;
  const body = token.slice(COLUMN_FILTER_PREFIX.length);
  const separator = body.indexOf(':');
  if (separator < 0) return null;
  const field = decodeURIComponent(body.slice(0, separator));
  const value = decodeURIComponent(body.slice(separator + 1)).trim();
  if (!field || !value) return null;
  return { field, value };
}

function matchesColumnFilterToken(token: string, valueForField: (field: string) => string) {
  const parsed = parseColumnFilterToken(token);
  if (!parsed) return false;
  return valueForField(parsed.field).toLowerCase().includes(parsed.value.toLowerCase());
}

function createUuid() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, char => {
    const random = Math.floor(Math.random() * 16);
    const value = char === 'x' ? random : (random & 0x3) | 0x8;
    return value.toString(16);
  });
}

function ListToolbarControls(props: {
  query: string;
  setQuery: (value: string) => void;
  queryField: string;
  setQueryField: (value: string) => void;
  queryFields: ListToolbarChoice[];
  tags: ListToolbarChoice[];
  activeTags: string[];
  setActiveTags: React.Dispatch<React.SetStateAction<string[]>>;
  filters: ListToolbarChoice[];
  activeFilters: string[];
  setActiveFilters: React.Dispatch<React.SetStateAction<string[]>>;
  columns: ListToolbarColumn[];
  visibleColumns: string[];
  setVisibleColumns: React.Dispatch<React.SetStateAction<string[]>>;
  onRefresh: () => void;
}) {
  const {
    query,
    setQuery,
    queryField,
    setQueryField,
    queryFields,
    tags,
    activeTags,
    setActiveTags,
    activeFilters,
    setActiveFilters,
    columns,
    visibleColumns,
    setVisibleColumns,
    onRefresh,
  } = props;
  const [openPanel, setOpenPanel] = useState<'tags' | 'filters' | 'columns' | null>(null);
  const [draftColumnFilters, setDraftColumnFilters] = useState<Record<string, string>>({});
  const activeBadgeCount = activeTags.length + activeFilters.length;
  const resetListTools = () => {
    setQuery('');
    setActiveTags([]);
    setActiveFilters([]);
  };

  useEffect(() => {
    if (queryFields.length > 0 && !queryFields.some(field => field.value === queryField)) {
      setQueryField(queryFields[0].value);
    }
  }, [queryField, queryFields, setQueryField]);

  useEffect(() => {
    if (openPanel !== 'filters') return;
    const nextColumnFilters: Record<string, string> = {};
    activeFilters.forEach(filter => {
      const parsed = parseColumnFilterToken(filter);
      if (parsed) {
        nextColumnFilters[parsed.field] = parsed.value;
      }
    });
    setDraftColumnFilters(nextColumnFilters);
  }, [activeFilters, openPanel]);

  const drawerFields = queryFields;
  const activeDraftCount = Object.values(draftColumnFilters).filter(value => value.trim()).length;
  const updateDraftColumnFilter = (field: string, value: string) => setDraftColumnFilters(prev => ({ ...prev, [field]: value }));
  const clearDraftColumnFilter = (field: string) => {
    setDraftColumnFilters(prev => {
      const next = { ...prev };
      delete next[field];
      return next;
    });
  };
  const submitAdvancedFilters = () => {
    const columnTokens = queryFields.flatMap(field => {
      const value = (draftColumnFilters[field.value] || '').trim();
      return value ? [makeColumnFilterToken(field.value, value)] : [];
    });
    setActiveFilters(columnTokens);
    setOpenPanel(null);
  };
  const resetAdvancedFilters = () => {
    setDraftColumnFilters({});
    setActiveFilters([]);
  };

  return (
    <div className="hbdr-dr-query-group">
      <select aria-label="Query Field" value={queryField} onChange={event => setQueryField(event.target.value)}>
        {queryFields.map(field => <option key={field.value} value={field.value}>{field.label}</option>)}
      </select>
      <label className="hbdr-dr-search"><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Enter search text" /></label>
      <button
        type="button"
        onClick={() => {
          resetListTools();
          onRefresh();
        }}
        title="Refresh"
      >
        <RefreshCw size={18} />
      </button>
      <div className="hbdr-list-tool">
        <button type="button" title="Tags" className={activeTags.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'tags' ? null : 'tags')}>
          <Archive size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'tags' && (
            <>
              <div className="hbdr-list-tool-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.div initial={{ opacity: 0, y: 8, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.98 }} className="hbdr-list-tool-popover">
                <div className="hbdr-list-tool-head">
                  <strong>Tags</strong>
                  <button type="button" onClick={() => setActiveTags([])}>Clear</button>
                </div>
                <div className="hbdr-list-tool-options">
                  {tags.map(tag => (
                    <button key={tag.value} type="button" className={activeTags.includes(tag.value) ? 'is-selected' : ''} onClick={() => setActiveTags(prev => toggleListValue(prev, tag.value))}>
                      <span>{tag.label}</span>
                      {typeof tag.count === 'number' && <em>{tag.count}</em>}
                    </button>
                  ))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
      <div className="hbdr-list-tool">
        <button type="button" title="Filter" className={activeFilters.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'filters' ? null : 'filters')}>
          <Filter size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'filters' && (
            <>
              <div className="hbdr-filter-drawer-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.aside
                initial={{ opacity: 0, x: 24 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 24 }}
                className="hbdr-filter-drawer"
              >
                <div className="hbdr-filter-drawer-head">
                  <strong>Advanced Filter</strong>
                </div>
                <div className="hbdr-filter-drawer-body">
                  <section className="hbdr-advanced-filter-section">
                    <h4><Filter size={15} />Filter Criteria</h4>
                    <div className="hbdr-advanced-filter-box">
                      {drawerFields.map(field => (
                        <div key={field.value} className="hbdr-advanced-filter-row">
                          <label>{field.label}</label>
                          <input value={draftColumnFilters[field.value] || ''} onChange={event => updateDraftColumnFilter(field.value, event.target.value)} placeholder="Please Enter" />
                          <button type="button" onClick={() => clearDraftColumnFilter(field.value)} title="Clear">
                            <X size={13} />
                          </button>
                        </div>
                      ))}
                    </div>
                  </section>
                </div>
                <div className="hbdr-filter-drawer-actions">
                  <button type="button" onClick={submitAdvancedFilters}>Submit</button>
                  <button type="button" onClick={resetAdvancedFilters}>Reset</button>
                  <button type="button" onClick={() => setOpenPanel(null)}>Cancel</button>
                  {activeDraftCount > 0 && <span>{activeDraftCount} criteria</span>}
                </div>
              </motion.aside>
            </>
          )}
        </AnimatePresence>
      </div>
      <div className="hbdr-list-tool">
        <button type="button" title="Column Settings" className={visibleColumns.length < columns.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'columns' ? null : 'columns')}>
          <Settings size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'columns' && (
            <>
              <div className="hbdr-list-tool-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.div initial={{ opacity: 0, y: 8, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.98 }} className="hbdr-list-tool-popover hbdr-list-tool-popover-wide">
                <div className="hbdr-list-tool-head">
                  <strong>Columns</strong>
                  <button type="button" onClick={() => setVisibleColumns(columns.map(column => column.value))}>Reset</button>
                </div>
                <div className="hbdr-list-tool-options">
                  {columns.map(column => (
                    <button
                      key={column.value}
                      type="button"
                      disabled={column.locked}
                      className={visibleColumns.includes(column.value) ? 'is-selected' : ''}
                      onClick={() => {
                        if (column.locked) return;
                        setVisibleColumns(prev => prev.includes(column.value) ? prev.filter(item => item !== column.value) : [...prev, column.value]);
                      }}
                    >
                      <span>{column.label}</span>
                      {column.locked ? <em>Fixed</em> : visibleColumns.includes(column.value) ? <Check size={13} /> : null}
                    </button>
                  ))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
      {activeBadgeCount > 0 && <button type="button" className="hbdr-list-tool-reset" onClick={resetListTools}>Clear {activeBadgeCount}</button>}
    </div>
  );
}

export default function App() {
  const [authSession, setAuthSession] = useState<AuthSession | null>(() => readStoredAuthSession());
  const [view, setView] = useState<View>(() => readStoredAuthSession() ? (readStoredView() || 'dashboard') : 'login');
  const [timeZoneLabel] = useState(() => currentTimeZoneLabel());
  const [loginEmail, setLoginEmail] = useState('admin');
  const [loginPassword, setLoginPassword] = useState('');
  const [loginCaptchaCode, setLoginCaptchaCode] = useState('');
  const [loginCaptcha, setLoginCaptcha] = useState<ApiCaptcha | null>(null);
  const [loginError, setLoginError] = useState('');
  const [loginSubmitting, setLoginSubmitting] = useState(false);
  const [locale, setLocale] = useState<LocaleCode>('en');
  const [clusters, setClusters] = useState<Cluster[]>(initialClusters);
  const [liveClusters, setLiveClusters] = useState<Cluster[] | null>(null);
  const [storage, setStorage] = useState<StorageRepo[]>(initialStorage);
  const [liveStorage, setLiveStorage] = useState<StorageRepo[] | null>(null);
  const [policies, setPolicies] = useState<PolicyItem[]>(initialPolicies);
  const [livePolicies, setLivePolicies] = useState<PolicyItem[] | null>(null);
  const [tags, setTags] = useState<TagItem[]>(initialTags);
  const [restorePointCount, setRestorePointCount] = useState(0);
  const [liveRestorePoints, setLiveRestorePoints] = useState<ApiRestorePointView[]>([]);
  const [liveApiClusters, setLiveApiClusters] = useState<ApiCluster[]>([]);
  const [liveApiStorageRepos, setLiveApiStorageRepos] = useState<ApiStorageRepo[]>([]);
  const [liveApiTasks, setLiveApiTasks] = useState<ApiTask[]>([]);
  const [liveApiRestorePointViews, setLiveApiRestorePointViews] = useState<ApiRestorePointView[]>([]);
  const [liveApiRestorePoints, setLiveApiRestorePoints] = useState<ApiRestorePoint[]>([]);
  const [liveApiPolicies, setLiveApiPolicies] = useState<ApiPolicy[]>([]);
  const [liveApiPlans, setLiveApiPlans] = useState<ApiProtectionPlan[]>([]);
  const [liveApiApps, setLiveApiApps] = useState<ApiApplication[]>([]);
  const [liveAppTasks, setLiveAppTasks] = useState<Record<string, ApiTask>>({});
  const [liveRecoveryTasks, setLiveRecoveryTasks] = useState<Record<string, ApiTask>>({});
  const [restorePointNamespaceFilter, setRestorePointNamespaceFilter] = useState<string[]>([]);
  const [secondaryCollapsed, setSecondaryCollapsed] = useState(false);
  const [selectedCluster, setSelectedCluster] = useState<Cluster | null>(null);
  const [defaultClusterId, setDefaultClusterId] = useState<string | null>(() => {
    try {
      return localStorage.getItem(DEFAULT_CLUSTER_KEY) || null;
    } catch {
      return null;
    }
  });
  const [clusterPickerOpen, setClusterPickerOpen] = useState(false);
  const [clusterMenuId, setClusterMenuId] = useState<string | null>(null);
  const prefetchedAgentTokenRef = useRef<ApiAgentToken | null>(null);
  const prefetchingAgentTokenRef = useRef<Promise<ApiAgentToken | null> | null>(null);

  const [appStage, setAppStage] = useState<'select' | 'config' | 'run'>('select');
  const [search, setSearch] = useState('');
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 2600);
    return () => window.clearTimeout(timer);
  }, [toast]);

  useEffect(() => {
    if (!selectedCluster?.id) return;
    try {
      localStorage.setItem(SELECTED_CLUSTER_KEY, selectedCluster.id);
    } catch {
      // Keep the current in-memory selection if localStorage is unavailable.
    }
  }, [selectedCluster?.id]);

  const requestAgentToken = useCallback(async () => apiPost<ApiAgentToken>('/api/v1/agent-tokens', {
    description: 'cluster registration from console',
    ttlSeconds: 1800,
  }), []);

  const prefetchAgentToken = useCallback(() => {
    if (isAgentTokenUsable(prefetchedAgentTokenRef.current) || prefetchingAgentTokenRef.current) return prefetchingAgentTokenRef.current;
    prefetchedAgentTokenRef.current = null;
    const request = requestAgentToken()
      .then(token => {
        prefetchedAgentTokenRef.current = token;
        return token;
      })
      .catch(() => null)
      .finally(() => {
        prefetchingAgentTokenRef.current = null;
      });
    prefetchingAgentTokenRef.current = request;
    return request;
  }, [requestAgentToken]);

  const takePrefetchedAgentToken = useCallback(() => {
    const token = prefetchedAgentTokenRef.current;
    prefetchedAgentTokenRef.current = null;
    return isAgentTokenUsable(token) ? token : null;
  }, []);

  const getAgentTokenForRegistration = useCallback(async () => {
    const prefetched = takePrefetchedAgentToken();
    if (prefetched) {
      void prefetchAgentToken();
      return prefetched;
    }
    const token = await (prefetchingAgentTokenRef.current || requestAgentToken());
    if (!isAgentTokenUsable(token)) throw new Error('agent token is unavailable');
    void prefetchAgentToken();
    return token;
  }, [prefetchAgentToken, requestAgentToken, takePrefetchedAgentToken]);

  useEffect(() => {
    if (!authSession || view === 'login') return;
    void prefetchAgentToken();
  }, [authSession, prefetchAgentToken, view]);

  const refreshLoginCaptcha = useCallback(async (clearError = true) => {
    try {
      const captcha = await apiGet<ApiCaptcha>('/api/v1/auth/captcha');
      setLoginCaptcha(captcha);
      setLoginCaptchaCode('');
      if (clearError) setLoginError('');
    } catch {
      setLoginCaptcha(null);
      setLoginError('Verification code failed to load');
    }
  }, []);

  useEffect(() => {
    if (view !== 'login' || authSession) return;
    void refreshLoginCaptcha();
  }, [authSession, refreshLoginCaptcha, view]);

  const enterAfterLogin = useCallback(() => {
    const liveList = liveClusters ?? clusters;
    const hasLive = liveClusters !== null;
    const needsOnboarding = hasLive && (liveList.length === 0 || !liveList.some(cluster => cluster.isDefault));
    if (needsOnboarding) {
      writeStoredView('clusters');
      setView('clusters');
    } else {
      const target = liveList.find(cluster => cluster.id === defaultClusterId || cluster.isDefault) || liveList[0] || clusters[0] || null;
      setSelectedCluster(target);
      try {
        if (target?.id) localStorage.setItem(SELECTED_CLUSTER_KEY, target.id);
      } catch {
        // localStorage can be blocked in embedded browsers.
      }
      writeStoredView('dashboard');
      setView('dashboard');
    }
  }, [clusters, defaultClusterId, liveClusters]);

  useEffect(() => {
    if (!authSession) return;
    const expiresAt = Date.parse(authSession.session.expiresAt);
    const delay = expiresAt - Date.now();
    if (delay <= 0) {
      clearStoredAuthSession();
      setAuthSession(null);
      setView('login');
      setLoginError('Your session has expired. Sign in again.');
      return;
    }
    const timer = window.setTimeout(() => {
      clearStoredAuthSession();
      setAuthSession(null);
      setView('login');
      setLoginError('Your session has expired. Sign in again.');
    }, Math.min(delay, 2147483647));
    return () => window.clearTimeout(timer);
  }, [authSession]);

  const submitLogin = useCallback(async () => {
    if (loginSubmitting) return;
    if (!loginEmail.trim() || !loginPassword || !loginCaptchaCode.trim()) {
      setLoginError('Email, password, and verification code are required');
      return;
    }
    if (!loginCaptcha?.id) {
      setLoginError('Verification code failed to load');
      return;
    }
    setLoginSubmitting(true);
    setLoginError('');
    try {
      const response = await apiPost<ApiLoginResponse>('/api/v1/auth/login', {
        email: loginEmail.trim(),
        password: loginPassword,
        captchaId: loginCaptcha.id,
        captchaCode: loginCaptchaCode.trim(),
      });
      const nextSession: AuthSession = {
        ...response,
        signedInAt: new Date().toISOString(),
      };
      setAuthSession(nextSession);
      writeStoredAuthSession(nextSession);
      clearStoredView();
      enterAfterLogin();
    } catch (error) {
      setLoginError(error instanceof Error ? error.message : 'Login failed');
      await refreshLoginCaptcha(false);
    } finally {
      setLoginSubmitting(false);
    }
  }, [enterAfterLogin, loginCaptcha?.id, loginCaptchaCode, loginEmail, loginPassword, loginSubmitting, refreshLoginCaptcha]);

  const signOut = useCallback(() => {
    setAuthSession(null);
    clearStoredAuthSession();
    clearStoredView();
    setLoginPassword('');
    setLoginCaptchaCode('');
    setLoginError('');
    setView('login');
  }, []);

  const [clusterTaskLogs, setClusterTaskLogs] = useState<Record<string, ClusterTaskLog[]>>({});
  const [activeClusterTaskIds, setActiveClusterTaskIds] = useState<Set<string>>(new Set());
  const clusterTaskLogsRef = useRef(clusterTaskLogs);
  clusterTaskLogsRef.current = clusterTaskLogs;
  const refreshInFlightRef = useRef<Promise<Cluster[]> | null>(null);
  const refreshLastStartedAtRef = useRef(0);
  const refreshLastResultRef = useRef<Cluster[]>([]);

  useEffect(() => {
    let cancelled = false;
    const loadTasks = async () => {
      try {
        const res = await apiGet<ApiList<ApiTask>>('/api/v1/tasks');
        const tasks = listItems(res);
        const clusterTasks = tasks
          .filter(task => task.type === 'register' || task.type === 'unregister')
          .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
        const visibleClusterTasks = clusterTasks.filter(task => isActiveTaskStatus(task.status)).concat(clusterTasks.filter(task => !isActiveTaskStatus(task.status)).slice(0, 8));
        if (cancelled) return;
        setClusterTaskLogs(prev => {
          const next: Record<string, ClusterTaskLog[]> = {};
          for (const task of visibleClusterTasks) {
            const key = String(task.clusterId || task.payload?.archivedClusterId || task.payload?.clusterId || 'platform');
            if (!next[key]) next[key] = [];
            const existing = prev[key]?.find(log => log.task.id === task.id);
            next[key].push({
              task,
              events: existing?.events ?? [],
              loading: !existing,
            });
          }
          return next;
        });
        setActiveClusterTaskIds(prev => {
          const next = new Set<string>();
          for (const task of clusterTasks) {
            if (isActiveTaskStatus(task.status)) {
              next.add(task.id);
            }
          }
          if (prev.size === next.size && Array.from(prev).every(id => next.has(id))) return prev;
          return next;
        });
      } catch {
        // Keep current state if request fails.
      }
    };
    loadTasks();
    const timer = window.setInterval(loadTasks, 10000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    const ids = Array.from(activeClusterTaskIds);
    if (ids.length === 0) return;
    let cancelled = false;
    const loadEvents = async () => {
      for (const taskId of ids) {
        try {
          const res = await apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${taskId}/events`);
          if (cancelled) return;
          const events = listItems(res);
          setClusterTaskLogs(prev => {
            const next: Record<string, ClusterTaskLog[]> = { ...prev };
            for (const key of Object.keys(next)) {
              next[key] = next[key].map(log => log.task.id === taskId ? { ...log, events, loading: false } : log);
            }
            return next;
          });
        } catch {
          // ignore
        }
      }
    };
    loadEvents();
    const timer = window.setInterval(loadEvents, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeClusterTaskIds]);

  const refreshPlatformData = useCallback(() => {
    const now = Date.now();
    if (refreshInFlightRef.current) return refreshInFlightRef.current;
    if (refreshLastResultRef.current.length > 0 && now - refreshLastStartedAtRef.current < 1200) {
      return Promise.resolve(refreshLastResultRef.current);
    }
    refreshLastStartedAtRef.current = now;
    const request = (async () => {
      const [clusterRes, appRes, storageRes, policyRes, planRes, taskRes] = await Promise.all([
        apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
        apiGet<ApiList<ApiApplication>>('/api/v1/applications'),
        apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
        apiGet<ApiList<ApiPolicy>>('/api/v1/policies'),
        apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
        apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
      ]);
      const restorePointRes = await apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points');
      const apiClusters = listItems(clusterRes);
      const apiApps = listItems(appRes);
      const apiStorage = listItems(storageRes);
      const apiPolicies = listItems(policyRes);
      const apiPlans = listItems(planRes);
      const apiRestorePoints = listItems(restorePointRes).map(mapRestorePoint);
      const apiTasks = listItems(taskRes);
      const nextAppTasks = buildAppTaskMap(apiTasks, apiApps, ['backup'], apiRestorePoints, apiPlans);
      const nextRecoveryTasks = buildAppTaskMap(apiTasks, apiApps, ['restore', 'drill', 'takeover'], apiRestorePoints, apiPlans);
      const nextStorage = apiStorage.map(mapStorageRepo);
      const nextPolicies = apiPolicies.map(mapPolicy);
      const nextClusters = apiClusters.map(cluster => {
        const apps = mapApps(
          apiApps.filter(app => app.clusterId === cluster.id),
          apiPlans,
          apiPolicies,
          apiStorage,
          apiClusters,
        );
        return mapCluster(cluster, apps);
      });
      refreshLastResultRef.current = nextClusters;
      setLiveClusters(nextClusters);
      setLiveStorage(nextStorage);
      setLivePolicies(nextPolicies.length > 0 ? nextPolicies : null);
      setLiveApiClusters(apiClusters);
      setLiveApiStorageRepos(apiStorage);
      if (!USE_PROTOTYPE_VISUAL_DATA) {
        setClusters(nextClusters);
        setStorage(nextStorage);
        setPolicies(nextPolicies);
        setRestorePointCount(apiRestorePoints.length);
        setLiveRestorePoints(apiRestorePoints);
        setLiveApiTasks(apiTasks);
        setLiveApiRestorePointViews(apiRestorePoints);
        setLiveApiRestorePoints(listItems(restorePointRes));
        setLiveApiPolicies(apiPolicies);
        setLiveApiPlans(apiPlans);
        setLiveApiApps(apiApps);
        setLiveAppTasks(nextAppTasks);
        setLiveRecoveryTasks(nextRecoveryTasks);
        setSelectedCluster(prev => {
          if (prev && nextClusters.some(cluster => cluster.id === prev.id)) {
            return nextClusters.find(cluster => cluster.id === prev.id) || nextClusters[0] || null;
          }
          let storedSelectedId = '';
          let storedDefaultId = '';
          try {
            storedSelectedId = localStorage.getItem(SELECTED_CLUSTER_KEY) || '';
            storedDefaultId = localStorage.getItem(DEFAULT_CLUSTER_KEY) || '';
          } catch {
            // localStorage may be unavailable in private contexts.
          }
          const apiDefault = nextClusters.find(cluster => cluster.isDefault);
          return nextClusters.find(cluster => cluster.id === storedSelectedId)
            || nextClusters.find(cluster => cluster.id === storedDefaultId)
            || apiDefault
            || nextClusters[0]
            || null;
        });
        setDefaultClusterId(prev => {
          const apiDefault = nextClusters.find(cluster => cluster.isDefault);
          const nextDefaultId = apiDefault?.id || (nextClusters.some(cluster => cluster.id === prev) ? prev : nextClusters[0]?.id || null);
          try {
            if (nextDefaultId) localStorage.setItem(DEFAULT_CLUSTER_KEY, nextDefaultId);
            else localStorage.removeItem(DEFAULT_CLUSTER_KEY);
          } catch {
            // localStorage may be unavailable in private contexts.
          }
          return nextDefaultId;
        });
      }
      return nextClusters;
    })().finally(() => {
      refreshInFlightRef.current = null;
    });
    refreshInFlightRef.current = request;
    return request;
  }, []);

  useEffect(() => {
    let cancelled = false;
    const realtimeViews = new Set<View>(['dashboard', 'applications', 'clusters', 'alerts']);
    const loadPlatformData = async () => {
      try {
        await refreshPlatformData();
      } catch {
        if (!cancelled) {
          // Keep the current state visible if the backend is temporarily unavailable.
        }
      }
    };
    loadPlatformData();
    const shouldPoll = authSession && realtimeViews.has(view);
    const pollIntervalMs = view === 'applications' ? 3000 : 10000;
    const timer = shouldPoll ? window.setInterval(loadPlatformData, pollIntervalMs) : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [authSession, refreshPlatformData, view]);

  const activeModule = moduleForView(view);
  const language = locales[locale];
  const defaultCluster = useMemo(
    () => clusters.find(cluster => cluster.isDefault) || clusters.find(cluster => cluster.id === defaultClusterId) || null,
    [clusters, defaultClusterId],
  );
  const defaultWorkspaceCluster = defaultCluster || clusters[0] || null;
  const workspaceCluster = selectedCluster || defaultWorkspaceCluster;
  const dashboardCluster = workspaceCluster;
  const dashboardApps = dashboardCluster?.apps || [];
  const protectedApps = dashboardApps.filter(app => app.isProtected).length;
  const activeRestorePointCount = dashboardCluster ? restorePointCount : 0;
  const drClusters = liveClusters ?? clusters;
  const drSelectedCluster = selectedCluster ? drClusters.find(cluster => cluster.id === selectedCluster.id) || null : null;
  const drDefaultCluster = drClusters.find(cluster => cluster.isDefault) || drClusters.find(cluster => cluster.id === defaultClusterId) || drClusters[0] || null;
  const drWorkspaceCluster = liveClusters
    ? (drSelectedCluster || drDefaultCluster)
    : workspaceCluster;
  const drActiveApps = drWorkspaceCluster?.apps || [];
  const drStorage = liveStorage ?? storage;
  const drPolicies = livePolicies ?? policies;
  const onboarding: 'register' | 'default' | 'ready' | 'loading' = liveClusters === null
    ? 'loading'
    : liveClusters.length === 0
      ? 'register'
      : (liveClusters.some(cluster => cluster.isDefault) ? 'ready' : 'default');
  const onboardingMessage = onboarding === 'register'
    ? 'Register a cluster before using this section'
    : onboarding === 'default'
      ? 'Set a default cluster before using this section'
      : '';
  const navBlockedViews: Set<View> = new Set<View>([
    'applications', 'failback', 'storage', 'policies', 'restore_points', 'dr_tasks', 'operations', 'tags',
  ]);


  const secondaryNav = useMemo(() => {
    if (activeModule === 'overview') return null;
    if (activeModule === 'dr') {
      return {
        title: 'DR',
        items: [
          { label: 'Application DR', desc: 'Select applications, configure policies, and start DR', view: 'applications' as View, icon: Layers },
          { label: 'Restore Points', desc: 'Browse recovery points and launch drill or takeover', view: 'restore_points' as View, icon: Clock },
          { label: 'Backup & Recovery Tasks', desc: 'Audit backup, drill, takeover, and restore task records', view: 'dr_tasks' as View, icon: History },
        ],
      };
    }
    if (activeModule === 'config') {
      return {
        title: 'Configuration',
        items: [
          { label: 'Clusters', desc: 'Registration, default cluster, and agent status', view: 'clusters' as View, icon: Server },
          { label: 'Storage', desc: 'Maintain shared restore-point repositories across clusters', view: 'storage' as View, icon: Database },
          { label: 'Policies', desc: 'Maintain application protection plans and recovery targets', view: 'policies' as View, icon: ShieldCheck },
        ],
      };
    }
    if (activeModule === 'ops') {
      return {
        title: 'Operations',
        items: [
          { label: 'History', desc: 'Backup, restore, and takeover audit', view: 'operations' as View, icon: History },
          { label: 'Tag Management', desc: 'Create tags and reuse them in DR lists', view: 'tags' as View, icon: Archive },
        ],
      };
    }
    if (activeModule === 'monitor') {
      return {
        title: 'Monitor & Alerts',
        items: [{ label: 'Alerts', desc: 'Cluster DR risks and event alerts', view: 'alerts' as View, icon: Bell }],
      };
    }
    return {
      title: 'Settings',
      items: [
        { label: 'System', desc: 'Platform parameters and security policies', view: 'settings' as View, icon: Settings },
        { label: 'Tenants', desc: 'Tenants and administrator accounts', view: 'tenants' as View, icon: User },
      ],
    };
  }, [activeModule]);

  const openView = (nextView: View, options: { preserveSelectedCluster?: boolean } = {}) => {
    if (!options.preserveSelectedCluster && (nextView === 'dashboard' || nextView === 'applications' || nextView === 'failback')) {
      const target = defaultWorkspaceCluster;
      if (target) setSelectedCluster(target);
    }
    writeStoredView(nextView);
    setView(nextView);
    if (nextView === 'applications') {
      setAppStage('select');
    }
    setSearch('');
    setClusterMenuId(null);
  };

  const setDefaultCluster = async (cluster: Cluster, event?: React.MouseEvent) => {
    event?.stopPropagation();
    try {
      await apiPost<ApiCluster>(`/api/v1/clusters/${cluster.id}/default`, {});
    } catch {
      setToast('Failed to persist default cluster, using local selection for now');
    }
    setDefaultClusterId(cluster.id);
    setSelectedCluster(cluster);
    try {
      localStorage.setItem(DEFAULT_CLUSTER_KEY, cluster.id);
      localStorage.setItem(SELECTED_CLUSTER_KEY, cluster.id);
    } catch {
      // localStorage can be blocked in embedded browsers.
    }
  };

  const clearDefaultCluster = async (event?: React.MouseEvent) => {
    event?.stopPropagation();
    if (defaultClusterId) {
      try {
        await apiPatch<ApiCluster>(`/api/v1/clusters/${defaultClusterId}`, { isDefault: false });
      } catch {
        setToast('Failed to clear default cluster from platform API');
      }
    }
    setDefaultClusterId(null);
    try {
      localStorage.removeItem(DEFAULT_CLUSTER_KEY);
    } catch {
      // localStorage can be blocked in embedded browsers.
    }
  };

  const unregisterCluster = async (cluster: Cluster, event?: React.MouseEvent): Promise<ApiTask | null> => {
    event?.stopPropagation();
    let result: ApiTaskResponse;
    try {
      result = await apiPost<ApiTaskResponse>(`/api/v1/clusters/${cluster.id}/unregister`, {
        deleteVelero: true,
        deleteNamespace: true,
        reason: 'requested from platform cluster page',
      });
    } catch {
      setToast('Failed to create unregister task from platform API');
      return null;
    }
    const task = 'task' in result ? result.task : result;
    const warning = 'warning' in result ? result.warning : undefined;
    setToast(warning || `${cluster.name} unregister task dispatched`);
    return task;
  };

  const updateAppTags = (clusterId: string | null, appNames: string[], updater: (currentTags: string[]) => string[]) => {
    if (!clusterId || appNames.length === 0) return;
    const patchCluster = (cluster: Cluster) => {
      if (cluster.id !== clusterId) return cluster;
      return {
        ...cluster,
        apps: cluster.apps.map(app => {
          if (!appNames.includes(app.name)) return app;
          return { ...app, tags: updater(app.tags || []) };
        }),
      };
    };
    setClusters(prev => prev.map(patchCluster));
    setLiveClusters(prev => prev ? prev.map(patchCluster) : prev);
    setSelectedCluster(prev => prev ? patchCluster(prev) : prev);
  };

  const onboardingGate = (
    <div className="p-6">
      <OnboardingGate onboarding={onboarding} openClusters={() => openView('clusters')} />
    </div>
  );

  if (view === 'login') {
    return (
      <div className="premium-login min-h-screen">
        <div className="relative flex min-h-screen items-center justify-center px-10">
          <div className="w-full max-w-md">
            <div className="mb-8">
              <div className="hbdr-login-top-brand" aria-label="HyperCDR">
                <span className="hbdr-login-brand-one">Hyper</span>
                <span className="hbdr-login-brand-pro">CDR</span>
              </div>
            </div>

            <div className="hbdr-login-hero">
              <div className="hbdr-login-hero-icon">
                <svg viewBox="0 0 128 128" aria-hidden="true" className="hypercdr-mark">
                  <defs>
                    <linearGradient id="hcdr-mark-frame" x1="24" y1="28" x2="104" y2="100" gradientUnits="userSpaceOnUse">
                      <stop offset="0" stopColor="#ffcf3d" />
                      <stop offset="0.48" stopColor="#67e8f9" />
                      <stop offset="1" stopColor="#6d7cff" />
                    </linearGradient>
                    <linearGradient id="hcdr-mark-core" x1="42" y1="42" x2="88" y2="88" gradientUnits="userSpaceOnUse">
                      <stop offset="0" stopColor="#67e8f9" />
                      <stop offset="1" stopColor="#ffcf3d" />
                    </linearGradient>
                    <filter id="hcdr-mark-soft-glow" x="-25%" y="-25%" width="150%" height="150%">
                      <feGaussianBlur stdDeviation="4" result="blur" />
                      <feColorMatrix in="blur" type="matrix" values="0 0 0 0 0.35 0 0 0 0 0.68 0 0 0 0 1 0 0 0 0.50 0" />
                      <feMerge>
                        <feMergeNode />
                        <feMergeNode in="SourceGraphic" />
                      </feMerge>
                    </filter>
                  </defs>
                  <path d="M64 18 101 39v50l-37 21-37-21V39l37-21Z" fill="rgba(7,10,18,0.84)" stroke="url(#hcdr-mark-frame)" strokeWidth="5" filter="url(#hcdr-mark-soft-glow)" />
                  <rect x="47" y="45" width="15" height="15" rx="4" fill="#67e8f9" />
                  <rect x="66" y="45" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
                  <rect x="47" y="64" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
                  <rect x="66" y="64" width="15" height="15" rx="4" fill="#ffcf3d" />
                </svg>
              </div>
              <div className="hbdr-login-hero-copy">
                <div className="hbdr-login-hero-title"><span>Hyper</span>CDR</div>
                <div className="hbdr-login-hero-subtitle">Container Disaster Recovery Platform</div>
                <div className="hbdr-login-hero-tagline">RECOVER FASTER: KEEP APPLICATIONS AVAILABLE</div>
              </div>
            </div>

            <div className="premium-login-card">
              <h2>
                <span>Welcome to HyperCDR</span>
                <LanguageSwitcher locale={locale} setLocale={setLocale} compact />
              </h2>
              <div className="hbdr-login-form grid gap-4">
                <label className="hbdr-login-field">
                  <User size={15} />
                  <input
                    placeholder="Email Address"
                    value={loginEmail}
                    onChange={event => setLoginEmail(event.target.value)}
                    autoComplete="username"
                  />
                </label>
                <label className="hbdr-login-field">
                  <ShieldCheck size={15} />
                  <input
                    placeholder="Password"
                    type="password"
                    value={loginPassword}
                    onChange={event => setLoginPassword(event.target.value)}
                    onKeyDown={event => {
                      if (event.key === 'Enter') void submitLogin();
                    }}
                    autoComplete="current-password"
                  />
                  <Eye size={15} className="hbdr-login-eye" />
                </label>
              </div>
              <div className="hbdr-login-captcha-code" aria-label="Verification code">
                <label className="hbdr-login-field">
                  <CheckCircle2 size={15} />
                  <input
                    placeholder="Verification Code"
                    value={loginCaptchaCode}
                    onChange={event => setLoginCaptchaCode(event.target.value.replace(/\D/g, '').slice(0, 4))}
                    onKeyDown={event => {
                      if (event.key === 'Enter') void submitLogin();
                    }}
                    inputMode="numeric"
                    autoComplete="off"
                  />
                </label>
                <button type="button" className="hbdr-login-captcha-image" aria-label="Refresh verification code" title="Refresh verification code" onClick={() => void refreshLoginCaptcha()}>
                  {loginCaptcha?.image ? <img src={loginCaptcha.image} alt="Verification code" /> : <span>----</span>}
                </button>
              </div>
              {loginError && <div className="hbdr-login-error">{loginError}</div>}
              <button
                className="w-full bg-blue-600 text-white"
                disabled={loginSubmitting}
                onClick={() => void submitLogin()}
              >
                {loginSubmitting ? 'Signing In...' : 'Sign In'}
              </button>
              <div className="hbdr-login-forgot"><button type="button">Forgot Password?</button></div>
              <div className="hbdr-login-divider"><span>or</span></div>
              <button type="button" className="hbdr-login-google">
                <span>G</span>
                Continue with Google
              </button>
              <p className="hbdr-login-sso-note">Use your work Google account</p>
              <div className="hbdr-login-signup">Do not have an account? <button type="button">Sign Up</button></div>
              <p className="hbdr-login-eula">By continuing, you agree to our Terms and Privacy Policy.</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (false && view === 'login') {
    return (
      <div className="premium-login min-h-screen">
        <div className="relative flex min-h-screen items-center justify-center px-10">
          <div className="w-full max-w-md">
            <div className="login-brand-mark">
              <div className="login-brand-icon"><ShieldCheck size={58} /></div>
              <div>
                <h1>HyperCDR</h1>
                <p>Cloud native, easy to deploy, accessible</p>
                <span>Build a new container DR experience</span>
              </div>
            </div>
            <div className="login-card">
              <h2>Welcome to HyperCDR</h2>
              <div className="space-y-3">
                <input placeholder="Enter tenant domain" />
                <input placeholder="Enter username" />
                <input placeholder="Enter password" type="password" />
                <div className="grid grid-cols-[1fr_104px] gap-2">
                  <input placeholder="Enter verification code" />
                  <div className="captcha-box">227G</div>
                </div>
              </div>
              <button
                onClick={() => {
                  const target = defaultCluster || clusters[0] || null;
                  setSelectedCluster(target);
                  setView('dashboard');
                }}
              >
                Log In
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="premium-app hbdr-workspace min-h-screen flex bg-slate-50 text-slate-900">
      <aside className="premium-sidebar w-64 bg-white border-r border-slate-200 flex flex-col z-20">
        <div>
          <div>
            <div><ShieldCheck size={22} /></div>
            <h1>HyperCDR</h1>
          </div>
          <nav>
            {topNav.map(item => {
              const requiresOnboarding = ['dr', 'ops', 'config'].includes(item.key) && onboarding !== 'ready';
              const isConfig = item.key === 'config';
              const showBadge = isConfig && onboarding === 'register';
              const showCounter = isConfig && onboarding === 'default';
              return (
                <button
                  key={item.key}
                  onClick={() => openView(item.view)}
                  disabled={false}
                  className={activeModule === item.key ? 'bg-blue-50 text-blue-700' : ''}
                >
                  <span className="flex items-center gap-2">
                    <span>{language.topNav[item.key]}</span>
                    {showBadge && (
                      <span className="ml-1 rounded-full bg-rose-500 px-1.5 py-0.5 text-[9px] font-black uppercase tracking-widest text-white">Setup</span>
                    )}
                    {showCounter && (
                      <span className="ml-1 flex h-4 min-w-[16px] items-center justify-center rounded-full bg-amber-500 px-1 text-[10px] font-black leading-none text-white">1</span>
                    )}
                  </span>
                </button>
              );
            })}
          </nav>
        </div>
        <div>
          <button className="hbdr-timezone-button">{timeZoneLabel}</button>
          <button className="hbdr-top-status hbdr-top-status-upgrade" onClick={() => setToast('No upgrade tasks are pending')}>
            <ArrowUp size={16} />
            <span>0</span>
          </button>
          <button className="hbdr-top-status hbdr-top-status-alert" onClick={() => setToast('No new alerts in Notification Center')}>
            <AlertCircle size={16} />
            <span>0</span>
          </button>
          <LanguageSwitcher locale={locale} setLocale={setLocale} compact />
          <button type="button" className="hbdr-top-user" onClick={signOut} title="Sign out">
            <User size={15} />
            <span>{authSession?.user.email || 'admin'}</span>
            <ChevronDown size={13} />
          </button>
          <button type="button" className="hbdr-top-avatar" onClick={signOut} title="Sign out"><User size={20} /></button>
        </div>
      </aside>

      <main className="flex flex-1 overflow-hidden">
        {secondaryNav && (
          <aside className={`hbdr-secondary-sidebar ${secondaryCollapsed ? 'is-collapsed' : ''}`}>
            <div className="hbdr-secondary-title">
              <span>{language.secondaryTitles[activeModule]}</span>
              <button
                type="button"
                className="hbdr-secondary-toggle"
                onClick={() => setSecondaryCollapsed(prev => !prev)}
                title={secondaryCollapsed ? 'Expand menu' : 'Collapse menu'}
                aria-label={secondaryCollapsed ? 'Expand secondary menu' : 'Collapse secondary menu'}
              >
                <ChevronDown size={14} />
              </button>
            </div>
            <div className="hbdr-secondary-menu">
              {secondaryNav.items.map(item => {
                const blocked = navBlockedViews.has(item.view) && onboarding !== 'ready' && item.view !== 'clusters';
                const disabled = blocked;
                return (
                  <button
                    key={item.view}
                    onClick={() => { if (disabled) { setToast(onboardingMessage); openView('clusters'); return; } openView(item.view); }}
                    disabled={disabled}
                    title={disabled ? onboardingMessage : language.secondaryMeta[item.view][0]}
                    className={`${view === item.view ? 'hbdr-secondary-active' : ''} ${disabled ? 'cursor-not-allowed opacity-50 hover:bg-transparent' : ''}`}
                  >
                    <item.icon size={16} />
                    <span>
                      <strong>{language.secondaryMeta[item.view][0]}</strong>
                      <small>{language.secondaryMeta[item.view][1]}</small>
                    </span>
                  </button>
                );
              })}
            </div>
          </aside>
        )}

        <section className="premium-content flex-1 overflow-y-auto">
          <AnimatePresence mode="wait">
            {view === 'dashboard' && (
              <motion.div key="dashboard" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                <OverviewPage
                  cluster={dashboardCluster}
                  clusters={clusters}
                  storage={storage}
                  protectedApps={protectedApps}
                  restorePointCount={activeRestorePointCount}
                  tasks={liveApiTasks}
                  restorePoints={liveApiRestorePointViews}
                  policies={liveApiPolicies}
                  protectionPlans={liveApiPlans}
                  applications={liveApiApps}
                  defaultClusterId={defaultClusterId}
                  openDr={() => openView('applications')}
                  openOperations={() => openView('operations')}
                  clusterContext={(
                    <ClusterContextCard
                      compact
                      cluster={workspaceCluster}
                      clusters={drClusters}
                      defaultClusterId={defaultClusterId}
                      pickerOpen={clusterPickerOpen}
                      setPickerOpen={setClusterPickerOpen}
                      setSelectedCluster={setSelectedCluster}
                      setDefaultCluster={setDefaultCluster}
                      openClusters={() => openView('clusters')}
                    />
                  )}
                />
              </motion.div>
            )}

            {view === 'applications' && (onboarding !== 'ready' ? onboardingGate : (
              <motion.div key="applications" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
                <div className="hbdr-app-workspace-bar">
                  <div className="min-w-0">
                    <h3 className="hbdr-app-workspace-title">Application DR</h3>
                    <p className="hbdr-app-workspace-desc">Select applications, configure policies, and start DR.</p>
                  </div>
                  <div className="hbdr-app-workspace-cluster">
                    <ClusterContextCard
                      compact
                      cluster={drWorkspaceCluster}
                      clusters={clusters}
                      defaultClusterId={defaultClusterId}
                      pickerOpen={clusterPickerOpen}
                      setPickerOpen={setClusterPickerOpen}
                      setSelectedCluster={setSelectedCluster}
                      setDefaultCluster={setDefaultCluster}
                      openClusters={() => openView('clusters')}
                    />
                  </div>
                </div>
                <ApplicationDrPage
                  key={drWorkspaceCluster?.id || 'no-dr-cluster'}
                  apps={drActiveApps}
                  clusters={drClusters}
                  currentCluster={drWorkspaceCluster}
                  storage={drStorage}
                  policies={drPolicies}
                  protectionPlans={liveApiPlans}
                  setProtectionPlans={setLiveApiPlans}
                  tags={tags}
                  stage={appStage}
                  setStage={setAppStage}
                  currentClusterId={drWorkspaceCluster?.id || null}
                  updateAppTags={updateAppTags}
                  openRestorePoints={(namespaces) => {
                    setRestorePointNamespaceFilter(Array.from(new Set(namespaces.filter(Boolean))));
                    openView('restore_points');
                  }}
                  openStorage={() => openView('storage')}
                  openClusters={() => openView('clusters')}
                  openPolicies={() => openView('policies')}
                  toast={setToast}
                  refreshPlatformData={refreshPlatformData}
                  liveAppTasks={liveAppTasks}
                  setLiveAppTasks={setLiveAppTasks}
                  liveRecoveryTasks={liveRecoveryTasks}
                  setLiveRecoveryTasks={setLiveRecoveryTasks}
                  liveRestorePoints={liveRestorePoints}
                />
              </motion.div>
            ))}

            {view === 'failback' && (onboarding !== 'ready' ? onboardingGate : <FailbackPage toast={setToast} />)}
            {view === 'clusters' && (
              <ClusterPage
                clusters={liveClusters ?? clusters}
                defaultClusterId={defaultClusterId}
                clusterMenuId={clusterMenuId}
                setClusterMenuId={setClusterMenuId}
                setSelectedCluster={setSelectedCluster}
                setDefaultCluster={setDefaultCluster}
                clearDefaultCluster={clearDefaultCluster}
                unregisterCluster={unregisterCluster}
                onRenameCluster={(clusterId, name) => {
                  const patchCluster = (cluster: Cluster) => cluster.id === clusterId ? { ...cluster, name } : cluster;
                  setClusters(prev => prev.map(patchCluster));
                  setLiveClusters(prev => prev ? prev.map(patchCluster) : prev);
                  setSelectedCluster(prev => prev ? patchCluster(prev) : prev);
                }}
                onUpgradeCluster={async (clusterId) => {
                  await apiPost<ApiTask>(`/api/v1/clusters/${clusterId}/agent/upgrade`, {});
                  const markUpgrading = (cluster: Cluster) => cluster.id === clusterId ? {
                    ...cluster,
                    agentUpgradeStatus: 'upgrading',
                  } : cluster;
                  setClusters(prev => prev.map(markUpgrading));
                  setLiveClusters(prev => prev ? prev.map(markUpgrading) : prev);
                  setSelectedCluster(prev => prev ? markUpgrading(prev) : prev);
                }}
                onRegisterCluster={(cluster) => {
                  setClusters(prev => [cluster, ...prev]);
                  setSelectedCluster(cluster);
                  setToast(`${cluster.name} registered. Agent connection is awaiting verification.`);
                }}
                onRefreshRegistration={refreshPlatformData}
                clusterTaskLogs={clusterTaskLogs}
                getAgentTokenForRegistration={getAgentTokenForRegistration}
                prefetchAgentToken={prefetchAgentToken}
                openDashboard={() => openView('dashboard', { preserveSelectedCluster: true })}
                toast={setToast}
              />
            )}
            {view === 'storage' && (onboarding !== 'ready' ? onboardingGate : <StoragePage storage={liveStorage ?? storage} clusters={liveClusters ?? clusters} onStorageCreated={(repo) => {
              setLiveStorage(prev => prev ? [repo, ...prev.filter(item => item.id !== repo.id)] : [repo]);
              setStorage(prev => [repo, ...prev.filter(item => item.id !== repo.id)]);
            }} />)}
            {view === 'policies' && (onboarding !== 'ready' ? onboardingGate : <PolicyPage policies={policies} setPolicies={setPolicies} />)}
            {view === 'restore_points' && (onboarding !== 'ready' ? onboardingGate : (
              <RealRestorePointPage
                openDr={() => openView('applications')}
                toast={setToast}
                workspaceCluster={drWorkspaceCluster}
                initialPoints={liveApiRestorePoints}
                initialClusters={liveApiClusters}
                initialStorageRepos={liveApiStorageRepos}
                initialPlans={liveApiPlans}
                initialTasks={liveApiTasks}
                namespaceFilter={restorePointNamespaceFilter}
                clusterContext={(
                  <ClusterContextCard
                    compact
                    cluster={drWorkspaceCluster}
                    clusters={clusters}
                    defaultClusterId={defaultClusterId}
                    pickerOpen={clusterPickerOpen}
                    setPickerOpen={setClusterPickerOpen}
                    setSelectedCluster={setSelectedCluster}
                    setDefaultCluster={setDefaultCluster}
                    openClusters={() => openView('clusters')}
                  />
                )}
              />
            ))}
            {view === 'dr_tasks' && (onboarding !== 'ready' ? onboardingGate : (
              <BackupRecoveryTaskPage
                toast={setToast}
                workspaceCluster={drWorkspaceCluster}
                initialTasks={liveApiTasks}
                initialRestorePoints={liveApiRestorePoints}
                initialClusters={liveApiClusters}
                initialApps={liveApiApps}
                initialStorageRepos={liveApiStorageRepos}
                clusterContext={(
                  <ClusterContextCard
                    compact
                    cluster={drWorkspaceCluster}
                    clusters={clusters}
                    defaultClusterId={defaultClusterId}
                    pickerOpen={clusterPickerOpen}
                    setPickerOpen={setClusterPickerOpen}
                    setSelectedCluster={setSelectedCluster}
                    setDefaultCluster={setDefaultCluster}
                    openClusters={() => openView('clusters')}
                  />
                )}
              />
            ))}
            {view === 'operations' && (onboarding !== 'ready' ? onboardingGate : <RealOperationsPage />)}
            {view === 'tags' && (onboarding !== 'ready' ? onboardingGate : <TagManagementPage tags={tags} setTags={setTags} clusters={clusters} setClusters={setClusters} toast={setToast} />)}
            {view === 'alerts' && <AlertsPage />}
            {view === 'settings' && <SettingsPage />}
            {view === 'tenants' && <TenantPage />}
          </AnimatePresence>
        </section>
      </main>

      {toast && (
        <div className="fixed right-5 top-20 z-50 rounded border border-slate-200 bg-white px-4 py-3 text-sm font-bold text-slate-700 shadow-xl">
          {toast}
        </div>
      )}
    </div>
  );
}

function ClusterContextCard(props: {
  cluster: Cluster | null;
  clusters: Cluster[];
  defaultClusterId: string | null;
  pickerOpen: boolean;
  compact?: boolean;
  setPickerOpen: (open: boolean) => void;
  setSelectedCluster: (cluster: Cluster) => void;
  setDefaultCluster: (cluster: Cluster, event?: React.MouseEvent) => void;
  openClusters: () => void;
}) {
  const { cluster, clusters, defaultClusterId, pickerOpen, compact, setPickerOpen, setSelectedCluster, setDefaultCluster, openClusters } = props;
  const isClusterOffline = Boolean(cluster && cluster.connectionStatus !== 'online');
  const clusterMeta = cluster
    ? isClusterOffline
      ? `Agent offline. Reconnecting... · ${cluster.version}`
      : `${agentReadiness(cluster).label} · ${cluster.version} · ${cluster.applications} namespaces`
    : 'Select a cluster to activate the DR workspace';
  return (
    <div className={`relative hbdr-cluster-context-wrap ${pickerOpen ? 'z-[110]' : ''}`}>
      <button
        type="button"
        onClick={() => setPickerOpen(!pickerOpen)}
        className={`hbdr-cluster-context ${compact ? 'hbdr-cluster-context-compact' : ''} ${cluster ? 'hbdr-cluster-context-active' : 'hbdr-cluster-context-empty'} ${isClusterOffline ? 'hbdr-cluster-context-offline' : ''}`}
      >
        <div className="hbdr-cluster-context-icon"><Server size={18} /></div>
        <div className="min-w-0 flex-1 text-left">
          <p className="hbdr-cluster-context-kicker">DR Workspace</p>
          <h4 className="hbdr-cluster-context-title">{cluster ? cluster.name : 'No default cluster selected'}</h4>
          <p className="hbdr-cluster-context-meta">
            {clusterMeta}
          </p>
        </div>
        {isClusterOffline && <span className="hbdr-cluster-offline-pill">Offline</span>}
        {cluster?.id === defaultClusterId && <span className="hbdr-cluster-default-pill">Default</span>}
        <ChevronDown size={14} className={`hbdr-cluster-context-chevron ${pickerOpen ? 'rotate-180' : ''}`} />
      </button>
      <AnimatePresence>
        {pickerOpen && (
          <>
            <div className="fixed inset-0 z-[90]" onClick={() => setPickerOpen(false)} />
            <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: 8 }} className="hbdr-cluster-picker">
              <div className="hbdr-cluster-picker-list">
                {clusters.map(item => {
                  const itemOffline = item.connectionStatus !== 'online';
                  return (
                  <div key={item.id} className={`hbdr-cluster-picker-row ${cluster?.id === item.id ? 'hbdr-cluster-picker-row-active' : ''} ${itemOffline ? 'hbdr-cluster-picker-row-offline' : ''}`}>
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedCluster(item);
                        setPickerOpen(false);
                      }}
                      className="min-w-0 flex-1 text-left"
                    >
                      <p className="hbdr-cluster-picker-name"><span className="hbdr-cluster-picker-status-dot" />{item.name}</p>
                      <p className="hbdr-cluster-picker-meta">{itemOffline ? 'Agent offline. Reconnecting...' : `${agentReadiness(item).label} · ${item.version} · ${item.applications} namespaces`}</p>
                    </button>
                    {itemOffline && <span className="hbdr-cluster-picker-offline-pill">Offline</span>}
                    <button type="button" onClick={(event) => setDefaultCluster(item, event)} className={`hbdr-cluster-picker-default ${item.id === defaultClusterId ? 'hbdr-cluster-picker-default-active' : ''}`}>
                      {item.id === defaultClusterId ? 'Default' : 'Set Default'}
                    </button>
                  </div>
                  );
                })}
              </div>
              <div className="hbdr-cluster-picker-footer">
                <button
                  type="button"
                  onClick={() => {
                    setPickerOpen(false);
                    openClusters();
                  }}
                  className="hbdr-cluster-picker-open"
                >
                  Go to cluster
                </button>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

const USE_PROTOTYPE_VISUAL_DATA = import.meta.env.VITE_USE_PROTOTYPE_VISUAL_DATA === 'true';

function resolveRecoveryCluster(cluster: Cluster | null, clusters: Cluster[]): Cluster | null {
  if (!cluster) return null;

  const targetNames = [...new Set(
    cluster.apps
      .filter(app => app.isProtected && app.targetCluster)
      .map(app => app.targetCluster!),
  )];

  if (targetNames.length === 1) {
    return clusters.find(item => item.name === targetNames[0])
      ?? clusters.find(item => item.id !== cluster.id)
      ?? null;
  }

  if (targetNames.length > 1) {
    return clusters.find(item => targetNames.includes(item.name))
      ?? clusters.find(item => item.id !== cluster.id)
      ?? null;
  }

  return clusters.find(item => item.id !== cluster.id) ?? null;
}

function policyIntervalMs(policy?: ApiPolicy): number | null {
  if (!policy || policy.scheduleType !== 'interval') return null;
  const value = policy.intervalValue || 0;
  if (value <= 0) return null;
  const unit = (policy.intervalUnit || '').toLowerCase();
  if (unit === 'minute' || unit === 'minutes') return value * 60 * 1000;
  if (unit === 'hour' || unit === 'hours') return value * 60 * 60 * 1000;
  if (unit === 'day' || unit === 'days') return value * 24 * 60 * 60 * 1000;
  return value * 60 * 60 * 1000;
}

function planIncludesNamespace(plan: ApiProtectionPlan, app: AppItem, apiApps: ApiApplication[]): boolean {
  const ids = new Set([plan.appId, ...(plan.appIds || [])].filter(Boolean));
  if (app.apiId && ids.has(app.apiId)) return true;
  if (!app.clusterId) return false;
  return apiApps.some(apiApp => ids.has(apiApp.id) && apiApp.clusterId === app.clusterId && apiApp.namespace === (app.namespace || app.name));
}

function restorePointMatchesApp(point: ApiRestorePointView, app: AppItem): boolean {
  const sourceNamespace = app.namespace || app.name;
  if (app.protectionPlanId && point.protectionPlanId && point.includedNamespaces?.length) {
    return point.protectionPlanId === app.protectionPlanId && point.includedNamespaces.includes(sourceNamespace);
  }
  if (app.apiId && point.appId) return point.appId === app.apiId;
  if (app.protectionPlanId && point.protectionPlanId) return point.protectionPlanId === app.protectionPlanId;
  if (!app.clusterId) return false;
  return point.sourceClusterId === app.clusterId && point.sourceNamespace === sourceNamespace;
}

function latestSuccessfulRestorePoint(points: ApiRestorePointView[], app: AppItem): ApiRestorePointView | undefined {
  return points
    .filter(point => point.status === 'available' && restorePointMatchesApp(point, app))
    .sort((a, b) => (b.time || '').localeCompare(a.time || ''))[0];
}

function taskMatchesApp(task: ApiTask, app: AppItem, points: ApiRestorePointView[]): boolean {
  const taskNamespaces = namespacesFromPayload(task.payload);
  const sourceNamespace = app.namespace || app.name;
  if (task.protectionPlanId && taskNamespaces.length > 0) {
    return task.protectionPlanId === app.protectionPlanId && taskNamespaces.includes(sourceNamespace);
  }
  if (task.appId && app.apiId) return task.appId === app.apiId;
  if (task.restorePointId) {
    const point = points.find(item => item.id === task.restorePointId);
    return point ? restorePointMatchesApp(point, app) : false;
  }
  if (!app.clusterId) return false;
  if (task.clusterId !== app.clusterId) return false;
  return task.payload?.sourceNamespace === sourceNamespace;
}

function isSucceededStatus(status?: string) {
  return status === 'succeeded' || status === 'completed';
}

function isFailedStatus(status?: string) {
  return status === 'failed' || status === 'canceled' || status === 'cancelled' || status === 'error' || status === 'timeout' || status === 'timed_out';
}

function taskHasWarning(task?: ApiTask) {
  return Boolean(task?.errorCode || task?.errorMessage);
}

function taskWarningTitle(task?: ApiTask) {
  if (!task) return '';
  if ((task.errorCode || '').startsWith('RETENTION_')) return 'Snapshot retention warning';
  return task.errorCode || 'Task warning';
}

function recentTasks(tasks: ApiTask[], days: number): ApiTask[] {
  const since = Date.now() - days * 24 * 60 * 60 * 1000;
  return tasks.filter(task => Date.parse(task.createdAt || '') >= since);
}

function taskTypeLabel(type?: string) {
  if (type === 'backup') return 'Data sync';
  if (type === 'drill') return 'DR drill';
  if (type === 'restore') return 'Restore';
  if (type === 'takeover') return 'Takeover';
  if (type === 'storage-sync') return 'Storage sync';
  if (type === 'unregister') return 'Cluster unregister';
  if (type === 'bsl-sync') return 'Storage configuration';
  return type ? type.replace(/-/g, ' ') : 'Task';
}

function taskTime(task: ApiTask) {
  return task.completedAt || task.createdAt || '';
}

function OverviewPage(props: {
  cluster: Cluster | null;
  clusters: Cluster[];
  storage: StorageRepo[];
  protectedApps: number;
  restorePointCount: number;
  tasks: ApiTask[];
  restorePoints: ApiRestorePointView[];
  policies: ApiPolicy[];
  protectionPlans: ApiProtectionPlan[];
  applications: ApiApplication[];
  defaultClusterId: string | null;
  openDr: () => void;
  openOperations: () => void;
  clusterContext: React.ReactNode;
}) {
  const { cluster, clusters, storage, protectedApps, restorePointCount, tasks, restorePoints, policies, protectionPlans, applications, defaultClusterId, openDr, openOperations, clusterContext } = props;
  const clusterApps = cluster?.apps ?? [];
  const clusterTasks = cluster ? tasks.filter(task => task.clusterId === cluster.id) : [];
  const clusterRestorePoints = cluster ? restorePoints.filter(point => point.sourceClusterId === cluster.id && point.status === 'available') : [];
  const clusterPlans = cluster ? protectionPlans.filter(plan => clusterApps.some(app => planIncludesNamespace(plan, app, applications))) : [];
  const totalApps = clusterApps.length;
  const namespaceCount = cluster?.applications ?? 0;
  const activeNamespaces = clusterApps.filter(app => app.status === 'Active' || app.status === 'Running' || app.status === 'Protected').length;
  const connectedStorage = storage.filter(item => item.status === 'connected').length;
  const recoveryCluster = resolveRecoveryCluster(cluster, clusters);
  const targetClusterNames = [...new Set(
    clusterApps.filter(app => app.isProtected && app.targetCluster).map(app => app.targetCluster!),
  )];
  const notConfigured = Math.max(totalApps - protectedApps, 0);
  const now = Date.now();
  const rpoStats = clusterApps.reduce((acc, app) => {
    if (!app.isProtected) return acc;
    const plan = clusterPlans.find(item => planIncludesNamespace(item, app, applications));
    const policy = policies.find(item => item.id === plan?.policyId);
    const intervalMs = policyIntervalMs(policy);
    const latestPoint = latestSuccessfulRestorePoint(clusterRestorePoints, app);
    if (!intervalMs || !latestPoint?.time) {
      acc.risk += 1;
      return acc;
    }
    const ageMs = now - Date.parse(latestPoint.time);
    if (Number.isFinite(ageMs) && ageMs <= intervalMs) acc.meeting += 1;
    else acc.risk += 1;
    return acc;
  }, { meeting: 0, risk: 0 });
  const syncConfiguredApps = clusterApps.filter(app => app.isProtected);
  const activeBackupTasks = clusterTasks.filter(task => task.type === 'backup' && isActiveTaskStatus(task.status));
  const failedBackupTasks = clusterTasks.filter(task => task.type === 'backup' && isFailedStatus(task.status));
  const syncCompleted = syncConfiguredApps.filter(app => Boolean(latestSuccessfulRestorePoint(clusterRestorePoints, app))).length;
  const syncNotStarted = Math.max(syncConfiguredApps.length - syncCompleted - activeBackupTasks.length - failedBackupTasks.length, 0);
  const syncRate = syncConfiguredApps.length > 0 ? Math.round((syncCompleted / syncConfiguredApps.length) * 100) : 0;
  const restoreTasks = clusterTasks.filter(task => ['restore', 'drill', 'takeover'].includes(task.type));
  const restoreInProgress = restoreTasks.filter(task => isActiveTaskStatus(task.status)).length;
  const restoreFailed = restoreTasks.filter(task => isFailedStatus(task.status)).length;
  const drillTasks30d = recentTasks(clusterTasks.filter(task => task.type === 'drill'), 30);
  const drillInProgress = drillTasks30d.filter(task => isActiveTaskStatus(task.status)).length;
  const drillCompleted = drillTasks30d.filter(task => isSucceededStatus(task.status)).length;
  const drillFailed = drillTasks30d.filter(task => isFailedStatus(task.status)).length;
  const storageUnavailable = storage.length - connectedStorage;
  const offlineClusters = clusters.filter(item => item.connectionStatus !== 'online').length;
  const failedRecentTasks = recentTasks(tasks, 30).filter(task => isFailedStatus(task.status)).length;
  const warningRecentTasks = recentTasks(tasks, 30).filter(task => task.status === 'warning' || task.errorCode).length;
  const criticalAlerts = offlineClusters + storageUnavailable + failedRecentTasks;
  const urgentAlerts = rpoStats.risk + syncConfiguredApps.filter(app => !latestSuccessfulRestorePoint(clusterRestorePoints, app)).length;
  const normalAlerts = notConfigured + warningRecentTasks;
  const recentEventTasks = [...tasks]
    .filter(task => ['backup', 'drill', 'restore', 'takeover', 'storage-sync', 'unregister'].includes(task.type))
    .sort((a, b) => taskTime(b).localeCompare(taskTime(a)))
    .slice(0, 4);
  const registeredClusters = clusters.length;
  const defaultClusterCount = defaultClusterId ? 1 : 0;
  const plansInUse = protectionPlans.length;
  const policiesInUse = new Set(protectionPlans.map(plan => plan.policyId).filter(Boolean)).size;
  const policiesAvailable = policies.filter(policy => !policy.boundCount && !protectionPlans.some(plan => plan.policyId === policy.id)).length;
  const alertRows = [
    offlineClusters > 0 ? `${offlineClusters} cluster${offlineClusters === 1 ? '' : 's'} offline` : null,
    storageUnavailable > 0 ? `${storageUnavailable} storage repositor${storageUnavailable === 1 ? 'y is' : 'ies are'} unavailable` : null,
    failedRecentTasks > 0 ? `${failedRecentTasks} failed task${failedRecentTasks === 1 ? '' : 's'} in 30 days` : null,
    rpoStats.risk > 0 ? `${rpoStats.risk} protected namespace${rpoStats.risk === 1 ? '' : 's'} at RPO risk` : null,
    notConfigured > 0 ? `${notConfigured} namespace${notConfigured === 1 ? '' : 's'} not protected` : null,
  ].filter((item): item is string => Boolean(item)).slice(0, 3);
  const clusterZoneKey = cluster?.id || 'no-cluster';
  const drSiteTitle = targetClusterNames.length > 1
    ? `${targetClusterNames.length} Targets`
    : (recoveryCluster?.name ?? 'N/A');
  const drSiteSubtitle = targetClusterNames.length > 1 ? 'Target Clusters' : 'Target Cluster';

  return (
    <div className="hbdr-dashboard hbdr-dashboard-zones">
      <div className="hbdr-dashboard-upper">
      <section className="hbdr-dashboard-workspace hbdr-dashboard-zone hbdr-dashboard-zone-cluster">
        <header className="hbdr-dashboard-zone-head">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-cluster" aria-hidden="true" />
            <div>
              <h2>Cluster DR</h2>
              <p>Updates when you switch the active cluster</p>
            </div>
          </div>
          <div className="hbdr-dashboard-zone-cluster-picker">{clusterContext}</div>
        </header>
        <motion.div
          key={clusterZoneKey}
          className="hbdr-dashboard-cluster-body"
          initial={{ opacity: 0.82 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.22 }}
        >
        <div className="hbdr-dashboard-flow">
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-prod-icon"><Server size={44} /></div>
            <strong>Production</strong>
          </div>
          <div className="hbdr-dashboard-flow-line"><span>Backup</span></div>
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-storage-icon"><Database size={44} /></div>
            <strong>Storage</strong>
          </div>
          <div className="hbdr-dashboard-flow-line"><span>DR Drill</span></div>
          <div className="hbdr-dashboard-flow-node">
            <div className="hbdr-dashboard-cloud-icon"><Cloud size={44} /></div>
            <strong>DR Site</strong>
          </div>
        </div>

        <div className="hbdr-dashboard-cards">
          <DashboardPanel title="Production" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{totalApps}</strong>
              <span>Namespaces</span>
            </div>
            <DashboardLegend color="green" label="Protected" value={protectedApps} />
            <DashboardLegend color="red" label="Unprotected" value={notConfigured} />
            {cluster && <DashboardLegend color="blue" label="Active" value={activeNamespaces || namespaceCount} />}
          </DashboardPanel>

          <DashboardPanel title="RPO">
            <div className="hbdr-dashboard-big-number">
              <strong>{rpoStats.risk}</strong>
              <span>At SLA Risk</span>
            </div>
            <DashboardLegend color="green" label="Meeting SLA" value={rpoStats.meeting} />
            <DashboardLegend color="red" label="Not Protected" value={notConfigured} />
          </DashboardPanel>

          <DashboardPanel title="Data Sync" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{syncRate}%</strong>
              <span>Sync Completion</span>
            </div>
            <DashboardLegend color="gray" label="Not Started" value={syncNotStarted} />
            <DashboardLegend color="green" label="In Progress" value={activeBackupTasks.length} />
            <DashboardLegend color="green" label="Completed" value={syncCompleted} />
            <DashboardLegend color="red" label="Failed" value={failedBackupTasks.length} />
          </DashboardPanel>

          <DashboardPanel title="Restore Points" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{clusterRestorePoints.length}</strong>
              <span>Restore Points</span>
            </div>
            <DashboardLegend color="blue" label="In Progress" value={restoreInProgress} />
            <DashboardLegend color="green" label="Completed" value={clusterRestorePoints.length} />
            <DashboardLegend color="red" label="Failed" value={restoreFailed} />
          </DashboardPanel>

          <DashboardPanel title="DR Drill" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number">
              <strong>{drillTasks30d.length}</strong>
              <span>Drills in 30 Days</span>
            </div>
            <DashboardLegend color="blue" label="In Progress" value={drillInProgress} />
            <DashboardLegend color="green" label="Completed" value={drillCompleted} />
            <DashboardLegend color="red" label="Failed" value={drillFailed} />
          </DashboardPanel>

          <DashboardPanel title="DR Site" detailAction={openDr}>
            <div className="hbdr-dashboard-big-number hbdr-dashboard-big-number-clip">
              <strong>{drSiteTitle}</strong>
              <span>{drSiteSubtitle}</span>
            </div>
            <DashboardLegend color="gray" label="Kubernetes" value={recoveryCluster?.version || '-'} />
            <DashboardLegend color="green" label="Nodes" value={recoveryCluster ? recoveryCluster.nodes : 0} />
            <DashboardLegend color="gray" label="Namespaces" value={recoveryCluster ? recoveryCluster.applications : 0} />
          </DashboardPanel>
        </div>
        </motion.div>
      </section>

      <aside className="hbdr-dashboard-side hbdr-dashboard-zone hbdr-dashboard-zone-platform">
        <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-platform" aria-hidden="true" />
            <div>
              <h2>Platform Overview</h2>
              <p>Shared resources across all clusters</p>
            </div>
          </div>
        </header>
        <div className="hbdr-dashboard-platform-body">
        <div className="hbdr-dashboard-side-cards hbdr-platform-grid">
        <DashboardPanel className="hbdr-platform-card-compact" title="Storage Repositories">
          <div className="hbdr-dashboard-big-number">
            <strong>{storage.length}</strong>
            <span>Repositories</span>
          </div>
          <DashboardLegend color="green" label="Connected" value={connectedStorage} />
          <DashboardLegend color="gray" label="Unavailable" value={storage.length - connectedStorage} />
        </DashboardPanel>
        <DashboardPanel className="hbdr-platform-card-compact" title="DR Policy">
          <div className="hbdr-dashboard-big-number">
            <strong>{plansInUse}</strong>
            <span>Protection Plans</span>
          </div>
          <DashboardLegend color="green" label="In Use" value={policiesInUse} />
          <DashboardLegend color="gray" label="Available" value={policiesAvailable} />
        </DashboardPanel>
        <PlatformLicenseCard />
        <DashboardPanel className="hbdr-platform-card-wide hbdr-platform-clusters-card" title="Registered Clusters">
          <div className="hbdr-dashboard-big-number">
            <strong>{registeredClusters}</strong>
            <span>Clusters</span>
          </div>
          <DashboardLegend color="blue" label="Default" value={defaultClusterCount} />
          <DashboardLegend color="green" label="Registered" value={registeredClusters} />
        </DashboardPanel>
        </div>
        </div>
      </aside>
      </div>

      <section className="hbdr-dashboard-zone hbdr-dashboard-zone-operations">
        <header className="hbdr-dashboard-zone-head hbdr-dashboard-zone-head-static">
          <div className="hbdr-dashboard-zone-label">
            <span className="hbdr-dashboard-zone-dot hbdr-dashboard-zone-dot-operations" aria-hidden="true" />
            <div>
              <h2>Operations</h2>
              <p>Monitoring, events, and alerts</p>
            </div>
          </div>
        </header>

        <div className="hbdr-dashboard-operations-row">
          <section className="hbdr-dashboard-card hbdr-dashboard-monitor">
            <header>
              <h3>DR Resources Monitoring & Analysis</h3>
              <button onClick={openOperations}>Details &gt;</button>
            </header>
            <div className="hbdr-dashboard-filters">
              <button>DR Agent <ChevronDown size={16} /></button>
              <span className="hbdr-dashboard-filter-sep">Filter</span>
              <button>{cluster?.name || 'All clusters'} <ChevronDown size={16} /></button>
              <button><RefreshCw size={18} /></button>
            </div>
            <div className="hbdr-dashboard-charts">
              <div className="hbdr-dashboard-empty-panel">
                <strong>CPU Usage</strong>
                <Cloud size={68} />
                <span>No monitoring data yet</span>
              </div>
              <div className="hbdr-dashboard-empty-panel">
                <strong>Network (bytes)</strong>
                <Cloud size={68} />
                <span>No monitoring data yet</span>
              </div>
            </div>
          </section>

          <section className="hbdr-dashboard-card hbdr-dashboard-events">
            <header>
              <h3>Events</h3>
              <button>Logs &gt;</button>
            </header>
            {recentEventTasks.length > 0 ? recentEventTasks.map(task => (
              <div key={task.id} className="hbdr-dashboard-event">
                <span />
                <p>{taskTypeLabel(task.type)} ({taskStatusLabel(task.status)})</p>
                <small>{formatDateTime(taskTime(task))}</small>
              </div>
            )) : (
              <div className="hbdr-dashboard-empty-list">
                <History size={20} />
                <p>No recent task events</p>
              </div>
            )}
          </section>

          <DashboardPanel className="hbdr-dashboard-zone-alert" title="Alert">
            <div className="hbdr-dashboard-alert-metrics">
              <div><strong>{criticalAlerts}</strong><span>Critical</span></div>
              <div><strong>{urgentAlerts}</strong><span>Urgent</span></div>
              <div><strong>{normalAlerts}</strong><span>Alert</span></div>
            </div>
            <div className="hbdr-dashboard-alert-list">
            {alertRows.length > 0 ? alertRows.map(item => (
              <div key={item} className="hbdr-dashboard-alert-row">
                <AlertCircle size={15} />
                <span>{item}</span>
              </div>
            )) : (
              <div className="hbdr-dashboard-empty-list hbdr-dashboard-empty-list-compact">
                <CheckCircle2 size={20} />
                <p>No active alerts</p>
              </div>
            )}
            </div>
          </DashboardPanel>
        </div>
      </section>
    </div>
  );
}


function DashboardPanel({
  title,
  detailAction,
  className,
  children,
}: {
  title: string;
  detailAction?: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={`hbdr-dashboard-card${className ? ` ${className}` : ''}`}>
      <header>
        <h3>{title}</h3>
        <div className="hbdr-dashboard-card-actions">
          {detailAction && <button onClick={detailAction}>Details&gt;</button>}
        </div>
      </header>
      {children}
    </section>
  );
}

function PlatformLicenseCard() {
  return (
    <section className="hbdr-dashboard-card hbdr-platform-card-wide hbdr-platform-license-card">
      <header>
        <h3>License Status</h3>
        <div className="hbdr-dashboard-card-actions">
          <button type="button">Details&gt;</button>
        </div>
      </header>
      <div className="hbdr-platform-wide-body">
        <div className="hbdr-dashboard-empty-list hbdr-dashboard-license-empty">
          <Lock size={22} />
          <p>No license data available</p>
          <small>License metrics will appear after the platform license API is connected.</small>
        </div>
      </div>
    </section>
  );
}

function DashboardLegend({ color, label, value }: { color: 'green' | 'red' | 'blue' | 'gray'; label: string; value: number | string }) {
  return (
    <div className="hbdr-dashboard-legend">
      <span className={`hbdr-dashboard-dot hbdr-dashboard-dot-${color}`} />
      <p>{label}</p>
      <strong>{value}</strong>
    </div>
  );
}

function ProtectionLegend({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div>
      <span className={color} />
      <p>{label}</p>
      <strong>{value}</strong>
    </div>
  );
}

function ApplicationDrPage(props: {
  apps: AppItem[];
  clusters: Cluster[];
  currentCluster: Cluster | null;
  storage: StorageRepo[];
  policies: PolicyItem[];
  protectionPlans: ApiProtectionPlan[];
  setProtectionPlans: React.Dispatch<React.SetStateAction<ApiProtectionPlan[]>>;
  tags: TagItem[];
  stage: 'select' | 'config' | 'run';
  setStage: (stage: 'select' | 'config' | 'run') => void;
  currentClusterId: string | null;
  updateAppTags: (clusterId: string | null, appNames: string[], updater: (currentTags: string[]) => string[]) => void;
  openRestorePoints: (namespaces: string[]) => void;
  openStorage: () => void;
  openClusters: () => void;
  openPolicies: () => void;
  toast: (msg: string) => void;
  refreshPlatformData: () => Promise<unknown>;
  liveAppTasks: Record<string, ApiTask>;
  setLiveAppTasks: React.Dispatch<React.SetStateAction<Record<string, ApiTask>>>;
  liveRecoveryTasks: Record<string, ApiTask>;
  setLiveRecoveryTasks: React.Dispatch<React.SetStateAction<Record<string, ApiTask>>>;
  liveRestorePoints: ApiRestorePointView[];
}) {
  const { apps, clusters, currentCluster, storage, policies, protectionPlans, setProtectionPlans, tags, stage, setStage, currentClusterId, updateAppTags, openRestorePoints, openStorage, openClusters, openPolicies, toast, refreshPlatformData, liveAppTasks, setLiveAppTasks, liveRecoveryTasks, setLiveRecoveryTasks, liveRestorePoints } = props;
  const [selectedSelectApps, setSelectedSelectApps] = useState<string[]>([]);
  const [selectedConfigApps, setSelectedConfigApps] = useState<string[]>([]);
  const [selectedRunApps, setSelectedRunApps] = useState<string[]>([]);
  const [configAppNames, setConfigAppNames] = useState<string[]>([]);
  const [protectedAppNames, setProtectedAppNames] = useState<string[]>(() => apps.filter(app => app.stage === 'run').map(app => app.name));
  const [appUiOverrides, setAppUiOverrides] = useState<Record<string, Partial<AppItem>>>({});
  const [protectWizardOpen, setProtectWizardOpen] = useState(false);
  const [protectWizardMode, setProtectWizardMode] = useState<'create' | 'modify'>('create');
  const [protectWizardStep, setProtectWizardStep] = useState<1 | 2 | 3 | 4 | 5 | 6>(1);
  const [protectConfig, setProtectConfig] = useState({
    scope: 'all',
    labels: '',
    labelConditions: [] as Array<{ key: string; operator: 'Equals' | 'Not Equals'; value: string }>,
    includeRules: [] as Array<{ group: string; resource: string; name: string; version: string; labels: string }>,
    storageType: 'local',
    storageId: storage[0]?.id || '',
    policy: 'manual',
    targetCluster: '',
    mergeNamespaces: false,
    excludeRules: [] as Array<{ group: string; resource: string; name: string; version: string; labels: string }>,
    preScripts: [] as Array<{ name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }>,
    postScripts: [] as Array<{ name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean }>,
  });
  const [showAddRuleForm, setShowAddRuleForm] = useState(false);
  const [newExcludeRule, setNewExcludeRule] = useState({ group: '', resource: '', name: '', version: '', labels: '' });
  const [editingRuleIndex, setEditingRuleIndex] = useState<number | null>(null);
  const syncTasks: Record<string, ApiTask> = liveAppTasks;
  const setSyncTasks = (updater: any) => {
    if (typeof updater === 'function') {
      setLiveAppTasks(prev => updater(prev));
    } else {
      setLiveAppTasks(updater);
    }
  };
  const [syncTaskDetail, setSyncTaskDetail] = useState<{ app: AppItem; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null>(null);
  const [drTaskEvents, setDrTaskEvents] = useState<Record<string, ApiTaskEvent[]>>({});
  const [resourceDetail, setResourceDetail] = useState<{ app: AppItem } | null>(null);
  const [resourceRefreshKey, setResourceRefreshKey] = useState('');
  const [resourceRefreshStatus, setResourceRefreshStatus] = useState<{ key: string; status: string; message?: string } | null>(null);
  const [drSupportCheckingKeys, setDrSupportCheckingKeys] = useState<string[]>([]);
  const drSupportAutoRequestedRef = useRef<Set<string>>(new Set());
  const [appBulkMenuOpen, setAppBulkMenuOpen] = useState(false);
  const [selectedDetailApp, setSelectedDetailApp] = useState<AppItem | null>(null);
  const [drSupportErrorDetail, setDrSupportErrorDetail] = useState<AppItem | null>(null);
  const currentClusterIdRef = useRef(currentClusterId);
  useEffect(() => {
    if (currentClusterIdRef.current === currentClusterId) return;
    currentClusterIdRef.current = currentClusterId;
    setStage('select');
    setSelectedSelectApps([]);
    setSelectedConfigApps([]);
    setSelectedRunApps([]);
    setConfigAppNames([]);
    setProtectedAppNames(apps.filter(app => app.isProtected).map(app => app.name));
    setAppUiOverrides({});
    setProtectWizardOpen(false);
    setProtectWizardStep(1);
    setSelectedDetailApp(null);
    setDrSupportErrorDetail(null);
    setSyncTaskDetail(null);
    setRestoreAction(null);
    setTagAction(null);
    setShowAddRuleForm(false);
    setEditingRuleIndex(null);
    setDrSupportCheckingKeys([]);
    drSupportAutoRequestedRef.current.clear();
    setSyncTasks({});
    setAppBulkMenuOpen(false);
  }, [currentClusterId, apps]);

  const [restoreAction, setRestoreAction] = useState<{ mode: 'drill' | 'takeover'; app: AppItem; config: RecoveryWizardConfig } | null>(null);
  const [tagAction, setTagAction] = useState<'attach' | 'detach' | null>(null);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleConfigColumns, setVisibleConfigColumns] = useState(['drSupport', 'resource', 'tags']);
  const [visibleRunColumns, setVisibleRunColumns] = useState(['resource', 'drStatus', 'scope', 'policy', 'repository', 'targetCluster', 'task', 'recoveryTask', 'tags', 'createdAt']);
  const [wizardPolicySearchQuery, setWizardPolicySearchQuery] = useState('');
  const [wizardPolicyPage, setWizardPolicyPage] = useState(1);
  const preScriptRef = useRef<HTMLInputElement>(null);
  const postScriptRef = useRef<HTMLInputElement>(null);
  const policyOptions = policies.map(policy => ({
    id: policy.id,
    name: policy.name,
    type: formatPolicyComposition(policy.composition),
    schedule: formatPolicySchedule(policy),
    retention: formatPolicyRetention(policy),
    desc: policy.composition === 'manual'
      ? 'Create recovery points only when an operator starts a backup. Retention is not defined by this manual option.'
      : `${formatPolicyComposition(policy.composition)} · ${policy.status}`,
    status: policy.status,
    hasRetention: policy.composition !== 'manual' && policy.composition !== 'schedule',
  }));
  const wizardPolicyKeyword = wizardPolicySearchQuery.trim().toLowerCase();
  const filteredPolicyOptions = policyOptions.filter(policy => {
    if (!wizardPolicyKeyword) return true;
    return [policy.name, policy.type, policy.schedule, policy.retention, policy.desc, policy.status].some(value => value.toLowerCase().includes(wizardPolicyKeyword));
  });
  const wizardPolicyPageSize = 4;
  const wizardPolicyTotalPages = Math.max(1, Math.ceil(filteredPolicyOptions.length / wizardPolicyPageSize));
  const paginatedPolicyOptions = filteredPolicyOptions.slice((wizardPolicyPage - 1) * wizardPolicyPageSize, wizardPolicyPage * wizardPolicyPageSize);
  const wizardTargetNames = protectWizardMode === 'modify' ? selectedRunApps : selectedConfigApps;
  const wizardTargetSummary = wizardTargetNames.length === 1
    ? wizardTargetNames[0]
    : `${wizardTargetNames.length} applications selected`;
  const wizardLabelOptions = buildLabelSelectorOptions(
    wizardTargetNames
      .map(name => apps.find(item => item.name === name))
      .filter((app): app is AppItem => Boolean(app))
  );
  const targetClusterOptions = clusters.map(cluster => ({
    id: cluster.id,
    name: cluster.name,
    region: cluster.region,
    version: cluster.version,
    nodes: cluster.nodes,
    applications: cluster.applications,
    isCurrent: currentCluster?.id === cluster.id,
  }));
  const displayApps = apps.map(app => ({ ...app, ...(appUiOverrides[appOverrideKey(app)] || {}) }));
  const stageOf = (app: AppItem): ApplicationStage => app.stage || (app.isProtected ? 'run' : 'select');
  const selectRows = displayApps.filter(app => stageOf(app) === 'select');
  const pendingRows = displayApps.filter(app => stageOf(app) === 'config');
  const protectedAppRows = displayApps.filter(app => stageOf(app) === 'run');
  const buildMergedProtectedRows = (rows: AppItem[]): AppItem[] => {
    const byPlan = new Map<string, AppItem[]>();
    const unplanned: AppItem[] = [];
    rows.forEach(app => {
      if (!app.protectionPlanId) {
        unplanned.push(app);
        return;
      }
      byPlan.set(app.protectionPlanId, [...(byPlan.get(app.protectionPlanId) || []), app]);
    });
    const merged: AppItem[] = [...unplanned];
    byPlan.forEach((members, planId) => {
      const ordered = [...members].sort((a, b) => (a.namespace || a.name).localeCompare(b.namespace || b.name));
      const first = ordered[0];
      const plan = protectionPlans.find(item => item.id === planId);
      const planAppCount = plan?.appIds?.length || 0;
      if (ordered.length <= 1 || planAppCount <= 1) {
        merged.push(first);
        return;
      }
      const namespaces = ordered.map(app => app.namespace || app.name);
      merged.push({
        ...first,
        name: `plan:${planId}`,
        namespace: namespaces.join(', '),
        memberApps: ordered,
        isMergedPlan: true,
        protectionPlanId: planId,
        protectionPlanCreatedAt: plan?.createdAt || first.protectionPlanCreatedAt,
        workloadCount: ordered.reduce((sum, app) => sum + (app.workloadCount || 0), 0),
        serviceCount: ordered.reduce((sum, app) => sum + (app.serviceCount || 0), 0),
        ingressCount: ordered.reduce((sum, app) => sum + (app.ingressCount || 0), 0),
        configMapCount: ordered.reduce((sum, app) => sum + (app.configMapCount || 0), 0),
        secretCount: ordered.reduce((sum, app) => sum + (app.secretCount || 0), 0),
        pvcCount: ordered.reduce((sum, app) => sum + (app.pvcCount || 0), 0),
        pvCapacityBytes: ordered.reduce((sum, app) => sum + (app.pvCapacityBytes || 0), 0),
        tags: Array.from(new Set(ordered.flatMap(app => app.tags || []))),
      });
    });
    return merged.sort((a, b) => {
      const planTimeDelta = (b.protectionPlanCreatedAt || '').localeCompare(a.protectionPlanCreatedAt || '');
      if (planTimeDelta !== 0) return planTimeDelta;
      return (a.memberApps?.[0]?.namespace || a.namespace || a.name).localeCompare(b.memberApps?.[0]?.namespace || b.namespace || b.name);
    });
  };
  const protectedRows = buildMergedProtectedRows(protectedAppRows);
  const namespaceRows = stage === 'select' ? selectRows : pendingRows;
  const protectedCount = protectedRows.length;
  const pendingCount = pendingRows.length;
  const activeDrTaskIds = [
    ...Object.values(syncTasks),
    ...Object.values(liveRecoveryTasks),
  ].filter(task => task?.id && isActiveTaskStatus(task.status)).map(task => task.id);
  const activeDrTaskKey = Array.from(new Set(activeDrTaskIds)).sort().join('|');
  const protectionPlanForApp = (app: AppItem) => {
    if (!app.protectionPlanId) return undefined;
    return protectionPlans.find(plan => plan.id === app.protectionPlanId);
  };
  const planStorageSizeForApp = (app: AppItem) => {
    const size = recordFromUnknown(protectionPlanForApp(app)?.planStorageSize);
    const totalBytes = numberFromUnknown(size.totalBytes);
    if (!totalBytes) return { label: '', title: app.storage || 'Repository' };
    const metadataBytes = numberFromUnknown(size.metadataBytes);
    const kopiaBytes = numberFromUnknown(size.kopiaBytes);
    const title = [
      `Repository: ${app.storage || 'Unknown'}`,
      `Used: ${formatBytes(totalBytes)}`,
      metadataBytes > 0 ? `Metadata: ${formatBytes(metadataBytes)}` : '',
      kopiaBytes > 0 ? `Kopia: ${formatBytes(kopiaBytes)}` : '',
    ].filter(Boolean).join('; ');
    return { label: `Used ${formatBytes(totalBytes)}`, title };
  };
  const nextSyncLabelForApp = (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan || !isProtectionPlanReady(plan.status)) return '';
    const policy = policies.find(item => item.id === plan.policyId);
    const next = formatNextSyncTime(plan.nextFireAt);
    if (next) return `Next: ${next}`;
    if (policy?.composition === 'manual') return 'Manual only';
    if (!policy && plan.scheduleEnabled === false) return 'Manual only';
    return '';
  };
  const renderNextSyncHint = (app: AppItem) => {
    const label = nextSyncLabelForApp(app);
    return label ? <small className="hbdr-dr-next-sync">{label}</small> : null;
  };
  const drStatusMetaForApp = (app: AppItem) => drStatusForPlan(protectionPlanForApp(app)?.status);
  const retryDrActivation = async (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan) {
      toast('No protection plan is associated with this namespace');
      return;
    }
    if (!canRetryDrActivation(plan.status)) {
      toast('Protection plan activation is not retryable in the current state');
      return;
    }
    setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: 'activating_storage' } : item));
    try {
      const updated = await apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${plan.id}/activate`, {});
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, ...updated, status: updated.status || 'activating_storage' } : item));
      toast(updated.warning ? `Activation retry submitted: ${updated.warning}` : 'Activation retry submitted');
      void refreshPlatformData();
    } catch (error) {
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: plan.status } : item));
      toast('Failed to retry activation: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const reconfigureDrStorage = async (app: AppItem) => {
    const plan = protectionPlanForApp(app);
    if (!plan) {
      toast('No protection plan is associated with this namespace');
      return;
    }
    setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: 'activating_storage' } : item));
    try {
      const updated = await apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${plan.id}/storage/reconfigure`, {});
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, ...updated, status: updated.status || 'activating_storage' } : item));
      toast(updated.warning ? `Storage reconfigure submitted: ${updated.warning}` : 'Storage reconfigure submitted');
      void refreshPlatformData();
    } catch (error) {
      setProtectionPlans(prev => prev.map(item => item.id === plan.id ? { ...item, status: plan.status } : item));
      toast('Failed to reconfigure storage: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  useEffect(() => {
    const ids = activeDrTaskKey ? activeDrTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const loadEvents = async () => {
      const entries = await Promise.all(ids.map(async taskId => {
        try {
          const res = await apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${taskId}/events`);
          return [taskId, listItems(res)] as const;
        } catch {
          return [taskId, null] as const;
        }
      }));
      if (cancelled) return;
      setDrTaskEvents(prev => {
        const next = { ...prev };
        for (const [taskId, events] of entries) {
          if (events) next[taskId] = events;
        }
        return next;
      });
      if (entries.some(([, events]) => events?.some(event => ['completed', 'backup_completed', 'velero-schedule'].includes(event.reason)))) {
        void refreshPlatformData();
      }
    };
    loadEvents();
    const timer = window.setInterval(loadEvents, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeDrTaskKey, refreshPlatformData]);
  const unitMembers = (app: AppItem) => app.memberApps?.length ? app.memberApps : [app];
  const unitNamespaces = (app: AppItem) => unitMembers(app).map(item => item.namespace || item.name);
  const taskForUnit = (tasks: Record<string, ApiTask>, app: AppItem) => {
    if (tasks[app.name]) return tasks[app.name];
    if (app.protectionPlanId) {
      const planTask = Object.values(tasks).find(task => task?.protectionPlanId === app.protectionPlanId);
      if (planTask) return planTask;
    }
    return unitMembers(app).map(member => tasks[member.name]).find(Boolean);
  };
  const normalizedQuery = query.trim().toLowerCase();
  const queryValueForApp = (app: AppItem, field: string) => {
    const profile = profileOf(app);
    const recoveryTask = taskForUnit(liveRecoveryTasks, app);
    if (field === 'namespace') return unitNamespaces(app).join(' ');
    if (field === 'policy') return app.policy || '';
    if (field === 'storage' || field === 'repository') return app.storage || '';
    if (field === 'target' || field === 'targetCluster') return app.targetCluster || 'Backup only';
    if (field === 'resource') return normalizeResourceCategories(app).map(category => `${category.label} ${category.total}`).join(' ');
    if (field === 'drStatus') return drStatusMetaForApp(app).label;
    if (field === 'drSupport') return drSupportMetaForApp(app).label;
    if (field === 'status') return stageOf(app) === 'run' ? 'protected' : stageOf(app) === 'config' ? 'pending_protection' : 'unprotected';
    if (field === 'scope') return formatScopeLabel(profile.scope);
    if (field === 'task') {
      const task = taskForUnit(syncTasks, app);
      return task ? `${task.type} ${task.status} ${task.progress || 0}` : app.lastBackup || '';
    }
    if (field === 'recoveryTask') return recoveryTask ? `${recoveryTask.type} ${recoveryTask.status} ${recoveryTask.payload?.targetNamespace || ''}` : '';
    if (field === 'createdAt') return app.protectionPlanCreatedAt ? formatDateTime(app.protectionPlanCreatedAt) : '';
    if (field === 'tags') return (app.tags || []).join(' ');
    return app.name;
  };
  const appHasTag = (app: AppItem, tag: string) => {
    return unitMembers(app).some(member => (member.tags || []).includes(tag));
  };
  const appMatchesFilter = (app: AppItem, filter: string) => {
    const profile = profileOf(app);
    const task = taskForUnit(syncTasks, app);
    const recoveryTask = taskForUnit(liveRecoveryTasks, app);
    if (filter === 'active') return unitMembers(app).some(member => member.status === 'Active' || member.status === 'Running');
    if (filter === 'protected') return stageOf(app) === 'run';
    if (filter === 'pending') return stageOf(app) === 'config';
    if (filter === 'unprotected') return stageOf(app) === 'select';
    if (filter === 'syncing') return isActiveTaskStatus(task?.status);
    if (filter === 'completed') return isCompletedTaskStatus(task?.status) || Boolean(app.lastBackup);
    if (filter === 'recovering') return isActiveTaskStatus(recoveryTask?.status);
    if (filter === 'warning') return profile.taskStatus === 'warning' || profile.score < 90 || isDRUnsupported(app);
    if (filter === 'supported') return !isDRSupportUnknown(app) && !isDRUnsupported(app);
    if (filter === 'unsupported') return isDRUnsupported(app);
    if (filter === 'notChecked') return isDRSupportUnknown(app);
    if (filter === 'hasPvc') return unitMembers(app).some(member => (member.pvcCount || member.resourceSummary?.pvcs || 0) > 0);
    if (filter === 'stateless') return unitMembers(app).every(member => (member.pvcCount || member.resourceSummary?.pvcs || 0) === 0);
    if (filter === 'manualOnly') return stageOf(app) === 'run' && !app.policy;
    if (filter === 'scheduled') return stageOf(app) === 'run' && Boolean(app.policy);
    return true;
  };
  const matchesQuery = (app: AppItem) => {
    const queryMatched = !normalizedQuery || queryValueForApp(app, queryField).toLowerCase().includes(normalizedQuery);
    const tagsMatched = activeTags.length === 0 || activeTags.some(tag => appHasTag(app, tag));
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => queryValueForApp(app, field));
      return appMatchesFilter(app, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  };
  const resourceProfiles: Record<string, {
    uid: string;
    resources: Array<[string, number]>;
    pvc: string;
    capacity: string;
    age: string;
    score: number;
    scope: string;
    scopeTag?: string;
    taskStatus: 'normal' | 'warning';
  }> = {};
  const profileOf = (app: AppItem) => resourceProfiles[app.name] || {
    uid: `${app.namespace.slice(0, 3)}-000`,
    resources: [['DEP', 1], ['SVC', 1]],
    pvc: app.storage ? '1 PVC mounted' : 'No persistent volume',
    capacity: app.storage ? '10.0 GB' : '0 GB',
    age: app.lastBackup || '1 days ago',
    score: app.isProtected ? 90 : 70,
    scope: app.isProtected ? 'Namespace' : 'Pending Selection',
    taskStatus: app.isProtected ? 'normal' as const : 'warning' as const,
  };
  const resourceSummaryTotal = (app: AppItem) => normalizeResourceCategories(app).reduce((sum, category) => sum + category.total, 0);
  const resourceSummaryTotalForUnit = (app: AppItem) => unitMembers(app).reduce((sum, member) => sum + resourceSummaryTotal(member), 0);
  const drSupportForApp = (app: AppItem): DRSupportSummary | undefined => app.resourceSummary?.drSupport;
  const drSupportStatus = (app: AppItem): string => (drSupportForApp(app)?.status || '').toLowerCase();
  const drSupportKeyForApp = (app: AppItem): string => `${app.clusterId || currentClusterId || ''}:${app.namespace || app.name}`;
  const isDRSupportUnknown = (app: AppItem): boolean => unitMembers(app).some(member => !drSupportStatus(member));
  const isDRSupportChecking = (app: AppItem): boolean => unitMembers(app).some(member => drSupportCheckingKeys.includes(drSupportKeyForApp(member)));
  const isDRUnsupported = (app: AppItem): boolean => unitMembers(app).some(member => drSupportStatus(member) === 'unsupported');
  const formatUnsupportedStorageSummary = (support: DRSupportSummary): string => {
    const checks = (support.checks || []).filter(check => (check.status || '').toLowerCase() === 'unsupported');
    const detected = checks.map(check => {
      const storage = check.storageClass || 'unknown StorageClass';
      const pvType = check.volumeType || 'unknown PV type';
      const provisioner = check.provisioner ? `, provisioner ${check.provisioner}` : '';
      return `${check.name || 'PVC'}: ${storage} / ${pvType}${provisioner}`;
    });
    return [
      'Storage type is not supported for DR.',
      'Supported: stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn.',
      'Unsupported: local-path, hostPath, and local PV storage.',
      detected.length > 0 ? `Detected in this namespace:\n${detected.map(item => `- ${item}`).join('\n')}` : '',
      'Impact: this namespace cannot be moved to Setup DR until its PVCs use supported storage.',
    ].filter(Boolean).join('\n');
  };
  const drSupportMetaForApp = (app: AppItem) => {
    const unsupported = unitMembers(app).filter(member => drSupportStatus(member) === 'unsupported');
    if (unsupported.length > 0) {
      const details = unsupported.map(member => {
        const support = drSupportForApp(member) || {};
        return `${member.namespace || member.name}\n${formatUnsupportedStorageSummary(support)}`;
      });
      return { label: 'Unsupported', tone: 'unsupported' as const, sort: 0, title: details.join('\n\n') };
    }
    const unknown = unitMembers(app).find(member => !drSupportStatus(member));
    if (unknown) {
      if (isDRSupportChecking(app)) {
        return {
          label: 'Checking...',
          tone: 'unknown' as const,
          sort: 1,
          title: 'Checking namespace storage compatibility for DR.',
        };
      }
      return {
        label: 'Not checked',
        tone: 'unknown' as const,
        sort: 1,
        title: 'DR support has not been checked yet. The system will check namespace storage before moving it to Setup DR.',
      };
    }
    const warning = unitMembers(app).find(member => drSupportStatus(member) === 'warning');
    if (warning) {
      return { label: 'Warning', tone: 'warning' as const, sort: 2, title: drSupportForApp(warning)?.reason || 'DR support needs attention' };
    }
    return { label: 'Supported', tone: 'supported' as const, sort: 3, title: 'Namespace storage is eligible for DR protection.' };
  };
  const unsupportedDRChecksForApp = (app: AppItem) => unitMembers(app).flatMap(member => {
    const support = drSupportForApp(member);
    return (support?.checks || [])
      .filter(check => (check.status || '').toLowerCase() === 'unsupported')
      .map(check => ({
        namespace: member.namespace || member.name,
        pvc: check.name || 'PVC',
        storageClass: check.storageClass || 'unknown',
        pvType: check.volumeType || 'unknown',
        provisioner: check.provisioner || 'unknown',
      }));
  });
  const drSupportFailureForApp = (app: AppItem) => {
    const namespace = app.namespace || app.name;
    const details = drSupportMetaForApp(app).title || 'This namespace contains storage that is not eligible for DR.';
    return {
      code: '100201',
      title: 'Unsupported DR Storage',
      description: `${namespace} contains storage that is not eligible for DR.`,
      fullText: details,
    };
  };
  const drSupportFailureDetailsForApp = (app: AppItem) => {
    const checks = unsupportedDRChecksForApp(app);
    if (checks.length === 0) {
      return drSupportFailureForApp(app).fullText.split('\n').map(line => line.trim()).filter(Boolean);
    }
    return [
      'Storage type is not supported for DR.',
      'Supported: stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn.',
      'Unsupported: local-path, hostPath, and local PV storage.',
      ...checks.map(check => `${check.namespace} / ${check.pvc}: storageClass=${check.storageClass}, volumeType=${check.pvType}, provisioner=${check.provisioner}`),
      'Impact: this namespace cannot be moved to Setup DR until its PVCs use supported storage.',
    ];
  };
  const renderDRSupportBadge = (app: AppItem) => {
    const meta = drSupportMetaForApp(app);
    const Icon = meta.tone === 'unsupported' ? AlertTriangle : meta.tone === 'warning' ? AlertCircle : meta.tone === 'unknown' ? Clock : CheckCircle2;
    const checks = unsupportedDRChecksForApp(app);
    const badgeContent = (
      <>
        <Icon size={13} />
        <span>{meta.label}</span>
      </>
    );
    return (
      <span className="hbdr-dr-support-wrap">
        {meta.tone === 'unsupported' ? (
          <button
            type="button"
            className={`hbdr-dr-support-badge hbdr-dr-support-${meta.tone}`}
            aria-label={meta.title}
            onClick={event => {
              event.stopPropagation();
              setDrSupportErrorDetail(app);
            }}
          >
            {badgeContent}
          </button>
        ) : (
          <span className={`hbdr-dr-support-badge hbdr-dr-support-${meta.tone}`} aria-label={meta.title}>
            {badgeContent}
          </span>
        )}
        {meta.tone === 'unsupported' && (
          <span className="hbdr-dr-support-error-popover" role="tooltip">
            <strong><AlertTriangle size={15} /> Storage type is not supported</strong>
            <em>This namespace cannot be moved to Setup DR until its PVCs use supported storage.</em>
            <span className="hbdr-dr-support-error-section">
              <b>Supported storage</b>
              <small>Stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn.</small>
            </span>
            <span className="hbdr-dr-support-error-section">
              <b>Unsupported storage</b>
              <small>local-path, hostPath, and local PV storage.</small>
            </span>
            {checks.length > 0 && (
              <span className="hbdr-dr-support-error-section">
                <b>Detected in this namespace</b>
                <span className="hbdr-dr-support-error-list">
                  {checks.slice(0, 6).map((check, index) => (
                    <small key={`${check.namespace}-${check.pvc}-${index}`}>
                      {check.namespace} / {check.pvc}: {check.storageClass} · {check.pvType} · {check.provisioner}
                    </small>
                  ))}
                  {checks.length > 6 && <small>+{checks.length - 6} more PVCs</small>}
                </span>
              </span>
            )}
          </span>
        )}
      </span>
    );
  };
  const appSortValue = (app: AppItem, column: string): string | number => {
    const profile = profileOf(app);
    const task = taskForUnit(syncTasks, app);
    const recoveryTask = taskForUnit(liveRecoveryTasks, app);
    if (column === 'name') return app.name;
    if (column === 'namespace') return unitNamespaces(app).join(' ');
    if (column === 'status') return app.status || '';
    if (column === 'resource') return resourceSummaryTotalForUnit(app);
    if (column === 'drSupport') return drSupportMetaForApp(app).sort;
    if (resourceCategoryMeta.some(meta => meta.key === column)) {
      return normalizeResourceCategories(app).find(category => category.key === column)?.total || 0;
    }
    if (column === 'protection') return app.stage === 'run' ? 2 : app.stage === 'config' ? 1 : 0;
    if (column === 'drStatus') return drStatusMetaForApp(app).label;
    if (column === 'score') return profile.score;
    if (column === 'scope') return profile.scopeTag ? `${formatScopeLabel(profile.scope)} ${profile.scopeTag}` : formatScopeLabel(profile.scope);
    if (column === 'policy') return app.policy || '';
    if (column === 'repository') return app.storage || '';
    if (column === 'targetCluster') return app.targetCluster || 'Backup only';
    if (column === 'task') return task ? `${task.status} ${task.progress}` : 'no task';
    if (column === 'recoveryTask') return recoveryTask ? `${recoveryTask.type} ${recoveryTask.status} ${recoveryTask.progress}` : 'no recovery task';
    if (column === 'createdAt') return app.protectionPlanCreatedAt || '';
    if (column === 'tags') return (app.tags || [])
      .map(tagId => tags.find(item => item.id === tagId)?.name || tagId)
      .join(' ');
    if (column === 'lastBackup') return app.lastBackup || '';
    return queryValueForApp(app, column);
  };
  const visibleNamespaceRows = namespaceRows.filter(matchesQuery);
  const visibleProtectedRows = protectedRows.filter(matchesQuery);
  const stageCards = [
    { key: 'select' as const, title: 'Select Application', desc: 'Add namespaces to the platform for protection.', list: 'List - Unconfigured application resources', count: selectRows.filter(app => !isDRUnsupported(app)).length, metric: 'Eligible Apps', icon: Layers, tone: 'blue' },
    { key: 'config' as const, title: 'Setup DR', desc: 'Configure DR strategy for the application.', list: 'List - Application resources to be configured', count: pendingCount, metric: 'Pending Configuration', icon: ShieldCheck, tone: 'orange' },
    { key: 'run' as const, title: 'Start DR', desc: 'Operate the configured applications.', list: 'List - Application resources have been configured', count: protectedCount, metric: 'Protected', icon: Play, tone: 'green' },
  ];
  const activeStage = stageCards.find(card => card.key === stage) || stageCards[0];
  const isRunStage = stage === 'run';
  const currentRows = isRunStage ? visibleProtectedRows : visibleNamespaceRows;
  const appConfigColumns = [
    { value: 'drSupport', label: 'DR Support' },
    { value: 'resource', label: 'Resource' },
    { value: 'tags', label: 'Tags' },
  ];
  const appRunColumns = [
    { value: 'resource', label: 'Resource' },
    { value: 'drStatus', label: 'DR Config' },
    { value: 'scope', label: 'Scope' },
    { value: 'policy', label: 'Policy' },
    { value: 'repository', label: 'Repository' },
    { value: 'targetCluster', label: 'Target Cluster' },
    { value: 'task', label: 'Sync Status' },
    { value: 'recoveryTask', label: 'Recovery Status' },
    { value: 'tags', label: 'Tags' },
    { value: 'createdAt', label: 'Create Time' },
  ];
  const visibleAppColumns = isRunStage ? visibleRunColumns : visibleConfigColumns;
  const appColumns = isRunStage ? appRunColumns : appConfigColumns;
  const appQueryFields = listToolbarQueryFields([
    { value: 'namespace', label: 'Namespace' },
  ], appColumns, visibleAppColumns);
  const appFilterChoices = isRunStage
    ? [
      { value: 'protected', label: 'Protected', count: currentRows.filter(app => appMatchesFilter(app, 'protected')).length },
      { value: 'syncing', label: 'Syncing', count: currentRows.filter(app => appMatchesFilter(app, 'syncing')).length },
      { value: 'recovering', label: 'Recovering', count: currentRows.filter(app => appMatchesFilter(app, 'recovering')).length },
      { value: 'completed', label: 'Has Restore Point', count: currentRows.filter(app => appMatchesFilter(app, 'completed')).length },
      { value: 'scheduled', label: 'Schedule Enabled', count: currentRows.filter(app => appMatchesFilter(app, 'scheduled')).length },
      { value: 'manualOnly', label: 'Manual Only', count: currentRows.filter(app => appMatchesFilter(app, 'manualOnly')).length },
      { value: 'warning', label: 'Needs Attention', count: currentRows.filter(app => appMatchesFilter(app, 'warning')).length },
    ]
    : [
      { value: 'active', label: 'Runtime Active', count: currentRows.filter(app => appMatchesFilter(app, 'active')).length },
      { value: 'supported', label: 'DR Supported', count: currentRows.filter(app => appMatchesFilter(app, 'supported')).length },
      { value: 'unsupported', label: 'DR Unsupported', count: currentRows.filter(app => appMatchesFilter(app, 'unsupported')).length },
      { value: 'notChecked', label: 'Not Checked', count: currentRows.filter(app => appMatchesFilter(app, 'notChecked')).length },
      { value: 'hasPvc', label: 'Has PVC', count: currentRows.filter(app => appMatchesFilter(app, 'hasPvc')).length },
      { value: 'stateless', label: 'Stateless', count: currentRows.filter(app => appMatchesFilter(app, 'stateless')).length },
      { value: 'warning', label: 'Needs Attention', count: currentRows.filter(app => appMatchesFilter(app, 'warning')).length },
    ];
  const appColumnMinWidth = (column: string) => {
    if (column === 'name') return 150;
    if (column === 'resource') return 420;
    if (column === 'drSupport') return 126;
    if (column === 'protection') return 116;
    if (column === 'drStatus') return 144;
    if (column === 'score') return 108;
    if (column === 'scope') return 96;
    if (column === 'policy') return 104;
    if (column === 'repository') return 130;
    if (column === 'targetCluster') return 140;
    if (column === 'task') return 160;
    if (column === 'recoveryTask') return 150;
    if (column === 'tags') return 96;
    if (column === 'createdAt') return 150;
    return 92;
  };
  const resourceCategoryTitle = (app: AppItem, category: ResourceCategory): string => {
    const items = (category.items || []).filter(item => item.count > 0);
    if (category.key === 'storage') {
      const capacity = app.pvCapacityBytes || app.resourceSummary?.pvCapacityBytes || 0;
      const capacityLabel = capacity > 0 ? formatBytes(capacity) : '0 B';
      const pvcItem = items.find(item => item.kind === 'PersistentVolumeClaim');
      const pvcLines = (pvcItem?.resources || []).slice(0, 8).map(resource => {
        const capacityValue = resource.fields?.CAPACITY || resource.fields?.Capacity || '';
        const statusValue = resource.fields?.STATUS || resource.fields?.Status || '';
        const storageClass = resource.fields?.STORAGECLASS || resource.fields?.StorageClass || '';
        return [resource.name, statusValue, storageClass, capacityValue].filter(Boolean).join(' · ');
      });
      return [`Storage`, `${category.total} PVC${category.total === 1 ? '' : 's'} · ${capacityLabel} total`, ...pvcLines].join('\n');
    }
    return [category.label, ...items.map(item => `${item.kind}: ${item.count}`)].join('\n');
  };
  const renderResourceSummaryCell = (app: AppItem) => {
    if (app.memberApps?.length) {
      return (
        <div className="hbdr-merged-resource-list">
          {app.memberApps.map(member => (
            <div key={member.name} className="hbdr-merged-resource-row">
              <strong>{member.namespace || member.name}</strong>
              {renderResourceSummaryCell(member)}
            </div>
          ))}
        </div>
      );
    }
    const categories = normalizeResourceCategories(app).filter(category => category.total > 0);
    if (categories.length === 0) {
      return <span className="hbdr-dr-resource-empty" aria-label="No namespaced resources found" />;
    }
    return (
      <div className="hbdr-dr-resource-summary">
        {categories.slice(0, 8).map(category => {
          const Icon = resourceCategoryIconMap[category.key as ResourceCategoryKey] || MoreVertical;
          const capacity = app.pvCapacityBytes || app.resourceSummary?.pvCapacityBytes || 0;
          const capacityLabel = category.key === 'storage' && capacity > 0 ? formatBytes(capacity) : '';
          return (
          <button
            key={category.key}
            type="button"
            className={`hbdr-dr-resource-summary-chip hbdr-dr-resource-summary-${category.key}`}
            title={resourceCategoryTitle(app, category)}
            aria-label={resourceCategoryTitle(app, category)}
            onClick={event => {
              event.stopPropagation();
              setResourceDetail({ app });
            }}
          >
            <Icon size={13} />
            <strong>{category.total}</strong>
            {capacityLabel && <em>{capacityLabel}</em>}
          </button>
          );
        })}
      </div>
    );
  };
  const resourceDetailGroups = resourceDetail
    ? normalizeResourceCategories(resourceDetail.app)
      .map(category => ({
        category,
        items: (category.items || []).filter(item => item.count > 0),
      }))
      .filter(group => group.items.length > 0)
    : [];
  const renderAppTags = (app: AppItem) => {
    const appTags = app.tags || [];
    if (appTags.length === 0) return <span className="hbdr-app-tag-empty" aria-label="No tags" />;
    return (
      <span className="hbdr-app-tag-list">
        {appTags.map(tagId => {
          const tag = tags.find(item => item.id === tagId);
          return <em key={tagId}>{tag?.name || tagId}</em>;
        })}
      </span>
    );
  };
  const selectedNames = stage === 'select' ? selectedSelectApps : stage === 'config' ? selectedConfigApps : selectedRunApps;
  const setSelectedNames = stage === 'select' ? setSelectedSelectApps : stage === 'config' ? setSelectedConfigApps : setSelectedRunApps;
  const selectableCurrentRows = currentRows;
  const selectedApps = selectedNames
    .map(name => currentRows.find(app => app.name === name) || apps.find(app => app.name === name))
    .filter((app): app is AppItem => Boolean(app));
  const selectedUnsupportedDRApp = stage === 'select' ? selectedApps.find(isDRUnsupported) || null : null;
  const selectedHasUnsupportedDR = Boolean(selectedUnsupportedDRApp);
  const unsupportedDRSelectionMessage = selectedUnsupportedDRApp
    ? `[100201] Unsupported DR Storage. ${selectedUnsupportedDRApp.namespace || selectedUnsupportedDRApp.name} contains storage that is not eligible for DR.`
    : '';
  const primaryDisabled = selectedNames.length === 0 || selectedHasUnsupportedDR;
  const selectedAttachedTagIds = Array.from(new Set(selectedApps.flatMap(app => app.tags || [])));
  const attachTagToSelected = (tagId: string) => {
    updateAppTags(currentClusterId, selectedNames, currentTags => Array.from(new Set([...currentTags, tagId])));
    setTagAction(null);
    setAppBulkMenuOpen(false);
    toast(`Tag attached to ${selectedNames.length} application${selectedNames.length === 1 ? '' : 's'}`);
  };
  const detachTagFromSelected = (tagId: string) => {
    updateAppTags(currentClusterId, selectedNames, currentTags => currentTags.filter(item => item !== tagId));
    setTagAction(null);
    setAppBulkMenuOpen(false);
    toast(`Tag detached from ${selectedNames.length} application${selectedNames.length === 1 ? '' : 's'}`);
  };
  const refreshNamespaceResources = async (app: AppItem) => {
    const clusterId = app.clusterId || currentClusterId;
    const namespace = app.namespace || app.name;
    if (!clusterId) {
      toast('Select a source cluster first');
      return;
    }
    const refreshKey = `${clusterId}:${namespace}`;
    setResourceRefreshKey(refreshKey);
    setResourceRefreshStatus({ key: refreshKey, status: 'pending', message: 'Requesting latest resources...' });
    try {
      const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
        scope: 'namespaceResources',
        namespace,
        includeDetails: true,
        reason: 'resource_modal_refresh',
        includeRecentVeleroObjects: false,
      });
      setResourceRefreshStatus({ key: refreshKey, status: 'pending', message: 'Waiting for agent response...' });
      for (let attempt = 0; attempt < 12; attempt += 1) {
        await new Promise(resolve => window.setTimeout(resolve, 1000));
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${clusterId}/inventory/requests/${submitted.requestId}`);
        if (status.status === 'succeeded') {
          setResourceRefreshStatus({ key: refreshKey, status: 'succeeded', message: 'Resource inventory updated.' });
          await refreshPlatformData();
          toast('Resource inventory updated');
          return;
        }
        if (status.status === 'failed' || status.status === 'timeout') {
          const message = status.message || status.errorCode || 'Resource refresh failed';
          setResourceRefreshStatus({ key: refreshKey, status: status.status, message });
          toast(message);
          return;
        }
      }
      setResourceRefreshStatus({ key: refreshKey, status: 'timeout', message: 'Agent response timed out.' });
      toast('Agent response timed out');
    } catch (error) {
      setResourceRefreshStatus({ key: refreshKey, status: 'failed', message: error instanceof Error ? error.message : 'unknown error' });
      toast('Failed to request resource refresh: ' + (error instanceof Error ? error.message : 'unknown error'));
    } finally {
      window.setTimeout(() => {
        setResourceRefreshKey(current => current === refreshKey ? '' : current);
      }, 1200);
    }
  };
  const refreshCurrentClusterInventory = async () => {
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    try {
      const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${currentClusterId}/inventory/request`, {
        scope: 'summary',
        includeDetails: true,
        reason: 'application_dr_manual_refresh',
        includeRecentVeleroObjects: false,
      });
      for (let attempt = 0; attempt < 35; attempt += 1) {
        await new Promise(resolve => window.setTimeout(resolve, 1000));
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${currentClusterId}/inventory/requests/${submitted.requestId}`);
        if (status.status === 'succeeded') {
          await refreshPlatformData();
          toast('Namespace inventory updated');
          return;
        }
        if (status.status === 'failed' || status.status === 'timeout') {
          toast(status.message || status.errorCode || 'Inventory refresh failed');
          return;
        }
      }
      toast('Inventory refresh timed out');
    } catch (error) {
      toast('Failed to request inventory refresh: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const checkDRSupportBeforeStageMove = async (targetApps: AppItem[]): Promise<ApiApplication[]> => {
    const requests = await Promise.all(targetApps.map(app => {
      const clusterId = app.clusterId || currentClusterId;
      const namespace = app.namespace || app.name;
      if (!clusterId) throw new Error(`Missing cluster for ${namespace}`);
      return apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
        scope: 'namespaceResources',
        namespace,
        includeDetails: true,
        reason: 'dr_support_precheck',
        includeRecentVeleroObjects: false,
      }).then(response => ({ clusterId, namespace, requestId: response.requestId }));
    }));

    const pending = new Map(requests.map(request => [request.requestId, request]));
    for (let attempt = 0; attempt < 35 && pending.size > 0; attempt += 1) {
      await new Promise(resolve => window.setTimeout(resolve, 1000));
      for (const request of Array.from(pending.values())) {
        const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${request.clusterId}/inventory/requests/${request.requestId}`);
        if (status.status === 'succeeded') {
          pending.delete(request.requestId);
        } else if (status.status === 'failed' || status.status === 'timeout') {
          throw new Error(`${request.namespace}: ${status.message || status.errorCode || 'DR support check failed'}`);
        }
      }
    }
    if (pending.size > 0) {
      throw new Error('DR support check timed out. Please refresh inventory and try again.');
    }
    await refreshPlatformData();
    const latest = await apiGet<ApiList<ApiApplication>>('/api/v1/applications');
    return latest.items;
  };
  useEffect(() => {
    if (stage !== 'select') return;
    const unknownApps = selectRows.filter(app => isDRSupportUnknown(app));
    if (unknownApps.length === 0) return;
    const unknownKeys = Array.from(new Set(unknownApps.map(drSupportKeyForApp)));
    if (unknownKeys.every(key => drSupportAutoRequestedRef.current.has(key))) return;
    unknownKeys.forEach(key => drSupportAutoRequestedRef.current.add(key));
    setDrSupportCheckingKeys(prev => Array.from(new Set([...prev, ...unknownKeys])));

    const probe = unknownApps[0];
    const clusterId = probe.clusterId || currentClusterId;
    const namespace = probe.namespace || probe.name;
    if (!clusterId) {
      setDrSupportCheckingKeys(prev => prev.filter(key => !unknownKeys.includes(key)));
      return;
    }

    let cancelled = false;
    void (async () => {
      try {
        const submitted = await apiPost<{ status: string; warning?: string; requestId: string }>(`/api/v1/clusters/${clusterId}/inventory/request`, {
          scope: 'namespaceResources',
          namespace,
          includeDetails: true,
          reason: 'dr_support_auto_check',
          includeRecentVeleroObjects: false,
        });
        for (let attempt = 0; attempt < 35; attempt += 1) {
          await new Promise(resolve => window.setTimeout(resolve, 1000));
          const status = await apiGet<{ status: string; message?: string; errorCode?: string }>(`/api/v1/clusters/${clusterId}/inventory/requests/${submitted.requestId}`);
          if (status.status === 'succeeded') {
            if (!cancelled) await refreshPlatformData();
            return;
          }
          if (status.status === 'failed' || status.status === 'timeout') {
            if (!cancelled) toast('DR support check failed: ' + (status.message || status.errorCode || 'unknown error'));
            return;
          }
        }
        if (!cancelled) toast('DR support check timed out. Please refresh inventory and try again.');
      } catch (error) {
        if (!cancelled) toast('DR support check failed: ' + (error instanceof Error ? error.message : 'unknown error'));
      } finally {
        if (!cancelled) {
          setDrSupportCheckingKeys(prev => prev.filter(key => !unknownKeys.includes(key)));
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [stage, selectRows, currentClusterId]);
  const allVisibleSelected = selectableCurrentRows.length > 0 && selectableCurrentRows.every(app => selectedNames.includes(app.name));
  const toggleVisibleRows = () => {
    setSelectedNames(allVisibleSelected ? [] : selectableCurrentRows.map(app => app.name));
  };
  const toggleSelectedName = (name: string) => {
    setSelectedNames(prev => prev.includes(name) ? prev.filter(item => item !== name) : [...prev, name]);
  };
  const appSelectColumn: HyperTableColumn<AppItem> = {
    id: 'select',
    header: () => (
      <input
        type="checkbox"
        checked={allVisibleSelected}
        disabled={selectableCurrentRows.length === 0}
        onClick={event => event.stopPropagation()}
        onChange={toggleVisibleRows}
      />
    ),
    cell: info => {
      return (
        <input
          type="checkbox"
          checked={selectedNames.includes(info.row.original.name)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedName(info.row.original.name)}
        />
      );
    },
    size: 42,
    minSize: 42,
    maxSize: 54,
    enableSorting: false,
    enableResizing: false,
    meta: { align: 'center' },
  };
  const appNameColumn: HyperTableColumn<AppItem> = {
    id: 'name',
    header: 'Namespace',
    accessorFn: app => unitNamespaces(app).join(' '),
    size: 240,
    minSize: 150,
    maxSize: 520,
    cell: info => {
      const app = info.row.original;
      const namespaces = unitNamespaces(app);
      return (
        <div className="hbdr-dr-name-cell">
          <div className="hbdr-dr-namespace-icon"><Layers size={18} /></div>
          <div>
            {app.memberApps?.length ? (
              <div className="hbdr-merged-namespace-list">
                {namespaces.map(namespace => <p key={namespace} className="hbdr-dr-app-name">{namespace}</p>)}
              </div>
            ) : (
              <p className="hbdr-dr-app-name">{app.name}</p>
            )}
          </div>
        </div>
      );
    },
    meta: { title: app => unitNamespaces(app).join(', ') },
  };
  const appConfigTableColumns = useMemo<HyperTableColumn<AppItem>[]>(() => {
    const columns: HyperTableColumn<AppItem>[] = [appSelectColumn, appNameColumn];
    if (visibleConfigColumns.includes('status')) {
      columns.push({
        id: 'status',
        header: 'Status',
        accessorFn: app => appSortValue(app, 'status'),
        size: 120,
        minSize: 92,
        maxSize: 220,
        cell: info => {
          const app = info.row.original;
          return <span className={`hbdr-dr-status hbdr-dr-status-${(app.status || 'unknown').toLowerCase()}`}>{app.status || 'Unknown'}</span>;
        },
        meta: { title: app => app.status || 'Unknown' },
      });
    }
    if (stage === 'select' && visibleConfigColumns.includes('drSupport')) {
      columns.push({
        id: 'drSupport',
        header: 'DR Support',
        accessorFn: app => appSortValue(app, 'drSupport'),
        size: 150,
        minSize: appColumnMinWidth('drSupport'),
        maxSize: 240,
        cell: info => renderDRSupportBadge(info.row.original),
        meta: { className: 'hbdr-dr-support-cell' },
      });
    }
    if (visibleConfigColumns.includes('resource')) {
      columns.push({
        id: 'resource',
        header: 'Resource',
        accessorFn: app => appSortValue(app, 'resource'),
        size: 480,
        minSize: appColumnMinWidth('resource'),
        maxSize: 760,
        cell: info => renderResourceSummaryCell(info.row.original),
        meta: { title: app => `${resourceSummaryTotalForUnit(app)} namespaced resources` },
      });
    }
    if (visibleConfigColumns.includes('tags')) {
      columns.push({
        id: 'tags',
        header: 'Tags',
        accessorFn: app => appSortValue(app, 'tags'),
        size: 112,
        minSize: 96,
        maxSize: 260,
        cell: info => renderAppTags(info.row.original),
        meta: { title: app => appSortValue(app, 'tags').toString() },
      });
    }
    return columns;
  }, [allVisibleSelected, selectableCurrentRows, selectedNames, visibleConfigColumns, tags, syncTasks, liveRecoveryTasks, stage]);
  const appRunTableColumns = useMemo<HyperTableColumn<AppItem>[]>(() => {
    const columns: HyperTableColumn<AppItem>[] = [appSelectColumn, appNameColumn];
    if (visibleRunColumns.includes('drStatus')) {
      columns.push({
        id: 'drStatus',
        header: 'DR Config',
        accessorFn: app => appSortValue(app, 'drStatus'),
        size: 168,
        minSize: 144,
        maxSize: 260,
        cell: info => {
          const app = info.row.original;
          const plan = protectionPlanForApp(app);
          const meta = drStatusForPlan(plan?.status);
          const retryable = canRetryDrActivation(plan?.status);
          const normalizedPlanStatus = (plan?.status || '').toLowerCase();
          const canReconfigureStorage = normalizedPlanStatus === 'storage_failed' || normalizedPlanStatus === 'active_with_warning';
          const reconfigureTitle = normalizedPlanStatus === 'active_with_warning'
            ? 'Retry target BackupStorageLocation configuration'
            : 'Reconfigure BackupStorageLocation';
          const reconfigureLabel = normalizedPlanStatus === 'active_with_warning' ? 'Retry BSL' : 'Reconfigure';
          return (
            <span className="hbdr-dr-status-cell">
              <span className={`hbdr-dr-status hbdr-dr-status-${meta.tone}`} title={meta.title}>
                {meta.tone === 'ok' && <CheckCircle2 size={14} />}
                {meta.tone === 'progress' && <RefreshCw size={14} />}
                {meta.tone === 'warn' && <AlertTriangle size={14} />}
                {meta.label}
              </span>
              {retryable && (
                <button
                  type="button"
                  className="hbdr-dr-status-retry"
                  title="Retry activation"
                  onClick={event => {
                    event.stopPropagation();
                    void retryDrActivation(app);
                  }}
                >
                  <RefreshCw size={12} />
                  Retry
                </button>
              )}
              {canReconfigureStorage && (
                <button
                  type="button"
                  className="hbdr-dr-status-retry"
                  title={reconfigureTitle}
                  onClick={event => {
                    event.stopPropagation();
                    void reconfigureDrStorage(app);
                  }}
                >
                  <RefreshCw size={12} />
                  {reconfigureLabel}
                </button>
              )}
            </span>
          );
        },
        meta: { title: app => drStatusMetaForApp(app).title },
      });
    }
    if (visibleRunColumns.includes('score')) {
      columns.push({
        id: 'score',
        header: 'Compliance',
        accessorFn: app => appSortValue(app, 'score'),
        size: 136,
        minSize: 108,
        maxSize: 220,
        cell: info => {
          const profile = profileOf(info.row.original);
          return <span className={`hbdr-dr-score ${profile.score === 100 ? 'hbdr-dr-score-ok' : 'hbdr-dr-score-warn'}`}><strong>{profile.score}</strong><em>POINTS</em></span>;
        },
      });
    }
    if (visibleRunColumns.includes('scope')) {
      columns.push({
        id: 'scope',
        header: 'Scope',
        accessorFn: app => appSortValue(app, 'scope'),
        size: 116,
        minSize: 96,
        maxSize: 260,
        cell: info => {
          const profile = profileOf(info.row.original);
          return <span className="hbdr-dr-scope">{formatScopeLabel(profile.scope)}{profile.scopeTag && <em>{profile.scopeTag}</em>}</span>;
        },
        meta: { title: app => String(appSortValue(app, 'scope')) },
      });
    }
    if (visibleRunColumns.includes('policy')) {
      columns.push({
        id: 'policy',
        header: 'Policy',
        accessorFn: app => appSortValue(app, 'policy'),
        size: 122,
        minSize: 104,
        maxSize: 260,
        cell: info => <span className="hbdr-dr-policy"><Clock size={15} />{info.row.original.policy || 'Daily Incremental'}</span>,
        meta: { title: app => app.policy || 'Daily Incremental' },
      });
    }
    if (visibleRunColumns.includes('repository')) {
      columns.push({
        id: 'repository',
        header: 'Repository',
        accessorFn: app => appSortValue(app, 'repository'),
        size: 190,
        minSize: 170,
        maxSize: 340,
        cell: info => {
          const usage = planStorageSizeForApp(info.row.original);
          return (
            <span className="hbdr-dr-storage-cell" title={usage.title}>
              <span className="hbdr-dr-storage">
                <Database size={15} />
                <span className="hbdr-dr-storage-text">
                  <strong>{info.row.original.storage || 'AWS-...'}</strong>
                </span>
              </span>
              {usage.label && <em className="hbdr-dr-storage-used">{usage.label}</em>}
            </span>
          );
        },
        meta: { title: app => planStorageSizeForApp(app).title || app.storage || 'AWS-...' },
      });
    }
    if (visibleRunColumns.includes('targetCluster')) {
      columns.push({
        id: 'targetCluster',
        header: 'Target Cluster',
        accessorFn: app => appSortValue(app, 'targetCluster'),
        size: 150,
        minSize: 140,
        maxSize: 300,
        cell: info => {
          const target = info.row.original.targetCluster;
          return <span className={`hbdr-dr-target ${target ? '' : 'hbdr-dr-target-empty'}`}><Server size={15} />{target || 'Backup only'}</span>;
        },
        meta: { title: app => app.targetCluster || 'Backup only' },
      });
    }
    if (visibleRunColumns.includes('task')) {
      columns.push({
        id: 'task',
        header: 'Sync Status',
        accessorFn: app => appSortValue(app, 'task'),
        size: 300,
        minSize: 280,
        maxSize: 520,
        cell: info => <span>{renderSyncTaskStatus(info.row.original)}</span>,
      });
    }
    if (visibleRunColumns.includes('recoveryTask')) {
      columns.push({
        id: 'recoveryTask',
        header: 'Recovery Status',
        accessorFn: app => appSortValue(app, 'recoveryTask'),
        size: 340,
        minSize: 300,
        maxSize: 580,
        cell: info => <span>{renderRecoveryTaskStatus(info.row.original)}</span>,
      });
    }
    if (visibleRunColumns.includes('resource')) {
      columns.push({
        id: 'resource',
        header: 'Resource',
        accessorFn: app => appSortValue(app, 'resource'),
        size: 480,
        minSize: appColumnMinWidth('resource'),
        maxSize: 760,
        cell: info => renderResourceSummaryCell(info.row.original),
        meta: { title: app => `${resourceSummaryTotalForUnit(app)} namespaced resources` },
      });
    }
    if (visibleRunColumns.includes('tags')) {
      columns.push({
        id: 'tags',
        header: 'Tags',
        accessorFn: app => appSortValue(app, 'tags'),
        size: 112,
        minSize: 96,
        maxSize: 260,
        cell: info => renderAppTags(info.row.original),
        meta: { title: app => appSortValue(app, 'tags').toString() },
      });
    }
    if (visibleRunColumns.includes('createdAt')) {
      columns.push({
        id: 'createdAt',
        header: 'Create Time',
        accessorFn: app => appSortValue(app, 'createdAt'),
        size: 170,
        minSize: appColumnMinWidth('createdAt'),
        maxSize: 260,
        cell: info => {
          const value = info.row.original.protectionPlanCreatedAt;
          return <span className="text-xs font-semibold text-slate-500">{value ? formatDateTime(value) : '-'}</span>;
        },
        meta: { title: app => app.protectionPlanCreatedAt ? formatDateTime(app.protectionPlanCreatedAt) : '-' },
      });
    }
    return columns;
  }, [allVisibleSelected, selectedNames, visibleRunColumns, tags, syncTasks, liveRecoveryTasks, liveRestorePoints, protectionPlans]);
  const singleSelectedApp = selectedNames.length === 1 ? currentRows.find(app => app.name === selectedNames[0]) || apps.find(app => app.name === selectedNames[0]) || null : null;
  const selectedRunRows = selectedRunApps
    .map(name => protectedRows.find(app => app.name === name) || apps.find(app => app.name === name))
    .filter((app): app is AppItem => Boolean(app));
  const selectedRunNotReadyRows = selectedRunRows.filter(app => !isProtectionPlanReady(protectionPlanForApp(app)?.status));
  const selectedRunHasCleanupRunning = selectedRunRows.some(app => isProtectionPlanCleaning(protectionPlanForApp(app)?.status));
  const selectedRunActiveSyncTasks = selectedRunRows
    .map(row => taskForUnit(syncTasks, row))
    .filter((task): task is ApiTask => Boolean(task?.id && isActiveTaskStatus(task.status)));
  const selectedRunCancelableSyncTasks = Array.from(new Map(
    selectedRunActiveSyncTasks
      .filter(task => (task.status || '').toLowerCase() !== 'canceling')
      .map(task => [task.id, task] as const),
  ).values());
  const selectedRunHasRunningSync = selectedRunActiveSyncTasks.length > 0;
  const canStartSync = selectedRunApps.length > 0 && !selectedRunHasRunningSync && !selectedRunHasCleanupRunning && selectedRunNotReadyRows.length === 0;
  const canStopSync = selectedRunCancelableSyncTasks.length > 0;
  const restorePointsForApp = (app: AppItem) => {
    const realPoints = liveRestorePoints
      .filter(point => {
        if (point.status !== 'available') return false;
        if (point.sourceClusterId !== currentClusterId) return false;
        if (app.apiId && point.appId) return point.appId === app.apiId;
        if (app.protectionPlanId && point.protectionPlanId) return point.protectionPlanId === app.protectionPlanId;
        return point.sourceClusterId === currentClusterId && point.sourceNamespace === (app.namespace || app.name);
      })
      .sort((a, b) => (b.time || '').localeCompare(a.time || ''))
      .map(point => ({
        id: point.id,
        backupTaskId: point.backupTaskId,
        veleroBackupName: point.veleroBackupName,
        title: point.title || point.veleroBackupName || 'Restore point',
        rawTime: point.time,
        time: restorePointDisplayLabel(point),
        type: point.pointType === 'local' ? 'Local Snapshot' : 'Remote Snapshot',
        status: 'Available',
      }));
    if (realPoints.length > 0) return realPoints;
    return [];
  };
  const selectedRunActiveRecoveryTask = selectedRunRows.length === 1 ? taskForUnit(liveRecoveryTasks, selectedRunRows[0]) : undefined;
  const hasSelectedRunActiveRecoveryTask = isActiveTaskStatus(selectedRunActiveRecoveryTask?.status);
  const canRestoreAction = selectedRunRows.length === 1 && isProtectionPlanReady(protectionPlanForApp(selectedRunRows[0])?.status) && restorePointsForApp(selectedRunRows[0]).length > 0 && !hasSelectedRunActiveRecoveryTask;
  const buildRecoveryDraft = (mode: 'drill' | 'takeover', app: AppItem, pointId: string): RecoveryWizardConfig => {
    const selectedPoint = restorePointsForApp(app).find(point => point.id === pointId);
    const sourceType = selectedPoint?.type.toLowerCase().includes('local') ? 'snapshot' : 'export';
    const currentClusterName = currentCluster?.name || clusters[0]?.name || '';
    const targetCluster = app.targetCluster || clusters.find(cluster => cluster.id !== currentCluster?.id)?.name || currentClusterName;
    const targetMode = targetCluster === currentClusterName
      ? mode === 'drill' ? 'sandbox' : 'inPlace'
      : 'crossCluster';
    const primaryNamespace = unitNamespaces(app)[0] || app.name;
    return {
      pointId,
      sourceType,
      targetMode,
      targetCluster,
      namespaceMode: mode === 'takeover' ? 'original' : 'generated',
      targetNamespace: mode === 'takeover' ? primaryNamespace : `${primaryNamespace}-drill`,
      restoreMode: 'full',
      artifactMode: 'all',
      conflictPolicy: mode === 'takeover' ? 'replace' : 'skip',
      originalNamespaceConfirmed: false,
      includeClusterScoped: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : targetMode === 'sandbox' ? 'drill' : 'none',
      storageProfileMode: 'original',
      alternateProfileId: '',
      preflightChecks: true,
      autoStartValidation: mode === 'drill',
      notes: mode === 'drill'
        ? 'Validate service startup, storage attachment, and application smoke test after recovery.'
        : 'Confirm routing cutover, service dependencies, and production freeze before takeover.',
    };
  };
  const openRestoreAction = (mode: 'drill' | 'takeover') => {
    if (selectedRunRows.length !== 1) {
      toast('Select one namespace first');
      return;
    }
    const activeTask = taskForUnit(liveRecoveryTasks, selectedRunRows[0]);
    if (isActiveTaskStatus(activeTask?.status)) {
      const label = activeTask?.type === 'drill' ? 'drill' : activeTask?.type === 'takeover' ? 'takeover' : 'recovery';
      toast(`A ${label} task is already running for this namespace`);
      return;
    }
    const points = restorePointsForApp(selectedRunRows[0]);
    if (points.length === 0) {
      toast('No recovery point is available for this namespace');
      return;
    }
    setRestoreAction({ mode, app: selectedRunRows[0], config: buildRecoveryDraft(mode, selectedRunRows[0], points[0].id) });
  };
  const confirmRestoreAction = async () => {
    if (!restoreAction) return;
    const point = restorePointsForApp(restoreAction.app).find(item => item.id === restoreAction.config.pointId);
    const livePoint = liveRestorePoints.find(item => item.id === restoreAction.config.pointId);
    if (!livePoint) {
      toast('Select a real restore point before starting recovery');
      return;
    }
    const sourceNamespaces = unitNamespaces(restoreAction.app);
    const targetNamespaces = restoreAction.config.namespaceMode === 'original'
      ? {}
      : sourceNamespaces.reduce<Record<string, string>>((acc, namespace) => {
          acc[namespace] = restoreAction.app.isMergedPlan ? `${namespace}-drill` : restoreAction.config.targetNamespace;
          return acc;
        }, {});
    const targetNamespace = restoreAction.config.namespaceMode === 'original'
      ? sourceNamespaces[0] || restoreAction.app.name
      : restoreAction.app.isMergedPlan ? targetNamespaces[sourceNamespaces[0]] || restoreAction.config.targetNamespace : restoreAction.config.targetNamespace;
    const targetCluster = clusters.find(cluster => cluster.name === restoreAction.config.targetCluster)
      || clusters.find(cluster => cluster.name === restoreAction.app.targetCluster)
      || clusters.find(cluster => cluster.id !== currentCluster?.id);
    if (!targetCluster) {
      toast('Select a target cluster before starting recovery');
      return;
    }
    const action = restoreAction;
    const submittedMessage = `${action.mode === 'drill' ? 'Drill' : 'Takeover'} job submitted: ${action.config.targetCluster} / ${targetNamespace} / ${point?.time || 'selected recovery point'}`;
    setRestoreAction(null);
    toast(submittedMessage);
    void (async () => {
      try {
        const createdTask = await apiPost<ApiTask>(`/api/v1/tasks/${action.mode}`, {
          clusterId: targetCluster.id,
          protectionPlanId: livePoint.protectionPlanId || action.app.protectionPlanId,
          restorePointId: livePoint.id,
          veleroBackupName: livePoint.veleroBackupName,
          sourceNamespace: sourceNamespaces[0] || livePoint.sourceNamespace || action.app.namespace || action.app.name,
          sourceNamespaces,
          targetNamespace,
          targetNamespaces,
          targetMode: action.config.targetMode,
          restoreMode: action.config.restoreMode,
          artifactMode: action.config.artifactMode,
          conflictPolicy: action.config.conflictPolicy,
          originalNamespaceConfirmed: action.config.originalNamespaceConfirmed,
          includeClusterScoped: action.config.includeClusterScoped,
          useTransforms: action.config.useTransforms,
          transformPreset: action.config.transformPreset,
          storageProfileMode: action.config.storageProfileMode,
          alternateProfileId: action.config.alternateProfileId,
        });
        setLiveRecoveryTasks(prev => ({ ...prev, [action.app.name]: createdTask }));
        setDrTaskEvents(prev => ({
          ...prev,
          [createdTask.id]: prev[createdTask.id] || [],
        }));
        await refreshPlatformData();
      } catch (error) {
        toast('Failed to submit recovery task: ' + (error instanceof Error ? error.message : 'unknown error'));
      }
    })();
  };
  const renderSyncTaskStatus = (app: AppItem) => {
    const task = taskForUnit(syncTasks, app);
    const sourceClusterId = unitMembers(app).map(member => member.clusterId).find(Boolean) || app.clusterId || currentClusterId;
    const sourceCluster = clusters.find(cluster => cluster.id === sourceClusterId) || currentCluster || clusters[0] || null;
    const sourceClusterOffline = Boolean(sourceCluster && sourceCluster.connectionStatus !== 'online');
    const appRestorePoints = restorePointsForApp(app);
    const taskPointId = task ? taskRestorePointId(task) : '';
    const taskRestorePoint = task
      ? appRestorePoints.find(point => (taskPointId && point.id === taskPointId) || point.backupTaskId === task.id)
      : undefined;
    const latestPoint = appRestorePoints[0];
    const taskBackupName = task ? String(task.payload?.veleroBackupName || task.payload?.backupName || '') : '';
    const taskVeleroRestorePoint = taskBackupName
      ? appRestorePoints.find(point => point.veleroBackupName === taskBackupName || point.title === taskBackupName || point.title.endsWith(`· ${taskBackupName}`))
      : undefined;
    const taskVisibleRestorePoint = taskRestorePoint || taskVeleroRestorePoint;
    const lastSnapshotTime = latestPoint?.time || '';
    const lastSnapshotLabel = lastSnapshotTime;
    const nextSyncHint = renderNextSyncHint(app);
    if (!task) {
      return lastSnapshotTime ? (
        <span className="hbdr-dr-last-snapshot">
          <strong>Last snapshot</strong>
          <em title={latestPoint?.veleroBackupName || latestPoint?.title || lastSnapshotLabel}>{lastSnapshotLabel}</em>
          {nextSyncHint}
        </span>
      ) : (
        <span className="hbdr-dr-sync-empty">
          <span className="hbdr-dr-task-neutral">No snapshot yet</span>
          {nextSyncHint}
        </span>
      );
    }

    if (isActiveTaskStatus(task.status)) {
      const events = drTaskEvents[task.id] || [];
      const finalizing = (task.status || '').toLowerCase() === 'finalizing';
      const canceling = (task.status || '').toLowerCase() === 'canceling';
      const waitingForAgent = sourceClusterOffline || (task.errorCode || '').toUpperCase() === 'AGENT_OFFLINE';
      const volume = taskProgressInfo(task, events);
      const hasVolumeBytes = Boolean(volume && volume.knownTotal && volume.totalBytes > 0);
      const configuringStorage = hasTaskEventReason(events, ['storage_preflight_started'])
        && !hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
      const progress = volume ? Math.max(0, Math.min(100, volume.percent)) : 0;
      const showProgressBar = !configuringStorage && !canceling && !finalizing && !waitingForAgent;
      if (waitingForAgent) {
        const failure = {
          code: normalizeErrorCode('AGENT_OFFLINE'),
          title: 'Cluster is offline',
          description: 'Reconnecting to source cluster agent. Pending tasks will resume automatically.',
          fullText: `Source cluster ${sourceCluster?.name || sourceClusterId || ''} agent is offline. The task will resume after the WebSocket connection is restored.`,
        };
        return (
          <TaskErrorStatus
            code={failure.code}
            title={failure.title}
            description={failure.description}
            detail={failure.fullText}
            onClick={event => {
              event.stopPropagation();
              setSyncTaskDetail({ app, task, failure });
            }}
          />
        );
      }
      const preparingMessage = syncPreparingMessage(events);
      const primary = canceling
        ? 'Canceling sync...'
        : finalizing
            ? 'Finalizing restore point...'
            : configuringStorage
              ? preparingMessage
              : volume
                ? `Syncing... ${formatPercent(progress)}%`
                : 'Syncing...';
      const details = !finalizing && hasVolumeBytes && volume
          ? [
              `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
              formatBytesPerSecond(volume.speedBytesPerSecond),
              formatEta(volume.etaSeconds),
            ].filter(Boolean).join(' · ')
          : '';
      return (
        <span className={`hbdr-dr-progress-cell ${canceling ? 'is-stopped' : 'is-syncing'}`}>
          <em className="hbdr-sync-label">{primary}</em>
          {showProgressBar && <i><b style={{ width: `${progress}%` }} /></i>}
          {details && <small>{details}</small>}
        </span>
      );
    }

    if (isCompletedTaskStatus(task.status)) {
      if (taskHasWarning(task)) {
        return (
          <button
            type="button"
            className="hbdr-dr-task-warning"
            onClick={event => {
              event.stopPropagation();
              setSyncTaskDetail({ app, task });
            }}
          >
            <span>Sync complete with warning</span>
            <em>{task.errorMessage || task.errorCode || 'View details'}</em>
          </button>
        );
      }
      const completedPoint = taskVisibleRestorePoint;
      const completedPointLabel = restorePointDisplayLabel(completedPoint) || 'Restore point creating...';
      return (
        <span className="hbdr-dr-last-snapshot">
          <strong>Sync complete</strong>
          <em title={completedPoint?.veleroBackupName || completedPoint?.title || completedPointLabel}>{completedPointLabel}</em>
          {nextSyncHint}
        </span>
      );
    }

    const failure = taskFailureSummary(task, drTaskEvents[task.id] || []);
    return (
      <TaskErrorStatus
        code={failure.code}
        title={failure.title}
        description={failure.description}
        detail={failure.fullText}
        onClick={event => {
          event.stopPropagation();
          setSyncTaskDetail({ app, task, failure });
        }}
      />
    );
  };
  const renderRecoveryTaskStatus = (app: AppItem) => {
    const task = taskForUnit(liveRecoveryTasks, app);
    if (!task) return <span className="hbdr-dr-task-neutral">No recovery task</span>;

    const actionText = recoveryActionText(task.type);
    const targetNamespace = String(task.payload?.targetNamespace || app.namespace || app.name);
    const targetClusterId = String(task.clusterId || task.payload?.targetClusterId || '');
    const targetClusterName = clusters.find(cluster => cluster.id === targetClusterId)?.name
      || String(task.payload?.targetCluster || app.targetCluster || '');
    if (isActiveTaskStatus(task.status)) {
      const events = drTaskEvents[task.id] || [];
      const volume = taskProgressInfo(task, events);
      const hasVolumeBytes = Boolean(volume && volume.knownTotal && volume.totalBytes > 0);
      const configuringStorage = hasTaskEventReason(events, ['storage_preflight_started'])
        && !hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
      const progress = volume ? Math.max(0, Math.min(100, volume.percent)) : Math.max(0, Math.min(100, task.progress || 0));
      const showProgressBar = !configuringStorage;
      const details = hasVolumeBytes && volume
        ? [
            `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
            formatBytesPerSecond(volume.speedBytesPerSecond),
            formatEta(volume.etaSeconds),
          ].filter(Boolean).join(' · ')
        : '';
      const preparingMessage = recoveryPreparingMessage(events, task.type);
      const primary = configuringStorage ? preparingMessage : `${actionText.running} ${formatPercent(progress)}%`;
      return (
        <span className="hbdr-dr-progress-cell hbdr-recovery-task-progress is-syncing">
          <em className="hbdr-sync-label">{primary}</em>
          {showProgressBar && <i><b style={{ width: `${progress}%` }} /></i>}
          {details && <small>{details}</small>}
        </span>
      );
    }

    if (isCompletedTaskStatus(task.status)) {
      const restorePointId = taskRestorePointId(task);
      const restorePoint = restorePointsForApp(app).find(point => point.id === restorePointId);
      const restorePointLabel = restorePointDisplayLabel(restorePoint) || 'restore point';
      const completedTitle = recoveryCompletedTargetTitle(restorePointLabel, task.completedAt, targetClusterName, targetNamespace, actionText.complete);
      const targetLabel = recoveryCompletedTargetLabel(targetClusterName, targetNamespace);
      return (
        <span className="hbdr-recovery-task-complete">
          <strong title={completedTitle}>[{restorePointLabel}] {actionText.complete.toLowerCase()}</strong>
          <em title={completedTitle}>{targetLabel}</em>
        </span>
      );
    }

    const failure = taskFailureSummary(task, drTaskEvents[task.id] || []);
    return (
      <TaskErrorStatus
        code={failure.code}
        title={failure.title}
        description={failure.description}
        detail={failure.fullText}
        onClick={event => {
          event.stopPropagation();
          setSyncTaskDetail({ app, task, failure });
        }}
      />
    );
  };
  const moveAppsToStage = async (names: string[], target: ApplicationStage) => {
    if (names.length === 0) return;
    const protection = target === 'run' ? 'protected' : target === 'config' ? 'pending_protection' : 'unprotected';
    const targetApps = apps.filter(app => names.includes(app.name) && app.apiId);
    if (targetApps.length === 0) {
      toast('No applications to update');
      return;
    }
    try {
      await Promise.all(targetApps.map(app => apiPatch(`/api/v1/applications/${app.apiId}`, { protectionStatus: protection })));
      setAppUiOverrides(prev => {
        const next = { ...prev };
        targetApps.forEach(app => {
          const key = appOverrideKey(app);
          next[key] = {
            ...(next[key] || {}),
            stage: target,
            protectionStatus: protection,
            isProtected: target === 'run',
            status: target === 'run' ? 'Protected' : mapApplicationStatus(app.status, false),
          };
        });
        return next;
      });
      toast(target === 'config' ? `Moved ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} to Setup DR`
        : target === 'select' ? `Moved ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} back to Select`
        : `Marked ${targetApps.length} application${targetApps.length === 1 ? '' : 's'} as protected`);
      void refreshPlatformData();
    } catch (error) {
      toast('Failed to update application stage: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const handlePrimaryAction = async () => {
    if (stage === 'select') {
      if (selectedSelectApps.length === 0) {
        toast('Select applications to protect first');
        return;
      }
      const selectedAppItems = selectedSelectApps
        .map(name => apps.find(app => app.name === name))
        .filter((app): app is AppItem => Boolean(app));
      const unchecked = selectedAppItems.filter(isDRSupportUnknown);
      if (unchecked.length > 0) {
        toast(`Checking DR support for ${unchecked.length} namespace${unchecked.length === 1 ? '' : 's'}...`);
        try {
          const latestApps = await checkDRSupportBeforeStageMove(unchecked);
          const latestByKey = new Map(latestApps.map(app => [`${app.clusterId}:${app.namespace || app.name}`, app]));
          const unresolved = selectedAppItems.filter(app => {
            const latest = latestByKey.get(`${app.clusterId || currentClusterId}:${app.namespace || app.name}`);
            return !latest?.resourceSummary?.drSupport?.status;
          });
          if (unresolved.length > 0) {
            toast(`${unresolved[0].namespace || unresolved[0].name} DR support is still not checked. Please refresh inventory and try again.`);
            return;
          }
          const unsupported = selectedAppItems.filter(app => {
            const latest = latestByKey.get(`${app.clusterId || currentClusterId}:${app.namespace || app.name}`);
            return (latest?.resourceSummary?.drSupport?.status || '').toLowerCase() === 'unsupported';
          });
          if (unsupported.length > 0) {
            setDrSupportErrorDetail(unsupported[0]);
            return;
          }
        } catch (error) {
          toast('DR support check failed: ' + (error instanceof Error ? error.message : 'unknown error'));
          return;
        }
      }
      const blocked = selectedAppItems.filter(isDRUnsupported);
      if (blocked.length > 0) {
        setDrSupportErrorDetail(blocked[0]);
        return;
      }
      const names = selectedSelectApps.filter(name => {
        const app = apps.find(item => item.name === name);
        return !app || !isDRUnsupported(app);
      });
      setSelectedSelectApps([]);
      await moveAppsToStage(names, 'config');
      setStage('config');
      return;
    }

    if (stage === 'config') {
      if (selectedConfigApps.length === 0) {
        toast('Select one application to configure protection');
        return;
      }
      setProtectWizardMode('create');
      setProtectWizardStep(1);
      setProtectWizardOpen(true);
      return;
    }

    if (!canStartSync) {
      const notReady = selectedRunNotReadyRows[0];
      if (selectedRunApps.length === 0) {
        toast('Select applications to start sync');
      } else if (notReady) {
        const meta = drStatusMetaForApp(notReady);
        toast(`DR configuration is not ready: ${meta.label}. ${meta.title}`);
      } else {
        toast('Selected applications are syncing. Please cancel or wait for completion first');
      }
      return;
    }

    if (selectedRunApps.length === 0) {
      toast('Select applications to start backup');
      return;
    }
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    const appsWithoutPlan = selectedRunRows.filter(app => !app.protectionPlanId);
    if (appsWithoutPlan.length > 0) {
      toast('Configure DR before starting sync');
      return;
    }
    let syncWarning = '';
    try {
      const responses = await Promise.all(selectedRunRows.map(app => {
        const plan = protectionPlanForApp(app);
        return apiPost<ApiTaskResponse>('/api/v1/tasks/backup', {
          clusterId: plan?.sourceClusterId || app.clusterId || currentClusterId,
          appId: app.isMergedPlan ? '' : app.apiId || '',
          protectionPlanId: app.protectionPlanId || '',
          sourceNamespace: app.isMergedPlan ? '' : app.namespace,
          sourceNamespaces: app.isMergedPlan ? unitNamespaces(app) : undefined,
          scope: 'namespace',
          labelSelector: '',
          storageRepo: app.storage || storage[0]?.name || 'default',
        });
      }));
      syncWarning = responses.map(response => 'warning' in response ? response.warning : '').find(Boolean) || '';
      setSyncTasks(prev => {
        const next = { ...prev };
        selectedRunRows.forEach((app, index) => {
          const response = responses[index];
          const task = 'task' in response ? response.task : response;
          if (task?.id) {
            next[app.name] = task;
            unitMembers(app).forEach(member => {
              next[member.name] = task;
            });
          }
        });
        return next;
      });
    } catch (error) {
      toast('Failed to submit sync job: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    }
    void refreshPlatformData();
    window.setTimeout(() => {
      void refreshPlatformData();
    }, 1300);
    setSelectedRunApps(selectedRunApps);
    toast(syncWarning || 'Sync job started');
  };
  const stopSync = async () => {
    if (!canStopSync) {
      toast(selectedRunApps.length === 0 ? 'Select applications to cancel sync' : 'Selected applications have no running sync job');
      return;
    }
    const taskCount = selectedRunCancelableSyncTasks.length;
    const confirmed = window.confirm(
      taskCount === 1
        ? 'Cancel this sync task? The source cluster agent will delete the running Velero backup. No restore point will be created for this sync.'
        : `Cancel ${taskCount} sync tasks? The source cluster agent will delete the running Velero backups. No restore point will be created for these syncs.`,
    );
    if (!confirmed) return;
    try {
      const responses = await Promise.all(selectedRunCancelableSyncTasks.map(task => apiPost<ApiTaskCancelResponse>(`/api/v1/tasks/${task.id}/cancel`, {})));
      setSyncTasks(prev => {
        const next = { ...prev };
        selectedRunRows.forEach(row => {
          const current = taskForUnit(next, row);
          const response = responses.find(item => item.task?.id === current?.id);
          if (response?.task) {
            next[row.name] = response.task;
            unitMembers(row).forEach(member => {
              next[member.name] = response.task;
            });
          }
        });
        return next;
      });
      await refreshPlatformData();
      const warning = responses.map(response => response.warning).find(Boolean);
      toast(warning ? warning.replace(/force stop/gi, 'cancel sync') : 'Cancel sync requested');
    } catch (error) {
      toast('Failed to cancel sync: ' + (error instanceof Error ? error.message : 'unknown error'));
    }
  };
  const modifyProtection = () => {
    if (selectedRunApps.length !== 1) {
      toast('Select one protected application to modify protection');
      return;
    }
    setProtectWizardMode('modify');
    setProtectWizardStep(1);
    setProtectWizardOpen(true);
  };
  const deleteDrConfiguration = async () => {
    if (selectedRunApps.length === 0) {
      toast('Select applications to cleanup resources');
      return;
    }
    const targetRows = selectedRunRows;
    const targetApps = targetRows.flatMap(app => app.memberApps?.length ? app.memberApps : [app]);
    const planIds = Array.from(new Set(targetRows.map(app => app.protectionPlanId).filter(Boolean) as string[]));
    const cleaningRows = targetRows.filter(app => isProtectionPlanCleaning(protectionPlanForApp(app)?.status));
    if (cleaningRows.length > 0) {
      toast('Selected resources are already being cleaned');
      return;
    }
    try {
      await Promise.all(planIds.map(planId => apiDelete<ApiProtectionPlan>(`/api/v1/protection-plans/${planId}`)));
      const appsWithoutPlan = targetApps.filter(app => !app.protectionPlanId && app.apiId);
      if (appsWithoutPlan.length > 0) {
        await Promise.all(appsWithoutPlan.map(app =>
          apiPatch(`/api/v1/applications/${app.apiId}`, { protectionStatus: 'pending_protection' })
        ));
      }
    } catch (error) {
      toast('Failed to cleanup resources: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    }
    setProtectionPlans(prev => prev.map(plan => planIds.includes(plan.id) ? { ...plan, status: 'cleanup_running' } : plan));
    setAppUiOverrides(prev => {
      const next = { ...prev };
      targetApps.forEach(app => {
        const key = appOverrideKey(app);
        next[key] = {
          ...(next[key] || {}),
          stage: 'run',
          protectionStatus: 'protected',
          isProtected: true,
          status: 'Protected',
        };
      });
      return next;
    });
    setSelectedRunApps([]);
    toast('Cleanup submitted');
    const pollCleanup = (remaining: number) => {
      window.setTimeout(async () => {
        try {
          await refreshPlatformData();
          const response = await apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
          const remainingPlans = listItems(response).filter(plan => planIds.includes(plan.id));
          if (remainingPlans.length > 0 && remaining > 0) {
            pollCleanup(remaining - 1);
          } else if (remainingPlans.length === 0) {
            setProtectionPlans(prev => prev.filter(plan => !planIds.includes(plan.id)));
            await refreshPlatformData();
            setAppUiOverrides(prev => {
              const next = { ...prev };
              targetApps.forEach(app => {
                const key = appOverrideKey(app);
                next[key] = {
                  ...(next[key] || {}),
                  stage: 'config',
                  protectionStatus: 'pending_protection',
                  isProtected: false,
                  status: mapApplicationStatus(app.status, false),
                  protectionPlanId: '',
                  protectionPlanCreatedAt: '',
                };
              });
              return next;
            });
            setSelectedConfigApps([]);
            setStage('config');
          }
        } catch {
          if (remaining > 0) pollCleanup(remaining - 1);
        }
      }, 3000);
    };
    void refreshPlatformData();
    pollCleanup(40);
  };
  const pollProtectionPlanActivation = (planIds: string[], remaining: number) => {
    if (planIds.length === 0 || remaining <= 0) return;
    window.setTimeout(async () => {
      try {
        await refreshPlatformData();
        const response = await apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
        const plans = listItems(response);
        const watched = plans.filter(plan => planIds.includes(plan.id));
        setProtectionPlans(prev => {
          const byId = new Map(prev.map(plan => [plan.id, plan]));
          watched.forEach(plan => {
            byId.set(plan.id, { ...(byId.get(plan.id) || {}), ...plan });
          });
          return Array.from(byId.values()).sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
        });
        const done = watched.length === planIds.length && watched.every(plan =>
          ['active', 'active_with_warning', 'storage_failed', 'schedule_failed', 'cleanup_failed'].includes((plan.status || '').toLowerCase())
        );
        if (!done) {
          pollProtectionPlanActivation(planIds, remaining - 1);
        } else {
          void refreshPlatformData();
        }
      } catch {
        pollProtectionPlanActivation(planIds, remaining - 1);
      }
    }, 1500);
  };
  const finishProtectWizard = async () => {
    const targetRows = protectWizardMode === 'modify'
      ? selectedRunRows
      : selectedConfigApps
        .map(name => apps.find(item => item.name === name))
        .filter((app): app is AppItem => Boolean(app));
    const targetApps = Array.from(new Set(targetRows.flatMap(row => unitMembers(row).map(app => app.name))));
    if (!currentClusterId) {
      toast('Select a source cluster first');
      return;
    }
    if (!protectConfig.storageId) {
      toast('Select a storage repository before saving DR configuration');
      return;
    }
    const targetCluster = clusters.find(cluster => cluster.name === protectConfig.targetCluster);
    const policyId = policies.some(policy => policy.id === protectConfig.policy) ? protectConfig.policy : '';
    const includeLabelSelector = protectConfig.includeRules
      .flatMap(rule => rule.labels.split(','))
      .map(value => value.trim())
      .filter(Boolean)
      .join(',');
    const labelSelector = protectConfig.scope === 'filter'
      ? includeLabelSelector || labelConditionsToSelector(protectConfig.labelConditions) || protectConfig.labels
      : '';
    const scopeType = protectConfig.scope === 'filter' ? 'label-selector' : 'namespace';
    const targetAppMeta = targetApps
      .map(name => apps.find(item => item.name === name))
      .filter((app): app is AppItem => Boolean(app?.apiId))
      .map(app => ({ apiId: app.apiId as string, name: app.name }));
    if (targetAppMeta.length === 0) {
      toast('No applications selected to protect');
      return;
    }
    const planGroups = protectConfig.mergeNamespaces || protectWizardMode === 'modify'
      ? [targetAppMeta]
      : targetAppMeta.map(meta => [meta]);
    const createdPlans: ApiProtectionPlan[] = [];
    const planIdByAppName: Record<string, string> = {};
    try {
      for (const group of planGroups) {
        const createdPlan = await apiPost<ApiProtectionPlan>('/api/v1/protection-plans', {
          sourceClusterId: currentClusterId,
          appIds: group.map(meta => meta.apiId),
          scopeType,
          labelSelector,
          includeClusterScoped: false,
          storageRepoId: protectConfig.storageId,
          policyId,
          targetClusterId: targetCluster?.id || currentClusterId,
          excludeRules: protectConfig.excludeRules,
          preHooks: protectConfig.preScripts.map(scriptPayload),
          postHooks: protectConfig.postScripts.map(scriptPayload),
        });
        createdPlans.push(createdPlan);
        group.forEach(meta => {
          planIdByAppName[meta.name] = createdPlan.id;
        });
      }
      // Promote the applications to "protected" so the protection_status in
      // the database matches the stage-3 UI state, and a fresh page load
      // still shows these apps in stage 3.
      await Promise.all(targetAppMeta.map(meta =>
        apiPatch(`/api/v1/applications/${meta.apiId}`, { protectionStatus: 'protected' })
      ));
      const selectedPolicy = policies.find(policy => policy.id === policyId);
      const selectedStorage = storage.find(repo => repo.id === protectConfig.storageId);
      const selectedTarget = targetCluster || clusters.find(cluster => cluster.id === currentClusterId);
      setProtectionPlans(prev => {
        const byId = new Map(prev.map(plan => [plan.id, plan]));
        createdPlans.forEach(plan => {
          byId.set(plan.id, { ...(byId.get(plan.id) || {}), ...plan });
        });
        return Array.from(byId.values()).sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
      });
      setAppUiOverrides(prev => {
        const next = { ...prev };
        targetAppMeta.forEach(meta => {
          const createdPlan = createdPlans.find(plan => plan.id === planIdByAppName[meta.name]);
          const app = apps.find(item => item.name === meta.name);
          const key = app ? appOverrideKey(app) : `${currentClusterId}::${meta.name}`;
          next[key] = {
            ...(next[key] || {}),
            stage: 'run',
            protectionStatus: 'protected',
            protectionPlanId: planIdByAppName[meta.name],
            protectionPlanCreatedAt: createdPlan?.createdAt,
            policy: selectedPolicy?.name,
            storage: selectedStorage?.name,
            targetCluster: selectedTarget?.name,
            isProtected: true,
            status: 'Protected',
            lastBackup: 'synced recently',
          };
        });
        return next;
      });
    } catch (error) {
      toast('Failed to create protection plan: ' + (error instanceof Error ? error.message : 'unknown error'));
      return;
    }
    setProtectedAppNames(prev => Array.from(new Set([...prev, ...targetApps])));
    if (protectWizardMode === 'create') {
      setConfigAppNames(prev => prev.filter(name => !targetApps.includes(name)));
    }
    setSelectedRunApps(protectConfig.mergeNamespaces && createdPlans.length === 1 ? [`plan:${createdPlans[0].id}`] : targetApps);
    setSelectedConfigApps([]);
    setProtectWizardOpen(false);
    setProtectWizardMode('create');
    setProtectWizardStep(1);
    setStage('run');
    void refreshPlatformData();
    pollProtectionPlanActivation(createdPlans.map(plan => plan.id), 40);
    const warning = createdPlans.map(plan => plan.warning).find(Boolean);
    const activating = createdPlans.some(plan => plan.status && plan.status !== 'active');
    if (warning) {
      toast(`DR configuration saved, activation pending: ${warning}`);
    } else if (activating) {
      toast('DR configuration saved. Protection plan activation is in progress.');
    } else {
      toast(protectWizardMode === 'modify' ? 'DR configuration updated' : 'DR protection enabled');
    }
  };
  const resetExcludeRuleForm = () => {
    setNewExcludeRule({ group: '', resource: '', name: '', version: '', labels: '' });
    setEditingRuleIndex(null);
    setShowAddRuleForm(false);
  };
  const saveExcludeRule = () => {
    const normalizedRule = {
      group: '',
      resource: newExcludeRule.resource,
      name: '',
      version: '',
      labels: '',
    };
    if (!Object.values(normalizedRule).some(Boolean)) {
      resetExcludeRuleForm();
      return;
    }
    setProtectConfig(prev => {
      const rules = [...prev.excludeRules];
      if (editingRuleIndex === null) {
        rules.push(normalizedRule);
      } else {
        rules[editingRuleIndex] = normalizedRule;
      }
      return { ...prev, excludeRules: rules };
    });
    resetExcludeRuleForm();
  };
  const editExcludeRule = (rule: typeof newExcludeRule, index: number) => {
    setNewExcludeRule({
      group: rule.group || '',
      resource: rule.resource || '',
      name: rule.name || '',
      version: rule.version || '',
      labels: rule.labels || '',
    });
    setEditingRuleIndex(index);
    setShowAddRuleForm(true);
  };
  const saveScript = (
    type: 'preScripts' | 'postScripts',
    script: { name: string; size: number; lastModified?: number; content: string; source: 'upload' | 'manual'; isEntry?: boolean },
    index: number | null = null
  ) => {
    setProtectConfig(prev => {
      const currentScripts = [...prev[type]];
      if (index === null) {
        const existingIndex = currentScripts.findIndex(item => item.name === script.name);
        const hasEntry = currentScripts.some((item, itemIndex) => (item.isEntry ?? itemIndex === 0));
        const nextScript = { ...script, isEntry: script.isEntry ?? !hasEntry };
        if (existingIndex >= 0) {
          currentScripts[existingIndex] = { ...nextScript, isEntry: currentScripts[existingIndex].isEntry ?? existingIndex === 0 };
        } else {
          currentScripts.push(nextScript);
        }
      } else {
        currentScripts[index] = { ...script, isEntry: script.isEntry ?? currentScripts[index]?.isEntry ?? index === 0 };
      }
      return { ...prev, [type]: currentScripts };
    });
  };
  const handleFileUpload = async (type: 'preScripts' | 'postScripts', event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const content = await file.text().catch(() => '');
    saveScript(type, {
      name: file.name,
      size: file.size,
      lastModified: file.lastModified,
      content,
      source: 'upload',
    });
    event.target.value = '';
  };
  const removeScript = (type: 'preScripts' | 'postScripts', index: number) => {
    setProtectConfig(prev => {
      const removedWasEntry = prev[type][index]?.isEntry ?? index === 0;
      const list = prev[type].filter((_, itemIndex) => itemIndex !== index);
      if (removedWasEntry && list.length > 0 && !list.some(item => item.isEntry)) {
        list[0] = { ...list[0], isEntry: true };
      }
      return { ...prev, [type]: list };
    });
  };
  const setEntryScript = (type: 'preScripts' | 'postScripts', index: number) => {
    setProtectConfig(prev => {
      if (index < 0 || index >= prev[type].length) return prev;
      const list = prev[type].map((script, itemIndex) => ({ ...script, isEntry: itemIndex === index }));
      return { ...prev, [type]: list };
    });
  };

  useEffect(() => {
    const activeIds = [...Object.values(liveAppTasks), ...Object.values(liveRecoveryTasks)]
      .filter(t => isActiveTaskStatus(t.status))
      .map(t => t.id);
    if (activeIds.length === 0) return;
    let cancelled = false;
    const activeIdSet = new Set(activeIds);
    const pollTaskStatus = async () => {
      try {
        const taskRes = await apiGet<ApiList<ApiTask>>('/api/v1/tasks');
        if (cancelled) return;
        const latestTasks = new Map(listItems(taskRes).filter(task => activeIdSet.has(task.id)).map(task => [task.id, task]));
        if (latestTasks.size === 0) return;
        const hasSettledTask = Array.from(latestTasks.values()).some(task => !isActiveTaskStatus(task.status));
        setLiveAppTasks(prev => {
          let changed = false;
          const next = Object.fromEntries(Object.entries(prev).map(([key, task]) => {
            const latest = latestTasks.get(task.id);
            if (!latest) return [key, task];
            changed = true;
            return [key, { ...task, ...latest }];
          }));
          return changed ? next : prev;
        });
        setLiveRecoveryTasks(prev => {
          let changed = false;
          const next = Object.fromEntries(Object.entries(prev).map(([key, task]) => {
            const latest = latestTasks.get(task.id);
            if (!latest) return [key, task];
            changed = true;
            return [key, { ...task, ...latest }];
          }));
          return changed ? next : prev;
        });
        if (hasSettledTask) {
          void refreshPlatformData();
        }
      } catch {
        // Keep the current list stable if status polling fails.
      }
    };
    pollTaskStatus();
    const timer = window.setInterval(pollTaskStatus, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [liveAppTasks, liveRecoveryTasks, setLiveAppTasks, setLiveRecoveryTasks, refreshPlatformData]);

  return (
    <>
      <div className="hbdr-stage-panel">
      <div className="hbdr-stage-grid hbdr-screenshot-stage-grid">
        {stageCards.map((card, index) => (
          <React.Fragment key={card.key}>
            <button onClick={() => setStage(card.key)} className={`hbdr-stage-card hbdr-dr-stage-card hbdr-dr-stage-${card.tone} ${stage === card.key ? 'hbdr-dr-stage-active' : ''}`}>
              <div className="hbdr-dr-stage-icon"><card.icon size={32} /></div>
              <div className="hbdr-dr-stage-copy">
                <h4>{index + 1}. {card.title}</h4>
                <p>{card.desc}</p>
                <span>{card.list}</span>
              </div>
              <div className="hbdr-dr-stage-check">{stage === card.key ? <Check size={17} /> : null}</div>
              <div className="hbdr-dr-stage-count">
                <strong>{card.count}</strong>
              </div>
            </button>
            {index < stageCards.length - 1 && <div className="hbdr-stage-arrow"><ChevronRight size={28} /></div>}
          </React.Fragment>
        ))}
      </div>
      </div>

      <div className="hbdr-dr-table-card">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className={`hbdr-dr-action-group hbdr-dr-action-group-managed hbdr-dr-action-group-${stage}`}>
            {stage === 'select' && (
              <button
                type="button"
                onClick={handlePrimaryAction}
                disabled={primaryDisabled}
                title={selectedHasUnsupportedDR ? `${unsupportedDRSelectionMessage} Open View Details or check DR Support for details.` : undefined}
                className="hbdr-dr-action-primary"
              >
                <ChevronRight size={15} />Next
              </button>
            )}
            {stage === 'config' && (
              <>
                <button
                  type="button"
                  onClick={async () => {
                    if (selectedConfigApps.length === 0) {
                      toast('Select applications to move back');
                      return;
                    }
                    const names = [...selectedConfigApps];
                    setSelectedConfigApps([]);
                    await moveAppsToStage(names, 'select');
                    setStage('select');
                  }}
                  disabled={selectedConfigApps.length === 0}
                  className="hbdr-dr-action-primary hbdr-dr-action-ghost"
                >
                  Move Back
                </button>
                <button onClick={handlePrimaryAction} disabled={primaryDisabled} className="hbdr-dr-action-primary">DR Configuration</button>
              </>
            )}
            {isRunStage && (
              <>
                <button onClick={handlePrimaryAction} disabled={!canStartSync} className="hbdr-dr-action-primary">Start Sync</button>
                <button disabled={!canRestoreAction} title={hasSelectedRunActiveRecoveryTask ? 'A recovery task is already running' : undefined} onClick={() => openRestoreAction('drill')} className="hbdr-dr-action-primary hbdr-dr-action-ghost">Drill</button>
                <button disabled={!canRestoreAction} title={hasSelectedRunActiveRecoveryTask ? 'A recovery task is already running' : undefined} onClick={() => openRestoreAction('takeover')} className="hbdr-dr-action-danger">Takeover</button>
              </>
            )}
            <div className="relative">
              <button onClick={() => setAppBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                More <ChevronDown size={15} className={appBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
              </button>
              <AnimatePresence>
                {appBulkMenuOpen && (
                  <>
                    <div className="fixed inset-0 z-30" onClick={() => setAppBulkMenuOpen(false)} />
                    <motion.div
                      initial={{ opacity: 0, y: 8, scale: 0.96 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 8, scale: 0.96 }}
                      className="absolute right-0 top-11 z-40 w-52 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"
                    >
                      <button
                        disabled={!singleSelectedApp}
                        onClick={() => {
                          if (!singleSelectedApp) return;
                          setSelectedDetailApp(singleSelectedApp);
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <span className="flex items-center gap-2"><Eye size={14} className="text-indigo-500" />View Details</span>
                        {!singleSelectedApp && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                      </button>
                      {isRunStage && (
                        <>
                          <button
                            disabled={selectedRunApps.length !== 1 || selectedRunHasCleanupRunning}
                            title="Edit protection configuration"
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              modifyProtection();
                            }}
                            className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <span className="flex items-center gap-2"><Edit2 size={14} className="text-blue-500" />Edit</span>
                            {selectedRunApps.length !== 1 && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                          </button>
                          <button
                            disabled={!canStopSync}
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              void stopSync();
                            }}
                            className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <AlertCircle size={14} />Cancel Sync
                          </button>
                          <button
                            disabled={selectedRunApps.length === 0 || selectedRunHasCleanupRunning}
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              deleteDrConfiguration();
                            }}
                            className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <ShieldOff size={14} />Cleanup Resources
                          </button>
                          <button
                            disabled={selectedRunRows.length === 0 || selectedRunHasCleanupRunning}
                            onClick={() => {
                              setAppBulkMenuOpen(false);
                              openRestorePoints(selectedRunRows.map(app => app.namespace || app.name));
                            }}
                            className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                          >
                            <span className="flex items-center gap-2"><Archive size={14} className="text-emerald-500" />View Restore Points</span>
                            {selectedRunRows.length > 1 && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">{selectedRunRows.length}</em>}
                          </button>
                        </>
                      )}
                      <button
                        disabled={selectedNames.length === 0 || tags.length === 0}
                        onClick={() => {
                          setTagAction('attach');
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <Archive size={14} className="text-blue-500" />Attach Tag
                      </button>
                      <button
                        disabled={selectedAttachedTagIds.length === 0}
                        onClick={() => {
                          setTagAction('detach');
                          setAppBulkMenuOpen(false);
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                      >
                        <Archive size={14} className="text-amber-500" />Detach Tag
                      </button>
                    </motion.div>
                  </>
                )}
              </AnimatePresence>
            </div>
            </div>
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={appQueryFields}
              tags={[
                ...tags.map(tag => ({ value: tag.id, label: tag.name, count: currentRows.filter(app => appHasTag(app, tag.id)).length })),
              ]}
              activeTags={activeTags}
              setActiveTags={setActiveTags}
              filters={appFilterChoices}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={appColumns}
              visibleColumns={visibleAppColumns}
              setVisibleColumns={isRunStage ? setVisibleRunColumns : setVisibleConfigColumns}
              onRefresh={async () => {
                setSelectedSelectApps([]);
                setSelectedConfigApps([]);
                setSelectedRunApps([]);
                if (isRunStage) {
                  try {
                    await refreshPlatformData();
                    toast('Application list refreshed');
                  } catch {
                    toast('Failed to refresh application list');
                  }
                  return;
                }
                await refreshCurrentClusterInventory();
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={isRunStage ? appRunTableColumns : appConfigTableColumns}
          data={currentRows}
          getRowId={row => row.name}
          onRowClick={row => toggleSelectedName(row.name)}
          getRowClassName={row => [
            selectedNames.includes(row.name) ? 'hbdr-dr-row-selected' : '',
            stage === 'select' && isDRUnsupported(row) ? 'hbdr-dr-row-unsupported' : '',
          ].filter(Boolean).join(' ')}
          selectedCount={selectedNames.length}
          emptyMessage={query ? 'No applications match the current search.' : 'No applications in the current stage. Switch to another stage to view data.'}
          className="hbdr-dr-main-table"
        />
      </div>

      <AnimatePresence>
        {resourceDetail && (
          <div className="fixed inset-0 z-50">
            {(() => {
              const namespace = resourceDetail.app.namespace || resourceDetail.app.name;
              const clusterId = resourceDetail.app.clusterId || currentClusterId || '';
              const isRefreshing = resourceRefreshKey === `${clusterId}:${namespace}`;
              const refreshState = resourceRefreshStatus?.key === `${clusterId}:${namespace}` ? resourceRefreshStatus : null;
              return (
                <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="hbdr-filter-drawer-backdrop"
              onClick={() => setResourceDetail(null)}
            />
            <motion.div
              initial={{ opacity: 0, x: 32 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 32 }}
              transition={{ duration: 0.16, ease: 'easeOut' }}
              className="hbdr-filter-drawer hbdr-resource-drawer"
            >
              <div className="hbdr-filter-drawer-head hbdr-resource-drawer-head">
                <div className="hbdr-resource-modal-title">
                  <span className="hbdr-resource-modal-icon"><Grid3X3 size={18} /></span>
                  <div>
                    <h3>Namespace Resources</h3>
                    <p>{resourceDetail.app.namespace || resourceDetail.app.name}</p>
                  </div>
                </div>
                <div className="hbdr-resource-modal-head-right">
                  <div className="hbdr-resource-modal-meta">
                    <span>
                      <em>Groups</em>
                      <strong>{resourceDetailGroups.length}</strong>
                    </span>
                    <span>
                      <em>Objects</em>
                      <strong>{resourceSummaryTotal(resourceDetail.app)}</strong>
                    </span>
                    <span>
                      <em>Storage</em>
                      <strong>{resourceDetail.app.pvCapacityBytes ? formatBytes(resourceDetail.app.pvCapacityBytes) : '0 B'}</strong>
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => void refreshNamespaceResources(resourceDetail.app)}
                    disabled={isRefreshing}
                    title="Refresh namespace resources from the cluster agent"
                    aria-label="Refresh namespace resources"
                  >
                    <RefreshCw size={18} className={isRefreshing ? 'animate-spin' : ''} />
                  </button>
                  <button type="button" onClick={() => setResourceDetail(null)} aria-label="Close resource details"><X size={18} /></button>
                </div>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-resource-detail">
                {refreshState && (
                  <div className={`hbdr-sync-detail-message ${refreshState.status === 'failed' || refreshState.status === 'timeout' ? 'is-warning' : ''}`}>
                    <strong>{refreshState.status === 'succeeded' ? 'Inventory refreshed' : refreshState.status === 'pending' ? 'Refreshing inventory' : 'Inventory refresh needs attention'}</strong>
                    <p>{refreshState.message || refreshState.status}</p>
                  </div>
                )}
                {resourceDetailGroups.length > 0 ? (
                  <div className="hbdr-resource-inventory">
                    <div className="hbdr-resource-overview-strip">
                      {resourceDetailGroups.map(group => {
                        const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                        return (
                          <span key={group.category.key}>
                            <Icon size={15} />
                            <strong>{group.category.total}</strong>
                            <em>{group.category.label}</em>
                          </span>
                        );
                      })}
                    </div>
                    {resourceDetailGroups.map(group => {
                      const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                      return (
                        <section key={group.category.key} className="hbdr-resource-inventory-section">
                          <div className="hbdr-resource-inventory-divider">
                            <span><Icon size={14} />{group.category.label}</span>
                            <em>{group.category.total} objects</em>
                          </div>
                          <div className="hbdr-resource-inventory-table">
                            <div className="hbdr-resource-inventory-columns">
                              <span>Resource</span>
                              <span>Type</span>
                              <span>Details</span>
                            </div>
                            {group.items.flatMap(item => {
                              const rows = item.resources || [];
                              if (rows.length === 0) {
                                return [(
                                  <div key={`${group.category.key}-${item.kind}-count`} className="hbdr-resource-inventory-count-row">
                                    <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                    <strong>{item.kind}</strong>
                                    <em>{item.count} reported, object details pending</em>
                                  </div>
                                )];
                              }
                              return rows.map(resource => {
                                const detailText = resourceInventoryDetailText(resource, item, resourceDetail.app.namespace);
                                return (
                                  <div
                                    key={`${item.kind}-${resource.namespace || resourceDetail.app.namespace}-${resource.name}`}
                                    className="hbdr-resource-inventory-item"
                                    title={resourceInventoryTitle(resource, item, resourceDetail.app.namespace)}
                                  >
                                    <div className="hbdr-resource-inventory-resource">
                                      <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                      <div>
                                        <strong>{resource.name}</strong>
                                        <em>{resource.namespace || resourceDetail.app.namespace} · {formatAge(resource.ageSeconds)}</em>
                                      </div>
                                    </div>
                                    <div className="hbdr-resource-inventory-type">{item.kind}</div>
                                    <div className="hbdr-resource-inventory-detail-text">{detailText}</div>
                                  </div>
                                );
                              });
                            })}
                          </div>
                        </section>
                      );
                    })}
                  </div>
                ) : (
                  <div className="hbdr-resource-detail-empty">
                    <Database size={18} />
                    <strong>No namespaced resources found</strong>
                    <span>The agent has not reported resources for this namespace yet.</span>
                  </div>
                )}
              </div>
            </motion.div>
                </>
              );
            })()}
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {tagAction && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0 bg-slate-900/45 backdrop-blur-sm" onClick={() => setTagAction(null)} />
            <motion.div initial={{ opacity: 0, y: 14, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 14, scale: 0.98 }} className="hbdr-tag-action-modal">
              <div className="hbdr-tag-action-head">
                <div>
                  <strong>{tagAction === 'attach' ? 'Attach Tag' : 'Detach Tag'}</strong>
                  <span>{selectedNames.length} selected application{selectedNames.length === 1 ? '' : 's'}</span>
                </div>
                <button type="button" onClick={() => setTagAction(null)} aria-label="Close"><X size={16} /></button>
              </div>
              <div className="hbdr-tag-action-list">
                {(tagAction === 'attach' ? tags : tags.filter(tag => selectedAttachedTagIds.includes(tag.id))).map(tag => (
                  <button key={tag.id} type="button" onClick={() => tagAction === 'attach' ? attachTagToSelected(tag.id) : detachTagFromSelected(tag.id)}>
                    <Archive size={15} />
                    <span>
                      <strong>{tag.name}</strong>
                      <em>{tagAction === 'attach' ? 'Available tag' : 'Attached tag'}</em>
                    </span>
                    <ChevronRight size={15} />
                  </button>
                ))}
                {(tagAction === 'attach' ? tags : tags.filter(tag => selectedAttachedTagIds.includes(tag.id))).length === 0 && (
                  <div className="hbdr-tag-action-empty">
                    <Archive size={24} />
                    <strong>No tags available</strong>
                    <span>{tagAction === 'attach' ? 'Create tags in Tag Management first.' : 'Selected applications do not have attached tags.'}</span>
                  </div>
                )}
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {drSupportErrorDetail && (
          <ErrorDetailModalFrame title="DR Support Details" onClose={() => setDrSupportErrorDetail(null)}>
            <TaskErrorDetailBlock
              failure={drSupportFailureForApp(drSupportErrorDetail)}
              details={drSupportFailureDetailsForApp(drSupportErrorDetail)}
            />
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {syncTaskDetail && (
          <ErrorDetailModalFrame title={`${taskDetailLabel(syncTaskDetail.task.type)} Task Details`} onClose={() => setSyncTaskDetail(null)}>
            {(() => {
              const events = drTaskEvents[syncTaskDetail.task.id] || [];
              const failure = syncTaskDetail.failure || taskFailureSummary(syncTaskDetail.task, events);
              const details = taskFailureDetails(syncTaskDetail.task, events);
              return (
                <div className="hbdr-sync-detail">
                  <TaskErrorDetailBlock failure={failure} details={details} />
                </div>
              );
            })()}
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {restoreAction && (
          <RecoveryWizardModal
            open
            mode={restoreAction.mode}
            app={restoreAction.app}
            profile={profileOf(restoreAction.app)}
            currentClusterName={currentCluster?.name || 'Current Cluster'}
            points={restorePointsForApp(restoreAction.app)}
            clusterOptions={clusters.map(cluster => ({
              id: cluster.id,
              name: cluster.name,
              region: cluster.region,
              version: cluster.version,
              isCurrent: currentCluster?.id === cluster.id,
            }))}
            repositoryOptions={storage.map(repo => ({
              id: repo.id,
              name: repo.name,
              type: repo.type,
              endpoint: repo.endpoint,
              bucket: repo.bucket,
            }))}
            config={restoreAction.config}
            setConfig={updater => {
              setRestoreAction(prev => {
                if (!prev) return prev;
                const nextConfig = typeof updater === 'function' ? updater(prev.config) : updater;
                return { ...prev, config: nextConfig };
              });
            }}
            onClose={() => setRestoreAction(null)}
            onSubmit={confirmRestoreAction}
          />
        )}
      </AnimatePresence>

      <DrConfigurationModal
        open={protectWizardOpen}
        step={protectWizardStep}
        setStep={setProtectWizardStep}
        onClose={() => setProtectWizardOpen(false)}
        onFinish={finishProtectWizard}
        targetSummary={wizardTargetSummary}
        targetCount={wizardTargetNames.length}
        targetNames={wizardTargetNames}
        protectConfig={protectConfig}
        setProtectConfig={setProtectConfig}
        showAddRuleForm={showAddRuleForm}
        setShowAddRuleForm={setShowAddRuleForm}
        newExcludeRule={newExcludeRule}
        setNewExcludeRule={setNewExcludeRule}
        editingRuleIndex={editingRuleIndex}
        resetExcludeRuleForm={resetExcludeRuleForm}
        saveExcludeRule={saveExcludeRule}
        editExcludeRule={editExcludeRule}
        storage={storage}
        policyOptions={policyOptions}
        filteredPolicyOptions={filteredPolicyOptions}
        paginatedPolicyOptions={paginatedPolicyOptions}
        wizardPolicySearchQuery={wizardPolicySearchQuery}
        setWizardPolicySearchQuery={setWizardPolicySearchQuery}
        setWizardPolicyPage={setWizardPolicyPage}
        wizardPolicyPage={wizardPolicyPage}
        wizardPolicyTotalPages={wizardPolicyTotalPages}
        wizardPolicyPageSize={wizardPolicyPageSize}
        targetClusterOptions={targetClusterOptions}
        labelOptions={wizardLabelOptions}
        preScriptRef={preScriptRef}
        postScriptRef={postScriptRef}
        handleFileUpload={handleFileUpload}
        saveScript={saveScript}
        removeScript={removeScript}
        setEntryScript={setEntryScript}
        onCreateStorage={openStorage}
        onRegisterCluster={openClusters}
        onCreatePolicy={openPolicies}
      />
      <AnimatePresence>
        {selectedDetailApp && (() => {
          const detailProfile = profileOf(selectedDetailApp);
          const protectedState = selectedDetailApp?.stage === 'run';
          const detailStorage = selectedDetailApp.storage || 'No backup repository';
          const detailPolicy = selectedDetailApp.policy || 'No policy bound';
          const detailStatus = protectedState ? 'Protected' : selectedDetailApp.stage === 'config' ? 'Configured' : 'Discovered';
          const supportMeta = drSupportMetaForApp(selectedDetailApp);
          const supportFailure = drSupportFailureForApp(selectedDetailApp);
          const supportDetails = drSupportFailureDetailsForApp(selectedDetailApp);
          const categories = normalizeResourceCategories(selectedDetailApp).filter(category => category.total > 0);
          const storageCategory = categories.find(category => category.key === 'storage');
          const pvcItem = storageCategory?.items?.find(item => item.kind === 'PersistentVolumeClaim');
          const pvcRows = pvcItem?.resources || [];
          const capacityBytes = selectedDetailApp.pvCapacityBytes || selectedDetailApp.resourceSummary?.pvCapacityBytes || 0;
          const detailStage = stageOf(selectedDetailApp);
          const detailPlan = protectionPlanForApp(selectedDetailApp);
          const detailPlanId = selectedDetailApp.protectionPlanId || detailPlan?.id || '';
          const detailPlanMeta = drStatusMetaForApp(selectedDetailApp);
          const detailPolicyObject = detailPlan ? policies.find(item => item.id === detailPlan.policyId) : undefined;
          const namespaces = unitNamespaces(selectedDetailApp);
          const namespaceTitle = selectedDetailApp.isMergedPlan ? `${namespaces.length} namespaces` : (namespaces[0] || selectedDetailApp.namespace || selectedDetailApp.name);
          const stageLabel = detailStage === 'run' ? 'Protected' : detailStage === 'config' ? 'Pending setup' : 'Discovered';
          const stageToneClass = detailStage === 'run'
            ? 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-100'
            : detailStage === 'config'
              ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-100'
              : 'bg-slate-100 text-slate-600 ring-1 ring-slate-200';
          const resourceCount = resourceSummaryTotal(selectedDetailApp);
          const storageUsage = planStorageSizeForApp(selectedDetailApp);
          const nextSyncLabel = nextSyncLabelForApp(selectedDetailApp);
          const primaryFacts = detailStage === 'run'
            ? [
              ['DR Config', detailPlanMeta.label],
              ['Repository', detailStorage],
              ['Policy', detailPolicy],
              ['Next Sync', nextSyncLabel || 'Manual only'],
            ]
            : detailStage === 'config'
              ? [
                ['Setup Status', 'Waiting for DR configuration'],
                ['DR Support', supportMeta.label],
                ['PVCs', String(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0)],
                ['Storage Request', formatBytes(capacityBytes)],
              ]
              : [
                ['DR Support', supportMeta.label],
                ['Runtime Status', selectedDetailApp.status || 'Unknown'],
                ['PVCs', String(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0)],
                ['Storage Request', formatBytes(capacityBytes)],
              ];
          return (
            <div className="fixed inset-0 z-50 flex justify-end">
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="hbdr-filter-drawer-backdrop"
                onClick={() => setSelectedDetailApp(null)}
              />
              <motion.div
                initial={{ opacity: 0, x: 32 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 32 }}
                transition={{ duration: 0.18, ease: 'easeOut' }}
                className="hbdr-filter-drawer hbdr-app-detail-drawer"
              >
                <div className="hbdr-filter-drawer-head hbdr-app-detail-drawer-head">
                  <div className="flex items-start justify-between gap-4">
                    <div className="hbdr-app-detail-title">
                      <div className="hbdr-app-detail-icon"><Layers size={18} /></div>
                      <div className="min-w-0">
                        <div className="hbdr-app-detail-title-line">
                          <h3>{detailStage === 'run' ? 'DR Plan' : namespaceTitle}</h3>
                          <span className={`hbdr-app-detail-status ${stageToneClass}`}>{stageLabel}</span>
                        </div>
                        {detailStage === 'run' && (
                          <p className="hbdr-app-detail-plan-id" title={detailPlanId || 'No DR plan ID'}>
                            <span>Plan ID</span><strong>{detailPlanId || '-'}</strong>
                          </p>
                        )}
                        {detailStage !== 'run' && (
                          <p className="mt-1 text-xs font-medium text-slate-500">
                            {resourceCount} resources · {formatBytes(capacityBytes)} requested storage
                          </p>
                        )}
                      </div>
                    </div>
                    <button onClick={() => setSelectedDetailApp(null)}><X size={18} /></button>
                  </div>
                </div>
                <div className="hbdr-filter-drawer-body hbdr-app-detail-drawer-body">
                  <div className="grid grid-cols-2 gap-3">
                    {primaryFacts.map(([label, value]) => (
                      <div key={label} className="hbdr-app-detail-fact">
                        <p>{label}</p>
                        <strong title={value}>{value}</strong>
                      </div>
                    ))}
                  </div>

                  <div className="mt-4 grid grid-cols-1 gap-4">
                    {detailStage === 'run' && (
                      <section className="hbdr-app-detail-section">
                        <div className="hbdr-app-detail-section-title">
                          <Layers size={15} className="text-indigo-500" />
                          <h4>Namespaces</h4>
                        </div>
                        <div className="hbdr-app-detail-chip-list">
                          {namespaces.map(namespace => (
                            <span key={namespace}>{namespace}</span>
                          ))}
                        </div>
                      </section>
                    )}

                    {detailStage !== 'run' && (
                      <section className="hbdr-app-detail-section">
                        <div className="hbdr-app-detail-section-title">
                          {supportMeta.tone === 'unsupported' ? <AlertTriangle size={15} className="text-rose-600" /> : <ShieldCheck size={15} className="text-slate-400" />}
                          <h4>DR Readiness</h4>
                        </div>
                        {supportMeta.tone === 'unsupported' ? (
                          <TaskErrorDetailBlock failure={supportFailure} details={supportDetails} />
                        ) : (
                          <div className="rounded-xl border border-emerald-100 bg-emerald-50 px-3 py-3">
                            <p className="text-sm font-black text-emerald-800">{supportMeta.label}</p>
                            <p className="mt-1 text-xs font-semibold leading-relaxed text-emerald-700">{supportMeta.title}</p>
                          </div>
                        )}
                      </section>
                    )}

                    {detailStage === 'run' && (
                      <section className="hbdr-app-detail-section">
                        <div className="hbdr-app-detail-section-title">
                          <Database size={15} className="text-slate-500" />
                          <h4>DR Configuration</h4>
                        </div>
                        <div className="hbdr-app-detail-config-grid">
                          <div className="hbdr-app-detail-config-item">
                            <p>Repository</p>
                            <strong title={detailStorage}>{detailStorage}</strong>
                            {storageUsage.label && <em>{storageUsage.label}</em>}
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>Target Cluster</p>
                            <strong title={selectedDetailApp.targetCluster || 'Backup only'}>{selectedDetailApp.targetCluster || 'Backup only'}</strong>
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>Policy</p>
                            <strong title={detailPolicy}>{detailPolicy}</strong>
                            {detailPolicyObject && <em>{formatPolicySchedule(detailPolicyObject)}</em>}
                          </div>
                          <div className="hbdr-app-detail-config-item">
                            <p>Create Time</p>
                            <strong>{selectedDetailApp.protectionPlanCreatedAt ? formatDateTime(selectedDetailApp.protectionPlanCreatedAt) : '-'}</strong>
                          </div>
                        </div>
                      </section>
                    )}

                  </div>

                  <div className="hbdr-app-detail-resource">
                    <div className="hbdr-app-detail-section-title">
                      <Grid3X3 size={15} className="text-slate-500" />
                      <h4>Resource Overview</h4>
                    </div>
                    <div className="hbdr-app-detail-chip-list hbdr-app-detail-resource-chips">
                      {categories.length > 0 ? categories.map(category => {
                        const Icon = resourceCategoryIconMap[category.key as ResourceCategoryKey] || MoreVertical;
                        return (
                          <span key={category.key}>
                            <Icon size={13} />
                            <strong>{category.total}</strong>
                            {category.label}
                          </span>
                        );
                      }) : (
                        <span>No resources reported</span>
                      )}
                    </div>
                    <div className="hbdr-app-detail-pvc">
                      <div className="hbdr-app-detail-section-title">
                        <HardDrive size={14} className="text-slate-500" />
                        <h5>PVC / Storage</h5>
                      </div>
                      {pvcRows.length > 0 ? (
                        <div className="hbdr-app-detail-pvc-list">
                          {pvcRows.slice(0, 6).map(pvc => {
                            const status = pvc.fields?.STATUS || pvc.fields?.Status || '-';
                            const storageClass = pvc.fields?.STORAGECLASS || pvc.fields?.StorageClass || '-';
                            const capacity = pvc.fields?.CAPACITY || pvc.fields?.Capacity || '-';
                            const usedBy = pvc.fields?.['USED BY'] || pvc.fields?.UsedBy || pvc.fields?.['Used By'] || '';
                            return (
                              <div key={`${pvc.namespace || selectedDetailApp.namespace}-${pvc.name}`} className="hbdr-app-detail-pvc-row">
                                <div className="min-w-0">
                                  <strong>{pvc.name}</strong>
                                  <p>{storageClass} · {capacity}{usedBy ? ` · Used by ${usedBy}` : ''}</p>
                                </div>
                                <span className={status === 'Bound' ? 'is-bound' : 'is-warning'}>{status}</span>
                              </div>
                            );
                          })}
                          {pvcRows.length > 6 && <p className="hbdr-app-detail-more">+{pvcRows.length - 6} more PVCs</p>}
                        </div>
                      ) : (
                        <p className="hbdr-app-detail-empty-line">{(selectedDetailApp.pvcCount || selectedDetailApp.resourceSummary?.pvcs || 0) > 0 ? 'PVC count is available, detailed PVC inventory is not reported yet.' : 'No PVC reported.'}</p>
                      )}
                    </div>
                  </div>
                </div>
                <div className="hbdr-filter-drawer-actions hbdr-app-detail-drawer-actions">
                  <button onClick={() => setSelectedDetailApp(null)}>Close</button>
                </div>
              </motion.div>
            </div>
          );
        })()}
      </AnimatePresence>
    </>
  );
}

function ClusterPage(props: {
  clusters: Cluster[];
  defaultClusterId: string | null;
  clusterMenuId: string | null;
  setClusterMenuId: (id: string | null) => void;
  setSelectedCluster: (cluster: Cluster) => void;
  setDefaultCluster: (cluster: Cluster, event?: React.MouseEvent) => void;
  clearDefaultCluster: (event?: React.MouseEvent) => void;
  unregisterCluster: (cluster: Cluster, event?: React.MouseEvent) => Promise<ApiTask | null>;
  onRenameCluster: (clusterId: string, name: string) => void;
  onUpgradeCluster: (clusterId: string) => Promise<void>;
  onRegisterCluster: (cluster: Cluster) => void;
  onRefreshRegistration: () => Promise<Cluster[]>;
  clusterTaskLogs: Record<string, ClusterTaskLog[]>;
  getAgentTokenForRegistration: () => Promise<ApiAgentToken>;
  prefetchAgentToken: () => Promise<ApiAgentToken | null> | null;
  openDashboard: () => void;
  toast: (msg: string) => void;
}) {
  const { clusters, defaultClusterId, clusterMenuId, setClusterMenuId, setSelectedCluster, setDefaultCluster, clearDefaultCluster, unregisterCluster, onRenameCluster, onUpgradeCluster, onRegisterCluster, onRefreshRegistration, clusterTaskLogs, getAgentTokenForRegistration, prefetchAgentToken, openDashboard, toast } = props;
  const [registerOpen, setRegisterOpen] = useState(false);
  const [registerStep, setRegisterStep] = useState<1 | 2 | 3>(1);
  const [copied, setCopied] = useState(false);
  const [caCopied, setCaCopied] = useState(false);
  const [prepareNodeCommand, setPrepareNodeCommand] = useState(`curl -sSL ${window.location.origin}/prepare-node.sh | bash`);
  const [installCommand, setInstallCommand] = useState(`curl -sSL ${window.location.origin}/install.sh | bash -s -- --token pending --endpoint ${window.location.origin.replace(/^http/, 'ws')}/ws/agent --executor-mode kubernetes`);
  const [installLoading, setInstallLoading] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [registrationBaseline, setRegistrationBaseline] = useState(0);
  const [registrationWaiting, setRegistrationWaiting] = useState(false);
  const [upgradeTarget, setUpgradeTarget] = useState<Cluster | null>(null);
  const [unregisterTarget, setUnregisterTarget] = useState<Cluster | null>(null);
  const [forceCleanupTarget, setForceCleanupTarget] = useState<Cluster | null>(null);
  const [renameTarget, setRenameTarget] = useState<Cluster | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [unregistering, setUnregistering] = useState(false);
  const [forceCleaning, setForceCleaning] = useState(false);
  const [upgradeSubmitting, setUpgradeSubmitting] = useState(false);
  const [unregisterTaskId, setUnregisterTaskId] = useState<string | null>(null);
  const [unregisterTask, setUnregisterTask] = useState<ApiTask | null>(null);
  const [unregisterEvents, setUnregisterEvents] = useState<ApiTaskEvent[]>([]);
  const [clusterResourceDetail, setClusterResourceDetail] = useState<{ cluster: Cluster; type: 'namespaces' | 'nodes' | 'storageClasses' } | null>(null);
  const [actionCopied, setActionCopied] = useState(false);
  const registryCACommandRef = useRef<HTMLTextAreaElement | null>(null);
  const installCommandRef = useRef<HTMLTextAreaElement | null>(null);
  const actionCommandRef = useRef<HTMLTextAreaElement | null>(null);
  const namespaceRowsForCluster = (cluster: Cluster): ClusterNamespaceRow[] => [...cluster.apps]
    .sort((a, b) => a.namespace.localeCompare(b.namespace))
    .map(app => ({
      name: app.namespace,
      status: app.namespaceStatus || app.status || 'Unknown',
      age: formatAge(app.resourceSummary?.ageSeconds),
    }));
  const nodeRowsForCluster = (cluster: Cluster) => {
    const details = cluster.nodeDetails || [];
    return [...details]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map(node => ({
        name: node.name,
        status: normalizeNodeStatus(node.status),
        roles: node.roles || '<none>',
        age: formatAge(node.ageSeconds),
      version: node.kubeletVersion || '-',
    }));
  };
  const storageClassRowsForCluster = (cluster: Cluster): ClusterStorageClassRow[] => [...(cluster.storageClasses || [])]
    .sort((a, b) => {
      if (Boolean(a.default) !== Boolean(b.default)) return a.default ? -1 : 1;
      return a.name.localeCompare(b.name);
    })
    .map(storageClass => ({
      name: `${storageClass.name}${storageClass.default ? ' (default)' : ''}`,
      provisioner: storageClass.provisioner || '-',
      reclaimPolicy: storageClass.reclaimPolicy || '-',
      volumeBindingMode: storageClass.volumeBindingMode || '-',
      allowVolumeExpansion: storageClass.allowVolumeExpansion || 'false',
      age: formatAge(storageClass.ageSeconds),
    }));
  const namespaceColumns = useMemo<HyperTableColumn<ClusterNamespaceRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 260,
      minSize: 150,
      maxSize: 560,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.name },
    },
    {
      accessorKey: 'status',
      header: 'STATUS',
      size: 112,
      minSize: 92,
      maxSize: 220,
      cell: info => <span className="hbdr-cluster-status-pill">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.status },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.age },
    },
  ], []);
  const nodeColumns = useMemo<HyperTableColumn<ClusterNodeRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 240,
      minSize: 150,
      maxSize: 520,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.name },
    },
    {
      accessorKey: 'status',
      header: 'STATUS',
      size: 110,
      minSize: 92,
      maxSize: 180,
      cell: info => <span className="hbdr-cluster-status-pill">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.status },
    },
    {
      accessorKey: 'roles',
      header: 'ROLES',
      size: 160,
      minSize: 110,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.roles },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.age },
    },
    {
      accessorKey: 'version',
      header: 'VERSION',
      size: 132,
      minSize: 108,
      maxSize: 220,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.version },
    },
  ], []);
  const storageClassColumns = useMemo<HyperTableColumn<ClusterStorageClassRow>[]>(() => [
    {
      accessorKey: 'name',
      header: 'NAME',
      size: 210,
      minSize: 150,
      maxSize: 420,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.name },
    },
    {
      accessorKey: 'provisioner',
      header: 'PROVISIONER',
      size: 230,
      minSize: 160,
      maxSize: 460,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.provisioner },
    },
    {
      accessorKey: 'reclaimPolicy',
      header: 'RECLAIMPOLICY',
      size: 128,
      minSize: 112,
      maxSize: 220,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.reclaimPolicy },
    },
    {
      accessorKey: 'volumeBindingMode',
      header: 'VOLUMEBINDINGMODE',
      size: 190,
      minSize: 150,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.volumeBindingMode },
    },
    {
      accessorKey: 'allowVolumeExpansion',
      header: 'ALLOWVOLUMEEXPANSION',
      size: 190,
      minSize: 150,
      maxSize: 320,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.allowVolumeExpansion },
    },
    {
      accessorKey: 'age',
      header: 'AGE',
      size: 76,
      minSize: 64,
      maxSize: 160,
      cell: info => <span className="hbdr-hyper-table-text">{String(info.getValue() || '-')}</span>,
      meta: { title: row => row.age },
    },
  ], []);
  useEffect(() => {
    if (!clusterMenuId) return;
    const closeMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest('[data-cluster-menu-root]')) return;
      setClusterMenuId(null);
    };
    window.addEventListener('click', closeMenu, true);
    return () => window.removeEventListener('click', closeMenu, true);
  }, [clusterMenuId, setClusterMenuId]);

  const openRegister = async () => {
    setRegisterStep(1);
    setCopied(false);
    setInstallError(null);
    setRegistrationBaseline(clusters.length);
    setRegistrationWaiting(false);
    setInstallLoading(true);
    setPrepareNodeCommand(`curl -sSL ${window.location.origin}/prepare-node.sh | bash`);
    try {
      const token = await getAgentTokenForRegistration();
      setPrepareNodeCommand(token.prepareNodeCommand || `curl -sSL ${window.location.origin}/prepare-node.sh | bash`);
      setInstallCommand(token.installCommand);
      setRegisterOpen(true);
      void prefetchAgentToken();
    } catch {
      setInstallError('Install token generation failed. Check whether the platform API is running.');
      setRegisterOpen(true);
      toast('Failed to generate install token');
    } finally {
      setInstallLoading(false);
    }
  };

  const closeRegister = () => {
    setRegisterOpen(false);
    setRegisterStep(1);
    setCopied(false);
    setCaCopied(false);
    setRegistrationWaiting(false);
  };

  const openUpgrade = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setActionCopied(false);
    setUpgradeTarget(cluster);
  };

  const closeUpgrade = () => {
    setUpgradeTarget(null);
    setActionCopied(false);
  };

  const openUnregister = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setClusterMenuId(null);
    setActionCopied(false);
    setUnregisterTarget(cluster);
  };

  const openForceCleanup = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setClusterMenuId(null);
    setActionCopied(false);
    setForceCleanupTarget(cluster);
  };

  const openRename = (cluster: Cluster, event: React.MouseEvent) => {
    event.stopPropagation();
    setClusterMenuId(null);
    setRenameValue(cluster.name === 'unknown-cluster' ? '' : cluster.name);
    setRenameTarget(cluster);
  };

  const closeRename = () => {
    setRenameTarget(null);
    setRenameValue('');
    setRenaming(false);
  };

  const closeUnregister = () => {
    setUnregisterTarget(null);
    setUnregisterTaskId(null);
    setUnregisterTask(null);
    setUnregisterEvents([]);
    setActionCopied(false);
  };

  const closeForceCleanup = () => {
    if (forceCleaning) return;
    setForceCleanupTarget(null);
    setActionCopied(false);
  };

  const copyInstallCommand = async () => {
    if (await copyTextToClipboard(installCommand, installCommandRef.current)) {
      setCopied(true);
      toast('Install command copied');
      window.setTimeout(() => setCopied(false), 1800);
    } else {
      installCommandRef.current?.focus();
      installCommandRef.current?.select();
      toast('Clipboard is unavailable. The install command is selected; press Ctrl+C to copy it.');
    }
  };

  const copyRegistryCACommand = async () => {
    if (await copyTextToClipboard(prepareNodeCommand, registryCACommandRef.current)) {
      setCaCopied(true);
      toast('Node prepare command copied');
      window.setTimeout(() => setCaCopied(false), 1800);
    } else {
      registryCACommandRef.current?.focus();
      registryCACommandRef.current?.select();
      toast('Clipboard is unavailable. The node prepare command is selected; press Ctrl+C to copy it.');
    }
  };

  const copyActionCommand = async (command: string) => {
    if (await copyTextToClipboard(command, actionCommandRef.current)) {
      setActionCopied(true);
      toast('Command copied');
      window.setTimeout(() => setActionCopied(false), 1800);
    } else {
      actionCommandRef.current?.focus();
      actionCommandRef.current?.select();
      toast('Clipboard is unavailable. The command is selected; press Ctrl+C to copy it.');
    }
  };

  const finishRegisterCluster = () => {
    toast('Waiting for the agent to connect. The cluster card appears after registration succeeds.');
    closeRegister();
  };

  useEffect(() => {
    if (!registerOpen || registerStep !== 3) return;
    let cancelled = false;
    setRegistrationWaiting(true);
    const poll = async () => {
      try {
        const nextClusters = await onRefreshRegistration();
        if (cancelled) return;
        if (nextClusters.length > registrationBaseline) {
          const latest = nextClusters[0];
          if (latest) {
            setSelectedCluster(latest);
          }
          toast(`${latest?.name || 'Cluster'} registered and connected`);
          closeRegister();
        }
      } catch {
        if (!cancelled) setInstallError('Waiting for agent connection. The platform API is temporarily unreachable.');
      }
    };
    poll();
    const timer = window.setInterval(poll, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [registerOpen, registerStep, registrationBaseline, onRefreshRegistration, setSelectedCluster, toast]);

  useEffect(() => {
    if (!registerOpen || registrationWaiting || clusters.length <= registrationBaseline) return;
    const latest = clusters[0];
    if (latest) {
      setSelectedCluster(latest);
      toast(`${latest.name === 'unknown-cluster' ? 'Cluster' : latest.name} registered and connected`);
    }
    closeRegister();
  }, [clusters, registerOpen, registrationBaseline, registrationWaiting, setSelectedCluster, toast]);

  useEffect(() => {
    if (!unregisterTaskId) return;
    let cancelled = false;
    const loadTask = async () => {
      try {
        const [taskRes, eventRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
          apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${unregisterTaskId}/events`),
        ]);
        if (cancelled) return;
        const task = listItems(taskRes).find(item => item.id === unregisterTaskId) || null;
        setUnregisterTask(task);
        setUnregisterEvents(listItems(eventRes));
        if (task?.status === 'succeeded') {
          toast('Cluster unregister completed');
          void onRefreshRegistration();
          setUnregisterTaskId(null);
        }
        if (task?.status === 'failed') {
          toast(task.errorMessage || 'Cluster unregister failed');
          setUnregisterTaskId(null);
        }
      } catch {
        if (!cancelled) toast('Failed to refresh unregister task status');
      }
    };
    loadTask();
    const timer = window.setInterval(loadTask, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [unregisterTaskId, onRefreshRegistration, toast]);

  const finishUpgradeCluster = async () => {
    if (!upgradeTarget) return;
    setUpgradeSubmitting(true);
    try {
      await onUpgradeCluster(upgradeTarget.id);
      toast(`${upgradeTarget.name} agent upgrade submitted`);
      closeUpgrade();
    } catch (error) {
      toast(error instanceof Error ? error.message : 'Agent upgrade failed to submit');
    } finally {
      setUpgradeSubmitting(false);
    }
  };

  const finishUnregisterCluster = async () => {
    if (!unregisterTarget) return;
    setUnregistering(true);
    const task = await unregisterCluster(unregisterTarget);
    if (task) {
      setUnregisterTaskId(task.id);
      setUnregisterTask(task);
      setUnregisterEvents([]);
      setUnregisterTarget(null);
      toast('Unregister task created. Track progress in Recent Tasks.');
    }
    setUnregistering(false);
  };

  const finishForceCleanupCluster = async () => {
    if (!forceCleanupTarget) return;
    const target = forceCleanupTarget;
    setForceCleaning(true);
    try {
      const result = await apiPost<{ warning?: string }>(`/api/v1/clusters/${target.id}/force-cleanup`, {
        reason: 'requested from platform cluster page',
      });
      setForceCleanupTarget(null);
      toast(result.warning || `${target.name} force cleanup completed`);
      await onRefreshRegistration();
    } catch (error) {
      toast(`Force cleanup failed: ${error instanceof Error ? error.message : 'unknown error'}`);
    } finally {
      setForceCleaning(false);
    }
  };

  const finishRenameCluster = async () => {
    if (!renameTarget) return;
    const target = renameTarget;
    const previousName = target.name;
    const nextName = renameValue.trim();
    if (!nextName) {
      toast('Cluster name is required');
      return;
    }
    if (nextName === previousName) {
      closeRename();
      return;
    }
    setRenaming(true);
    onRenameCluster(target.id, nextName);
    closeRename();
    try {
      const updated = await apiPatch<ApiCluster>(`/api/v1/clusters/${target.id}`, { name: nextName });
      onRenameCluster(target.id, updated.name || nextName);
      void onRefreshRegistration().catch(() => {
        toast('Cluster name updated, but refresh failed');
      });
      toast('Cluster name updated');
    } catch (err) {
      onRenameCluster(target.id, previousName);
      toast(`Failed to update cluster name: ${err instanceof Error ? err.message : 'unknown error'}`);
    }
  };

  return (
    <motion.div key="clusters" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Clusters" desc="Register clusters and maintain the default cluster." />

      {clusters.length > 0 ? (
        <div className="hbdr-section-card">
          <div className="hbdr-section-toolbar">
            <div>
              <h3>Registered Clusters</h3>
            </div>
            <button type="button" onClick={openRegister} className="hbdr-dr-action-primary inline-flex items-center gap-1.5"><Plus size={14} />Register Cluster</button>
          </div>
          <div className="flex flex-wrap gap-3 p-3">
            {clusters.map(cluster => (
              <motion.div key={cluster.id} whileHover={{ y: -2 }} className={`cluster-card-premium ${cluster.connectionStatus !== 'online' ? 'cluster-card-offline' : ''} relative w-full cursor-default overflow-visible rounded-xl border border-slate-200 bg-white p-3.5 shadow-sm transition-all hover:border-blue-200 hover:shadow-lg md:w-[340px] group ${clusterMenuId === cluster.id ? 'z-40' : 'z-0'}`}>
              {(() => {
                const readiness = agentReadiness(cluster);
                const unregisterTaskForCluster = unregisterTask?.clusterId === cluster.id ? unregisterTask : null;
                const unregisterActive = unregisterTaskForCluster && !['succeeded', 'failed'].includes(unregisterTaskForCluster.status);
                return (
                  <>
              {unregisterTaskForCluster && (
                <div className={`absolute left-5 top-5 z-20 rounded-full border px-2 py-0.5 text-[10px] font-bold ${unregisterTaskForCluster.status === 'failed' ? 'border-rose-100 bg-rose-50 text-rose-700' : unregisterTaskForCluster.status === 'succeeded' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>
                  {unregisterActive ? `Unregistering ${formatPercent(unregisterTaskForCluster.progress || 0)}%` : `Unregister ${taskStatusLabel(unregisterTaskForCluster.status)}`}
                </div>
              )}
              <div className="absolute right-5 top-5 z-20" data-cluster-menu-root>
                <button onClick={(event) => { event.stopPropagation(); setClusterMenuId(clusterMenuId === cluster.id ? null : cluster.id); }} className="cluster-card-action-button flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700" aria-label="Cluster Actions">
                  <MoreVertical size={17} />
                </button>
                <AnimatePresence>
                  {clusterMenuId === cluster.id && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={(event) => { event.stopPropagation(); setClusterMenuId(null); }} />
                      <motion.div data-cluster-menu-root initial={{ opacity: 0, scale: 0.96, y: 8 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.96, y: 8 }} className="cluster-card-action-menu absolute right-0 top-9 z-50 w-44 rounded-xl border border-slate-100 bg-white py-2 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5" onClick={(event) => event.stopPropagation()}>
                        <button onClick={(event) => openRename(cluster, event)} className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-slate-600 hover:bg-slate-50"><Edit2 size={15} />Edit Name</button>
                        <button onClick={(event) => openUnregister(cluster, event)} className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-rose-600 hover:bg-rose-50"><Trash2 size={15} />Unregister Cluster</button>
                        <button onClick={(event) => openForceCleanup(cluster, event)} className="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-rose-700 hover:bg-rose-50"><Trash2 size={15} />Force Cleanup</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>

              <div className="cluster-card-head mb-2 flex items-start justify-between">
                <div className="cluster-card-icon rounded-lg bg-slate-50 p-2 transition-colors group-hover:bg-blue-50"><Server className="text-blue-600" size={20} /></div>
                <div className="cluster-card-state-stack flex flex-col items-end gap-1.5 pr-10">
                  <ProtectionBadge cluster={cluster} />
                  {cluster.id === defaultClusterId ? (
                    <button type="button" onClick={(event) => clearDefaultCluster(event)} className="cluster-default-button cluster-default-button-active inline-flex items-center gap-1 rounded-full border border-blue-100 bg-blue-50 px-2 py-0.5 text-[10px] font-semibold text-blue-700 transition-colors hover:border-blue-200 hover:bg-blue-100">
                      <Star size={10} className="fill-blue-500 text-blue-500" />Default
                    </button>
                  ) : (
                    <button type="button" onClick={(event) => setDefaultCluster(cluster, event)} className="cluster-default-button inline-flex items-center gap-1 rounded-full border border-slate-200 bg-white px-2 py-0.5 text-[10px] font-semibold text-slate-500 transition-colors hover:border-blue-200 hover:bg-blue-50 hover:text-blue-700">
                      <Star size={10} />Default
                    </button>
                  )}
                </div>
              </div>
              {renameTarget?.id === cluster.id ? (
                <div className="mb-1 flex min-w-0 items-center gap-1.5 pr-10">
                  <input
                    value={renameValue}
                    onChange={event => setRenameValue(event.target.value)}
                    onClick={event => event.stopPropagation()}
                    onKeyDown={event => {
                      if (event.key === 'Enter') {
                        event.preventDefault();
                        finishRenameCluster();
                      }
                      if (event.key === 'Escape') {
                        event.preventDefault();
                        closeRename();
                      }
                    }}
                    autoFocus
                    placeholder="source-cluster-01"
                    className="h-8 min-w-0 flex-1 rounded border border-blue-200 bg-white px-2.5 text-[1rem] font-extrabold tracking-tight text-slate-900 outline-none focus:border-blue-500"
                    aria-label="Cluster display name"
                  />
                  <button type="button" disabled={renaming} onClick={(event) => { event.stopPropagation(); finishRenameCluster(); }} className="flex h-8 w-8 shrink-0 items-center justify-center rounded bg-blue-600 text-white transition-colors hover:bg-blue-700 disabled:cursor-wait disabled:bg-blue-300" aria-label="Save cluster name">
                    <Check size={14} />
                  </button>
                  <button type="button" disabled={renaming} onClick={(event) => { event.stopPropagation(); closeRename(); }} className="flex h-8 w-8 shrink-0 items-center justify-center rounded border border-slate-200 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-600 disabled:cursor-wait disabled:opacity-60" aria-label="Cancel cluster name edit">
                    <X size={14} />
                  </button>
                </div>
              ) : (
                <div className="mb-1 flex min-w-0 items-center gap-2 pr-10">
                  <h4 className={`cluster-card-title min-w-0 truncate text-[1.08rem] font-extrabold tracking-tight transition-colors group-hover:text-blue-700 ${cluster.name === 'unknown-cluster' ? 'text-slate-500' : 'text-slate-950'}`}>{cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : cluster.name}</h4>
                  <button type="button" onClick={(event) => openRename(cluster, event)} className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded text-slate-400 transition-colors hover:bg-blue-50 hover:text-blue-600" aria-label="Edit cluster name">
                    <Edit2 size={14} />
                  </button>
                </div>
              )}
              <p className="mb-2 break-all font-mono text-[10px] font-semibold leading-4 text-slate-500">{cluster.id.slice(0,8)}…</p>
              <p className="cluster-card-meta mb-2 text-[11px] font-medium text-slate-500">Kubernetes {cluster.version} · {cluster.connectionStatus === 'online' ? 'Online' : 'Offline'}</p>
              {cluster.connectionStatus !== 'online' && (
                <div className="cluster-offline-alert mb-2">
                  <AlertTriangle size={13} />
                  <span>Agent offline. Reconnecting...</span>
                </div>
              )}
              <div className="cluster-agent-panel mb-2 grid grid-cols-3 gap-1.5 rounded-md border border-transparent bg-slate-50 px-2 py-1.5">
                <div>
                  <span className="block text-[9px] font-semibold uppercase tracking-wider text-slate-400">Agent</span>
                  <p className="truncate font-mono text-[11px] font-semibold leading-tight text-slate-700">{cluster.agentVersion}</p>
                  {cluster.agentUpgradeAvailable && cluster.connectionStatus === 'online' && (
                    <button
                      type="button"
                      onClick={(event) => openUpgrade(cluster, event)}
                      className="mt-0.5 block max-w-full truncate text-left font-mono text-[10px] font-semibold leading-tight text-blue-600 hover:text-blue-700"
                      title={`Update available: ${cluster.latestAgentVersion || ''}@${shortDigest(cluster.latestAgentImageDigest)}`}
                    >
                      Update available: {cluster.latestAgentVersion || 'new'}{cluster.latestAgentImageDigest ? `@${shortDigest(cluster.latestAgentImageDigest)}` : ''}
                    </button>
                  )}
                  {cluster.agentUpgradeStatus === 'upgrading' && !cluster.agentUpgradeAvailable && (
                    <span className="mt-0.5 block truncate text-[10px] font-semibold leading-tight text-blue-600">Upgrading...</span>
                  )}
                </div>
                <div><span className="block text-[9px] font-semibold uppercase tracking-wider text-slate-400">Status</span><p className={`truncate text-[11px] font-semibold leading-tight ${readiness.className}`}>{readiness.label}</p></div>
                <div><span className="block text-[9px] font-semibold uppercase tracking-wider text-slate-400">Last Seen</span><p className="text-[11px] font-semibold leading-tight text-slate-700">{formatLastSeen(cluster.lastSeenAt)}</p></div>
              </div>
              <div className="cluster-metrics-grid grid grid-cols-3 gap-2 border-t border-slate-50 pt-2 text-xs">
                <Metric label="Namespaces" value={cluster.namespaces} onClick={() => setClusterResourceDetail({ cluster, type: 'namespaces' })} />
                <Metric label="Nodes" value={cluster.nodes} onClick={() => setClusterResourceDetail({ cluster, type: 'nodes' })} />
                <Metric label="StorageClasses" value={(cluster.storageClasses || []).length} onClick={() => setClusterResourceDetail({ cluster, type: 'storageClasses' })} />
              </div>
              <button type="button" onClick={() => { setSelectedCluster(cluster); openDashboard(); }} className="cluster-entry-bar mt-2 flex w-full items-center justify-between rounded-md border border-transparent bg-slate-50/70 px-2.5 py-1.5 text-left text-[11px] font-semibold text-slate-500 transition-all hover:border-blue-100 hover:bg-blue-50/80 hover:text-blue-700">
                <span>DR Workspace</span><span className="flex items-center gap-1">Enter <ChevronRight size={13} /></span>
              </button>
                  </>
                );
              })()}
              </motion.div>
            ))}
          </div>
        </div>
      ) : (

        <div className="rounded-2xl border border-dashed border-slate-200 bg-white p-14 text-center shadow-sm">
          <Server size={36} className="mx-auto mb-3 text-slate-300" />
          <h3 className="text-sm font-bold text-slate-800">No registered clusters yet</h3>
          <p className="mt-1 text-xs text-slate-400">Register first Kubernetes cluster, Agent After reconnection, enter the Container DR console.</p>
          <button onClick={openRegister} className="mt-5 inline-flex items-center gap-1.5 rounded-xl bg-blue-600 px-5 py-2.5 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700"><Plus size={15} />Register Cluster</button>
        </div>
      )}

      <AnimatePresence>
        {clusterResourceDetail && (() => {
          const isNamespaces = clusterResourceDetail.type === 'namespaces';
          const isStorageClasses = clusterResourceDetail.type === 'storageClasses';
          const isNodes = clusterResourceDetail.type === 'nodes';
          const cluster = clusterResourceDetail.cluster;
          const namespaceRows = namespaceRowsForCluster(clusterResourceDetail.cluster);
          const nodeRows = nodeRowsForCluster(clusterResourceDetail.cluster);
          const storageClassRows = storageClassRowsForCluster(clusterResourceDetail.cluster);
          const hasRealNodeDetails = (clusterResourceDetail.cluster.nodeDetails || []).length > 0;
          const protectedCount = getProtectedAppCount(cluster);
          const readiness = agentReadiness(cluster);
          return (
            <>
              <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => setClusterResourceDetail(null)} />
              <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-cluster-detail-drawer">
                <div className="hbdr-filter-drawer-head hbdr-cluster-detail-drawer-head">
                  <div>
                    <strong>Cluster Details</strong>
                    <p>{cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : cluster.name}</p>
                    <div className="hbdr-cluster-resource-status-line">
                      <span className={`hbdr-cluster-connection-dot ${cluster.connectionStatus === 'online' ? 'is-online' : 'is-offline'}`} />
                      <strong>{cluster.connectionStatus === 'online' ? 'Online' : 'Offline'}</strong>
                      <span>Kubernetes {cluster.version}</span>
                      <span>Last seen {formatLastSeen(cluster.lastSeenAt)}</span>
                    </div>
                  </div>
                  <button type="button" onClick={() => setClusterResourceDetail(null)} aria-label="Close details"><X size={18} /></button>
                </div>
                <div className="hbdr-filter-drawer-body hbdr-cluster-detail-drawer-body">
                  <section className="hbdr-cluster-detail-section hbdr-cluster-detail-overview">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Overview</h4>
                    </div>
                    <div className="hbdr-cluster-overview-grid">
                      <div><span>Nodes</span><strong>{cluster.nodes}</strong></div>
                      <div><span>Namespaces</span><strong>{cluster.namespaces}</strong></div>
                      <div><span>Storage Classes</span><strong>{(cluster.storageClasses || []).length}</strong></div>
                      <div><span>Applications</span><strong>{cluster.applications}</strong></div>
                      <div><span>Protected</span><strong>{protectedCount}</strong></div>
                      <div><span>Restore Status</span><strong>{cluster.veleroStatus || 'Unknown'}</strong></div>
                    </div>
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isNodes ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Nodes</h4>
                        <p>{nodeRows.length || cluster.nodes} total</p>
                      </div>
                      <span>kubectl get nodes</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={nodeColumns}
                      data={nodeRows}
                      getRowId={row => row.name}
                      emptyMessage="Node details are waiting for the next agent inventory report."
                    />
                    {!hasRealNodeDetails && (
                      <p className="hbdr-cluster-resource-note">Node detail rows will show real Kubernetes node names after the next detailed inventory report from the agent.</p>
                    )}
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isNamespaces ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Namespaces</h4>
                        <p>{namespaceRows.length || cluster.namespaces} total</p>
                      </div>
                      <span>kubectl get namespaces</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={namespaceColumns}
                      data={namespaceRows}
                      getRowId={row => row.name}
                      emptyMessage="Namespace details are waiting for the next agent inventory report."
                    />
                  </section>

                  <section className={`hbdr-cluster-detail-section ${isStorageClasses ? 'is-active' : ''}`}>
                    <div className="hbdr-cluster-detail-section-head">
                      <div>
                        <h4>Storage Classes</h4>
                        <p>{storageClassRows.length} total</p>
                      </div>
                      <span>kubectl get storageclass</span>
                    </div>
                    <HyperTable
                      variant="modal"
                      density="compact"
                      columns={storageClassColumns}
                      data={storageClassRows}
                      getRowId={row => row.name}
                      emptyMessage="StorageClass details are waiting for the next agent inventory report."
                    />
                  </section>

                  <section className="hbdr-cluster-detail-section">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Agent</h4>
                    </div>
                    <div className="hbdr-cluster-key-values">
                      <div><span>Status</span><strong className={readiness.className}>{readiness.label}</strong></div>
                      <div><span>Version</span><strong>{cluster.agentVersion}</strong></div>
                      <div><span>Latest</span><strong>{cluster.latestAgentVersion}{cluster.latestAgentImageDigest ? `@${shortDigest(cluster.latestAgentImageDigest)}` : ''}</strong></div>
                      <div><span>Namespace</span><strong>hypercdr-agent</strong></div>
                      <div><span>Last Heartbeat</span><strong>{formatLastSeen(cluster.lastSeenAt)}</strong></div>
                      <div><span>Upgrade</span><strong>{cluster.agentUpgradeAvailable ? 'Available' : cluster.agentUpgradeStatus === 'upgrading' ? 'Upgrading' : 'Current'}</strong></div>
                    </div>
                  </section>

                  <section className="hbdr-cluster-detail-section">
                    <div className="hbdr-cluster-detail-section-head">
                      <h4>Protection</h4>
                    </div>
                    <div className="hbdr-cluster-key-values">
                      <div><span>Protected Applications</span><strong>{protectedCount}</strong></div>
                      <div><span>Unprotected Applications</span><strong>{Math.max(0, cluster.apps.length - protectedCount)}</strong></div>
                      <div><span>Protection State</span><strong>{isClusterProtected(cluster) ? 'Protected' : 'Unprotected'}</strong></div>
                    </div>
                  </section>
                </div>
              </motion.div>
            </>
          );
        })()}
      </AnimatePresence>

<ClusterActivityPanel logs={clusterTaskLogs} clusters={clusters} />
            <AnimatePresence>
        {registerOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeRegister} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative max-h-[82vh] w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="max-h-[82vh] overflow-y-auto p-4">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-xl font-bold tracking-tight text-slate-900"><PlusCircle className="text-blue-600" />Register New Cluster</h2>
                    <p className="mt-1 text-xs text-slate-500">Install node trust, then install the agent stack.</p>
                  </div>
                  <button onClick={closeRegister} className="rounded-full p-2 transition-colors hover:bg-slate-100"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="space-y-4">
                  <div className="flex gap-3 rounded-xl border border-blue-100 bg-blue-50 p-3">
                    <div className="mt-1"><ShieldCheck size={20} className="text-blue-600" /></div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-blue-900">1. Install Harbor CA on every node</p>
                      <p className="leading-relaxed text-blue-700">Run on every Kubernetes node to trust the internal registry.</p>
                    </div>
                  </div>

                  <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - prepare node</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <textarea
                          ref={registryCACommandRef}
                          readOnly
                          value={prepareNodeCommand}
                          className="h-[34px] flex-1 resize-none overflow-auto border-0 bg-transparent p-0 font-mono text-[11px] leading-5 text-blue-300 outline-none"
                          aria-label="Registry CA command"
                          onFocus={event => event.currentTarget.select()}
                        />
                      </div>
                    </div>
                    <button onClick={copyRegistryCACommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95">
                      {caCopied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {caCopied ? 'Copied' : 'Copy'}
                    </button>
                  </div>

                  <div className="flex gap-3 rounded-xl border border-blue-100 bg-blue-50 p-3">
                    <div className="mt-1"><Terminal size={18} className="text-blue-600" /></div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-blue-900">2. Install agent stack</p>
                      <p className="leading-relaxed text-blue-700">Run once with cluster-admin kubectl access to install Velero and comm-agent.</p>
                    </div>
                  </div>

                  <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - install agent</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <textarea
                          ref={installCommandRef}
                          readOnly
                          value={installLoading ? 'Generating install command...' : installCommand}
                          className="h-[48px] flex-1 resize-none overflow-auto border-0 bg-transparent p-0 font-mono text-[11px] leading-5 text-blue-300 outline-none"
                          aria-label="Install command"
                          onFocus={event => event.currentTarget.select()}
                        />
                      </div>
                    </div>
                    <button disabled={installLoading} onClick={copyInstallCommand} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95 disabled:cursor-wait disabled:opacity-60">
                      {copied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  {installError && <p className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-medium text-rose-700">{installError}</p>}

                  <div className="flex gap-3 rounded-xl border border-emerald-100 bg-emerald-50 p-3">
                    <div className="mt-1">
                      {registrationWaiting ? <RefreshCw size={20} className="animate-spin text-emerald-600" /> : <CheckCircle2 size={20} className="text-emerald-600" />}
                    </div>
                    <div className="text-sm">
                      <p className="mb-1 font-bold text-emerald-900">3. Wait for connection</p>
                      <p className="leading-relaxed text-emerald-700">Start waiting after the install command finishes. The cluster appears after agent connection.</p>
                    </div>
                  </div>

                  <div className="flex justify-end gap-3 pt-1">
                    <button onClick={closeRegister} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                    {registrationWaiting ? (
                      <button onClick={finishRegisterCluster} className="rounded-xl bg-emerald-600 px-6 py-2 font-bold text-white shadow-lg shadow-emerald-200 transition-all hover:bg-emerald-700 active:scale-95">Continue in Background</button>
                    ) : (
                      <button disabled={installLoading} onClick={() => setRegisterStep(3)} className="rounded-xl bg-blue-600 px-6 py-2 font-bold text-white shadow-lg shadow-blue-200 transition-all hover:bg-blue-700 active:scale-95 disabled:cursor-wait disabled:opacity-60">Start Waiting</button>
                    )}
                  </div>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {upgradeTarget && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeUpgrade} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="p-8">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-2xl font-bold tracking-tight text-slate-900"><Upload className="text-blue-600" />Upgrade Agent</h2>
                    <p className="mt-1 text-sm text-slate-500">The agent deployment will roll out and reconnect automatically after the new pod starts.</p>
                  </div>
                  <button onClick={closeUpgrade} className="rounded-full p-2 transition-colors hover:bg-slate-100"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="mb-5 rounded-xl border border-blue-100 bg-blue-50 p-4 text-sm text-blue-700">
                  <p className="font-bold text-blue-900">{upgradeTarget.name}</p>
                  <p className="mt-1">Current Version {upgradeTarget.agentVersion}{upgradeTarget.agentImageDigest ? `@${shortDigest(upgradeTarget.agentImageDigest)}` : ''}</p>
                  <p className="mt-1">Target Version {upgradeTarget.latestAgentVersion}{upgradeTarget.latestAgentImageDigest ? `@${shortDigest(upgradeTarget.latestAgentImageDigest)}` : ''}</p>
                </div>

                <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
                  Confirm the upgrade only when no backup, restore, or cleanup task is running on this cluster. The agent connection may briefly show offline during rollout.
                </div>

                <div className="mt-8 flex justify-end gap-3">
                  <button onClick={closeUpgrade} disabled={upgradeSubmitting} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60">Cancel</button>
                  <button onClick={finishUpgradeCluster} disabled={upgradeSubmitting} className="rounded-xl bg-blue-600 px-6 py-2 font-bold text-white shadow-lg shadow-blue-200 transition-all hover:bg-blue-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-60">{upgradeSubmitting ? 'Upgrading...' : 'Upgrade'}</button>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {unregisterTarget && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeUnregister} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="p-8">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-2xl font-bold tracking-tight text-slate-900"><Trash2 className="text-rose-600" />Unregister Cluster</h2>
                    <p className="mt-1 text-sm text-slate-500">Send an unregister task to the cluster agent and remove the cluster after agent cleanup succeeds.</p>
                  </div>
                  <button onClick={closeUnregister} className="rounded-full p-2 transition-colors hover:bg-slate-100"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="mb-5 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700">
                  <p className="font-bold text-rose-900">Confirm unregister</p>
                  <p className="mt-2">The platform will send an unregister task to the cluster agent. The agent will uninstall HyperCDR components from this Kubernetes cluster, including the agent namespace and Velero resources configured by HyperCDR.</p>
                  <p className="mt-2 font-semibold text-rose-800">This is a destructive operation for the HyperCDR management stack on this cluster. Existing application workloads are not intentionally removed, but backup/restore management for this cluster will stop after cleanup.</p>
                </div>

                <div className="mb-5 rounded-xl border border-slate-100 bg-slate-50 p-4 text-sm">
                  <p className="font-bold text-slate-900">{unregisterTarget.name === 'unknown-cluster' ? 'Unnamed cluster' : unregisterTarget.name}</p>
                  <p className="mt-1 break-all font-mono text-[11px] text-slate-500">{unregisterTarget.id}</p>
                </div>

                <div className="mt-8 flex justify-end gap-3">
                  <button onClick={closeUnregister} disabled={unregistering} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-wait disabled:opacity-60">Cancel</button>
                  <button disabled={unregistering} onClick={finishUnregisterCluster} className="rounded-xl bg-rose-600 px-6 py-2 font-bold text-white shadow-lg shadow-rose-200 transition-all hover:bg-rose-700 active:scale-95 disabled:cursor-wait disabled:bg-rose-300">{unregistering ? 'Creating Task...' : 'Confirm Unregister'}</button>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {forceCleanupTarget && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closeForceCleanup} className="absolute inset-0 bg-slate-900/15" />
            <motion.div initial={{ opacity: 0, scale: 0.95, y: 20 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.95, y: 20 }} className="relative w-full max-w-xl overflow-hidden rounded-2xl bg-white shadow-2xl">
              <div className="p-7">
                <div className="mb-4 flex items-start justify-between">
                  <div>
                    <h2 className="flex items-center gap-2 text-xl font-bold tracking-tight text-slate-900"><Trash2 className="text-rose-600" />Force Cleanup</h2>
                    <p className="mt-1 text-sm text-slate-500">Clean platform records and object storage data when the cluster agent is no longer available.</p>
                  </div>
                  <button onClick={closeForceCleanup} disabled={forceCleaning} className="rounded-full p-2 transition-colors hover:bg-slate-100 disabled:cursor-wait disabled:opacity-50"><X size={20} className="text-slate-400" /></button>
                </div>

                <div className="mb-5 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700">
                  <p className="font-bold text-rose-900">This operation bypasses the cluster agent.</p>
                  <p className="mt-2">The platform will delete this cluster's HyperCDR records, protection plans, tasks, restore points, storage bindings, and object storage data under the cluster prefix.</p>
                  <p className="mt-2 font-semibold text-rose-800">Kubernetes cluster-scoped resources cannot be verified if the agent namespace was already removed. Check the source cluster manually for remaining Velero CRDs, ClusterRoles, and ClusterRoleBindings.</p>
                </div>

                <div className="mb-5 rounded-xl border border-slate-100 bg-slate-50 p-4 text-sm">
                  <p className="font-bold text-slate-900">{forceCleanupTarget.name === 'unknown-cluster' ? 'Unnamed cluster' : forceCleanupTarget.name}</p>
                  <p className="mt-1 break-all font-mono text-[11px] text-slate-500">{forceCleanupTarget.id}</p>
                  <p className="mt-2 break-all font-mono text-[11px] text-slate-500">hypercdr/clusters/{forceCleanupTarget.id}/</p>
                </div>

                <div className="mt-7 flex justify-end gap-3">
                  <button onClick={closeForceCleanup} disabled={forceCleaning} className="rounded-xl px-5 py-2 font-medium text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-wait disabled:opacity-60">Cancel</button>
                  <button disabled={forceCleaning} onClick={finishForceCleanupCluster} className="rounded-xl bg-rose-600 px-6 py-2 font-bold text-white shadow-lg shadow-rose-200 transition-all hover:bg-rose-700 active:scale-95 disabled:cursor-wait disabled:bg-rose-300">{forceCleaning ? 'Cleaning...' : 'Confirm Cleanup'}</button>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function ClusterActivityPanel({ logs, clusters }: { logs: Record<string, ClusterTaskLog[]>; clusters: Cluster[] }) {
  const entries = Object.entries(logs)
    .flatMap(([clusterId, clusterLogs]) => {
      const cluster = clusters.find(item => item.id === clusterId) || null;
      return clusterLogs.map(log => ({ cluster, clusterId, log }));
    })
    .sort((a, b) => (b.log.task.createdAt || '').localeCompare(a.log.task.createdAt || ''));
  const visibleEntries = entries.filter(entry => !['succeeded', 'failed', 'canceled'].includes(entry.log.task.status || '')).concat(entries.filter(entry => ['succeeded', 'failed', 'canceled'].includes(entry.log.task.status || '')).slice(0, 8));
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const taskTypeLabel = (type: string) => type === 'register' ? 'Register cluster' : type === 'unregister' ? 'Unregister cluster' : type;
  const taskInitials = (entry: { log: ClusterTaskLog }) => entry.log.task.type === 'register' ? 'R' : entry.log.task.type === 'unregister' ? 'U' : (entry.log.task.type || '?').charAt(0).toUpperCase();
  const clusterDisplay = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    if (entry.cluster) return entry.cluster.name === 'unknown-cluster' ? 'Unnamed cluster' : entry.cluster.name;
    const archived = entry.log.task.payload?.archivedClusterId as string | undefined;
    if (archived) return 'Unregistered cluster';
    return (entry.log.task.payload?.clusterName as string) || 'Unnamed cluster';
  };
  const clusterInitials = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    const name = clusterDisplay(entry);
    const tokens = name.split(/[^a-zA-Z0-9]+/).filter(Boolean);
    if (tokens.length === 0) return 'CL';
    if (tokens.length === 1) return tokens[0].slice(0, 2).toUpperCase();
    return (tokens[0][0] + tokens[1][0]).toUpperCase();
  };
  const targetLabel = (entry: { cluster: Cluster | null; clusterId: string; log: ClusterTaskLog }) => {
    if (entry.cluster) return entry.cluster.id;
    const archived = entry.log.task.payload?.archivedClusterId as string | undefined;
    return archived || entry.clusterId;
  };
  const isActive = (task: ApiTask) => !['succeeded', 'failed', 'canceled'].includes(task.status || '');
  const activityColumns: HyperTableColumn<typeof visibleEntries[number]>[] = [
    {
      id: 'task',
      header: 'Task',
      accessorFn: entry => taskTypeLabel(entry.log.task.type),
      size: 180,
      minSize: 150,
      maxSize: 280,
      cell: info => {
        const entry = info.row.original;
        return (
          <div className="flex min-w-0 items-center gap-2">
            <span className={`flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full text-[10px] font-black text-white ${entry.log.task.type === 'register' ? 'bg-blue-500' : 'bg-rose-500'}`}>{taskInitials(entry)}</span>
            <div className="min-w-0">
              <p className="truncate font-bold text-slate-900">{taskTypeLabel(entry.log.task.type)}</p>
              <p className="truncate font-mono text-[10px] text-slate-400">{entry.log.task.id.slice(0, 8)}</p>
            </div>
          </div>
        );
      },
      meta: { title: entry => `${taskTypeLabel(entry.log.task.type)} / ${entry.log.task.id}` },
    },
    {
      id: 'target',
      header: 'Target',
      accessorFn: entry => clusterDisplay(entry),
      size: 260,
      minSize: 180,
      maxSize: 420,
      cell: info => {
        const entry = info.row.original;
        return (
          <div className="flex min-w-0 items-center gap-2">
            <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-slate-100 text-[9px] font-black text-slate-500">{clusterInitials(entry)}</span>
            <div className="min-w-0">
              <p className="truncate font-semibold text-slate-700">{clusterDisplay(entry)}</p>
              <p className="truncate font-mono text-[10px] text-slate-400">{targetLabel(entry)}</p>
            </div>
          </div>
        );
      },
      meta: { title: entry => `${clusterDisplay(entry)} / ${targetLabel(entry)}` },
    },
    {
      id: 'status',
      header: 'Status',
      accessorFn: entry => entry.log.task.status || '',
      size: 130,
      minSize: 110,
      cell: info => (
        <div>
          <p className={`font-bold ${taskStatusClass(info.row.original.log.task.status)}`}>{taskStatusLabel(info.row.original.log.task.status)}</p>
          <p className="text-[9px] text-slate-400">{formatPercent(info.row.original.log.task.progress || 0)}%</p>
        </div>
      ),
      meta: { title: entry => `${taskStatusLabel(entry.log.task.status)} ${formatPercent(entry.log.task.progress || 0)}%` },
    },
    {
      id: 'progress',
      header: 'Progress / Latest Event',
      accessorFn: entry => entry.log.events.at(-1)?.message || entry.log.task.errorMessage || '',
      size: 300,
      minSize: 220,
      maxSize: 520,
      cell: info => {
        const entry = info.row.original;
        const active = isActive(entry.log.task);
        const lastEvent = entry.log.events.length > 0 ? entry.log.events[entry.log.events.length - 1] : null;
        const volume = taskProgressInfo(entry.log.task, entry.log.events);
        const latestMessage = volume
          ? [
              `${formatBytes(volume.bytesDone)} / ${formatBytes(volume.totalBytes)}`,
              formatBytesPerSecond(volume.speedBytesPerSecond),
              formatEta(volume.etaSeconds),
            ].filter(Boolean).join(' · ')
          : lastEvent?.message || (active ? 'Awaiting task events from agent...' : (entry.log.task.errorMessage || 'No events recorded yet.'));
        const progress = volume ? volume.percent : (active ? 0 : (entry.log.task.progress || 0));
        return (
          <div className="flex min-w-0 items-center gap-2">
            <div className="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-slate-100">
              <div className={`h-full rounded-full transition-all ${entry.log.task.status === 'failed' ? 'bg-rose-500' : entry.log.task.status === 'succeeded' ? 'bg-emerald-500' : 'bg-blue-500'}`} style={{ width: `${Math.max(4, progress)}%` }} />
            </div>
            <p className={`truncate ${lastEvent?.level === 'error' ? 'text-rose-600' : 'text-slate-500'}`}>{latestMessage}</p>
          </div>
        );
      },
      meta: { title: entry => entry.log.events.at(-1)?.message || entry.log.task.errorMessage || 'No events recorded yet.' },
    },
    {
      id: 'time',
      header: 'Requested / Completed',
      accessorFn: entry => entry.log.task.createdAt || '',
      size: 180,
      minSize: 150,
      maxSize: 260,
      cell: info => {
        const entry = info.row.original;
        const active = isActive(entry.log.task);
        return (
          <div className="text-right text-slate-500">
            <p className="font-semibold">{formatDateTime(entry.log.task.createdAt)}</p>
            <p className="text-[10px] text-slate-400">{entry.log.task.completedAt ? `Done ${formatDateTime(entry.log.task.completedAt)}` : (active ? `Updated ${formatDateTime(entry.log.task.createdAt)}` : '-')}</p>
          </div>
        );
      },
      meta: { align: 'right', title: entry => entry.log.task.completedAt ? formatDateTime(entry.log.task.completedAt) : formatDateTime(entry.log.task.createdAt) },
    },
  ];
  const renderActivityExpandedRow = (entry: typeof visibleEntries[number]) => {
    const expanded = expandedId === entry.log.task.id;
    if (!expanded) return null;
    return (
      <div className="hbdr-hyper-table-expanded-row">
        {entry.log.task.status === 'failed' && (
          <div className="mb-2 rounded-lg border border-rose-200 bg-rose-50 p-2 text-xs text-rose-700">
            <p className="font-bold text-rose-900">{entry.log.task.errorCode || 'TASK_FAILED'}</p>
            <p className="mt-1">{entry.log.task.errorMessage || 'Cluster task failed. Check agent logs and RBAC permissions.'}</p>
          </div>
        )}
        <div className="mb-1.5 flex items-center justify-between">
          <p className="text-[10px] font-bold uppercase tracking-widest text-slate-500">Event Log</p>
          <p className="text-[10px] text-slate-400">{entry.log.events.length} event{entry.log.events.length === 1 ? '' : 's'}</p>
        </div>
        <div className="max-h-40 overflow-y-auto rounded-lg border border-slate-100 bg-white">
          {entry.log.events.length > 0 ? entry.log.events.map(event => (
            <div key={event.id} className="flex items-start gap-2 border-b border-slate-100 px-2.5 py-1.5 last:border-b-0">
              <span className="w-20 shrink-0 text-[9px] text-slate-400">{formatDateTime(event.createdAt)}</span>
              <span className={`shrink-0 text-[10px] font-bold uppercase ${event.level === 'error' ? 'text-rose-600' : 'text-slate-700'}`}>{(event.reason || event.level || 'info').replace(/_/g, ' ')}</span>
              <p className="min-w-0 flex-1 text-[11px] text-slate-600">{event.message}</p>
            </div>
          )) : (
            <p className="px-3 py-3 text-center text-xs font-medium text-slate-400">{entry.log.loading ? 'Awaiting task events from agent...' : 'No events recorded yet.'}</p>
          )}
        </div>
      </div>
    );
  };

  if (entries.length === 0) {
    return (
      <div className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div>
            <h3 className="text-sm font-black text-slate-900">Recent Tasks</h3>
          </div>
          <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] font-bold text-slate-500">Idle</span>
        </div>
        <div className="px-4 py-4 text-center text-xs font-medium text-slate-400">No recent tasks yet. Register or unregister a cluster to see activity here.</div>
      </div>
    );
  }
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-sm">
      <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
        <div>
          <h3 className="text-sm font-black text-slate-900">Recent Tasks</h3>
        </div>
        <span className="rounded-full border border-blue-100 bg-blue-50 px-2 py-0.5 text-[10px] font-bold text-blue-600">Live</span>
      </div>
      <HyperTable
        variant="page"
        density="compact"
        columns={activityColumns}
        data={visibleEntries}
        getRowId={row => row.log.task.id}
        onRowClick={row => setExpandedId(expandedId === row.log.task.id ? null : row.log.task.id)}
        getRowClassName={row => expandedId === row.log.task.id ? 'hbdr-dr-row-selected' : ''}
        renderExpandedRow={renderActivityExpandedRow}
        initialPageSize={8}
        pageSizeOptions={[8, 20, 50]}
        emptyMessage="No recent tasks yet."
      />
    </div>
  );
}


function OnboardingGate({ onboarding, openClusters, children }: { onboarding: 'register' | 'default' | 'ready' | 'loading'; openClusters: () => void; children?: React.ReactNode }) {
  if (onboarding === 'ready' || onboarding === 'loading') return <>{children}</>;
  const title = onboarding === 'register' ? 'Register a cluster to continue' : 'Set a default cluster to continue';
  const desc = onboarding === 'register'
    ? 'This section needs at least one registered Kubernetes cluster. Register a cluster first, then come back.'
    : 'This section needs a default cluster. Pick one of your registered clusters as default, then return.';
  const steps: Array<{ title: string; desc: string }> = onboarding === 'register' ? [
    { title: '1. Register your first cluster', desc: 'Run the prepare-node and install commands on a Kubernetes cluster.' },
    { title: '2. Wait for the agent to connect', desc: 'The first registered cluster becomes the default automatically.' },
    { title: '3. Return here', desc: 'After the agent connects, this section will unlock.' },
  ] : [
    { title: '1. Open Clusters', desc: 'Find the cluster you want to make default.' },
    { title: '2. Click the star to set default', desc: 'The first cluster without a default makes the section lock.' },
    { title: '3. Return here', desc: 'After a default is set, this section unlocks.' },
  ];
  return (
    <div className="hbdr-section-card mx-auto max-w-3xl">
      <div className="flex flex-col items-center gap-3 p-10 text-center">
        <span className="flex h-14 w-14 items-center justify-center rounded-full bg-blue-50 text-blue-600"><ShieldCheck size={28} /></span>
        <h2 className="text-2xl font-bold text-slate-900">{title}</h2>
        <p className="max-w-xl text-sm text-slate-500">{desc}</p>
        <button onClick={openClusters} className="mt-2 inline-flex items-center gap-2 rounded-xl bg-blue-600 px-6 py-2.5 text-sm font-bold text-white shadow-lg shadow-blue-200 transition-all hover:bg-blue-700 active:scale-95">
          {onboarding === 'register' ? <PlusCircle size={16} /> : <Star size={16} />}Go to Clusters
        </button>
      </div>
      <div className="grid grid-cols-1 gap-3 border-t border-slate-100 px-8 py-6 md:grid-cols-3">
        {steps.map(step => (
          <div key={step.title} className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-left">
            <p className="text-sm font-bold text-slate-900">{step.title}</p>
            <p className="mt-1 text-xs leading-relaxed text-slate-500">{step.desc}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: ClusterStatus | string }) {
  const cls = status === 'healthy' ? 'bg-emerald-50 text-emerald-700 border-emerald-100' : status === 'syncing' ? 'bg-blue-50 text-blue-700 border-blue-100' : 'bg-amber-50 text-amber-700 border-amber-100';
  const label = status === 'healthy' || status === 'syncing' || status === 'warning' ? clusterStatusMeta(status).label : status.toUpperCase();
  return <span className={`px-2.5 py-0.5 rounded-full text-xs font-semibold border ${cls}`}>{label}</span>;
}

function ProtectionBadge({ cluster }: { cluster: Cluster }) {
  const protectedCount = getProtectedAppCount(cluster);
  const cls = protectedCount > 0
    ? 'border-emerald-100 bg-emerald-50 text-emerald-700'
    : 'border-slate-200 bg-slate-50 text-slate-500';
  const label = protectedCount > 0 ? 'Protected' : 'Unprotected';
  const title = protectedCount > 0 ? `Protected ${protectedCount}/${cluster.apps.length} applications` : 'No protected applications';

  return (
    <span title={title} className={`cluster-protection-badge inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-semibold ${cls}`}>
      <ShieldCheck size={12} />
      {label}
    </span>
  );
}

function getProtectedAppCount(cluster: Cluster) {
  return cluster.apps.filter(app => app.isProtected).length;
}

function isClusterProtected(cluster: Cluster) {
  return getProtectedAppCount(cluster) > 0;
}

function Metric({ label, value, success, onClick }: { label: string; value: string | number; success?: boolean; onClick?: () => void }) {
  const content = (
    <>
      <p className={`${success ? 'text-emerald-700' : 'text-slate-400'} text-[10px] font-medium tracking-wide`}>{label}</p>
      <p className={`mt-0.5 text-[15px] font-semibold ${success ? 'text-emerald-600' : 'text-slate-900'}`}>{value}</p>
    </>
  );
  const className = `cluster-metric-tile ${success ? 'cluster-metric-tile-success' : ''} ${onClick ? 'cluster-metric-tile-clickable' : ''} rounded-lg bg-slate-50/70 px-2.5 py-2`;
  return onClick ? (
    <button type="button" className={className} onClick={(event) => { event.stopPropagation(); onClick(); }}>
      {content}
    </button>
  ) : (
    <div className={className}>
      {content}
    </div>
  );
}

function StoragePage({ storage, clusters, onStorageCreated }: { storage: StorageRepo[]; clusters: Cluster[]; onStorageCreated?: (repo: StorageRepo) => void }) {
	const [repos, setRepos] = useState(storage.map(repo => normalizeStorageRepo(repo)));
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState(['type', 'bucket', 'region', 'endpoint', 'tls', 'urlStyle', 'status', 'lastValidatedAt']);
  const [selectedRepoIds, setSelectedRepoIds] = useState<string[]>([]);
  const [storageBulkMenuOpen, setStorageBulkMenuOpen] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [detailRepo, setDetailRepo] = useState<StorageRepo | null>(null);
  const [editingRepo, setEditingRepo] = useState<StorageRepo | null>(null);
	const [deleteRepo, setDeleteRepo] = useState<StorageRepo | null>(null);
	const [storageTypeOpen, setStorageTypeOpen] = useState(false);
	const [savingStorage, setSavingStorage] = useState(false);
	const [syncingStorage, setSyncingStorage] = useState(false);
	const [storageError, setStorageError] = useState<string | null>(null);
	const [storageTestMessage, setStorageTestMessage] = useState<{ tone: "ok" | "fail"; text: string } | null>(null);

	useEffect(() => {
		setRepos(storage.map(repo => normalizeStorageRepo(repo)));
	}, [storage]);

  useEffect(() => {
    if (!menuId) return;
    const closeMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest('[data-policy-menu-root]')) return;
      setMenuId(null);
    };
    window.addEventListener('click', closeMenu, true);
    return () => window.removeEventListener('click', closeMenu, true);
  }, [menuId]);

	function normalizeStorageRepo(repo: StorageRepo): StorageRepo {
		if (repo.config) return repo;
		if (repo.type === 'S3') return { ...repo, config: { bucket: repo.bucket, region: repo.region, accessKey: '', secretKey: '' } };
		if (isS3CompatibleType(repo.type)) return { ...repo, type: 'S3-Compatible', config: { bucket: repo.bucket, region: repo.region, endpoint: repo.endpoint, accessKey: '', secretKey: '', useSsl: repo.useTls, urlStyle: 'path' } };
    if (repo.type === 'Azure') return { ...repo, config: { accountName: '', accountKey: '', container: repo.bucket, blobDomain: repo.endpoint } };
    if (repo.type === 'Google Cloud' || repo.type === 'GCS') return { ...repo, type: 'Google Cloud', config: { bucket: repo.bucket, region: repo.region, serviceAccountKey: '' } };
    return { ...repo, config: { nfsServer: repo.endpoint.replace(/^nfs:\/\//, '').split(':')[0] || '', nfsPath: repo.bucket } };
  }

  const storageTypeOptions = [
    { type: 'S3', title: 'Amazon S3', icon: Database, color: 'bg-amber-50 text-amber-600 border-amber-100' },
    { type: 'S3-Compatible', title: 'S3 Compatible', icon: Database, color: 'bg-indigo-50 text-indigo-600 border-indigo-100' },
    { type: 'Azure', title: 'Azure Blob', icon: Cloud, color: 'bg-blue-50 text-blue-600 border-blue-100' },
    { type: 'Google Cloud', title: 'Google Cloud', icon: Cloud, color: 'bg-rose-50 text-rose-600 border-rose-100' },
  ];

  const createStorageDraft = (type: string): StorageRepo => {
    const base = {
      id: 'repo-' + Date.now(),
      name: '',
      type,
      endpoint: '',
      bucket: '',
      region: type === 'NFS' ? 'local' : '',
      useTls: type !== 'NFS',
      status: 'warning' as const,
      updatedAt: new Date().toISOString(),
    };
    if (type === 'S3') return { ...base, config: { bucket: '', region: '', accessKey: '', secretKey: '' } };
    if (type === 'S3-Compatible') return { ...base, config: { bucket: '', region: '', endpoint: '', accessKey: '', secretKey: '', useSsl: true, urlStyle: 'path' } };
    if (type === 'Azure') return { ...base, region: 'N/A', config: { accountName: '', accountKey: '', container: '', blobDomain: 'blob.core.windows.net' } };
    if (type === 'Google Cloud') return { ...base, config: { bucket: '', region: '', serviceAccountKey: '' } };
    return { ...base, useTls: false, config: { nfsServer: '', nfsPath: '' } };
  };

  const storageConfigValue = (key: string) => String(editingRepo?.config?.[key] ?? '');

  const updateEditingConfig = (key: string, value: string | boolean) => {
    if (!editingRepo) return;
    const config = { ...(editingRepo.config || {}), [key]: value };
    const patch: Partial<StorageRepo> = { config };
    if (key === 'bucket' || key === 'container' || key === 'nfsPath') patch.bucket = String(value);
    if (key === 'region') patch.region = String(value || (editingRepo.type === 'Azure' ? 'N/A' : ''));
    if (key === 'endpoint' || key === 'blobDomain') patch.endpoint = String(value);
    if (key === 'useSsl') patch.useTls = Boolean(value);
    if (key === 'nfsServer' || key === 'nfsPath') {
      const server = String(key === 'nfsServer' ? value : config.nfsServer || '');
      const nfsPath = String(key === 'nfsPath' ? value : config.nfsPath || '');
      patch.endpoint = server && nfsPath ? 'nfs://' + server + ':' + nfsPath : server;
    }
    setEditingRepo({ ...editingRepo, ...patch });
  };

  const storageReady = (repo: StorageRepo | null) => {
    if (!repo?.name.trim()) return false;
    const c = repo.config || {};
    if (repo.type === 'S3') return Boolean(c.bucket && c.region && c.accessKey && c.secretKey);
		if (isS3CompatibleType(repo.type)) return Boolean(c.bucket && c.endpoint && c.accessKey && c.secretKey);
    if (repo.type === 'Azure') return Boolean(c.accountName && c.accountKey && c.container);
    if (repo.type === 'Google Cloud') return Boolean(c.bucket && c.serviceAccountKey);
    if (repo.type === 'NFS') return Boolean(c.nfsServer && c.nfsPath);
    return false;
  };

  const storageQueryValue = (repo: StorageRepo, field: string) => {
    if (field === 'type') return repo.type;
    if (field === 'bucket') return repo.bucket || '';
    if (field === 'endpoint') return repo.endpoint || '';
    if (field === 'region') return repo.region || '';
    if (field === 'tls') return repo.useTls ? 'SSL TLS enabled' : 'SSL TLS disabled off';
    if (field === 'urlStyle') return repo.urlStyle === 'virtual' ? 'Virtual-host' : 'Path';
    if (field === 'status') return repo.status;
    if (field === 'lastValidatedAt') return repo.lastValidatedAt ? formatDateTime(repo.lastValidatedAt) : 'Never';
    return repo.name;
  };
  const storageMatchesFilter = (repo: StorageRepo, filter: string) => {
    if (filter === 'connected') return repo.status === 'connected';
    if (filter === 'warning') return repo.status !== 'connected';
    if (filter === 'tls') return repo.useTls;
    if (filter === 'noTls') return !repo.useTls;
    return true;
  };
  const filteredRepos = repos.filter(repo => {
    const keyword = query.trim().toLowerCase();
    const queryMatched = !keyword || storageQueryValue(repo, queryField).toLowerCase().includes(keyword);
    const tagsMatched = activeTags.length === 0 || activeTags.includes(repo.type);
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => storageQueryValue(repo, field));
      return storageMatchesFilter(repo, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  });
  const storageColumns = [
    { value: 'type', label: 'Type', minWidth: 150 },
    { value: 'bucket', label: 'Bucket', minWidth: 140 },
    { value: 'region', label: 'Region', minWidth: 120 },
    { value: 'endpoint', label: 'Endpoint', minWidth: 200 },
    { value: 'tls', label: 'SSL', minWidth: 78 },
    { value: 'urlStyle', label: 'URL Style', minWidth: 110 },
    { value: 'status', label: 'Status', minWidth: 100 },
    { value: 'lastValidatedAt', label: 'Last Verified', minWidth: 150 },
  ];
  const storageQueryFields = listToolbarQueryFields([{ value: 'name', label: 'Repository Name' }], storageColumns, visibleColumns);
  const selectedRepos = repos.filter(repo => selectedRepoIds.includes(repo.id));
  const singleSelectedRepo = selectedRepos.length === 1 ? selectedRepos[0] : null;
  const allVisibleReposSelected = filteredRepos.length > 0 && filteredRepos.every(repo => selectedRepoIds.includes(repo.id));

  const toggleSelectedRepo = (repoId: string) => {
    setSelectedRepoIds(prev => prev.includes(repoId) ? prev.filter(id => id !== repoId) : [...prev, repoId]);
  };

  const toggleVisibleRepos = () => {
    setSelectedRepoIds(prev => {
      const visibleIds = filteredRepos.map(repo => repo.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };

  const storageTableColumns = useMemo<HyperTableColumn<StorageRepo>[]>(() => {
    const columns: HyperTableColumn<StorageRepo>[] = [
      {
        id: 'select',
        header: () => (
          <input
            type="checkbox"
            checked={allVisibleReposSelected}
            onClick={event => event.stopPropagation()}
            onChange={toggleVisibleRepos}
          />
        ),
        cell: info => (
          <input
            type="checkbox"
            checked={selectedRepoIds.includes(info.row.original.id)}
            onClick={event => event.stopPropagation()}
            onChange={() => toggleSelectedRepo(info.row.original.id)}
          />
        ),
        size: 42,
        minSize: 42,
        maxSize: 54,
        enableSorting: false,
        enableResizing: false,
        meta: { align: 'center' },
      },
      {
        id: 'name',
        header: 'Repository Name',
        accessorFn: repo => repo.name,
        size: 260,
        minSize: 190,
        maxSize: 520,
        cell: info => {
          const repo = info.row.original;
          return (
            <div className="hbdr-dr-name-cell">
              <div className={'hbdr-dr-namespace-icon ' + repoIconClass(repo.type)}>
                <Database size={18} />
              </div>
              <div>
                <p className="hbdr-dr-app-name">{repo.name}</p>
              </div>
            </div>
          );
        },
        meta: { title: repo => repo.name },
      },
    ];
    const addColumn = (column: HyperTableColumn<StorageRepo>) => {
      if (visibleColumns.includes(column.id as string)) columns.push(column);
    };
    addColumn({
      id: 'type',
      header: 'Type',
      accessorFn: repo => repo.type || '',
      size: 150,
      minSize: 120,
      cell: info => <span className="hbdr-dr-storage">{info.row.original.type || 'N/A'}</span>,
      meta: { title: repo => repo.type || 'N/A' },
    });
    addColumn({
      id: 'bucket',
      header: 'Bucket',
      accessorFn: repo => repo.bucket || '',
      size: 180,
      minSize: 140,
      cell: info => info.row.original.bucket || <span className="hbdr-dr-na">N/A</span>,
      meta: { title: repo => repo.bucket || 'N/A' },
    });
    addColumn({
      id: 'region',
      header: 'Region',
      accessorFn: repo => repo.region || '',
      size: 130,
      minSize: 110,
      cell: info => info.row.original.region || <span className="hbdr-dr-na">N/A</span>,
      meta: { title: repo => repo.region || 'N/A' },
    });
    addColumn({
      id: 'endpoint',
      header: 'Endpoint',
      accessorFn: repo => repo.endpoint || '',
      size: 260,
      minSize: 180,
      maxSize: 520,
      cell: info => info.row.original.endpoint || <span className="hbdr-dr-na">N/A</span>,
      meta: { title: repo => repo.endpoint || 'N/A' },
    });
    addColumn({
      id: 'tls',
      header: 'SSL',
      accessorFn: repo => repo.useTls ? 1 : 0,
      size: 90,
      minSize: 78,
      cell: info => info.row.original.useTls
        ? <span className="hbdr-dr-ssl hbdr-dr-ssl-on"><Lock size={11} />SSL</span>
        : <span className="hbdr-dr-ssl hbdr-dr-ssl-off">Off</span>,
      meta: { title: repo => repo.useTls ? 'SSL enabled' : 'SSL disabled' },
    });
    addColumn({
      id: 'urlStyle',
      header: 'URL Style',
      accessorFn: repo => repo.urlStyle || '',
      size: 126,
      minSize: 110,
      cell: info => <span className="hbdr-dr-url-style">{info.row.original.urlStyle === 'virtual' ? 'Virtual-host' : 'Path'}</span>,
      meta: { title: repo => repo.urlStyle === 'virtual' ? 'Virtual-host' : 'Path' },
    });
    addColumn({
      id: 'status',
      header: 'Status',
      accessorFn: repo => repo.status || '',
      size: 126,
      minSize: 100,
      cell: info => {
        const repo = info.row.original;
        return (
          <span className={
            repo.status === 'connected' ? 'hbdr-dr-task-ok'
              : repo.status === 'warning' ? 'hbdr-dr-task-warn'
                : 'hbdr-dr-task-unknown'
          }>
            {repo.status === 'connected' ? 'CONNECTED' : repo.status === 'warning' ? 'WARNING' : 'UNKNOWN'}
          </span>
        );
      },
      meta: { title: repo => repo.status || 'unknown' },
    });
    addColumn({
      id: 'lastValidatedAt',
      header: 'Last Verified',
      accessorFn: repo => repo.lastValidatedAt || '',
      size: 168,
      minSize: 150,
      maxSize: 260,
      cell: info => (
        <span className="hbdr-dr-last-verified">
          {info.row.original.lastValidatedAt ? formatDateTime(info.row.original.lastValidatedAt) : <span className="hbdr-dr-na">Never</span>}
        </span>
      ),
      meta: { title: repo => repo.lastValidatedAt ? formatDateTime(repo.lastValidatedAt) : 'Never' },
    });
    return columns;
  }, [allVisibleReposSelected, selectedRepoIds, visibleColumns]);

  const closeStorageWizard = () => {
    setStorageTypeOpen(false);
    setEditingRepo(null);
  };

	const saveStorage = async () => {
		if (!editingRepo || !storageReady(editingRepo)) return;
		setSavingStorage(true);
		setStorageError(null);
		try {
			const created = await apiPost<ApiStorageRepo>('/api/v1/storage-repositories', buildStorageRepositoryInput(editingRepo));
			const saved = normalizeStorageRepo(mapStorageRepo(created));
			setRepos(prev => prev.some(repo => repo.id === saved.id) ? prev.map(repo => repo.id === saved.id ? saved : repo) : [saved, ...prev]);
			onStorageCreated?.(saved);
			closeStorageWizard();
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to save storage repository');
		} finally {
			setSavingStorage(false);
		}
	};

	const saveEditedStorage = () => {
    if (!editingRepo || !storageReady(editingRepo)) return;
    setRepos(prev => prev.map(repo => repo.id === editingRepo.id ? normalizeStorageRepo(editingRepo) : repo));
    setEditingRepo(null);
	};

	const syncSelectedStorage = async () => {
		if (selectedRepos.length === 0 || clusters.length === 0) return;
		setSyncingStorage(true);
		setStorageError(null);
		try {
			await Promise.all(selectedRepos.flatMap(repo =>
				clusters.map(cluster => apiPost<ApiTask>(`/api/v1/storage-repositories/${repo.id}/sync`, {
					clusterId: cluster.id,
				})),
			));
			const timestamp = new Date().toISOString();
			setRepos(prev => prev.map(repo => selectedRepoIds.includes(repo.id) ? { ...repo, status: 'connected', updatedAt: timestamp } : repo));
			setStorageBulkMenuOpen(false);
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to sync storage repository');
		} finally {
			setSyncingStorage(false);
		}
	};

	const testStorageConnection = async (repoId: string) => {
		setStorageError(null);
		try {
			const result = await apiPost<{ status: string; detail: string; repository: ApiStorageRepo }>(
				`/api/v1/storage-repositories/${repoId}/test`,
				{},
			);
			const updated = normalizeStorageRepo(mapStorageRepo(result.repository));
			setRepos(prev => prev.map(repo => repo.id === repoId ? updated : repo));
			setDetailRepo(prev => (prev && prev.id === repoId ? updated : prev));
			setEditingRepo(prev => (prev && prev.id === repoId ? updated : prev));
			return result;
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Test connection failed';
			setStorageError(message);
			throw error;
		}
	};

	const testSelectedStorage = async () => {
		if (selectedRepos.length === 0) return;
		setSyncingStorage(true);
		try {
			await Promise.all(selectedRepos.map(repo => testStorageConnection(repo.id).catch(() => null)));
			setStorageBulkMenuOpen(false);
		} finally {
			setSyncingStorage(false);
		}
	};

  const closeEditStorage = () => {
    setEditingRepo(null);
    setStorageTestMessage(null);
  };

  const runSavedStorageConnectionTest = async () => {
    if (!editingRepo) return;
    setSyncingStorage(true);
    setStorageTestMessage(null);
    try {
      const result = await testStorageConnection(editingRepo.id);
      setStorageTestMessage({
        tone: result.status === 'connected' ? 'ok' : 'fail',
        text: result.detail || (result.status === 'connected' ? 'Reachability OK' : 'Test failed'),
      });
      setStorageError(null);
    } catch (error) {
      setStorageTestMessage({ tone: 'fail', text: error instanceof Error ? error.message : 'Test connection failed' });
    } finally {
      setSyncingStorage(false);
    }
  };


  const deleteStorage = () => {
    if (!deleteRepo) return;
    setRepos(prev => prev.filter(repo => repo.id !== deleteRepo.id));
    setSelectedRepoIds(prev => prev.filter(id => id !== deleteRepo.id));
    setDeleteRepo(null);
  };

  function repoIconClass(type: string) {
    if (type.toLowerCase().includes('s3')) return 'bg-amber-50 text-amber-600 border-amber-100';
    if (type === 'Azure') return 'bg-blue-50 text-blue-600 border-blue-100';
    if (type === 'Google Cloud') return 'bg-rose-50 text-rose-600 border-rose-100';
    return 'bg-slate-50 text-slate-600 border-slate-200';
  }

  const renderStorageFields = (allowTypeChange: boolean) => {
    if (!editingRepo) return null;
    return (
      <div className="space-y-3">
        <div className={allowTypeChange ? 'grid grid-cols-1 gap-4' : 'grid grid-cols-1 gap-4 md:grid-cols-2'}>
          <EditField label="Name" value={editingRepo.name} placeholder="My Backup Repo" onChange={value => setEditingRepo({ ...editingRepo, name: value })} />
          {!allowTypeChange && (
            <label className="flex flex-col gap-1.5 text-xs font-bold uppercase tracking-widest text-slate-600">
              Type
              <div className="flex h-10 items-center rounded-lg border border-slate-200 bg-slate-50 px-3.5 text-xs font-bold uppercase text-slate-600">
                <span>{editingRepo.type}</span>
              </div>
            </label>
          )}
        </div>

		{(editingRepo.type === 'S3' || isS3CompatibleType(editingRepo.type)) && (
          <div className="hbdr-storage-field-stack">
            {isS3CompatibleType(editingRepo.type) && (
              <EditField label="Endpoint (ENDPOINT)" value={storageConfigValue('endpoint')} placeholder="http://minio:9000" onChange={value => updateEditingConfig('endpoint', value)} />
            )}
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="ACCESS KEY ID (AK)" value={storageConfigValue('accessKey')} placeholder="AKIA..." onChange={value => updateEditingConfig('accessKey', value)} />
              <EditField label="SECRET ACCESS KEY (SK)" type="password" value={storageConfigValue('secretKey')} placeholder="Enter secret access key" onChange={value => updateEditingConfig('secretKey', value)} />
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="BUCKET/Path" value={storageConfigValue('bucket')} placeholder="bucket-name" onChange={value => updateEditingConfig('bucket', value)} />
              <EditField label="Region (REGION)" value={storageConfigValue('region')} placeholder="us-west-2" onChange={value => updateEditingConfig('region', value)} />
            </div>
            {isS3CompatibleType(editingRepo.type) && (() => {
              const ssl = Boolean(editingRepo.config?.useSsl ?? editingRepo.useTls);
              const urlStyle = storageConfigValue('urlStyle') || 'path';
              return (
                <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 bg-white px-3.5 py-2.5">
                    <div className="flex items-center gap-2.5">
                      <span className={'flex h-7 w-7 shrink-0 items-center justify-center rounded-md border transition-colors ' + (ssl ? 'border-emerald-100 bg-emerald-50 text-emerald-600' : 'border-slate-200 bg-slate-50 text-slate-400')}>
                        <ShieldCheck size={13} />
                      </span>
                      <div>
                        <p className="text-[11px] font-bold uppercase tracking-wider text-slate-700">SSL/TLS</p>
                        <p className={'text-[10px] font-semibold ' + (ssl ? 'text-emerald-600' : 'text-slate-400')}>{ssl ? 'Encrypted' : 'Disabled'}</p>
                      </div>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={ssl}
                      onClick={() => updateEditingConfig('useSsl', !ssl)}
                      className={
                        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-300 ' +
                        (ssl ? 'border-emerald-500 bg-emerald-500' : 'border-slate-200 bg-slate-200')
                      }
                    >
                      <span className={'inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ' + (ssl ? 'translate-x-4' : 'translate-x-0.5')} />
                    </button>
                  </div>
                  <div className="flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                    <p className="shrink-0 text-[11px] font-bold uppercase tracking-wider text-slate-700">URL Style</p>
                    <div className="grid flex-1 grid-cols-2 gap-1">
                      {[
                        { value: 'path', label: 'Path' },
                        { value: 'virtual', label: 'Virtual-host' },
                      ].map(opt => {
                        const active = urlStyle === opt.value;
                        return (
                          <button
                            type="button"
                            key={opt.value}
                            onClick={() => updateEditingConfig('urlStyle', opt.value)}
                            className={
                              'rounded-md px-2 py-1 text-[11px] font-bold transition-all ' +
                              (active
                                ? 'bg-blue-50 text-blue-700 ring-1 ring-blue-200'
                                : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700')
                            }
                          >
                            {opt.label}
                        </button>
                        );
                      })}
                    </div>
                  </div>
                </div>
              );
            })()}
          </div>
        )}

        {editingRepo.type === 'Azure' && (
          <div className="hbdr-storage-field-stack">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Storage Account Name" value={storageConfigValue('accountName')} placeholder="mystorageaccount" onChange={value => updateEditingConfig('accountName', value)} />
              <EditField label="Account Key" type="password" value={storageConfigValue('accountKey')} placeholder="Azure Storage Account Key" onChange={value => updateEditingConfig('accountKey', value)} />
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Container Name" value={storageConfigValue('container')} placeholder="my-backups" onChange={value => updateEditingConfig('container', value)} />
              <EditField label="Endpoint Suffix" value={storageConfigValue('blobDomain')} placeholder="blob.core.windows.net" onChange={value => updateEditingConfig('blobDomain', value)} />
            </div>
          </div>
        )}

        {editingRepo.type === 'Google Cloud' && (
          <div className="hbdr-storage-field-stack">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Bucket/Path" value={storageConfigValue('bucket')} placeholder="gcs-bucket-name" onChange={value => updateEditingConfig('bucket', value)} />
              <EditField label="Region" value={storageConfigValue('region')} placeholder="us-central1" onChange={value => updateEditingConfig('region', value)} />
            </div>
            <label className="flex flex-col gap-1.5 text-xs font-bold uppercase tracking-widest text-slate-600">
              SERVICE ACCOUNT KEY
              <textarea value={storageConfigValue('serviceAccountKey')} onChange={event => updateEditingConfig('serviceAccountKey', event.target.value)} placeholder={'{ "type": "service_account", ... }'} rows={4} className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 font-mono text-xs text-slate-700 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-100" />
            </label>
          </div>
        )}

        {editingRepo.type === 'NFS' && (
          <div className="grid grid-cols-1 gap-4">
            <EditField label="NFS Server Address" value={storageConfigValue('nfsServer')} placeholder="192.168.1.100" onChange={value => updateEditingConfig('nfsServer', value)} />
            <EditField label="Mount Path" value={storageConfigValue('nfsPath')} placeholder="/mnt/backups" onChange={value => updateEditingConfig('nfsPath', value)} />
          </div>
        )}
      </div>
    );
  };

  return (
    <motion.div key="storage" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Archive size={18} /></div>
            <div className="hbdr-storage-page-title">
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Storage</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Maintain shared restore-point repositories across clusters.</p>
            </div>
          </div>
          <div />
        </div>
      </div>

      <div className="hbdr-dr-table-card hbdr-storage-table-list">
        <div className="hbdr-dr-table-head">
            <div className="hbdr-dr-toolbar">
              <div className="hbdr-dr-action-group">
                <button onClick={() => { setEditingRepo(createStorageDraft('S3-Compatible')); setStorageTestMessage(null); setStorageError(null); setStorageTypeOpen(true); }} className="hbdr-dr-action-primary">New Storage</button>
              <div className="relative">
                <button disabled={selectedRepos.length === 0} onClick={() => setStorageBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={storageBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {storageBulkMenuOpen && selectedRepos.length > 0 && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setStorageBulkMenuOpen(false)} />
                      <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                        <button disabled={!singleSelectedRepo} onClick={() => { if (!singleSelectedRepo) return; setDetailRepo(normalizeStorageRepo(singleSelectedRepo)); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Eye size={14} />View Details</button>
                        <button disabled={!singleSelectedRepo} onClick={() => { if (!singleSelectedRepo) return; setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(singleSelectedRepo)); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Settings size={14} />Edit Storage</button>
                        <button onClick={() => { if (!selectedRepos[0]) return; setDeleteRepo(selectedRepos[0]); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete Repository</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={storageQueryFields}
              tags={storageTypeOptions.map(option => ({ value: option.type, label: option.title, count: repos.filter(repo => repo.type === option.type).length }))}
              activeTags={activeTags}
              setActiveTags={setActiveTags}
              filters={[
                { value: 'connected', label: 'Connected', count: repos.filter(repo => storageMatchesFilter(repo, 'connected')).length },
                { value: 'warning', label: 'Warning', count: repos.filter(repo => storageMatchesFilter(repo, 'warning')).length },
                { value: 'tls', label: 'TLS Enabled', count: repos.filter(repo => storageMatchesFilter(repo, 'tls')).length },
                { value: 'noTls', label: 'TLS Disabled', count: repos.filter(repo => storageMatchesFilter(repo, 'noTls')).length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={storageColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={() => {
                const timestamp = new Date().toISOString();
                setRepos(prev => prev.map(repo => ({ ...repo, updatedAt: timestamp })));
                setSelectedRepoIds([]);
              }}
            />
          </div>
          {storageError && <p className="mt-3 text-xs font-bold text-amber-600">Storage operation warning: {storageError}</p>}
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={storageTableColumns}
          data={filteredRepos}
          getRowId={row => row.id}
          onRowClick={row => toggleSelectedRepo(row.id)}
          getRowClassName={row => selectedRepoIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRepoIds.length}
          emptyMessage="No matching storage repositories"
          className="hbdr-storage-hyper-table"
        />
      </div>

      <div className="hidden grid-cols-1 gap-4">
        {filteredRepos.map(repo => (
          <motion.div
            key={repo.id}
            whileHover={{ y: -2 }}
            className={`group relative flex flex-col gap-5 overflow-visible rounded-2xl border border-slate-200 bg-white p-6 shadow-sm transition-all hover:border-slate-300 hover:shadow-md lg:flex-row lg:items-center lg:justify-between ${menuId === repo.id ? 'z-40' : 'z-0'}`}
          >
            <div className="flex min-w-0 items-center gap-5">
              <div className={'flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl border shadow-sm transition-transform group-hover:scale-105 ' + repoIconClass(repo.type)}>
                <Database size={24} />
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="truncate text-lg font-bold tracking-tight text-slate-900">{repo.name}</h3>
                  <span className="rounded-full bg-slate-100 px-2.5 py-1 font-mono text-[10px] font-bold uppercase text-slate-500">{repo.type}</span>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm font-medium text-slate-500">
                  <span className="flex items-center gap-1.5"><Archive size={14} className="text-slate-400" />Bucket: {repo.bucket || '-'}</span>
                  <span className="flex items-center gap-1.5"><Grid3X3 size={14} className="text-slate-400" />Region: {repo.region || 'N/A'}</span>
                </div>
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-6 lg:justify-end">
              <div className="text-right">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-slate-400">Status</p>
                <div className="flex items-center justify-end gap-1.5">
                  <span className={'h-2 w-2 rounded-full ' + (repo.status === 'connected' ? 'bg-emerald-500' : 'bg-amber-500')} />
                  <span className="text-xs font-bold uppercase text-slate-700">{repo.status === 'connected' ? 'CONNECTED' : 'WARNING'}</span>
                </div>
              </div>
              <div className="border-l border-slate-100 pl-6 text-right">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-slate-400">Last Verified</p>
                <p className="text-xs font-medium text-slate-600">{repo.updatedAt}</p>
              </div>
              <div className="relative">
                <button onClick={(event) => { event.stopPropagation(); setMenuId(menuId === repo.id ? null : repo.id); }} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700"><MoreVertical size={18} /></button>
                <AnimatePresence>
                  {menuId === repo.id && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setMenuId(null)} />
                      <motion.div onClick={(event) => event.stopPropagation()} initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute right-0 top-10 z-50 w-40 overflow-hidden rounded-xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5">
                        <button onClick={() => { setDetailRepo(normalizeStorageRepo(repo)); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Eye size={14} />View Details</button>
                        <button onClick={() => { setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(repo)); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Settings size={14} />Edit</button>
                        <button onClick={() => { setDeleteRepo(repo); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
          </motion.div>
        ))}
      </div>

      <AnimatePresence>
        {storageTypeOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={closeStorageWizard} />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-storage-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>New Storage Repository</strong>
                  <span>Create a repository for Velero backup and restore data.</span>
                </div>
                <button type="button" onClick={closeStorageWizard} aria-label="Close storage drawer"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-storage-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Repository Type</h4>
                  {(() => {
                    const currentType = editingRepo?.type ?? 'S3-Compatible';
                    return (
                      <div className="hbdr-advanced-filter-box hbdr-storage-type-select-box">
                        <label>
                          <span>Type</span>
                          <select
                            value={currentType}
                            onChange={event => {
                              const draft = createStorageDraft(event.target.value);
                              draft.name = editingRepo?.name ?? '';
                              setEditingRepo(draft);
                            }}
                          >
                            {storageTypeOptions.map(option => (
                              <option key={option.type} value={option.type}>{option.title}</option>
                            ))}
                          </select>
                        </label>
                      </div>
                    );
                  })()}
                </section>

                {editingRepo && (
                  <section className="hbdr-advanced-filter-section">
                    <h4>Configuration</h4>
                    <div className="hbdr-advanced-filter-box hbdr-storage-config-box">{renderStorageFields(true)}</div>
                  </section>
                )}
              </div>
              {editingRepo && (
                <div className="hbdr-storage-drawer-footer">
                  {storageTestMessage && (
                    <div className={`hbdr-storage-test-result ${storageTestMessage.tone === 'ok' ? 'is-ok' : 'is-fail'}`}>
                      {storageTestMessage.text}
                    </div>
                  )}
                  <div className="hbdr-filter-drawer-actions hbdr-storage-drawer-actions">
                    <button type="button" onClick={saveStorage} disabled={!storageReady(editingRepo) || savingStorage}>{savingStorage ? "Saving..." : "Create Storage"}</button>
                    <button type="button" onClick={async () => {
  if (!editingRepo) return;
  const isDraft = !editingRepo.id || editingRepo.id.startsWith("repo-");
  if (isDraft) {
    if (!editingRepo.endpoint || !editingRepo.bucket) {
      setStorageTestMessage({ tone: "fail", text: "Enter endpoint and bucket first." });
      return;
    }
    setSyncingStorage(true);
    setStorageTestMessage(null);
    try {
      const input = buildStorageRepositoryInput(editingRepo);
      const result = await apiPost<{ status: string; detail: string }>("/api/v1/storage-repositories/test", input);
      if (result.status === "connected") {
        setStorageTestMessage({ tone: "ok", text: `Reachability OK: ${result.detail}` });
      } else {
        setStorageTestMessage({ tone: "fail", text: result.detail || "Reachability test failed" });
      }
    } catch (e) {
      setStorageTestMessage({ tone: "fail", text: e instanceof Error ? e.message : "Test connection failed" });
    } finally {
      setSyncingStorage(false);
    }
    return;
  }
  setSyncingStorage(true);
  try {
    const result = await testStorageConnection(editingRepo.id);
    setStorageTestMessage({ tone: result.status === "connected" ? "ok" : "fail", text: result.detail || (result.status === "connected" ? "Reachability OK" : "Test failed") });
    setStorageError(null);
  } catch (e) {
    setStorageTestMessage({ tone: "fail", text: e instanceof Error ? e.message : "Test connection failed" });
  } finally { setSyncingStorage(false); }
}} disabled={syncingStorage} className="hbdr-storage-test-button"><Activity size={14} />{syncingStorage ? "Testing..." : "Test Connection"}</button>
                    <button type="button" onClick={closeStorageWizard}>Cancel</button>
                  </div>
                </div>
              )}
            </motion.div>
          </>
        )}

        {detailRepo && (
          <ModalFrame title="Storage Repository Details" onClose={() => setDetailRepo(null)}>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <Info label="Name" value={detailRepo.name} />
              <Info label="Type" value={detailRepo.type} />
              <Info label="Endpoint" value={detailRepo.endpoint || '-'} />
              <Info label="Bucket/Path" value={detailRepo.bucket || '-'} />
              <Info label="Region" value={detailRepo.region || 'N/A'} />
              <Info label="Use TLS" value={detailRepo.useTls ? 'Yes' : 'No'} />
              <Info label="Status" value={detailRepo.status === 'connected' ? 'CONNECTED' : 'WARNING'} />
              <Info label="Last Verified" value={detailRepo.updatedAt} />
            </div>
            <div className="mt-5 flex justify-end gap-3">
              <button onClick={() => setDetailRepo(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Close</button>
              <button onClick={() => { setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(detailRepo)); setDetailRepo(null); }} className="rounded-xl bg-blue-600 px-6 py-2.5 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700">Edit Storage Repository</button>
            </div>
          </ModalFrame>
        )}

        {editingRepo && !storageTypeOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={closeEditStorage} />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-storage-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>Edit Storage Repository</strong>
                  <span>Update repository connection and backup location settings.</span>
                </div>
                <button type="button" onClick={closeEditStorage} aria-label="Close storage drawer"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-storage-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Configuration</h4>
                  <div className="hbdr-advanced-filter-box hbdr-storage-config-box">{renderStorageFields(false)}</div>
                </section>
              </div>
              <div className="hbdr-storage-drawer-footer">
                {storageTestMessage && (
                  <div className={`hbdr-storage-test-result ${storageTestMessage.tone === 'ok' ? 'is-ok' : 'is-fail'}`}>
                    {storageTestMessage.text}
                  </div>
                )}
                <div className="hbdr-filter-drawer-actions hbdr-storage-drawer-actions">
                  <button type="button" onClick={saveEditedStorage} disabled={!storageReady(editingRepo)}>Save Changes</button>
                  <button type="button" onClick={runSavedStorageConnectionTest} disabled={syncingStorage} className="hbdr-storage-test-button"><Activity size={14} />{syncingStorage ? 'Testing...' : 'Test Connection'}</button>
                  <button type="button" onClick={closeEditStorage}>Cancel</button>
                </div>
              </div>
            </motion.div>
          </>
        )}

        {deleteRepo && (
          <ModalFrame title="Delete Storage Repository" onClose={() => setDeleteRepo(null)}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Confirm storage repository deletion <strong>{deleteRepo.name}</strong>? After deletion, this repository can no longer be used as a new DR recovery target.
              </div>
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Name" value={deleteRepo.name} />
                <Info label="Type" value={deleteRepo.type} />
                <Info label="Bucket/Path" value={deleteRepo.bucket || '-'} />
                <Info label="Last Verified" value={deleteRepo.updatedAt} />
              </div>
              <div className="flex justify-end gap-3">
                <button onClick={() => setDeleteRepo(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button onClick={deleteStorage} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700 active:scale-95">Confirm Delete</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function PolicyPage({ policies, setPolicies }: { policies: PolicyItem[]; setPolicies: React.Dispatch<React.SetStateAction<PolicyItem[]>> }) {
  const defaultPolicyForm = (): Omit<PolicyItem, 'id' | 'status' | 'bound'> => ({
    name: '',
    composition: 'combined',
    type: 'interval',
    intervalValue: 5,
    intervalUnit: 'minutes',
    hour: 0,
    minute: 0,
    weekDay: 0,
    monthDay: 1,
    retention: 7,
  });

  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState(['composition', 'schedule', 'retention', 'bound', 'status']);
  const [selectedPolicyIds, setSelectedPolicyIds] = useState<string[]>([]);
  const [policyBulkMenuOpen, setPolicyBulkMenuOpen] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [editingPolicyId, setEditingPolicyId] = useState<string | null>(null);
  const [policyForm, setPolicyForm] = useState(defaultPolicyForm());
  const [policyModalOpen, setPolicyModalOpen] = useState(false);
  const [deletePolicy, setDeletePolicy] = useState<PolicyItem | null>(null);
  const showScheduleConfig = policyForm.composition !== 'retention';
  const showRetentionConfig = policyForm.composition !== 'schedule';

  useEffect(() => {
    if (!menuId) return;
    const closeMenu = () => setMenuId(null);
    window.addEventListener('click', closeMenu);
    return () => window.removeEventListener('click', closeMenu);
  }, [menuId]);

  const policyQueryValue = (policy: PolicyItem, field: string) => {
    if (field === 'composition') return formatPolicyComposition(policy.composition);
    if (field === 'schedule') return formatPolicySchedule(policy);
    if (field === 'retention') return formatPolicyRetention(policy);
    if (field === 'bound') return `${policy.bound} applications`;
    if (field === 'status') return policy.status;
    if (field === 'type') return formatPolicyType(policy.type);
    return policy.name;
  };
  const policyMatchesFilter = (policy: PolicyItem, filter: string) => {
    if (filter === 'active') return policy.status === 'Active';
    if (filter === 'disabled') return policy.status !== 'Active';
    if (filter === 'bound') return policy.bound > 0;
    if (filter === 'unbound') return policy.bound === 0;
    return true;
  };
  const filteredPolicies = policies.filter(policy => {
    const keyword = query.trim().toLowerCase();
    const queryMatched = !keyword || policyQueryValue(policy, queryField).toLowerCase().includes(keyword);
    const tagsMatched = activeTags.length === 0 || activeTags.includes(policy.composition);
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => policyQueryValue(policy, field));
      return policyMatchesFilter(policy, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  });
  const policyColumns = [
    { value: 'composition', label: 'Schedule Type' },
    { value: 'schedule', label: 'Execution Plan' },
    { value: 'retention', label: 'Retained Copies' },
    { value: 'bound', label: 'Bound Apps' },
    { value: 'status', label: 'Status' },
  ];
  const policyQueryFields = listToolbarQueryFields([{ value: 'name', label: 'Policy Name' }], policyColumns, visibleColumns);
  const selectedPolicies = policies.filter(policy => selectedPolicyIds.includes(policy.id));
  const singleSelectedPolicy = selectedPolicies.length === 1 ? selectedPolicies[0] : null;
  const allVisiblePoliciesSelected = filteredPolicies.length > 0 && filteredPolicies.every(policy => selectedPolicyIds.includes(policy.id));

  const toggleSelectedPolicy = (policyId: string) => {
    setSelectedPolicyIds(prev => prev.includes(policyId) ? prev.filter(id => id !== policyId) : [...prev, policyId]);
  };

  const toggleVisiblePolicies = () => {
    setSelectedPolicyIds(prev => {
      const visibleIds = filteredPolicies.map(policy => policy.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };

  const policyTableColumns = useMemo<HyperTableColumn<PolicyItem>[]>(() => {
    const columns: HyperTableColumn<PolicyItem>[] = [
      {
        id: 'select',
        header: () => (
          <input
            type="checkbox"
            checked={allVisiblePoliciesSelected}
            onClick={event => event.stopPropagation()}
            onChange={toggleVisiblePolicies}
          />
        ),
        cell: info => (
          <input
            type="checkbox"
            checked={selectedPolicyIds.includes(info.row.original.id)}
            onClick={event => event.stopPropagation()}
            onChange={() => toggleSelectedPolicy(info.row.original.id)}
          />
        ),
        size: 42,
        minSize: 42,
        maxSize: 54,
        enableSorting: false,
        enableResizing: false,
        meta: { align: 'center' },
      },
      {
        id: 'name',
        header: 'Policy Name',
        accessorFn: policy => policy.name,
        size: 280,
        minSize: 210,
        maxSize: 540,
        cell: info => {
          const policy = info.row.original;
          return (
            <div className="hbdr-dr-name-cell">
              <div className="hbdr-dr-namespace-icon">
                <ShieldCheck size={18} />
              </div>
              <div>
                <p className="hbdr-dr-app-name">{policy.name}</p>
              </div>
            </div>
          );
        },
        meta: { title: policy => `${policy.name} (${policy.id})` },
      },
    ];
    const addColumn = (column: HyperTableColumn<PolicyItem>) => {
      if (visibleColumns.includes(column.id as string)) columns.push(column);
    };
    addColumn({
      id: 'composition',
      header: 'Schedule Type',
      accessorFn: policy => formatPolicyComposition(policy.composition),
      size: 190,
      minSize: 170,
      cell: info => <span className="hbdr-dr-policy">{formatPolicyComposition(info.row.original.composition)}</span>,
      meta: { title: policy => formatPolicyComposition(policy.composition) },
    });
    addColumn({
      id: 'schedule',
      header: 'Execution Plan',
      accessorFn: policy => formatPolicySchedule(policy),
      size: 210,
      minSize: 160,
      maxSize: 360,
      cell: info => formatPolicySchedule(info.row.original),
      meta: { title: policy => formatPolicySchedule(policy) },
    });
    addColumn({
      id: 'retention',
      header: 'Retained Copies',
      accessorFn: policy => policy.retention,
      size: 150,
      minSize: 130,
      cell: info => formatPolicyRetention(info.row.original),
      meta: { title: policy => formatPolicyRetention(policy) },
    });
    addColumn({
      id: 'bound',
      header: 'Bound Apps',
      accessorFn: policy => policy.bound,
      size: 138,
      minSize: 120,
      cell: info => `${info.row.original.bound} applications`,
      meta: { title: policy => `${policy.bound} applications` },
    });
    addColumn({
      id: 'status',
      header: 'Status',
      accessorFn: policy => policy.status,
      size: 116,
      minSize: 100,
      cell: info => <span className={info.row.original.status === 'Active' ? 'hbdr-dr-task-ok' : 'hbdr-dr-task-warn'}>{info.row.original.status === 'Active' ? 'ACTIVE' : 'DISABLED'}</span>,
      meta: { title: policy => policy.status },
    });
    return columns;
  }, [allVisiblePoliciesSelected, selectedPolicyIds, visibleColumns]);

  const openCreatePolicy = () => {
    setEditingPolicyId(null);
    setPolicyForm(defaultPolicyForm());
    setPolicyModalOpen(true);
  };

  const openEditPolicy = (policy: PolicyItem) => {
    setEditingPolicyId(policy.id);
    setPolicyForm({
      name: policy.name,
      composition: policy.composition || 'combined',
      type: policy.type,
      intervalValue: policy.intervalValue,
      intervalUnit: policy.intervalUnit,
      hour: policy.hour,
      minute: policy.minute,
      weekDay: policy.weekDay,
      monthDay: policy.monthDay,
      retention: policy.retention,
    });
    setPolicyModalOpen(true);
    setMenuId(null);
  };

  const closePolicyModal = () => {
    setPolicyModalOpen(false);
    setEditingPolicyId(null);
    setPolicyForm(defaultPolicyForm());
  };

  const savePolicy = async () => {
    if (!policyForm.name.trim()) return;
    const normalizedForm = {
      ...policyForm,
      intervalValue: Math.max(1, Number(policyForm.intervalValue) || 1),
      retention: Math.max(1, Number(policyForm.retention) || 1),
    };
    const input = {
      name: normalizedForm.name.trim(),
      composition: normalizedForm.composition,
      scheduleType: normalizedForm.type,
      intervalValue: normalizedForm.intervalValue,
      intervalUnit: normalizedForm.intervalUnit,
      hour: normalizedForm.hour,
      minute: normalizedForm.minute,
      weekDay: normalizedForm.weekDay,
      monthDay: normalizedForm.monthDay,
      retentionCount: normalizedForm.retention,
      retentionDays: 0,
      status: 'active',
    };
    try {
      const created = await apiPost<ApiPolicy>('/api/v1/policies', input);
      const mapped = mapPolicy(created);
      setPolicies(prev => prev.some(p => p.id === mapped.id) ? prev.map(p => p.id === mapped.id ? mapped : p) : [mapped, ...prev]);
      closePolicyModal();
    } catch (error) {
      console.error('Failed to create policy', error);
    }
  };

  const togglePolicyStatus = (policy: PolicyItem) => {
    setPolicies(prev => prev.map(item => item.id === policy.id ? { ...item, status: item.status === 'Active' ? 'Disabled' : 'Active' } : item));
    setMenuId(null);
  };

  const confirmDeletePolicy = () => {
    if (!deletePolicy) return;
    setPolicies(prev => prev.filter(policy => policy.id !== deletePolicy.id));
    setSelectedPolicyIds(prev => prev.filter(id => id !== deletePolicy.id));
    setDeletePolicy(null);
  };

  return (
    <motion.div key="policies" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><ShieldCheck size={18} /></div>
            <div className="hbdr-policy-page-title">
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Policies</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Maintain application protection plans and recovery targets.</p>
            </div>
          </div>
          <div />
        </div>
      </div>

      {true ? (
        <>
        <div className="hbdr-dr-table-card hbdr-policy-table-list">
          <div className="hbdr-dr-table-head">
            <div className="hbdr-dr-toolbar">
              <div className="hbdr-dr-action-group">
                <button onClick={openCreatePolicy} className="hbdr-dr-action-primary">New DR Policy</button>
                <div className="relative">
                  <button disabled={selectedPolicies.length === 0} onClick={() => setPolicyBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                    More <ChevronDown size={15} className={policyBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                  </button>
                  <AnimatePresence>
                    {policyBulkMenuOpen && selectedPolicies.length > 0 && (
                      <>
                        <div className="fixed inset-0 z-30" onClick={() => setPolicyBulkMenuOpen(false)} />
                        <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; openEditPolicy(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Edit2 size={14} />Edit Policy</button>
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; togglePolicyStatus(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><RefreshCw size={14} />{singleSelectedPolicy?.status === 'Active' ? 'Disable Policy' : 'Enable Policy'}</button>
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; setDeletePolicy(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Trash2 size={14} />Delete Policy</button>
                        </motion.div>
                      </>
                    )}
                  </AnimatePresence>
                </div>
              </div>
              <ListToolbarControls
                query={query}
                setQuery={setQuery}
                queryField={queryField}
                setQueryField={setQueryField}
                queryFields={policyQueryFields}
                tags={[
                  { value: 'manual', label: 'Manual', count: policies.filter(policy => policy.composition === 'manual').length },
                  { value: 'combined', label: 'Schedule + Retention', count: policies.filter(policy => policy.composition === 'combined').length },
                  { value: 'schedule', label: 'Schedule Only', count: policies.filter(policy => policy.composition === 'schedule').length },
                  { value: 'retention', label: 'Retention Only', count: policies.filter(policy => policy.composition === 'retention').length },
                ]}
                activeTags={activeTags}
                setActiveTags={setActiveTags}
                filters={[
                  { value: 'active', label: 'Active', count: policies.filter(policy => policyMatchesFilter(policy, 'active')).length },
                  { value: 'disabled', label: 'Disabled', count: policies.filter(policy => policyMatchesFilter(policy, 'disabled')).length },
                  { value: 'bound', label: 'Bound to Apps', count: policies.filter(policy => policyMatchesFilter(policy, 'bound')).length },
                  { value: 'unbound', label: 'Not Bound', count: policies.filter(policy => policyMatchesFilter(policy, 'unbound')).length },
                ]}
                activeFilters={activeFilters}
                setActiveFilters={setActiveFilters}
                columns={policyColumns}
                visibleColumns={visibleColumns}
                setVisibleColumns={setVisibleColumns}
                onRefresh={() => {
                  setPolicies(prev => [...prev]);
                  setSelectedPolicyIds([]);
                }}
              />
            </div>
          </div>
          <HyperTable
            variant="page"
            density="comfortable"
            columns={policyTableColumns}
            data={filteredPolicies}
            getRowId={row => row.id}
            onRowClick={row => toggleSelectedPolicy(row.id)}
            getRowClassName={row => selectedPolicyIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
            selectedCount={selectedPolicyIds.length}
            emptyMessage={policies.length === 0 ? 'No DR policies yet' : 'No policies match the current filters'}
            className="hbdr-policy-hyper-table"
          />
        </div>
        <div className="hidden grid-cols-1 gap-4">
          {filteredPolicies.map(policy => (
            <div key={policy.id} className={`relative flex flex-col gap-5 overflow-visible rounded-2xl border border-slate-200 bg-white p-6 shadow-sm transition-all hover:border-slate-300 hover:shadow-md lg:flex-row lg:items-center lg:justify-between ${menuId === policy.id ? 'z-40' : 'z-0'}`}>
              <div className="flex min-w-0 items-center gap-5">
                <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl border border-violet-100 bg-violet-50 text-violet-600 shadow-sm"><ShieldCheck size={22} /></div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="truncate text-lg font-bold tracking-tight text-slate-900">{policy.name}</h3>
                    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold uppercase text-slate-500">{formatPolicyComposition(policy.composition)}</span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm font-medium text-slate-500">
                    <span className="flex items-center gap-1.5"><Clock size={14} className="text-slate-400" />{formatPolicySchedule(policy)}</span>
                    <span className="flex items-center gap-1.5"><History size={14} className="text-slate-400" />Retention: {formatPolicyRetention(policy)}</span>
                    <span className="flex items-center gap-1.5"><Layers size={14} className="text-slate-400" />Bound {policy.bound} applications</span>
                  </div>
                </div>
              </div>
              <div className="flex shrink-0 items-center justify-between gap-5 lg:justify-end">
                <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ${policy.status === 'Active' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>{policy.status === 'Active' ? 'Active' : 'Disabled'}</span>
                <div className="relative" data-policy-menu-root>
                  <button onClick={(event) => { event.stopPropagation(); setMenuId(menuId === policy.id ? null : policy.id); }} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700"><MoreVertical size={18} /></button>
                  <AnimatePresence>
                    {menuId === policy.id && (
                      <>
                        <div className="fixed inset-0 z-30" onClick={() => setMenuId(null)} />
                        <motion.div data-policy-menu-root onClick={(event) => event.stopPropagation()} initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute right-0 top-10 z-50 w-44 overflow-hidden rounded-xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5">
                          <button onClick={() => openEditPolicy(policy)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Edit2 size={14} />Edit Policy</button>
                          <button onClick={() => togglePolicyStatus(policy)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><RefreshCw size={14} />{policy.status === 'Active' ? 'Disable Policy' : 'Enable Policy'}</button>
                          <button onClick={() => { setDeletePolicy(policy); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete Policy</button>
                        </motion.div>
                      </>
                    )}
                  </AnimatePresence>
                </div>
              </div>
            </div>
          ))}
        </div>
        </>
      ) : (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-slate-200 bg-white p-16 text-center shadow-sm">
          <div className="mb-4 rounded-full border border-slate-100 bg-slate-50 p-4 text-slate-400"><Search size={28} /></div>
          <h4 className="text-sm font-bold text-slate-800">No matching DR policies</h4>
          <p className="mt-1 max-w-sm text-xs text-slate-400">No matching backup policies. Try another keyword or create a new policy.</p>
          {query && <button onClick={() => setQuery('')} className="mt-4 rounded-lg border border-blue-100 bg-blue-50 px-4 py-1.5 text-xs font-semibold text-blue-600 transition-colors hover:bg-blue-100">Reset Filters</button>}
        </div>
      )}

      <AnimatePresence>
        {policyModalOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closePolicyModal} className="hbdr-filter-drawer-backdrop" />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-policy-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>{editingPolicyId ? 'Edit DR Policy' : 'New DR Policy'}</strong>
                  <span>Define backup schedule and retention rules.</span>
                </div>
                <button type="button" onClick={closePolicyModal} aria-label="Close policy drawer"><X size={18} /></button>
              </div>

              <div className="hbdr-filter-drawer-body hbdr-policy-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Name</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <span className="text-[11px] font-black uppercase tracking-[0.2em] text-indigo-600">Policy Name</span>
                  <input type="text" value={policyForm.name} onChange={event => setPolicyForm({ ...policyForm, name: event.target.value })} className="w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm font-medium outline-none transition-all focus:border-indigo-500 focus:ring-4 focus:ring-indigo-100" placeholder="Example: Core workload-5minute rapid DR" />
                  </div>
                </section>

                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Composition</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid gap-2 md:grid-cols-3">
                    {[
                      { id: 'combined' as PolicyComposition, title: 'Schedule + Retention', badge: 'Recommended' },
                      { id: 'schedule' as PolicyComposition, title: 'Schedule Only', badge: 'Timing' },
                      { id: 'retention' as PolicyComposition, title: 'Retention Only', badge: 'Lifecycle' },
                    ].map(item => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => setPolicyForm({ ...policyForm, composition: item.id })}
                        aria-pressed={policyForm.composition === item.id}
                        className={`hbdr-policy-composition-card flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-left transition-all ${policyForm.composition === item.id ? 'border-indigo-500 bg-indigo-50 text-indigo-950 shadow-sm' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:bg-slate-50'}`}
                      >
                        <span>
                          <span className={`mb-1 inline-flex rounded-full px-2 py-0.5 text-[10px] font-black ${policyForm.composition === item.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}>{item.badge}</span>
                          <strong className="block text-sm font-black">{item.title}</strong>
                        </span>
                        <span className={`h-4 w-4 rounded-full border ${policyForm.composition === item.id ? 'border-indigo-600 bg-indigo-600' : 'border-slate-300 bg-white'}`} />
                      </button>
                    ))}
                  </div>
                  </div>
                </section>

                {showScheduleConfig && (
                <section className="hbdr-advanced-filter-section">
                  <h4>Schedule Type</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid grid-cols-2 gap-2">
                    {[
                      { id: 'interval' as PolicyScheduleType, label: 'Interval', icon: Zap },
                      { id: 'daily' as PolicyScheduleType, label: 'Daily Backup', icon: Sun },
                      { id: 'weekly' as PolicyScheduleType, label: 'Weekly Backup', icon: Calendar },
                      { id: 'monthly' as PolicyScheduleType, label: 'Monthly Backup', icon: Layers },
                    ].map(type => {
                      const TypeIcon = type.icon;
                      return (
                        <button key={type.id} onClick={() => setPolicyForm({ ...policyForm, type: type.id })} className={`flex items-center gap-2 rounded-xl border-2 px-3 py-2 transition-all ${policyForm.type === type.id ? 'border-indigo-600 bg-indigo-50/50 shadow-sm' : 'border-slate-100 hover:border-slate-200'}`}>
                          <span className={`rounded-lg p-1.5 ${policyForm.type === type.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}><TypeIcon size={16} /></span>
                          <span className={`text-sm font-bold ${policyForm.type === type.id ? 'text-indigo-900' : 'text-slate-600'}`}>{type.label}</span>
                        </button>
                      );
                    })}
                  </div>

                  <AnimatePresence mode="wait">
                    <motion.div key={policyForm.type} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -10 }} className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                      {policyForm.type === 'interval' && (
                        <div className="flex flex-wrap items-center gap-3">
                          <span className="text-sm font-bold text-slate-700">Run every: </span>
                          <input type="number" min={1} value={policyForm.intervalValue} onChange={event => setPolicyForm({ ...policyForm, intervalValue: Number(event.target.value) })} className="w-20 rounded-lg border border-slate-200 bg-white px-3 py-2 text-center text-sm font-bold outline-none focus:border-indigo-500" />
                          <select value={policyForm.intervalUnit} onChange={event => setPolicyForm({ ...policyForm, intervalUnit: event.target.value as 'minutes' | 'hours' })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            <option value="minutes">minutes</option>
                            <option value="hours">hours</option>
                          </select>
                        </div>
                      )}
                      {policyForm.type === 'daily' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Run every:</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'weekly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Daily execution time:</span>
                          <select value={policyForm.weekDay} onChange={event => setPolicyForm({ ...policyForm, weekDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {weekdays.map((day, index) => <option key={day} value={index}>{day}</option>)}
                          </select>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'monthly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Every month</span>
                          <select value={policyForm.monthDay} onChange={event => setPolicyForm({ ...policyForm, monthDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {Array.from({ length: 31 }).map((_, index) => <option key={index + 1} value={index + 1}>{index + 1}</option>)}
                          </select>
                          <span className="text-sm font-bold text-slate-700">Day</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                    </motion.div>
                  </AnimatePresence>
                  </div>
                </section>
                )}

                {showRetentionConfig && (
                <section className="hbdr-advanced-filter-section">
                  <h4>Retention Policy</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                    <div className="flex max-w-[260px] items-center gap-3">
                      <input type="number" min={1} value={policyForm.retention} onChange={event => setPolicyForm({ ...policyForm, retention: Number(event.target.value) })} className="w-full rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-bold outline-none focus:border-indigo-500" />
                      <span className="text-xs font-bold uppercase text-slate-400">valid copies</span>
                    </div>
                  </div>
                  </div>
                </section>
                )}
              </div>

              <div className="hbdr-filter-drawer-actions hbdr-policy-drawer-actions">
                <button type="button" onClick={savePolicy} disabled={!policyForm.name.trim()}><CheckCircle2 size={15} />{editingPolicyId ? 'Update Policy' : 'Save Policy'}</button>
                <button type="button" onClick={closePolicyModal}>Cancel</button>
              </div>
            </motion.div>
          </>
        )}

        {deletePolicy && (
          <ModalFrame title="Delete Policy" onClose={() => setDeletePolicy(null)}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Confirm policy deletion <strong>{deletePolicy.name}</strong>? Bound applications will not be automatically migrated.
              </div>
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Policy Name" value={deletePolicy.name} />
                <Info label="Schedule" value={formatPolicySchedule(deletePolicy)} />
                <Info label="Retained Copies" value={formatPolicyRetention(deletePolicy)} />
                <Info label="Bound Apps" value={`${deletePolicy.bound}`} />
              </div>
              <div className="flex justify-end gap-3">
                <button onClick={() => setDeletePolicy(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button onClick={confirmDeletePolicy} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700 active:scale-95">Delete</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function TimeSelector({ hour, minute, onChange }: { hour: number; minute: number; onChange: (hour: number, minute: number) => void }) {
  return (
    <div className="flex items-center gap-2">
      <select value={hour} onChange={event => onChange(Number(event.target.value), minute)} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
        {Array.from({ length: 24 }).map((_, index) => <option key={index} value={index}>{String(index).padStart(2, '0')}</option>)}
      </select>
      <span className="font-bold text-slate-500">:</span>
      <select value={minute} onChange={event => onChange(hour, Number(event.target.value))} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
        {Array.from({ length: 60 }).map((_, index) => <option key={index} value={index}>{String(index).padStart(2, '0')}</option>)}
      </select>
    </div>
  );
}

function RealRestorePointPage({
  openDr,
  toast,
  workspaceCluster,
  clusterContext,
  initialPoints = [],
  initialClusters = [],
  initialStorageRepos = [],
  initialPlans = [],
  initialTasks = [],
  namespaceFilter = [],
}: {
  openDr: () => void;
  toast: (msg: string) => void;
  workspaceCluster?: Cluster | ApiCluster | null;
  clusterContext?: React.ReactNode;
  initialPoints?: ApiRestorePoint[];
  initialClusters?: ApiCluster[];
  initialStorageRepos?: ApiStorageRepo[];
  initialPlans?: ApiProtectionPlan[];
  initialTasks?: ApiTask[];
  namespaceFilter?: string[];
}) {
  type Row = {
    id: string;
    name: string;
    namespace: string;
    namespaces: string[];
    namespaceLabel: string;
    uid: string;
    pointId: string;
    time: string;
    media: string;
    backupMode: string;
    storage: string;
    size: string;
    sizeTitle: string;
    sizeBytes: number;
    storageSize: string;
    storageSizeTitle: string;
    storageSizeBytes: number;
    sourceCluster: string;
    targetCluster: string;
    targetClusterId: string;
    sourceClusterId: string;
    protectionPlanId?: string;
    status: string;
    veleroBackupName: string;
  };
  const [points, setPoints] = useState<ApiRestorePoint[]>(initialPoints);
  const [clusters, setClusters] = useState<ApiCluster[]>(initialClusters);
  const [storageRepos, setStorageRepos] = useState<ApiStorageRepo[]>(initialStorageRepos);
  const [plans, setPlans] = useState<ApiProtectionPlan[]>(initialPlans);
  const [tasks, setTasks] = useState<ApiTask[]>(initialTasks);
  const [loading, setLoading] = useState(initialPoints.length === 0);
  const [selectedRestorePoints, setSelectedRestorePoints] = useState<string[]>([]);
  const [activeRestorePointId, setActiveRestorePointId] = useState('');
  const [restoreAction, setRestoreAction] = useState<{ mode: 'drill' | 'takeover'; row: Row; config: RecoveryWizardConfig } | null>(null);
  const [recoveryTaskDetail, setRecoveryTaskDetail] = useState<{ row: Row; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null>(null);
  const [recoveryTaskDetailEvents, setRecoveryTaskDetailEvents] = useState<ApiTaskEvent[]>([]);
  const [deleteDialog, setDeleteDialog] = useState<{
    rows: Row[];
    status: 'confirm' | 'submitting' | 'deleting' | 'succeeded' | 'failed';
    taskIds: string[];
    message: string;
  } | null>(null);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('namespace');
  const [quickNamespaceFilters, setQuickNamespaceFilters] = useState<string[]>([]);
  const [draftNamespaceFilters, setDraftNamespaceFilters] = useState<string[]>([]);
  const [namespaceFilterSearch, setNamespaceFilterSearch] = useState('');
  const [quickNamespaceMenuOpen, setQuickNamespaceMenuOpen] = useState(false);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState<string[]>(['time', 'storage', 'size', 'storageSize', 'media', 'backupMode', 'task']);
  const [restoreBulkMenuOpen, setRestoreBulkMenuOpen] = useState(false);

  const load = async () => {
    const [pointRes, clusterRes, storageRes, planRes, taskRes] = await Promise.all([
      apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
      apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
      apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
      apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
      apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
    ]);
    setPoints(listItems(pointRes));
    setClusters(listItems(clusterRes));
    setStorageRepos(listItems(storageRes));
    setPlans(listItems(planRes));
    setTasks(listItems(taskRes));
    setLoading(false);
  };

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        if (!cancelled) await load();
      } catch {
        if (!cancelled) setLoading(false);
        toast('Failed to refresh restore points');
      }
    };
    refresh();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  useEffect(() => {
    const nextNamespaces = Array.from(new Set(namespaceFilter.map(item => item.trim()).filter(Boolean)));
    setSelectedRestorePoints([]);
    setActiveRestorePointId('');
    setQuickNamespaceFilters(nextNamespaces);
    setDraftNamespaceFilters(nextNamespaces);
    setNamespaceFilterSearch('');
    setQuickNamespaceMenuOpen(false);
  }, [namespaceFilter.join('\n')]);

  useEffect(() => {
    if (!recoveryTaskDetail?.task.id) {
      setRecoveryTaskDetailEvents([]);
      return;
    }
    let cancelled = false;
    const loadEvents = async () => {
      try {
        const result = await apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${recoveryTaskDetail.task.id}/events`);
        if (!cancelled) setRecoveryTaskDetailEvents(listItems(result));
      } catch {
        if (!cancelled) setRecoveryTaskDetailEvents([]);
      }
    };
    void loadEvents();
    const timer = isActiveTaskStatus(recoveryTaskDetail.task.status)
      ? window.setInterval(loadEvents, 3000)
      : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [recoveryTaskDetail?.task.id, recoveryTaskDetail?.task.status]);

  useEffect(() => {
    setSelectedRestorePoints([]);
    setActiveRestorePointId('');
  }, [quickNamespaceFilters.join('\n')]);

  useEffect(() => {
    if (!deleteDialog || deleteDialog.status !== 'deleting' || deleteDialog.taskIds.length === 0) return;
    const deleteTasks = deleteDialog.taskIds
      .map(id => tasks.find(task => task.id === id))
      .filter(Boolean) as ApiTask[];
    if (deleteTasks.length === 0) return;
    const failed = deleteTasks.find(task => isFailedStatus(task.status));
    if (failed) {
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'failed',
        message: failed.errorMessage || failed.errorCode || 'Velero delete failed.',
      } : prev);
      return;
    }
    if (deleteTasks.length === deleteDialog.taskIds.length && deleteTasks.every(task => isCompletedTaskStatus(task.status))) {
      setSelectedRestorePoints([]);
      setActiveRestorePointId('');
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'succeeded',
        message: 'Restore points deleted from Velero and the backup repository.',
      } : prev);
      void load();
    }
  }, [tasks, deleteDialog]);

  const rows: Row[] = points.filter(point => !workspaceCluster?.id || point.sourceClusterId === workspaceCluster.id).map(point => {
    const sourceCluster = clusters.find(item => item.id === point.sourceClusterId);
    const plan = plans.find(item => item.id === point.protectionPlanId || item.appId === point.appId || Boolean(point.appId && item.appIds?.includes(point.appId)));
    const targetCluster = clusters.find(item => item.id === plan?.targetClusterId) || clusters.find(item => item.id !== point.sourceClusterId);
    const repo = storageRepos.find(item => item.id === point.storageRepoId);
    const originalSize = restorePointOriginalSize(point);
    const storageSize = restorePointStorageSize(point);
    const namespaces = restorePointNamespaces(point);
    const primaryNamespace = namespaces[0] || point.sourceNamespace || point.veleroBackupName;
    const namespaceLabel = namespaces.length > 0 ? namespaces.join(', ') : point.veleroBackupName;
    return {
      id: point.id,
      name: namespaceLabel,
      namespace: primaryNamespace,
      namespaces,
      namespaceLabel,
      uid: point.id.slice(0, 8),
      pointId: point.id,
      time: restorePointDisplayLabel(point),
      media: point.pointType === 'backup' ? 'Remote Snapshot' : point.pointType,
      backupMode: restorePointIsScheduled(point) ? 'Automatic' : 'Manual',
      storage: repo?.name || point.backupStorageName || 'default',
      size: originalSize.label,
      sizeTitle: originalSize.title,
      sizeBytes: originalSize.bytes,
      storageSize: storageSize.label,
      storageSizeTitle: storageSize.title,
      storageSizeBytes: storageSize.bytes,
      sourceCluster: sourceCluster?.name || 'Unknown cluster',
      targetCluster: targetCluster?.name || sourceCluster?.name || 'Target Cluster',
      targetClusterId: targetCluster?.id || sourceCluster?.id || point.sourceClusterId,
      sourceClusterId: point.sourceClusterId,
      protectionPlanId: point.protectionPlanId,
      status: restorePointListStatus(point),
      veleroBackupName: point.veleroBackupName,
    };
  }).sort((a, b) => (b.time || '').localeCompare(a.time || ''));
  const availableRows = rows.filter(row => row.status === 'available');
  const namespaceOptions = Array.from(availableRows.reduce((acc, row) => {
    const namespaces = row.namespaces.length ? row.namespaces : [row.namespace].filter(Boolean);
    namespaces.forEach(namespace => {
      acc.set(namespace, (acc.get(namespace) || 0) + 1);
    });
    return acc;
  }, new Map<string, number>()))
    .map(([namespace, count]) => ({ namespace, count }))
    .sort((a, b) => a.namespace.localeCompare(b.namespace));
  const quickNamespaceFilterSet = new Set(quickNamespaceFilters);
  const quickNamespaceFilterLabel = quickNamespaceFilters.length === 0
    ? 'All namespaces'
    : quickNamespaceFilters.length === 1
      ? quickNamespaceFilters[0]
      : `${quickNamespaceFilters.length} namespaces`;
  const namespaceFilterSearchTerm = namespaceFilterSearch.trim().toLowerCase();
  const visibleNamespaceOptions = namespaceOptions.filter(option => !namespaceFilterSearchTerm || option.namespace.toLowerCase().includes(namespaceFilterSearchTerm));
  const restorePointToolbarColumns = [
    { value: 'time', label: 'Restore Point' },
    { value: 'storage', label: 'Repository' },
    { value: 'size', label: 'Original Size' },
    { value: 'storageSize', label: 'Storage Size' },
    { value: 'media', label: 'Type' },
    { value: 'backupMode', label: 'Backup Mode' },
    { value: 'task', label: 'Recovery Task' },
  ];
  const restorePointQueryFields = listToolbarQueryFields([
    { value: 'namespace', label: 'Namespace' },
    { value: 'backup', label: 'Backup Name' },
    { value: 'cluster', label: 'Cluster' },
  ], restorePointToolbarColumns, visibleColumns);
  const openQuickNamespaceFilter = () => {
    setDraftNamespaceFilters(quickNamespaceFilters);
    setNamespaceFilterSearch('');
    setQuickNamespaceMenuOpen(true);
  };
  const applyQuickNamespaceFilter = () => {
    setQuickNamespaceFilters(Array.from(new Set(draftNamespaceFilters)));
    setQuickNamespaceMenuOpen(false);
  };
  const filteredRows = availableRows.filter(row => {
    const rowNamespaces = row.namespaces.length ? row.namespaces : [row.namespace].filter(Boolean);
    if (quickNamespaceFilterSet.size > 0 && !rowNamespaces.some(namespace => quickNamespaceFilterSet.has(namespace))) return false;
    const q = query.trim().toLowerCase();
    const searchableValues: Record<string, string> = {
      namespace: row.namespaceLabel,
      backup: row.veleroBackupName || row.pointId,
      cluster: row.sourceCluster,
      time: row.time,
      storage: row.storage,
      size: row.size,
      storageSize: row.storageSize,
      media: row.media,
      backupMode: row.backupMode,
      task: latestTaskForRestorePoint(tasks, row.id)?.status || 'No recovery task',
    };
    const matchesQuery = !q || (searchableValues[queryField] || Object.values(searchableValues).join(' ')).toLowerCase().includes(q);
    const latestTask = latestTaskForRestorePoint(tasks, row.id);
    const matchesFilters = activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => searchableValues[field] || '');
      if (filter === 'remote') return row.media === 'Remote Snapshot';
      if (filter === 'local') return row.media === 'Local Snapshot';
      if (filter === 'manual') return row.backupMode === 'Manual';
      if (filter === 'automatic') return row.backupMode === 'Automatic';
      if (filter === 'running') return isActiveTaskStatus(latestTask?.status);
      if (filter === 'failed') return isFailedStatus(latestTask?.status);
      return true;
    });
    return matchesQuery && matchesFilters;
  });
  const selectedRestorePointId = activeRestorePointId || selectedRestorePoints.at(-1) || '';
  const selectedRow = selectedRestorePointId
    ? rows.find(row => row.id === selectedRestorePointId)
      || rows.find(row => selectedRestorePoints.includes(row.id))
      || null
    : rows.find(row => selectedRestorePoints.includes(row.id)) || null;
  const activeRecoveryTaskForSelectedRow = selectedRow
    ? tasks.find(task => ['restore', 'drill', 'takeover'].includes(task.type)
      && isActiveTaskStatus(task.status)
      && taskMatchesRestorePoint(task, selectedRow.id))
    : undefined;
  const activeRecoveryTaskIds = tasks
    .filter(task => ['restore', 'drill', 'takeover'].includes(task.type) && isActiveTaskStatus(task.status))
    .map(task => task.id)
    .filter(Boolean)
    .sort();
  const activeRecoveryTaskKey = activeRecoveryTaskIds.join('|');

  useEffect(() => {
    const ids = activeRecoveryTaskKey ? activeRecoveryTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const idSet = new Set(ids);
    const pollActiveRecoveryTasks = async () => {
      try {
        const [taskRes, pointRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
          apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
        ]);
        if (cancelled) return;
        const latestTasks = new Map(listItems(taskRes).map(task => [task.id, task]));
        setTasks(prev => prev.map(task => {
          if (!idSet.has(task.id)) return task;
          const next = latestTasks.get(task.id);
          return next ? { ...task, ...next } : task;
        }));
        setPoints(listItems(pointRes));
      } catch {
        // Keep the current restore point list stable if a status poll fails.
      }
    };
    void pollActiveRecoveryTasks();
    const timer = window.setInterval(pollActiveRecoveryTasks, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeRecoveryTaskKey]);

  useEffect(() => {
    if (!recoveryTaskDetail?.task.id) return;
    const latest = tasks.find(task => task.id === recoveryTaskDetail.task.id);
    if (!latest) return;
    const current = recoveryTaskDetail.task;
    if (
      latest.status === current.status
      && Number(latest.progress || 0) === Number(current.progress || 0)
      && latest.completedAt === current.completedAt
      && latest.errorCode === current.errorCode
      && latest.errorMessage === current.errorMessage
    ) return;
    setRecoveryTaskDetail(prev => {
      if (!prev || prev.task.id !== latest.id) return prev;
      return { ...prev, task: { ...prev.task, ...latest } };
    });
  }, [
    tasks,
    recoveryTaskDetail?.task.id,
    recoveryTaskDetail?.task.status,
    recoveryTaskDetail?.task.progress,
    recoveryTaskDetail?.task.completedAt,
    recoveryTaskDetail?.task.errorCode,
    recoveryTaskDetail?.task.errorMessage,
  ]);
  const hasRestorePointSelection = selectedRestorePoints.some(id => rows.some(row => row.id === id));
  const selectedDisabled = !hasRestorePointSelection || Boolean(activeRecoveryTaskForSelectedRow);
  const allVisibleSelected = filteredRows.length > 0 && filteredRows.every(row => selectedRestorePoints.includes(row.id));
  const toggleVisibleRows = () => {
    if (allVisibleSelected) {
      setSelectedRestorePoints([]);
      setActiveRestorePointId('');
      return;
    }
    const ids = filteredRows.map(row => row.id);
    setSelectedRestorePoints(ids);
    setActiveRestorePointId(ids[0] || '');
  };
  const toggleSelectedRestorePoint = (id: string) => {
    setSelectedRestorePoints(prev => {
      if (prev.includes(id)) {
        const next = prev.filter(item => item !== id);
        if (activeRestorePointId === id) setActiveRestorePointId(next.at(-1) || '');
        return next;
      }
      setActiveRestorePointId(id);
      return [...prev, id];
    });
  };
  const clusterOptions = clusters.map((cluster, index) => ({
    id: cluster.id,
    name: cluster.name,
    region: cluster.connectionStatus === 'online' ? 'connected' : 'disconnected',
    version: cluster.kubeVersion || 'unknown',
    isCurrent: index === 0,
  }));
  const repositoryOptions = storageRepos.map(repo => ({
    id: repo.id,
    name: repo.name,
    type: repo.type,
    endpoint: repo.endpoint || '',
    bucket: repo.bucket || '',
  }));
  const pointsForRow = (row: Row) => [
    { id: row.id, title: 'Backup task', time: row.time, type: row.media, status: 'Available' },
  ];
  const buildConfig = (mode: 'drill' | 'takeover', row: Row): RecoveryWizardConfig => {
    const currentClusterName = clusters.find(cluster => cluster.id === row.sourceClusterId)?.name || 'Source Cluster';
    const targetCluster = row.targetCluster || currentClusterName;
    const targetMode = targetCluster === currentClusterName
      ? mode === 'drill' ? 'sandbox' : 'inPlace'
      : 'crossCluster';
    return {
      pointId: row.id,
      sourceType: row.media.toLowerCase().includes('local') ? 'snapshot' : 'export',
      targetMode,
      targetCluster,
      namespaceMode: mode === 'takeover' ? 'original' : 'generated',
      targetNamespace: mode === 'takeover' ? row.namespace : `${row.namespace}-drill`,
      restoreMode: 'full',
      artifactMode: 'all',
      conflictPolicy: mode === 'takeover' ? 'replace' : 'skip',
      originalNamespaceConfirmed: false,
      includeClusterScoped: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : targetMode === 'sandbox' ? 'drill' : 'none',
      storageProfileMode: 'original',
      alternateProfileId: '',
      preflightChecks: true,
      autoStartValidation: mode === 'drill',
      notes: mode === 'drill'
        ? 'Validate service startup, storage attachment, and namespace isolation after recovery.'
        : 'Confirm traffic cutover and production freeze before takeover.',
    };
  };
  const startRestoreAction = (mode: 'drill' | 'takeover') => {
    const row = selectedRow || rows.find(item => selectedRestorePoints.includes(item.id)) || null;
    if (!row) {
      toast('Select a restore point first');
      return;
    }
    const activeTask = tasks.find(task => ['restore', 'drill', 'takeover'].includes(task.type)
      && isActiveTaskStatus(task.status)
      && taskMatchesRestorePoint(task, row.id));
    if (activeTask) {
      const label = activeTask.type === 'drill' ? 'drill' : activeTask.type === 'takeover' ? 'takeover' : 'recovery';
      toast(`A ${label} task is already running for this restore point`);
      return;
    }
    setActiveRestorePointId(row.id);
    setRestoreAction({ mode, row, config: buildConfig(mode, row) });
  };
  const openDeleteRestorePointDialog = () => {
    const targetRows = selectedRestorePoints
      .map(id => rows.find(row => row.id === id))
      .filter(Boolean) as Row[];
    if (targetRows.length === 0) {
      toast('Select restore points to delete');
      return;
    }
    setRestoreBulkMenuOpen(false);
    setDeleteDialog({
      rows: targetRows,
      status: 'confirm',
      taskIds: [],
      message: 'This will create Velero DeleteBackupRequest objects on the source cluster.',
    });
  };
  const submitRestorePointDelete = async () => {
    if (!deleteDialog || deleteDialog.status !== 'confirm') return;
    const ids = deleteDialog.rows.map(row => row.id);
    setDeleteDialog(prev => prev ? { ...prev, status: 'submitting', message: 'Submitting delete request to the source cluster agent...' } : prev);
    try {
      const result = await apiPost<{ task?: ApiTask; tasks?: ApiTask[]; warning?: string }>('/api/v1/restore-points/delete', { restorePointIds: ids });
      const nextTasks = result.tasks?.length ? result.tasks : result.task ? [result.task] : [];
      await load();
      setDeleteDialog(prev => prev ? {
        ...prev,
        status: 'deleting',
        taskIds: nextTasks.map(task => task.id).filter(Boolean),
        message: result.warning || 'Waiting for Velero to delete the selected backups...',
      } : prev);
      toast(result.warning || `Delete requested for ${ids.length} restore point${ids.length === 1 ? '' : 's'}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'unknown error';
      setDeleteDialog(prev => prev ? { ...prev, status: 'failed', message } : prev);
      toast('Failed to delete restore points: ' + message);
    }
  };
  const renderRestorePageTask = (row: Row) => {
    const task = latestTaskForRestorePoint(tasks, row.id);
    if (!task) return <span className="hbdr-dr-task-neutral">No recovery task</span>;
    const label = task.type === 'drill' ? 'Drill' : task.type === 'takeover' ? 'Takeover' : 'Restore';
    if (task.status === 'succeeded') {
      const targetNamespace = String(task.payload?.targetNamespace || row.namespace);
      const targetCluster = clusters.find(cluster => cluster.id === task.clusterId)?.name
        || String(task.payload?.targetCluster || row.targetCluster || '');
      const restorePointLabel = row.time || row.veleroBackupName || row.pointId || 'restore point';
      const completedTitle = recoveryCompletedTargetTitle(restorePointLabel, task.completedAt, targetCluster, targetNamespace, `${label} complete`);
      const targetLabel = recoveryCompletedTargetLabel(targetCluster, targetNamespace);
      return <span className="hbdr-recovery-task-complete"><strong title={completedTitle}>[{restorePointLabel}] {label.toLowerCase()} complete</strong><em title={completedTitle}>{targetLabel}</em></span>;
    }
    if (task.status === 'failed') {
      const failure = taskFailureSummary(task);
      return (
        <TaskErrorStatus
          code={failure.code}
          title={failure.title}
          description={failure.description}
          detail={failure.fullText}
          onClick={event => {
            event.stopPropagation();
            setActiveRestorePointId(row.id);
            setRecoveryTaskDetail({ row, task, failure });
          }}
        />
      );
    }
    const progress = Math.max(0, Math.min(100, task.progress || 0));
    return (
      <span className="hbdr-dr-progress-cell hbdr-recovery-task-progress is-syncing">
        <em className="hbdr-sync-label">{label} running {formatPercent(progress)}%</em>
        <i><b style={{ width: `${progress}%` }} /></i>
        <small>{String(task.payload?.targetNamespace || row.namespace)}</small>
      </span>
    );
  };
  const restorePointTableColumns = useMemo<HyperTableColumn<Row>[]>(() => [
    {
      id: 'select',
      header: () => (
        <input
          type="checkbox"
          checked={allVisibleSelected}
          onClick={event => event.stopPropagation()}
          onChange={toggleVisibleRows}
        />
      ),
      cell: info => (
        <input
          type="checkbox"
          checked={selectedRestorePoints.includes(info.row.original.id)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedRestorePoint(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Namespace',
      accessorFn: row => row.namespaceLabel,
      size: 270,
      minSize: 190,
      maxSize: 520,
      cell: info => {
        const row = info.row.original;
        const namespaces = row.namespaces.length ? row.namespaces : [row.namespaceLabel].filter(Boolean);
        return (
          <div className="hbdr-dr-name-cell">
            <div className="hbdr-dr-namespace-icon"><Layers size={18} /></div>
            <div>
              {namespaces.length > 1 ? (
                <div className="hbdr-merged-namespace-list">
                  {namespaces.map(namespace => <p key={namespace} className="hbdr-dr-app-name">{namespace}</p>)}
                </div>
              ) : (
                <p className="hbdr-dr-app-name">{row.namespaceLabel}</p>
              )}
            </div>
          </div>
        );
      },
      meta: { title: row => `${row.namespaceLabel} / ${row.pointId}` },
    },
    ...(visibleColumns.includes('time') ? [{ id: 'time', header: 'Restore Point', accessorFn: (row: Row) => row.time, size: 190, minSize: 160, maxSize: 260, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.time}</span>, meta: { title: (row: Row) => row.time } }] : []),
    ...(visibleColumns.includes('storage') ? [{ id: 'storage', header: 'Repository', accessorFn: (row: Row) => row.storage, size: 150, minSize: 120, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.storage}</span>, meta: { title: (row: Row) => row.storage } }] : []),
    ...(visibleColumns.includes('size') ? [{ id: 'size', header: 'Original Size', accessorFn: (row: Row) => row.sizeBytes, size: 150, minSize: 130, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.size}</span>, meta: { title: (row: Row) => row.sizeTitle } }] : []),
    ...(visibleColumns.includes('storageSize') ? [{ id: 'storageSize', header: 'Storage Size', accessorFn: (row: Row) => row.storageSizeBytes, size: 150, minSize: 130, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.storageSize}</span>, meta: { title: (row: Row) => row.storageSizeTitle } }] : []),
    ...(visibleColumns.includes('media') ? [{ id: 'media', header: 'Type', accessorFn: (row: Row) => row.media, size: 145, minSize: 120, maxSize: 220, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.media}</span>, meta: { title: (row: Row) => row.media } }] : []),
    ...(visibleColumns.includes('backupMode') ? [{ id: 'backupMode', header: 'Backup Mode', accessorFn: (row: Row) => row.backupMode, size: 140, minSize: 120, maxSize: 190, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.backupMode}</span>, meta: { title: (row: Row) => row.backupMode } }] : []),
    ...(visibleColumns.includes('task') ? [{ id: 'task', header: 'Recovery Task', accessorFn: (row: Row) => latestTaskForRestorePoint(tasks, row.id)?.status || 'No recovery task', size: 210, minSize: 170, maxSize: 360, cell: (info: any) => renderRestorePageTask(info.row.original) }] : []),
  ], [allVisibleSelected, selectedRestorePoints, visibleColumns, tasks]);

  return (
    <motion.div key="restore" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
      <div className="hbdr-app-workspace-bar">
        <div className="min-w-0">
          <h3 className="hbdr-app-workspace-title">Restore Points</h3>
          <p className="hbdr-app-workspace-desc">Browse recovery points and launch drill or takeover.</p>
        </div>
        {clusterContext && <div className="hbdr-app-workspace-cluster">{clusterContext}</div>}
      </div>
      <div className="hbdr-dr-table-card hbdr-restore-point-table-list">
        {namespaceOptions.length > 0 && (
          <div className="hbdr-restore-quick-filter">
            <span>Namespace</span>
            <div className="hbdr-restore-namespace-menu">
              <button type="button" onClick={() => quickNamespaceMenuOpen ? setQuickNamespaceMenuOpen(false) : openQuickNamespaceFilter()}>
                <strong>{quickNamespaceFilterLabel}</strong>
                <ChevronDown size={14} className={quickNamespaceMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
              </button>
              <AnimatePresence>
                {quickNamespaceMenuOpen && (
                  <>
                    <div className="fixed inset-0 z-30" onClick={() => setQuickNamespaceMenuOpen(false)} />
                    <motion.div
                      initial={{ opacity: 0, y: 6, scale: 0.98 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 6, scale: 0.98 }}
                      className="hbdr-restore-namespace-menu-panel"
                    >
                      <div className="hbdr-restore-namespace-menu-search">
                        <Search size={13} />
                        <input value={namespaceFilterSearch} onChange={event => setNamespaceFilterSearch(event.target.value)} placeholder="Filter namespace" autoFocus />
                      </div>
                      <div className="hbdr-restore-namespace-menu-list">
                      {visibleNamespaceOptions.map(option => {
                        const selected = draftNamespaceFilters.includes(option.namespace);
                        return (
                          <button
                            key={option.namespace}
                            type="button"
                            className={selected ? 'is-active' : ''}
                            onClick={() => setDraftNamespaceFilters(prev => selected ? prev.filter(item => item !== option.namespace) : [...prev, option.namespace])}
                          >
                            <i aria-hidden="true">{selected && <Check size={12} />}</i>
                            <span>{option.namespace}</span>
                          </button>
                        );
                      })}
                      {visibleNamespaceOptions.length === 0 && <div className="hbdr-restore-namespace-menu-empty">No namespaces found</div>}
                      </div>
                      <div className="hbdr-restore-namespace-menu-actions">
                        <button type="button" onClick={() => setDraftNamespaceFilters([])}>Clear</button>
                        <button type="button" onClick={applyQuickNamespaceFilter}>Apply</button>
                      </div>
                    </motion.div>
                  </>
                )}
              </AnimatePresence>
            </div>
          </div>
        )}
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group hbdr-dr-action-group-managed hbdr-dr-action-group-run">
              <button disabled={selectedDisabled} title={!selectedRow ? 'Select one restore point' : activeRecoveryTaskForSelectedRow ? 'A recovery task is already running' : undefined} onClick={() => startRestoreAction('drill')} className="hbdr-dr-action-primary">Drill</button>
              <button disabled={selectedDisabled} title={!selectedRow ? 'Select one restore point' : activeRecoveryTaskForSelectedRow ? 'A recovery task is already running' : undefined} onClick={() => startRestoreAction('takeover')} className="hbdr-dr-action-danger">Takeover</button>
              <div className="relative">
                <button onClick={() => setRestoreBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={restoreBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {restoreBulkMenuOpen && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setRestoreBulkMenuOpen(false)} />
                      <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.96 }}
                        className="absolute right-0 top-11 z-40 w-52 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"
                      >
                        <button
                          disabled={selectedRestorePoints.length === 0}
                          onClick={() => {
                            setSelectedRestorePoints([]);
                            setActiveRestorePointId('');
                            setRestoreBulkMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                        >
                          <X size={14} className="text-slate-400" />Clear Selection
                        </button>
                        <button
                          disabled={selectedRestorePoints.length === 0}
                          onClick={openDeleteRestorePointDialog}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"
                        >
                          <Trash2 size={14} className="text-rose-500" />Delete Restore Points
                        </button>
                        <button
                          onClick={async () => {
                            setRestoreBulkMenuOpen(false);
                            try {
                              await load();
                              toast('Restore point list refreshed');
                            } catch {
                              toast('Failed to refresh restore points');
                            }
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 transition-colors hover:bg-slate-50"
                        >
                          <RefreshCw size={14} className="text-blue-500" />Refresh
                        </button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={restorePointQueryFields}
              tags={[]}
              activeTags={[]}
              setActiveTags={() => {}}
              filters={[
                { value: 'remote', label: 'Remote Snapshot', count: availableRows.filter(row => row.media === 'Remote Snapshot').length },
                { value: 'local', label: 'Local Snapshot', count: availableRows.filter(row => row.media === 'Local Snapshot').length },
                { value: 'manual', label: 'Manual', count: availableRows.filter(row => row.backupMode === 'Manual').length },
                { value: 'automatic', label: 'Automatic', count: availableRows.filter(row => row.backupMode === 'Automatic').length },
                { value: 'running', label: 'Recovery Running', count: availableRows.filter(row => isActiveTaskStatus(latestTaskForRestorePoint(tasks, row.id)?.status)).length },
                { value: 'failed', label: 'Recovery Failed', count: availableRows.filter(row => isFailedStatus(latestTaskForRestorePoint(tasks, row.id)?.status)).length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={restorePointToolbarColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={async () => {
                setSelectedRestorePoints([]);
                setActiveRestorePointId('');
                try {
                  await load();
                  toast('Restore point list refreshed');
                } catch {
                  toast('Failed to refresh restore points');
                }
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={restorePointTableColumns}
          data={filteredRows}
          getRowId={row => row.id}
          onRowClick={row => {
            setActiveRestorePointId(row.id);
            setSelectedRestorePoints(prev => prev.includes(row.id) ? prev : [...prev, row.id]);
          }}
          getRowClassName={row => selectedRestorePoints.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRestorePoints.length}
          emptyMessage={loading ? 'Loading restore points...' : query || activeFilters.length ? 'No restore points match the current search.' : 'No restore points available.'}
          className="hbdr-dr-main-table hbdr-restore-point-hyper-table"
        />
      </div>
      <AnimatePresence>
        {deleteDialog && (
          <motion.div
            className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/35 px-4 py-8 backdrop-blur-sm"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <motion.div
              initial={{ opacity: 0, y: 18, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 18, scale: 0.97 }}
              className="w-full max-w-xl overflow-hidden rounded-2xl border border-slate-100 bg-white shadow-2xl shadow-slate-900/20"
            >
              <div className="flex items-start gap-3 border-b border-slate-100 px-6 py-5">
                <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl ${deleteDialog.status === 'failed' ? 'bg-rose-50 text-rose-600' : deleteDialog.status === 'succeeded' ? 'bg-emerald-50 text-emerald-600' : 'bg-amber-50 text-amber-600'}`}>
                  {deleteDialog.status === 'succeeded' ? <CheckCircle2 size={20} /> : deleteDialog.status === 'failed' ? <AlertCircle size={20} /> : <Trash2 size={20} />}
                </span>
                <div className="min-w-0 flex-1">
                  <h4 className="text-base font-black text-slate-900">
                    {deleteDialog.status === 'succeeded' ? 'Restore Points Deleted' : deleteDialog.status === 'failed' ? 'Delete Failed' : 'Delete Restore Points'}
                  </h4>
                  <p className="mt-1 text-sm font-medium leading-5 text-slate-500">{deleteDialog.message}</p>
                </div>
                <button
                  type="button"
                  disabled={deleteDialog.status === 'submitting'}
                  onClick={() => setDeleteDialog(null)}
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-600 disabled:cursor-wait disabled:opacity-40"
                  aria-label="Close delete restore points dialog"
                >
                  <X size={17} />
                </button>
              </div>
              <div className="space-y-4 px-6 py-5">
                <div className="rounded-xl border border-slate-100 bg-slate-50/70">
                  <div className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
                    <span className="text-xs font-black uppercase text-slate-500">Selected Restore Points</span>
                    <span className="rounded-full bg-white px-2.5 py-1 text-[11px] font-black text-slate-500">{deleteDialog.rows.length}</span>
                  </div>
                  <div className="max-h-52 divide-y divide-slate-100 overflow-auto">
                    {deleteDialog.rows.map(row => (
                      <div key={row.id} className="grid grid-cols-[1fr_auto] gap-3 px-4 py-3">
                        <div className="min-w-0">
                          <p className="truncate text-sm font-black text-slate-800">{row.name}</p>
                          <p className="mt-0.5 truncate text-xs font-semibold text-slate-400">{row.veleroBackupName}</p>
                        </div>
                        <div className="text-right">
                          <p className="text-xs font-bold text-slate-500">{row.backupMode}</p>
                          <p className="mt-0.5 text-[11px] font-semibold text-slate-400">{row.time}</p>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                {deleteDialog.status === 'deleting' && (
                  <div className="rounded-xl border border-blue-100 bg-blue-50/60 px-4 py-3">
                    <div className="flex items-center gap-2 text-sm font-black text-blue-700">
                      <RefreshCw size={15} className="animate-spin" />
                      Velero deletion is running
                    </div>
                    <div className="mt-3 space-y-2">
                      {deleteDialog.taskIds.length === 0 ? (
                        <p className="text-xs font-semibold text-blue-600">Delete task has been submitted. Waiting for task status...</p>
                      ) : deleteDialog.taskIds.map(taskId => {
                        const task = tasks.find(item => item.id === taskId);
                        const progress = Math.max(0, Math.min(100, task?.progress || 0));
                        return (
                          <div key={taskId}>
                            <div className="mb-1 flex items-center justify-between text-[11px] font-bold text-blue-700">
                              <span>{task ? taskStatusLabel(task.status) : 'Waiting'}</span>
                              <span>{progress}%</span>
                            </div>
                            <div className="h-1.5 overflow-hidden rounded-full bg-white">
                              <div className="h-full rounded-full bg-blue-500 transition-all" style={{ width: `${progress}%` }} />
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
              <div className="flex items-center justify-end gap-3 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
                {deleteDialog.status === 'confirm' && (
                  <>
                    <button type="button" onClick={() => setDeleteDialog(null)} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-500 transition-colors hover:bg-white">Cancel</button>
                    <button type="button" onClick={submitRestorePointDelete} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-5 py-2 text-sm font-black text-white shadow-lg shadow-rose-100 transition-colors hover:bg-rose-700">
                      <Trash2 size={15} />Delete from Velero
                    </button>
                  </>
                )}
                {deleteDialog.status === 'submitting' && (
                  <button type="button" disabled className="inline-flex cursor-wait items-center gap-2 rounded-xl bg-slate-200 px-5 py-2 text-sm font-black text-slate-500">
                    <RefreshCw size={15} className="animate-spin" />Submitting
                  </button>
                )}
                {(deleteDialog.status === 'deleting' || deleteDialog.status === 'succeeded' || deleteDialog.status === 'failed') && (
                  <button type="button" onClick={() => setDeleteDialog(null)} className="rounded-xl bg-slate-900 px-5 py-2 text-sm font-black text-white transition-colors hover:bg-slate-700">
                    Close
                  </button>
                )}
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
      <AnimatePresence>
        {recoveryTaskDetail && (
          <ErrorDetailModalFrame
            title={`${taskDetailLabel(recoveryTaskDetail.task.type)} Task Details`}
            onClose={() => setRecoveryTaskDetail(null)}
          >
            {(() => {
              const failure = recoveryTaskDetail.failure || taskFailureSummary(recoveryTaskDetail.task, recoveryTaskDetailEvents);
              const details = taskFailureDetails(recoveryTaskDetail.task, recoveryTaskDetailEvents);
              return (
                <div className="hbdr-task-detail">
                  <TaskErrorDetailBlock failure={failure} details={details} />

                  <div className="hbdr-task-detail-section">
                    <div className="hbdr-task-detail-section-title">
                      <strong>Task events</strong>
                      <span>{recoveryTaskDetailEvents.length} records</span>
                    </div>
                    <div className="hbdr-task-detail-events">
                      {recoveryTaskDetailEvents.length > 0 ? recoveryTaskDetailEvents.map(event => {
                        const errors = eventRestoreResultErrors(event);
                        return (
                          <section key={event.id}>
                            <div>
                              <strong className={event.level === 'error' ? 'is-error' : ''}>{(event.reason || event.level || 'event').replace(/_/g, ' ')}</strong>
                              <span>{formatLocalDateTime(event.createdAt) || '-'}</span>
                            </div>
                            <p>{event.message}</p>
                            {errors.length > 0 && (
                              <ul>
                                {errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}
                              </ul>
                            )}
                          </section>
                        );
                      }) : (
                        <p className="hbdr-task-detail-empty">No task events recorded.</p>
                      )}
                    </div>
                  </div>

                  <details className="hbdr-task-detail-raw">
                    <summary>Technical payload</summary>
                    <pre>{JSON.stringify(recoveryTaskDetail.task.payload || {}, null, 2)}</pre>
                  </details>
                </div>
              );
            })()}
          </ErrorDetailModalFrame>
        )}
      </AnimatePresence>
      {restoreAction && (
        <RecoveryWizardModal
          open
          mode={restoreAction.mode}
          app={{ name: restoreAction.row.namespaceLabel, namespace: restoreAction.row.namespace, storage: restoreAction.row.storage, targetCluster: restoreAction.row.targetCluster }}
          profile={{ uid: restoreAction.row.uid }}
          currentClusterName={clusters.find(cluster => cluster.id === restoreAction.row.sourceClusterId)?.name || 'Source Cluster'}
          points={pointsForRow(restoreAction.row)}
          clusterOptions={clusterOptions}
          repositoryOptions={repositoryOptions}
          config={restoreAction.config}
          setConfig={updater => {
            setRestoreAction(prev => {
              if (!prev) return prev;
              const nextConfig = typeof updater === 'function' ? updater(prev.config) : updater;
              return { ...prev, config: nextConfig };
            });
          }}
          onClose={() => setRestoreAction(null)}
          onSubmit={async () => {
            const action = restoreAction;
            const targetNamespace = action.config.namespaceMode === 'original' ? action.row.namespace : action.config.targetNamespace;
            const targetCluster = clusters.find(cluster => cluster.name === action.config.targetCluster);
            setRestoreAction(null);
            toast(action.mode === 'drill' ? 'DR drill job submitted' : 'DR takeover job submitted');
            void (async () => {
              try {
                await apiPost<ApiTask>(`/api/v1/tasks/${action.mode}`, {
                clusterId: targetCluster?.id || action.row.targetClusterId,
                protectionPlanId: action.row.protectionPlanId,
                restorePointId: action.row.id,
                veleroBackupName: action.row.veleroBackupName,
                sourceNamespace: action.row.namespace,
                sourceNamespaces: action.row.namespaces,
                targetNamespace,
                targetMode: action.config.targetMode,
                restoreMode: action.config.restoreMode,
                artifactMode: action.config.artifactMode,
                conflictPolicy: action.config.conflictPolicy,
                originalNamespaceConfirmed: action.config.originalNamespaceConfirmed,
                includeClusterScoped: action.config.includeClusterScoped,
                useTransforms: action.config.useTransforms,
                transformPreset: action.config.transformPreset,
                storageProfileMode: action.config.storageProfileMode,
                alternateProfileId: action.config.alternateProfileId,
              });
              await load();
              } catch (error) {
                toast('Failed to submit recovery task: ' + (error instanceof Error ? error.message : 'unknown error'));
              }
            })();
          }}
        />
      )}
    </motion.div>
  );
}

function BackupRecoveryTaskPage({
  toast,
  workspaceCluster,
  clusterContext,
  initialTasks = [],
  initialRestorePoints = [],
  initialClusters = [],
  initialApps = [],
  initialStorageRepos = [],
}: {
  toast: (msg: string) => void;
  workspaceCluster?: Cluster | ApiCluster | null;
  clusterContext?: React.ReactNode;
  initialTasks?: ApiTask[];
  initialRestorePoints?: ApiRestorePoint[];
  initialClusters?: ApiCluster[];
  initialApps?: ApiApplication[];
  initialStorageRepos?: ApiStorageRepo[];
}) {
  type Row = {
    id: string;
    operation: string;
    taskType: string;
    namespace: string;
    cluster: string;
    repository: string;
    status: string;
    progress: number;
    restorePointState: 'available' | 'cleared' | 'not_created' | 'failed' | 'pending';
    restorePointLabel: string;
    restorePointName: string;
    createdAt: string;
    completedAt: string;
    task: ApiTask;
    point?: ApiRestorePoint;
  };

  const [tasks, setTasks] = useState<ApiTask[]>(initialTasks);
  const [restorePoints, setRestorePoints] = useState<ApiRestorePoint[]>(initialRestorePoints);
  const [clusters, setClusters] = useState<ApiCluster[]>(initialClusters);
  const [apps, setApps] = useState<ApiApplication[]>(initialApps);
  const [storageRepos, setStorageRepos] = useState<ApiStorageRepo[]>(initialStorageRepos);
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('namespace');
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState<string[]>(['namespace', 'repository', 'restorePoint', 'createdAt', 'completedAt']);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [selectedTaskEvents, setSelectedTaskEvents] = useState<ApiTaskEvent[]>([]);
  const [timeWindow, setTimeWindow] = useState<'24h' | '7d' | '30d' | 'all'>('7d');

  const load = async () => {
    const [taskRes, pointRes, clusterRes, appRes, storageRes] = await Promise.all([
      apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
      apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
      apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
      apiGet<ApiList<ApiApplication>>('/api/v1/applications'),
      apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
    ]);
    setTasks(listItems(taskRes));
    setRestorePoints(listItems(pointRes));
    setClusters(listItems(clusterRes));
    setApps(listItems(appRes));
    setStorageRepos(listItems(storageRes));
  };

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        if (!cancelled) await load();
      } catch {
        if (!cancelled) toast('Failed to refresh backup and recovery tasks');
      }
    };
    refresh();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  const pointsById = new Map(restorePoints.map(point => [point.id, point]));
  const pointsByBackupTaskId = new Map<string, ApiRestorePoint>();
  restorePoints.forEach(point => {
    const taskId = String(point.metadata?.backupTaskId || (point as any).backupTaskId || '');
    if (taskId) pointsByBackupTaskId.set(taskId, point);
  });
  const clustersById = new Map(clusters.map(cluster => [cluster.id, cluster]));
  const appsById = new Map(apps.map(app => [app.id, app]));
  const storageById = new Map(storageRepos.map(repo => [repo.id, repo]));
  const taskTypes = new Set(['backup', 'restore', 'drill', 'takeover', 'failover', 'failback']);

  const taskPoint = (task: ApiTask): ApiRestorePoint | undefined => {
    const pointByTask = pointsByBackupTaskId.get(task.id);
    if (pointByTask) return pointByTask;
    const ids = [
      task.restorePointId,
      String(task.payload?.restorePointId || ''),
      String(task.payload?.archivedRestorePointId || ''),
      String(task.payload?.pointId || ''),
    ].filter(Boolean);
    for (const id of ids) {
      const point = pointsById.get(id);
      if (point) return point;
    }
    const backupName = String(task.payload?.veleroBackupName || task.payload?.backupName || '');
    if (backupName) {
      const point = restorePoints.find(item => item.veleroBackupName === backupName);
      if (point) return point;
    }
    return undefined;
  };

  const namespaceForTask = (task: ApiTask, point?: ApiRestorePoint) => {
    const app = task.appId ? appsById.get(task.appId) : undefined;
    return String(task.payload?.sourceNamespace || point?.sourceNamespace || app?.namespace || '-');
  };

  const repositoryForTask = (task: ApiTask, point?: ApiRestorePoint) => {
    const repoId = point?.storageRepoId || String(task.payload?.storageRepoId || task.payload?.repositoryId || '');
    const veleroStorage = point?.metadata?.velero && typeof point.metadata.velero === 'object' ? String((point.metadata.velero as Record<string, any>).storageLocation || '') : '';
    return (repoId && storageById.get(repoId)?.name)
      || point?.backupStorageName
      || String(point?.metadata?.backupStorageName || '')
      || String(task.payload?.storageRepo || task.payload?.backupStorageName || task.payload?.storageLocation || task.payload?.repository || task.payload?.name || '')
      || veleroStorage
      || 'Unknown';
  };

  const restorePointStateForTask = (task: ApiTask, point?: ApiRestorePoint): Row['restorePointState'] => {
    if (point) return restorePointListStatus(point) === 'available' ? 'available' : 'cleared';
    if (isFailedStatus(task.status)) return 'failed';
    if (isActiveTaskStatus(task.status)) return 'pending';
    if (isSucceededStatus(task.status) && task.type === 'backup') return 'cleared';
    if (isSucceededStatus(task.status) && ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(task.type)) return task.restorePointId || task.payload?.restorePointId ? 'cleared' : 'not_created';
    return 'not_created';
  };

  const operationLabel = (type: string) => {
    if (type === 'backup') return 'Backup';
    if (type === 'drill') return 'DR Drill';
    if (type === 'takeover' || type === 'failover') return 'Takeover';
    if (type === 'failback') return 'Failback';
    return 'Restore';
  };

  const rows: Row[] = tasks
    .filter(task => taskTypes.has(task.type))
    .map(task => {
      const point = taskPoint(task);
      const pointState = restorePointStateForTask(task, point);
      const cluster = clustersById.get(task.clusterId);
      return {
        id: task.id,
        operation: operationLabel(task.type),
        taskType: task.type,
        namespace: namespaceForTask(task, point),
        cluster: cluster?.name || task.clusterId || '-',
        repository: repositoryForTask(task, point),
        status: task.status || 'unknown',
        progress: Number(task.progress || 0),
        restorePointState: pointState,
        restorePointLabel: point
          ? restorePointDisplayLabel(point)
          : pointState === 'cleared' ? 'Cleared' : pointState === 'failed' ? 'Not created' : pointState === 'pending' ? 'Pending' : '-',
        restorePointName: point?.veleroBackupName || String(task.payload?.veleroBackupName || task.payload?.backupName || '') || '-',
        createdAt: formatLocalDateTime(task.createdAt),
        completedAt: formatLocalDateTime(task.completedAt),
        task,
        point,
      };
    })
    .sort((a, b) => (b.task.createdAt || '').localeCompare(a.task.createdAt || ''));
  const activeLedgerTaskIds = rows.filter(row => isActiveTaskStatus(row.status)).map(row => row.id);
  const activeLedgerTaskKey = activeLedgerTaskIds.sort().join('|');

  useEffect(() => {
    const ids = activeLedgerTaskKey ? activeLedgerTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const idSet = new Set(ids);
    const pollActiveTasks = async () => {
      try {
        const [taskRes, pointRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks'),
          apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points'),
        ]);
        if (cancelled) return;
        const latestTasks = new Map(listItems(taskRes).map(task => [task.id, task]));
        setTasks(prev => prev.map(task => {
          if (!idSet.has(task.id)) return task;
          const next = latestTasks.get(task.id);
          return next ? { ...task, ...next } : task;
        }));
        setRestorePoints(listItems(pointRes));
      } catch {
        // Keep the current list stable if a status poll fails.
      }
    };
    pollActiveTasks();
    const timer = window.setInterval(pollActiveTasks, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeLedgerTaskKey]);

  const timeWindowOptions = [
    { value: '24h', label: 'Last 24 hours' },
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
    { value: 'all', label: 'All history' },
  ] as const;

  const timeWindowLabel = timeWindowOptions.find(option => option.value === timeWindow)?.label || 'Last 7 days';
  const rowsInTimeWindow = rows.filter(row => {
    if (timeWindow === 'all') return true;
    if (isActiveTaskStatus(row.status) && !row.task.createdAt) return true;
    const created = row.task.createdAt ? new Date(row.task.createdAt).getTime() : 0;
    if (!created || Number.isNaN(created)) return false;
    const spanMs = timeWindow === '24h' ? 24 * 60 * 60 * 1000 : timeWindow === '7d' ? 7 * 24 * 60 * 60 * 1000 : 30 * 24 * 60 * 60 * 1000;
    return created >= Date.now() - spanMs;
  });

  const filteredRows = rowsInTimeWindow.filter(row => {
    const q = query.trim().toLowerCase();
    const searchableValues: Record<string, string> = {
      operation: row.operation,
      namespace: row.namespace,
      cluster: row.cluster,
      repository: row.repository,
      restorePoint: row.restorePointLabel,
      restorePointName: row.restorePointName,
      createdAt: row.createdAt,
      completedAt: row.completedAt,
      status: row.status,
    };
    const matchesQuery = !q || (searchableValues[queryField] || Object.values(searchableValues).join(' ')).toLowerCase().includes(q);
    const matchesFilters = activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => searchableValues[field] || '');
      if (filter === 'backup') return row.taskType === 'backup';
      if (filter === 'recovery') return ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType);
      if (filter === 'running') return isActiveTaskStatus(row.status);
      if (filter === 'succeeded') return isSucceededStatus(row.status);
      if (filter === 'failed') return isFailedStatus(row.status);
      if (filter === 'cleared') return row.restorePointState === 'cleared';
      return true;
    });
    return matchesQuery && matchesFilters;
  });

  const selectedRow = selectedTaskId ? rows.find(row => row.id === selectedTaskId) || null : null;
  useEffect(() => {
    if (!selectedTaskId) {
      setSelectedTaskEvents([]);
      return;
    }
    let cancelled = false;
    apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${selectedTaskId}/events`)
      .then(result => {
        if (!cancelled) setSelectedTaskEvents(listItems(result));
      })
      .catch(() => {
        if (!cancelled) setSelectedTaskEvents([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedTaskId]);

  const summary = {
    total: rowsInTimeWindow.length,
    backup: rowsInTimeWindow.filter(row => row.taskType === 'backup').length,
    recovery: rowsInTimeWindow.filter(row => ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType)).length,
    running: rowsInTimeWindow.filter(row => isActiveTaskStatus(row.status)).length,
    succeeded: rowsInTimeWindow.filter(row => isSucceededStatus(row.status)).length,
    failed: rowsInTimeWindow.filter(row => isFailedStatus(row.status)).length,
  };
  const taskToolbarColumns = [
    { value: 'namespace', label: 'Namespace' },
    { value: 'cluster', label: 'Cluster' },
    { value: 'repository', label: 'Repository' },
    { value: 'restorePoint', label: 'Restore Point' },
    { value: 'createdAt', label: 'Started' },
    { value: 'completedAt', label: 'Completed' },
  ];
  const taskQueryFields = listToolbarQueryFields([
    { value: 'operation', label: 'Task Type' },
    { value: 'status', label: 'Task Status' },
  ], taskToolbarColumns, visibleColumns);

  const renderTaskStatus = (row: Row) => {
    const tone = isSucceededStatus(row.status) ? 'ok' : isFailedStatus(row.status) ? 'fail' : isActiveTaskStatus(row.status) ? 'running' : 'muted';
    const title = isFailedStatus(row.status)
      ? (row.task.errorMessage || row.task.errorCode || 'Task failed')
      : taskStatusLabel(row.status);
    return <span className={`hbdr-task-ledger-status is-${tone}`} title={title}>{taskStatusLabel(row.status)}</span>;
  };

  const renderPointState = (row: Row) => {
    const label = row.restorePointState === 'available' ? 'Available'
      : row.restorePointState === 'cleared' ? 'Cleared'
        : row.restorePointState === 'failed' ? 'Not created'
          : row.restorePointState === 'pending' ? 'Pending'
            : '-';
    const primary = row.point ? row.restorePointLabel : label;
    const secondary = row.point ? label : row.restorePointLabel;
    return <span className={`hbdr-task-ledger-point is-${row.restorePointState}`}><strong>{primary}</strong><em>{secondary}</em></span>;
  };

  const renderTaskType = (row: Row) => {
    const Icon = row.taskType === 'backup' ? Archive
      : row.taskType === 'drill' ? Play
        : row.taskType === 'takeover' || row.taskType === 'failover' ? Zap
          : row.taskType === 'failback' ? ArrowDown
            : RefreshCw;
    return (
      <span className={`hbdr-task-ledger-op is-${row.taskType}`}>
        <i aria-hidden="true"><Icon size={14} /></i>
        <strong>{row.operation}</strong>
      </span>
    );
  };

  const progressLabel = (row: Row) => isActiveTaskStatus(row.status) ? `${formatPercent(row.progress || 0)}%` : isSucceededStatus(row.status) ? '100.00%' : row.progress ? `${formatPercent(row.progress)}%` : '-';
  const taskDurationLabel = (row: Row) => {
    const start = row.task.createdAt ? new Date(row.task.createdAt).getTime() : 0;
    const end = row.task.completedAt ? new Date(row.task.completedAt).getTime() : Date.now();
    if (!start || Number.isNaN(start) || Number.isNaN(end) || end < start) return '-';
    const seconds = Math.max(1, Math.round((end - start) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    return `${hours}h ${minutes % 60}m`;
  };

  const columns = useMemo<HyperTableColumn<Row>[]>(() => [
    { id: 'operation', header: 'Task Type', accessorFn: row => row.operation, size: 145, minSize: 125, cell: info => renderTaskType(info.row.original), meta: { title: row => row.id } },
    ...(visibleColumns.includes('namespace') ? [{ id: 'namespace', header: 'Namespace', accessorFn: (row: Row) => row.namespace, size: 190, minSize: 150, maxSize: 320, cell: (info: any) => <span className="font-bold text-slate-800">{info.row.original.namespace}</span>, meta: { title: (row: Row) => row.namespace } }] : []),
    ...(visibleColumns.includes('cluster') ? [{ id: 'cluster', header: 'Cluster', accessorFn: (row: Row) => row.cluster, size: 160, minSize: 130, maxSize: 260, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.cluster}</span>, meta: { title: (row: Row) => row.cluster } }] : []),
    ...(visibleColumns.includes('repository') ? [{ id: 'repository', header: 'Repository', accessorFn: (row: Row) => row.repository, size: 145, minSize: 120, maxSize: 240, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.repository}</span>, meta: { title: (row: Row) => row.repository } }] : []),
    { id: 'status', header: 'Task Status', accessorFn: row => taskStatusLabel(row.status), size: 130, minSize: 110, cell: info => renderTaskStatus(info.row.original), meta: { title: row => isFailedStatus(row.status) ? (row.task.errorMessage || row.task.errorCode || 'Task failed') : taskStatusLabel(row.status) } },
    ...(visibleColumns.includes('restorePoint') ? [{ id: 'restorePoint', header: 'Restore Point', accessorFn: (row: Row) => row.restorePointLabel, size: 220, minSize: 170, maxSize: 360, cell: (info: any) => renderPointState(info.row.original), meta: { title: (row: Row) => row.point ? `${row.restorePointLabel} / ${row.restorePointName}` : row.restorePointLabel } }] : []),
    ...(visibleColumns.includes('createdAt') ? [{ id: 'createdAt', header: 'Started', accessorFn: (row: Row) => row.createdAt, size: 170, minSize: 140, maxSize: 230, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.createdAt || '-'}</span>, meta: { title: (row: Row) => row.createdAt } }] : []),
    ...(visibleColumns.includes('completedAt') ? [{ id: 'completedAt', header: 'Completed', accessorFn: (row: Row) => row.completedAt, size: 170, minSize: 140, maxSize: 230, cell: (info: any) => <span className="text-xs font-semibold text-slate-500">{info.row.original.completedAt || '-'}</span>, meta: { title: (row: Row) => row.completedAt } }] : []),
  ], [visibleColumns]);

  return (
    <motion.div key="dr-tasks" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
      <div className="hbdr-app-workspace-bar">
        <div className="min-w-0">
          <h3 className="hbdr-app-workspace-title">Backup & Recovery Tasks</h3>
          <p className="hbdr-app-workspace-desc">Audit backup, drill, takeover, and restore tasks across outcomes. Default view shows recent tasks.</p>
        </div>
        {clusterContext && <div className="hbdr-app-workspace-cluster">{clusterContext}</div>}
      </div>
      <div className="hbdr-task-ledger-range">
        <div className="hbdr-task-ledger-range-options">
          {timeWindowOptions.map(option => (
            <button key={option.value} type="button" className={timeWindow === option.value ? 'is-active' : ''} onClick={() => setTimeWindow(option.value)}>
              {option.label}
            </button>
          ))}
        </div>
      </div>
      <div className="hbdr-task-ledger-summary">
        <section><strong>{summary.total}</strong><span>Total</span></section>
        <section><strong>{summary.backup}</strong><span>Backups</span></section>
        <section><strong>{summary.recovery}</strong><span>Recovery</span></section>
        <section><strong>{summary.running}</strong><span>Running</span></section>
        <section><strong>{summary.succeeded}</strong><span>Succeeded</span></section>
        <section><strong>{summary.failed}</strong><span>Failed</span></section>
      </div>
      <div className="hbdr-dr-table-card hbdr-task-ledger-table hbdr-task-ledger-table-clean">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={taskQueryFields}
              tags={[]}
              activeTags={[]}
              setActiveTags={() => {}}
              filters={[
                { value: 'backup', label: 'Backup', count: rowsInTimeWindow.filter(row => row.taskType === 'backup').length },
                { value: 'recovery', label: 'Recovery', count: rowsInTimeWindow.filter(row => ['restore', 'drill', 'takeover', 'failover', 'failback'].includes(row.taskType)).length },
                { value: 'running', label: 'Running', count: rowsInTimeWindow.filter(row => isActiveTaskStatus(row.status)).length },
                { value: 'succeeded', label: 'Succeeded', count: rowsInTimeWindow.filter(row => isSucceededStatus(row.status)).length },
                { value: 'failed', label: 'Failed', count: rowsInTimeWindow.filter(row => isFailedStatus(row.status)).length },
                { value: 'cleared', label: 'Restore Point Cleared', count: rowsInTimeWindow.filter(row => row.restorePointState === 'cleared').length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={taskToolbarColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={async () => {
                try {
                  await load();
                  toast('Backup and recovery tasks refreshed');
                } catch {
                  toast('Failed to refresh backup and recovery tasks');
                }
              }}
            />
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={columns}
          data={filteredRows}
          getRowId={row => row.id}
          onRowClick={row => setSelectedTaskId(row.id)}
          getRowClassName={row => selectedTaskId === row.id ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRow ? 1 : 0}
          resetPageOnDataChange={false}
          emptyMessage={query || activeFilters.length ? 'No tasks match the current search.' : `No backup or recovery tasks in ${timeWindowLabel.toLowerCase()}.`}
          className="hbdr-dr-main-table"
        />
      </div>
      <AnimatePresence>
        {selectedRow && (
          <ModalFrame title="Task Details" subtitle={`${selectedRow.operation} / ${selectedRow.namespace}`} icon={<History size={20} />} maxWidthClass="max-w-3xl" onClose={() => setSelectedTaskId(null)}>
            <div className="hbdr-task-detail">
              <div className="hbdr-task-detail-hero">
                <div>
                  <span>{selectedRow.operation}</span>
                  <strong>{selectedRow.namespace}</strong>
                  <em>{selectedRow.id}</em>
                </div>
                <div className="hbdr-task-detail-state">
                  {renderTaskStatus(selectedRow)}
                  {renderPointState(selectedRow)}
                </div>
              </div>

              <div className="hbdr-task-detail-grid">
                <Info label="Cluster" value={selectedRow.cluster} />
                <Info label="Repository" value={selectedRow.repository} />
                <Info label="Progress" value={progressLabel(selectedRow)} />
                <Info label="Duration" value={taskDurationLabel(selectedRow)} />
                <Info label="Started" value={selectedRow.createdAt || '-'} />
                <Info label="Completed" value={selectedRow.completedAt || '-'} />
              </div>

              {(selectedRow.task.errorCode || selectedRow.task.errorMessage || taskFailureDetails(selectedRow.task, selectedTaskEvents).length > 0) && (
                <TaskErrorDetailBlock
                  failure={taskFailureSummary(selectedRow.task, selectedTaskEvents)}
                  details={taskFailureDetails(selectedRow.task, selectedTaskEvents)}
                />
              )}

              <div className="hbdr-task-detail-section">
                <div className="hbdr-task-detail-section-title">
                  <strong>Restore point relationship</strong>
                  <span>{selectedRow.restorePointState === 'available' ? 'Usable recovery artifact' : selectedRow.restorePointState === 'cleared' ? 'Task record retained after artifact cleanup' : selectedRow.restorePointState === 'pending' ? 'Waiting for task completion' : 'No recovery artifact was created'}</span>
                </div>
                <div className="hbdr-task-detail-list">
                  <span>Restore point</span><strong>{selectedRow.point ? selectedRow.restorePointLabel : selectedRow.restorePointState}</strong>
                  <span>Velero backup</span><strong>{selectedRow.restorePointName}</strong>
                  <span>Point type</span><strong>{selectedRow.point?.pointType || '-'}</strong>
                  <span>Point completed</span><strong>{formatLocalDateTime(selectedRow.point?.completedAt) || '-'}</strong>
                </div>
              </div>

              <div className="hbdr-task-detail-section">
                <div className="hbdr-task-detail-section-title">
                  <strong>Task events</strong>
                  <span>{selectedTaskEvents.length} records</span>
                </div>
                <div className="hbdr-task-detail-events">
                  {selectedTaskEvents.length > 0 ? selectedTaskEvents.map(event => {
                    const errors = eventRestoreResultErrors(event);
                    return (
                      <section key={event.id}>
                        <div>
                          <strong className={event.level === 'error' ? 'is-error' : ''}>{(event.reason || event.level || 'event').replace(/_/g, ' ')}</strong>
                          <span>{formatLocalDateTime(event.createdAt) || '-'}</span>
                        </div>
                        <p>{event.message}</p>
                        {errors.length > 0 && (
                          <ul>
                            {errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}
                          </ul>
                        )}
                      </section>
                    );
                  }) : (
                    <p className="hbdr-task-detail-empty">No task events recorded.</p>
                  )}
                </div>
              </div>

              <details className="hbdr-task-detail-raw">
                <summary>Technical payload</summary>
                <pre>{JSON.stringify(selectedRow.task.payload || {}, null, 2)}</pre>
              </details>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function RestorePointPage({ openDr, toast }: { openDr: () => void; toast: (msg: string) => void }) {
  const rows: Array<{ name: string; namespace: string; uid: string; pointId: string; time: string; media: string; storage: string; targetCluster: string }> = [];
  const clusterOptions: Array<{ id: string; name: string; region: string; version: string; isCurrent: boolean }> = [];
  const repositoryOptions: Array<{ id: string; name: string; type: string; endpoint: string; bucket: string }> = [];
  const [selectedRestorePoint, setSelectedRestorePoint] = useState<string | null>(null);
  const [restoreAction, setRestoreAction] = useState<{ mode: 'drill' | 'takeover'; row: typeof rows[number]; config: RecoveryWizardConfig } | null>(null);
  const [recoveryTasks, setRecoveryTasks] = useState<Record<string, RecoveryTaskState>>({});
  const selectedRow = rows.find(row => row.name === selectedRestorePoint) || null;
  const selectedDisabled = !selectedRow;
  const pointsForRow = (row: typeof rows[number]) => [
    { id: row.pointId, title: 'Policy schedule', time: row.time, type: row.media, status: 'Available' },
  ];
  const buildConfig = (mode: 'drill' | 'takeover', row: typeof rows[number]): RecoveryWizardConfig => {
    const currentClusterName = 'Production Cluster 01';
    const targetCluster = row.targetCluster || currentClusterName;
    const targetMode = targetCluster === currentClusterName
      ? mode === 'drill' ? 'sandbox' : 'inPlace'
      : 'crossCluster';
    return {
      pointId: row.pointId,
      sourceType: row.media.toLowerCase().includes('local') ? 'snapshot' : 'export',
      targetMode,
      targetCluster,
      namespaceMode: mode === 'takeover' ? 'original' : 'generated',
      targetNamespace: mode === 'takeover' ? row.name : `${row.name}-drill`,
      restoreMode: 'full',
      artifactMode: 'all',
      conflictPolicy: mode === 'takeover' ? 'replace' : 'skip',
      originalNamespaceConfirmed: false,
      includeClusterScoped: false,
      useTransforms: targetMode !== 'inPlace',
      transformPreset: targetMode === 'crossCluster' ? 'migration' : targetMode === 'sandbox' ? 'drill' : 'none',
      storageProfileMode: 'original',
      alternateProfileId: '',
      preflightChecks: true,
      autoStartValidation: mode === 'drill',
      notes: mode === 'drill'
        ? 'Validate service startup, storage attachment, and namespace isolation after recovery.'
        : 'Confirm traffic cutover and production freeze before takeover.',
    };
  };
  const startRestoreAction = (mode: 'drill' | 'takeover') => {
    if (!selectedRow) {
      toast('Select a restore point first');
      return;
    }
    setRestoreAction({ mode, row: selectedRow, config: buildConfig(mode, selectedRow) });
  };
  const renderRestorePageTask = (row: typeof rows[number]) => {
    const task = recoveryTasks[row.name];
    if (!task) return <span className="hbdr-dr-task-neutral">No recovery task</span>;
    const label = task.mode === 'drill' ? 'Drill' : 'Takeover';
    if (task.status === 'running') {
      const progress = Math.max(0, Math.min(100, task.progress));
      return (
        <span className="hbdr-dr-progress-cell hbdr-recovery-task-progress is-syncing">
          <em className="hbdr-sync-label">{label} running {formatPercent(progress)}%</em>
          <i><b style={{ width: `${progress}%` }} /></i>
          <small>{task.targetNamespace}</small>
        </span>
      );
    }
    if (task.status === 'completed') {
      return (
        <span className="hbdr-recovery-task-complete">
          <strong>{label} complete</strong>
          <em>{task.targetNamespace}</em>
        </span>
      );
    }
    return <TaskErrorStatus code="RECOVERY_TASK_FAILED" title={`${label} failed`} description={task.message} detail={task.message} />;
  };
  const restorePointTableColumns = useMemo<HyperTableColumn<typeof rows[number]>[]>(() => [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedRestorePoint === info.row.original.name}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedRestorePoint(info.row.original.name)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Namespace',
      accessorFn: row => row.name,
      size: 270,
      minSize: 190,
      maxSize: 520,
      cell: info => <div><p className="text-sm font-black text-slate-900">{info.row.original.name}</p><p className="text-xs text-slate-400">{info.row.original.pointId}</p></div>,
      meta: { title: row => `${row.name} / ${row.pointId}` },
    },
    { id: 'time', header: 'Time', accessorFn: row => row.time, size: 180, minSize: 150, maxSize: 260, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.time}</span>, meta: { title: row => row.time } },
    { id: 'media', header: 'Media', accessorFn: row => row.media, size: 160, minSize: 130, maxSize: 240, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.media}</span>, meta: { title: row => row.media } },
    { id: 'status', header: 'Status', accessorFn: () => 'Available', size: 120, minSize: 100, cell: () => <span className="text-xs font-bold text-emerald-600">Available</span>, meta: { title: () => 'Available' } },
    { id: 'task', header: 'Recovery Task', accessorFn: row => recoveryTasks[row.name]?.status || 'No recovery task', size: 210, minSize: 170, maxSize: 360, cell: info => renderRestorePageTask(info.row.original) },
  ], [selectedRestorePoint, recoveryTasks]);

  useEffect(() => {
    const running = Object.keys(recoveryTasks).some(name => recoveryTasks[name].status === 'running');
    if (!running) return;
    const timer = window.setInterval(() => {
      setRecoveryTasks(prev => {
        const next = { ...prev };
        Object.keys(next).forEach(name => {
          const task = next[name];
          if (task.status !== 'running') return;
          const progress = Math.min(100, task.progress + 10 + Math.floor(Math.random() * 8));
          next[name] = progress >= 100
            ? { ...task, status: 'completed', progress: 100, message: `${task.mode === 'drill' ? 'Drill' : 'Takeover'} completed` }
            : { ...task, progress };
        });
        return next;
      });
    }, 900);
    return () => window.clearInterval(timer);
  }, [recoveryTasks]);

  return (
    <motion.div key="restore" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Restore Points" desc="View, drill, and take over restore points." action="Refresh Restore Points" onAction={() => toast('Restore point list refreshed')} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div>
            <p className="text-sm font-black text-slate-900">Restore Point List</p>
            <p className="mt-1 text-xs font-medium text-slate-400">{selectedRow ? `Selected ${selectedRow.name} / ${selectedRow.pointId}` : 'Select a restore point before drill or takeover'}</p>
          </div>
          <div className="flex gap-2">
            <button disabled={selectedDisabled} onClick={() => startRestoreAction('drill')} className="hbdr-dr-action-primary">Drill</button>
            <button disabled={selectedDisabled} onClick={() => startRestoreAction('takeover')} className="hbdr-dr-action-danger">Takeover</button>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={restorePointTableColumns}
          data={rows}
          getRowId={row => row.name}
          onRowClick={row => setSelectedRestorePoint(row.name)}
          getRowClassName={row => selectedRestorePoint === row.name ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No restore points available."
          className="hbdr-restore-point-hyper-table"
        />
      </div>
      {restoreAction && (
        <RecoveryWizardModal
          open
          mode={restoreAction.mode}
          app={{
            name: restoreAction.row.name,
            namespace: restoreAction.row.namespace,
            storage: restoreAction.row.storage,
            targetCluster: restoreAction.row.targetCluster,
          }}
          profile={{ uid: restoreAction.row.uid }}
          currentClusterName="Production Cluster 01"
          points={pointsForRow(restoreAction.row)}
          clusterOptions={clusterOptions}
          repositoryOptions={repositoryOptions}
          config={restoreAction.config}
          setConfig={updater => {
            setRestoreAction(prev => {
              if (!prev) return prev;
              const nextConfig = typeof updater === 'function' ? updater(prev.config) : updater;
              return { ...prev, config: nextConfig };
            });
          }}
          onClose={() => setRestoreAction(null)}
          onSubmit={() => {
            const targetNamespace = restoreAction.config.namespaceMode === 'original'
              ? restoreAction.row.name
              : restoreAction.config.targetNamespace;
            setRecoveryTasks(prev => ({
              ...prev,
              [restoreAction.row.name]: {
                mode: restoreAction.mode,
                status: 'running',
                progress: 6,
                targetCluster: restoreAction.config.targetCluster,
                targetNamespace,
                pointId: restoreAction.config.pointId,
                message: `Recovery point ${restoreAction.row.time}`,
              },
            }));
            toast(restoreAction.mode === 'drill' ? 'DR drill job submitted' : 'DR takeover job submitted');
            setRestoreAction(null);
          }}
        />
      )}
    </motion.div>
  );
}

function FailbackPage({ toast }: { toast: (msg: string) => void }) {
  const [selectedApp, setSelectedApp] = useState<string | null>(null);
  const rows: Array<{ name: string; source: string; target: string; point: string; status: string }> = [];
  const failbackColumns: HyperTableColumn<typeof rows[number]>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedApp === info.row.original.name}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedApp(info.row.original.name)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Application',
      accessorFn: row => row.name,
      size: 270,
      minSize: 200,
      maxSize: 520,
      cell: info => <div><p className="text-sm font-black text-slate-900">{info.row.original.name}</p><p className="text-xs text-slate-400">Application-level DR runtime</p></div>,
      meta: { title: row => row.name },
    },
    { id: 'source', header: 'Source', accessorFn: row => row.source, size: 190, minSize: 150, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.source}</span>, meta: { title: row => row.source } },
    { id: 'target', header: 'Target', accessorFn: row => row.target, size: 210, minSize: 160, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.target}</span>, meta: { title: row => row.target } },
    { id: 'point', header: 'Restore Point', accessorFn: row => row.point, size: 170, minSize: 140, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.point}</span>, meta: { title: row => row.point } },
    { id: 'status', header: 'Status', accessorFn: row => row.status, size: 150, minSize: 120, cell: info => <span className="w-fit rounded-full border border-blue-100 bg-blue-50 px-2 py-1 text-[10px] font-black text-blue-700">{info.row.original.status}</span>, meta: { title: row => row.status } },
  ];

  return (
    <motion.div key="failback" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Failback" desc="Fail back taken-over applications to the production cluster." action="Refresh Status" onAction={() => toast('Failback status refreshed')} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div>
            <p className="text-sm font-black text-slate-900">Failback Applications</p>
            <p className="mt-1 text-xs font-medium text-slate-400">{selectedApp ? `Selected ${selectedApp}` : 'Select a taken-over application'}</p>
          </div>
          <button disabled={!selectedApp} onClick={() => toast(`${selectedApp} submitted failback precheck`)} className="hbdr-dr-action-primary">Start Failback Precheck</button>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={failbackColumns}
          data={rows}
          getRowId={row => row.name}
          onRowClick={row => setSelectedApp(row.name)}
          getRowClassName={row => selectedApp === row.name ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No failback applications available."
        />
      </div>
    </motion.div>
  );
}

function RealOperationsPage() {
  const [selectedJob, setSelectedJob] = useState<string | null>(null);
  const [tasks, setTasks] = useState<ApiTask[]>([]);
  const [taskEvents, setTaskEvents] = useState<ApiTaskEvent[]>([]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const response = await apiGet<ApiList<ApiTask>>('/api/v1/tasks');
        if (!cancelled) setTasks(listItems(response));
      } catch {
        // Keep the current task list visible if one refresh fails.
      }
    };
    load();
    return () => {
      cancelled = true;
    };
  }, []);

  const selectedTask = tasks.find(task => task.id === selectedJob) || null;

  useEffect(() => {
    if (!selectedJob) {
      setTaskEvents([]);
      return;
    }
    let cancelled = false;
    const loadEvents = async () => {
      try {
        const response = await apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${selectedJob}/events`);
        if (!cancelled) setTaskEvents(listItems(response));
      } catch {
        if (!cancelled) setTaskEvents([]);
      }
    };
    loadEvents();
    const timer = selectedTask && isActiveTaskStatus(selectedTask.status) ? window.setInterval(loadEvents, 5000) : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [selectedJob, selectedTask?.status]);

  const statusLabel = (status: string) => {
    if (status === 'succeeded') return 'Success';
    if (status === 'failed') return 'Alert';
    if (['accepted', 'dispatched', 'running', 'queued'].includes(status)) return 'Running';
    return status || 'Unknown';
  };
  const jobs = tasks.map(task => ({
    id: task.id,
    type: task.type === 'backup' ? 'Backup' : task.type === 'drill' ? 'DR Drill' : task.type === 'takeover' ? 'Takeover' : task.type === 'restore' ? 'Restore' : task.type,
    object: String(task.payload?.sourceNamespace || task.appId || task.restorePointId || task.clusterId),
    status: statusLabel(task.status),
    user: 'system',
    time: formatLocalDateTime(task.createdAt),
  }));
  const operationColumns: HyperTableColumn<typeof jobs[number]>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedJob === info.row.original.id}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedJob(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    { id: 'type', header: 'Type', accessorFn: job => job.type, size: 150, minSize: 120, cell: info => <span className="font-black text-slate-900">{info.row.original.type}</span>, meta: { title: job => job.type } },
    { id: 'object', header: 'Object', accessorFn: job => job.object, size: 280, minSize: 180, maxSize: 520, cell: info => info.row.original.object, meta: { title: job => job.object } },
    { id: 'status', header: 'Status', accessorFn: job => job.status, size: 130, minSize: 110, cell: info => <span className={info.row.original.status === 'Success' ? 'text-emerald-600' : info.row.original.status === 'Alert' ? 'text-amber-600' : 'text-blue-600'}>{info.row.original.status}</span>, meta: { title: job => job.status } },
    { id: 'user', header: 'User', accessorFn: job => job.user, size: 150, minSize: 120, cell: info => info.row.original.user, meta: { title: job => job.user } },
    { id: 'time', header: 'Time', accessorFn: job => job.time, size: 190, minSize: 150, maxSize: 280, cell: info => info.row.original.time, meta: { title: job => job.time } },
  ];

  return (
    <motion.div key="operations" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="History" desc="Backup, restore, and takeover audit." action="Export Audit" onAction={() => setSelectedJob(null)} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div>
            <p className="text-sm font-black text-slate-900">Task Audit</p>
            <p className="mt-1 text-xs font-medium text-slate-400">{selectedJob ? `Selected ${selectedJob}` : 'Select a job to view execution details'}</p>
          </div>
          <button disabled={!selectedJob} onClick={() => setSelectedJob(null)} className="hbdr-dr-action-primary hbdr-dr-action-ghost">Clear Selection</button>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={operationColumns}
          data={jobs}
          getRowId={row => row.id}
          onRowClick={row => setSelectedJob(row.id)}
          getRowClassName={row => selectedJob === row.id ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No task audit records available."
        />
      </div>
      <AnimatePresence>
        {selectedTask && (
          <ModalFrame title="Task Details" onClose={() => setSelectedJob(null)}>
            <div className="space-y-4">
              <div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">{selectedTask.type}</p>
                    <p className="mt-1 break-all font-mono text-xs font-bold text-slate-700">{selectedTask.id}</p>
                  </div>
                  <span className={`text-sm font-black ${taskStatusClass(selectedTask.status)}`}>{taskStatusLabel(selectedTask.status)}</span>
                </div>
                <div className="mt-4 h-2 overflow-hidden rounded-full bg-white">
                  <div className={`h-full rounded-full ${selectedTask.status === 'failed' ? 'bg-rose-500' : selectedTask.status === 'succeeded' ? 'bg-emerald-500' : 'bg-blue-500'}`} style={{ width: `${Math.max(4, selectedTask.progress || 0)}%` }} />
                </div>
                <div className="mt-4 grid grid-cols-2 gap-3 text-xs text-slate-500">
                  <div><span className="font-semibold text-slate-400">Cluster</span><p className="mt-1 break-all font-mono text-slate-700">{selectedTask.clusterId}</p></div>
                  <div><span className="font-semibold text-slate-400">Command</span><p className="mt-1 break-all font-mono text-slate-700">{selectedTask.commandId || '-'}</p></div>
                  <div><span className="font-semibold text-slate-400">Created</span><p className="mt-1 font-semibold text-slate-700">{formatDateTime(selectedTask.createdAt)}</p></div>
                  <div><span className="font-semibold text-slate-400">Completed</span><p className="mt-1 font-semibold text-slate-700">{formatDateTime(selectedTask.completedAt)}</p></div>
                </div>
              </div>
              {selectedTask.status === 'failed' && (
                <div className="rounded-xl border border-rose-100 bg-rose-50 p-4 text-sm text-rose-700">
                  <p className="font-bold text-rose-900">{selectedTask.errorCode || 'TASK_FAILED'}</p>
                  {taskFailureDetails(selectedTask, taskEvents).map((detail, index) => (
                    <p key={`${index}-${detail}`} className="mt-1 whitespace-pre-wrap break-words">{detail}</p>
                  ))}
                  {taskFailureDetails(selectedTask, taskEvents).length === 0 && <p className="mt-1">Task failed.</p>}
                </div>
              )}
              <div className="max-h-72 overflow-y-auto rounded-xl border border-slate-100">
                {taskEvents.length > 0 ? taskEvents.map(event => (
                  <div key={event.id} className="border-b border-slate-100 px-4 py-3 text-xs last:border-b-0">
                    <div className="flex items-center justify-between gap-3">
                      <span className={`font-bold ${event.level === 'error' ? 'text-rose-600' : 'text-slate-700'}`}>{event.reason || event.level}</span>
                      <span className="shrink-0 text-slate-400">{formatDateTime(event.createdAt)}</span>
                    </div>
                    <p className="mt-1 whitespace-pre-wrap break-words text-slate-500">{event.message}</p>
                    {eventRestoreResultErrors(event).length > 0 && (
                      <ul className="mt-2 space-y-1 text-rose-600">
                        {eventRestoreResultErrors(event).map((error, index) => (
                          <li key={`${index}-${error}`} className="whitespace-pre-wrap break-words">- {error}</li>
                        ))}
                      </ul>
                    )}
                  </div>
                )) : (
                  <div className="px-4 py-6 text-center text-xs font-medium text-slate-400">No task events yet.</div>
                )}
              </div>

            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function OperationsPage() {
  const [selectedJob, setSelectedJob] = useState<string | null>(null);
  const jobs: Array<{ id: string; type: string; object: string; status: string; user: string; time: string }> = [];
  const operationColumns: HyperTableColumn<typeof jobs[number]>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedJob === info.row.original.id}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedJob(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    { id: 'type', header: 'Type', accessorFn: job => job.type, size: 150, minSize: 120, cell: info => <span className="font-black text-slate-900">{info.row.original.type}</span>, meta: { title: job => job.type } },
    { id: 'object', header: 'Object', accessorFn: job => job.object, size: 280, minSize: 180, maxSize: 520, cell: info => info.row.original.object, meta: { title: job => job.object } },
    { id: 'status', header: 'Status', accessorFn: job => job.status, size: 130, minSize: 110, cell: info => <span className={info.row.original.status === 'Success' ? 'text-emerald-600' : info.row.original.status === 'Alert' ? 'text-amber-600' : 'text-blue-600'}>{info.row.original.status}</span>, meta: { title: job => job.status } },
    { id: 'user', header: 'User', accessorFn: job => job.user, size: 150, minSize: 120, cell: info => info.row.original.user, meta: { title: job => job.user } },
    { id: 'time', header: 'Time', accessorFn: job => job.time, size: 190, minSize: 150, maxSize: 280, cell: info => info.row.original.time, meta: { title: job => job.time } },
  ];

  return (
    <motion.div key="operations" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="History" desc="Backup, restore, and takeover audit." action="Export Audit" onAction={() => setSelectedJob(null)} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div>
            <p className="text-sm font-black text-slate-900">Task Audit</p>
            <p className="mt-1 text-xs font-medium text-slate-400">{selectedJob ? `Selected ${selectedJob}` : 'Select a job to view execution details'}</p>
          </div>
          <button disabled={!selectedJob} onClick={() => setSelectedJob(null)} className="hbdr-dr-action-primary hbdr-dr-action-ghost">View Details</button>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={operationColumns}
          data={jobs}
          getRowId={row => row.id}
          onRowClick={row => setSelectedJob(row.id)}
          getRowClassName={row => selectedJob === row.id ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No task audit records available."
        />
      </div>
    </motion.div>
  );
}

function TagManagementPage({
  tags,
  setTags,
  clusters,
  setClusters,
  toast,
}: {
  tags: TagItem[];
  setTags: React.Dispatch<React.SetStateAction<TagItem[]>>;
  clusters: Cluster[];
  setClusters: React.Dispatch<React.SetStateAction<Cluster[]>>;
  toast: (msg: string) => void;
}) {
  const [query, setQuery] = useState('');
  const [editingTag, setEditingTag] = useState<TagItem | null>(null);
  const [draftName, setDraftName] = useState('');
  const [tagModalOpen, setTagModalOpen] = useState(false);
  const [deleteTags, setDeleteTags] = useState<TagItem[]>([]);
  const [selectedTagIds, setSelectedTagIds] = useState<string[]>([]);
  const [tagBulkMenuOpen, setTagBulkMenuOpen] = useState(false);

  const tagResources = (tagId: string) => clusters.flatMap(cluster =>
    cluster.apps
      .filter(app => (app.tags || []).includes(tagId))
      .map(app => `${cluster.name} / ${app.name}`)
  );
  const filteredTags = tags.filter(tag => {
    const keyword = query.trim().toLowerCase();
    if (!keyword) return true;
    return [tag.name, tag.createdAt, tagResources(tag.id).join(' ')].some(value => value.toLowerCase().includes(keyword));
  });
  const selectedTags = tags.filter(tag => selectedTagIds.includes(tag.id));
  const singleSelectedTag = selectedTags.length === 1 ? selectedTags[0] : null;
  const allVisibleTagsSelected = filteredTags.length > 0 && filteredTags.every(tag => selectedTagIds.includes(tag.id));
  const toggleVisibleTags = () => {
    setSelectedTagIds(prev => {
      const visibleIds = filteredTags.map(tag => tag.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };
  const toggleSelectedTag = (tagId: string) => {
    setSelectedTagIds(prev => prev.includes(tagId) ? prev.filter(id => id !== tagId) : [...prev, tagId]);
  };
  const tagTableColumns = useMemo<HyperTableColumn<TagItem>[]>(() => [
    {
      id: 'select',
      header: () => (
        <input
          type="checkbox"
          checked={allVisibleTagsSelected}
          onClick={event => event.stopPropagation()}
          onChange={toggleVisibleTags}
        />
      ),
      cell: info => (
        <input
          type="checkbox"
          checked={selectedTagIds.includes(info.row.original.id)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedTag(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Name',
      accessorFn: tag => tag.name,
      size: 260,
      minSize: 180,
      maxSize: 420,
      cell: info => <span className="hbdr-tag-name"><Archive size={15} />{info.row.original.name}</span>,
      meta: { title: tag => tag.name },
    },
    {
      id: 'resources',
      header: 'Attach Resources',
      accessorFn: tag => tagResources(tag.id).join(', '),
      size: 390,
      minSize: 240,
      maxSize: 620,
      cell: info => {
        const resources = tagResources(info.row.original.id);
        return (
          <span className="hbdr-tag-resources">
            {resources.length > 0 ? resources.slice(0, 2).join(', ') : 'Not attached'}
            {resources.length > 2 ? ` +${resources.length - 2}` : ''}
          </span>
        );
      },
      meta: { title: tag => {
        const resources = tagResources(tag.id);
        return resources.length > 0 ? resources.join(', ') : 'Not attached';
      } },
    },
    {
      id: 'createdAt',
      header: 'Create Time',
      accessorFn: tag => tag.createdAt,
      size: 180,
      minSize: 150,
      maxSize: 260,
      cell: info => info.row.original.createdAt,
      meta: { title: tag => tag.createdAt },
    },
  ], [allVisibleTagsSelected, selectedTagIds, clusters]);
  const openCreateTag = () => {
    setEditingTag(null);
    setDraftName('');
    setTagModalOpen(true);
  };
  const openEditTag = (tag: TagItem) => {
    setEditingTag(tag);
    setDraftName(tag.name);
    setTagModalOpen(true);
  };
  const closeTagModal = () => {
    setTagModalOpen(false);
    setEditingTag(null);
    setDraftName('');
  };
  const saveTag = () => {
    const normalizedName = draftName.trim();
    if (!normalizedName) {
      toast('Enter a tag name');
      return;
    }
    const duplicate = tags.some(tag => tag.id !== editingTag?.id && tag.name.toLowerCase() === normalizedName.toLowerCase());
    if (duplicate) {
      toast('Tag name already exists');
      return;
    }
    if (editingTag) {
      setTags(prev => prev.map(tag => tag.id === editingTag.id ? { ...tag, name: normalizedName } : tag));
      toast('Tag updated');
    } else {
      const id = createUuid();
      setTags(prev => [{ id, name: normalizedName, createdAt: new Date().toISOString() }, ...prev]);
      toast('Tag created');
    }
    closeTagModal();
  };
  const confirmDeleteTags = () => {
    if (deleteTags.length === 0) return;
    const deleteIds = deleteTags.map(tag => tag.id);
    setTags(prev => prev.filter(tag => !deleteIds.includes(tag.id)));
    setClusters(prev => prev.map(cluster => ({
      ...cluster,
      apps: cluster.apps.map(app => ({ ...app, tags: (app.tags || []).filter(tagId => !deleteIds.includes(tagId)) })),
    })));
    setSelectedTagIds(prev => prev.filter(tagId => !deleteIds.includes(tagId)));
    toast(`${deleteTags.length} tag${deleteTags.length === 1 ? '' : 's'} deleted and detached from resources`);
    setDeleteTags([]);
  };

  return (
    <motion.div key="tags" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Archive size={18} /></div>
            <div>
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Tag Management</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Create tags and attach them to application DR resources.</p>
            </div>
          </div>
          <button type="button" className="hbdr-dr-action-primary" onClick={openCreateTag}>New Tag</button>
        </div>
      </div>

      <div className="hbdr-dr-table-card hbdr-tag-table-list">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group">
              <button type="button" className="hbdr-dr-action-primary" onClick={openCreateTag}>New Tag</button>
              <div className="relative">
                <button type="button" disabled={selectedTags.length === 0} onClick={() => setTagBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={tagBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {tagBulkMenuOpen && selectedTags.length > 0 && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setTagBulkMenuOpen(false)} />
                      <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-44 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                        <button disabled={!singleSelectedTag} onClick={() => { if (!singleSelectedTag) return; openEditTag(singleSelectedTag); setTagBulkMenuOpen(false); }} className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300">
                          <span className="flex items-center gap-2"><Edit2 size={14} />Edit Tag</span>
                          {!singleSelectedTag && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                        </button>
                        <button onClick={() => { setDeleteTags(selectedTags); setTagBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete Tag</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <label className="hbdr-dr-search hbdr-tag-quick-search"><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Quick search" /></label>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={tagTableColumns}
          data={filteredTags}
          getRowId={row => row.id}
          onRowClick={row => toggleSelectedTag(row.id)}
          getRowClassName={row => selectedTagIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedTagIds.length}
          emptyMessage={query ? 'No tags match the current search.' : 'No tags have been created.'}
          className="hbdr-tag-hyper-table"
        />
      </div>

      <AnimatePresence>
        {tagModalOpen && (
          <ModalFrame title={editingTag ? 'Edit Tag' : 'New Tag'} onClose={closeTagModal}>
            <div className="space-y-5">
              <EditField label="Name" value={draftName} placeholder="critical" onChange={setDraftName} />
              <div className="flex justify-end gap-3">
                <button type="button" onClick={closeTagModal} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button type="button" onClick={saveTag} className="rounded-xl bg-blue-600 px-6 py-2 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700">Save Tag</button>
              </div>
            </div>
          </ModalFrame>
        )}
        {deleteTags.length > 0 && (
          <ModalFrame title="Delete Tag" onClose={() => setDeleteTags([])}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Delete {deleteTags.length} selected tag{deleteTags.length === 1 ? '' : 's'}? They will be detached from all application resources.
              </div>
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Name" value={deleteTags.map(tag => tag.name).join(', ')} />
                <Info label="Attach Resources" value={String(deleteTags.reduce((total, tag) => total + tagResources(tag.id).length, 0))} />
                <Info label="Create Time" value={deleteTags.length === 1 ? deleteTags[0].createdAt : 'Multiple'} />
              </div>
              <div className="flex justify-end gap-3">
                <button type="button" onClick={() => setDeleteTags([])} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button type="button" onClick={confirmDeleteTags} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700">Confirm Delete</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function AlertsPage() {
  const [alerts, setAlerts] = useState<Array<{ id: string; level: string; source: string; message: string; time: string; handled: boolean }>>([]);
  const pending = alerts.filter(item => !item.handled).length;
  const alertColumns: HyperTableColumn<typeof alerts[number]>[] = [
    {
      id: 'level',
      header: 'Level',
      accessorFn: alert => alert.level,
      size: 120,
      minSize: 100,
      cell: info => {
        const alert = info.row.original;
        return <span className={`w-fit rounded-full border px-2 py-1 text-[10px] font-black ${alert.level === 'Critical' ? 'border-rose-100 bg-rose-50 text-rose-700' : alert.level === 'Warning' ? 'border-amber-100 bg-amber-50 text-amber-700' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>{alert.level}</span>;
      },
      meta: { title: alert => alert.level },
    },
    { id: 'source', header: 'Source', accessorFn: alert => alert.source, size: 170, minSize: 130, cell: info => <span className="text-xs font-bold text-slate-700">{info.row.original.source}</span>, meta: { title: alert => alert.source } },
    { id: 'message', header: 'Message', accessorFn: alert => alert.message, size: 360, minSize: 220, maxSize: 720, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.message}</span>, meta: { title: alert => alert.message } },
    { id: 'time', header: 'Time', accessorFn: alert => alert.time, size: 190, minSize: 150, cell: info => <span className="text-xs font-semibold text-slate-400">{info.row.original.time}</span>, meta: { title: alert => alert.time } },
    {
      id: 'action',
      header: 'Action',
      accessorFn: alert => alert.handled ? 'Acknowledged' : 'Acknowledge',
      size: 150,
      minSize: 130,
      enableSorting: false,
      cell: info => {
        const alert = info.row.original;
        return (
          <button
            disabled={alert.handled}
            onClick={event => {
              event.stopPropagation();
              setAlerts(prev => prev.map(item => item.id === alert.id ? { ...item, handled: true } : item));
            }}
            className="hbdr-dr-action-primary hbdr-dr-action-ghost"
          >
            {alert.handled ? 'Acknowledged' : 'Acknowledge'}
          </button>
        );
      },
    },
  ];

  return (
    <motion.div key="alerts" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Alerts" desc="Cluster DR risks and event alerts." action="Refresh Alerts" onAction={() => setAlerts(prev => [...prev])} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div><p className="text-sm font-black text-slate-900">Current Alerts</p><p className="mt-1 text-xs font-medium text-slate-400">Pending {pending} items</p></div>
          <button disabled={pending === 0} onClick={() => setAlerts(prev => prev.map(item => ({ ...item, handled: true })))} className="hbdr-dr-action-primary">Acknowledge All</button>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={alertColumns}
          data={alerts}
          getRowId={row => row.id}
          emptyMessage="No alerts available."
        />
      </div>
    </motion.div>
  );
}

function SettingsPage() {
  const [saved, setSaved] = useState(false);
  const [settings, setSettings] = useState({
    adminDomain: 'system',
    captchaTtl: '120 seconds',
    sessionTimeout: '30 minutes',
    email: 'ops@onepro.local',
    auditRetention: '180 days',
    webhook: 'https://hooks.example.local/hypercdr',
  });
  const updateSetting = (key: keyof typeof settings, value: string) => {
    setSettings(prev => ({ ...prev, [key]: value }));
    setSaved(false);
  };

  return (
    <motion.div key="settings" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="System" desc="Platform parameters and security policies." action="Save Settings" onAction={() => setSaved(true)} />
      <div className="hbdr-section-card">
        <div className="hbdr-section-toolbar">
          <div>
            <h3>Platform Settings</h3>
            <p>Keep tenant access, session controls, notifications, and audit retention aligned.</p>
          </div>
          <button type="button" onClick={() => setSaved(true)} className="hbdr-dr-action-primary">Save Settings</button>
        </div>
        <div className="grid gap-5 p-5 lg:grid-cols-2">
        <div className="rounded border border-slate-200 bg-white p-5 shadow-sm">
          <h3 className="text-sm font-black text-slate-900">Login & Tenant Domain</h3>
          <div className="mt-4 grid gap-3">
            <EditField label="Platform Admin Domain" value={settings.adminDomain} onChange={value => updateSetting('adminDomain', value)} />
            <EditField label="Captcha TTL" value={settings.captchaTtl} onChange={value => updateSetting('captchaTtl', value)} />
            <EditField label="Session Timeout" value={settings.sessionTimeout} onChange={value => updateSetting('sessionTimeout', value)} />
          </div>
        </div>
        <div className="rounded border border-slate-200 bg-white p-5 shadow-sm">
          <h3 className="text-sm font-black text-slate-900">Notifications & Audit</h3>
          <div className="mt-4 grid gap-3">
            <EditField label="Notification Email" value={settings.email} onChange={value => updateSetting('email', value)} />
            <EditField label="Audit Retention" value={settings.auditRetention} onChange={value => updateSetting('auditRetention', value)} />
            <EditField label="Alert Webhook" value={settings.webhook} onChange={value => updateSetting('webhook', value)} />
          </div>
        </div>
        </div>
      </div>
      {saved && <div className="rounded border border-emerald-100 bg-emerald-50 px-4 py-3 text-xs font-bold text-emerald-700">Settings saved.</div>}
    </motion.div>
  );
}

function TenantPage() {
  const [tenants, setTenants] = useState([
    { id: 'tenant-prod', name: 'Production Tenant', domain: 'prod', admin: 'prod-admin', users: 12, status: 'Enabled' },
    { id: 'tenant-dev', name: 'Development Tenant', domain: 'dev', admin: 'dev-admin', users: 8, status: 'Enabled' },
  ]);
  const [selectedTenant, setSelectedTenant] = useState<string | null>(null);
  const [tenantMessage, setTenantMessage] = useState('');
  const tenantColumns: HyperTableColumn<typeof tenants[number]>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedTenant === info.row.original.id}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedTenant(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Tenant',
      accessorFn: tenant => tenant.name,
      size: 280,
      minSize: 200,
      maxSize: 520,
      cell: info => <div><p className="text-sm font-black text-slate-900">{info.row.original.name}</p><p className="text-xs text-slate-400">Login Domain: {info.row.original.domain}</p></div>,
      meta: { title: tenant => `${tenant.name} / ${tenant.domain}` },
    },
    { id: 'users', header: 'Users', accessorFn: tenant => tenant.users, size: 120, minSize: 100, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.users} users</span>, meta: { title: tenant => `${tenant.users} users` } },
    { id: 'admin', header: 'Admin', accessorFn: tenant => tenant.admin, size: 170, minSize: 130, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.admin}</span>, meta: { title: tenant => tenant.admin } },
    { id: 'status', header: 'Status', accessorFn: tenant => tenant.status, size: 120, minSize: 100, cell: info => <span className={`w-fit rounded-full border px-2 py-1 text-[10px] font-black ${info.row.original.status === 'Enabled' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>{info.row.original.status}</span>, meta: { title: tenant => tenant.status } },
    { id: 'mode', header: 'Mode', accessorFn: () => 'Multi-tenant', size: 140, minSize: 120, cell: () => <span className="text-xs font-semibold text-slate-400">Multi-tenant</span>, meta: { title: () => 'Multi-tenant' } },
  ];

  return (
    <motion.div key="tenants" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Tenants" desc="Tenants and administrator accounts." action="Add Tenant" onAction={() => setTenants(prev => [{ id: `tenant-${Date.now()}`, name: 'New Tenant', domain: 'new', admin: 'tenant-admin', users: 1, status: 'Enabled' }, ...prev])} />
      <div className="hbdr-dr-table-card">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div><p className="text-sm font-black text-slate-900">Tenant List</p><p className="mt-1 text-xs font-medium text-slate-400">{selectedTenant ? `Selected ${selectedTenant}` : 'Select a tenant to disable it or manage users'}</p></div>
          <div className="flex gap-2">
            <button onClick={() => setTenants(prev => [{ id: `tenant-${Date.now()}`, name: 'New Tenant', domain: 'new', admin: 'tenant-admin', users: 1, status: 'Enabled' }, ...prev])} className="hbdr-dr-action-primary">Add Tenant</button>
            <button disabled={!selectedTenant} onClick={() => setTenants(prev => prev.map(item => item.id === selectedTenant ? { ...item, status: item.status === 'Enabled' ? 'Disabled' : 'Enabled' } : item))} className="hbdr-dr-action-primary hbdr-dr-action-ghost">Enable / Disable Tenant</button>
            <button disabled={!selectedTenant} onClick={() => setTenantMessage('User management panel opened. Maintain tenant administrators and users here.')} className="hbdr-dr-action-primary">User Management</button>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={tenantColumns}
          data={tenants}
          getRowId={row => row.id}
          onRowClick={row => setSelectedTenant(row.id)}
          getRowClassName={row => selectedTenant === row.id ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No tenants available."
        />
      </div>
      {tenantMessage && <div className="rounded border border-blue-100 bg-blue-50 px-4 py-3 text-xs font-bold text-blue-700">{tenantMessage}</div>}
    </motion.div>
  );
}

function SearchBar({ title, desc, action, onAction }: { title: string; desc: string; action?: string; onAction?: () => void }) {
  const ActionIcon = action?.includes('Refresh') ? RefreshCw : action?.includes('Save') ? Check : action?.includes('Export') ? Archive : Plus;
  return (
    <div className="hbdr-page-hero">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded border border-blue-100 bg-blue-50 text-blue-600"><Search size={18} /></div>
          <div><h3 className="text-sm font-black text-slate-900">{title}</h3><p className="text-xs font-medium text-slate-400">{desc}</p></div>
        </div>
        <div className="flex gap-2">
          <input className="h-9 w-72 rounded border border-slate-200 px-3 text-xs font-semibold outline-none focus:border-blue-400" placeholder="Quick Search" />
          {action && <button onClick={onAction} className="rounded bg-blue-600 px-4 py-2 text-xs font-black text-white"><ActionIcon size={14} className="mr-1 inline" />{action}</button>}
        </div>
      </div>
    </div>
  );
}

function SimplePanel({ keyName, icon: Icon, title, desc }: { keyName: string; icon: typeof Server; title: string; desc: string }) {
  return (
    <motion.div key={keyName} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="rounded border border-slate-200 bg-white p-6 shadow-sm">
        <div className="flex items-center gap-3">
          <div className="flex h-11 w-11 items-center justify-center rounded border border-blue-100 bg-blue-50 text-blue-600"><Icon size={20} /></div>
          <div><h3 className="text-base font-black text-slate-900">{title}</h3><p className="mt-1 text-sm text-slate-500">{desc}</p></div>
        </div>
      </div>
    </motion.div>
  );
}

function ModalFrame({ title, subtitle, icon, children, onClose, maxWidthClass = 'max-w-2xl' }: { title: string; subtitle?: string; icon?: React.ReactNode; children: React.ReactNode; onClose: () => void; maxWidthClass?: string }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-6">
      <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0 bg-slate-950/25" onClick={onClose} />
      <motion.div initial={{ opacity: 0, y: 16, scale: 0.97 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 16, scale: 0.97 }} transition={{ duration: 0.18, ease: 'easeOut' }} className={'relative w-full ' + maxWidthClass + ' overflow-hidden rounded-2xl bg-white shadow-2xl ring-1 ring-slate-900/5'}>
        <div className="relative overflow-hidden border-b border-slate-100 bg-gradient-to-br from-slate-50 via-white to-slate-50 px-6 py-5">
          <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full bg-blue-50 blur-2xl" />
          <div className="relative flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              {icon && <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500 to-blue-600 text-white shadow-lg shadow-blue-200">{icon}</div>}
              <div>
                <h3 className="text-lg font-black leading-tight text-slate-900">{title}</h3>
                {subtitle && <p className="mt-1 text-sm text-slate-500">{subtitle}</p>}
              </div>
            </div>
            <button onClick={onClose} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-white hover:text-slate-700"><X size={18} /></button>
          </div>
        </div>
        <div className="p-6">{children}</div>
      </motion.div>
    </div>
  );
}

function ErrorDetailModalFrame({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-5">
      <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0 bg-slate-950/18" onClick={onClose} />
      <motion.div initial={{ opacity: 0, y: 10, scale: 0.99 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 10, scale: 0.99 }} transition={{ duration: 0.14, ease: 'easeOut' }} className="relative w-full max-w-xl overflow-hidden rounded-xl bg-white shadow-xl ring-1 ring-slate-900/8">
        <div className="flex items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
          <h3 className="text-sm font-black text-slate-900">{title}</h3>
          <button onClick={onClose} className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700" aria-label="Close"><X size={16} /></button>
        </div>
        <div className="p-4">{children}</div>
      </motion.div>
    </div>
  );
}

function EditField({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
  hint,
  required,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  hint?: string;
  required?: boolean;
}) {
  return (
    <label className="flex flex-col gap-1 text-[11px] font-bold uppercase tracking-[0.08em] text-slate-500">
      <span className="flex items-center gap-1.5">
        {label}
        {required && <span className="text-rose-500">*</span>}
      </span>
      <input
        type={type}
        value={value}
        placeholder={placeholder}
        onChange={event => onChange(event.target.value)}
        className="h-10 w-full rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-800 outline-none transition-all placeholder:font-normal placeholder:text-slate-300 hover:border-slate-300 focus:border-blue-500 focus:bg-white focus:shadow-[0_0_0_4px_rgba(59,130,246,0.12)]"
      />
      {hint && <span className="text-[10px] font-medium normal-case tracking-normal text-slate-400">{hint}</span>}
    </label>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded bg-slate-50 p-3">
      <p className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</p>
      <p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p>
    </div>
  );
}
