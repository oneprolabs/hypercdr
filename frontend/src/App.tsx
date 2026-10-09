import { mapClusterStatus, mapRestorePoint, taskPlanId, recoveryTaskMatchesApp, buildAppTaskMap, includePointedTasks, mergeTaskMapKeepingActive, planIncludesApp, mapApps, mapCluster, mapStorageRepo } from './app/platform-data';
import ClusterContextCard from './components/cluster-context-card';
import { RequiredPasswordChange, PasswordChangeSuccess, PasswordRecoveryPage } from './auth/password-pages';
import PageTitleBar from './components/page-title-bar';
﻿import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { DrConfigurationModal } from './dr-configuration-modal';
import { RecoveryWizardModal, type RecoveryWizardConfig } from './recovery-wizard-modal';
import { HyperTable, type HyperTableColumn } from './components/table';
import { SearchBar } from './components/search-bar';
import { EditField } from './components/edit-field';
import ListToolbarControls from './components/list-toolbar-controls';
import { ReleaseNotesModal } from './components/release-notes-modal';
import {
  Activity,
  AlertCircle,
  AlertTriangle,
  ArrowDown,
  ArrowUpCircle,
  Archive,
  Bell,
  Boxes,
  Building2,
  Calendar,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ClipboardList,
  Clock,
  Cloud,
  Database,
  DatabaseBackup,
  Edit2,
  Eye,
  EyeOff,
  FileCode,
  FileArchive,
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
  ListChecks,
  LogOut,
  Mail,
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
  Moon,
  Terminal,
  Tags,
  Trash2,
  User,
  X,
  Zap,
} from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiGet, apiHeaders, apiPatch, apiPost, apiPut, ensureApiResponse } from './api/client';
import { AUTH_EXPIRED_EVENT, clearStoredAuthSession, readStoredAuthSession, writeStoredAuthSession } from './auth/session';
import type { ApiLoginResponse, AuthSession } from './auth/types';
import { setUserTimeZone as setSharedUserTimeZone } from './lib/date-time';
import { hasUnreadReleaseNotes, markReleaseNotesViewed } from './lib/release-notes';
import type {
  AppItem,
  Cluster,
  ClusterNamespaceRow,
  ClusterNode,
  ClusterNodeRow,
  ClusterStatus,
  ClusterStorageClass,
  ClusterStorageClassRow,
  DRSupportCheck,
  DRSupportSummary,
  LabelResourceMatch,
  LabelSelectorOption,
  ResourceCategory,
  ResourceCategoryKey,
  ResourceKindSummary,
  ResourceRef,
  ResourceSummary,
} from './features/clusters/types';
import {
  listItems,
  type ApiApplication,
  type ApiList,
  type ApiPolicy,
  type ApiProtectionPlan,
  type ApiRestorePoint,
  type ApiRestorePointView,
  type ApiTask,
  type ApiTaskCancelResponse,
  type ApiTaskEvent,
  type ApiTaskResponse,
  type PolicyComposition,
  type PolicyItem,
  type PolicyScheduleType,
  type StorageRepo,
  type TagItem,
  type VolumeProgressInfo,
} from './features/recovery/types';
import { isActiveTaskStatus, isCompletedTaskStatus, isFailedStatus, isSucceededStatus, taskHasWarning } from './features/recovery/task-status';
import { selectPointedPlanTask } from './features/recovery/plan-task-selection';
import type { ApiCluster, ApiStorageRepo } from './features/recovery/platform-types';
import {
  TaskErrorDetailBlock, TaskErrorStatus, TaskFinalResult, TaskOriginLabel, TaskProcessTimeline,
  canRetryDrActivation, drStatusForPlan, eventRestoreResultErrors, formatAge, formatBytes,
  formatBytesPerSecond, formatEta, formatPercent, hasTaskEventReason, isProtectionPlanCleaning,
  isProtectionPlanReady, mapApplicationStatus, mergeResourceItems, normalizeErrorCode,
  numberFromUnknown, recordFromUnknown, recoveryActionText, recoveryPreparingMessage,
  resourceCategoryForKind, resourceCategoryIconMap, resourceCategoryKeys, resourceCategoryMeta,
  resourceInventoryDetailText, resourceInventoryTitle, restorePointOriginalSize,
  restorePointStorageSize, shortResourceKind, stageOfApp, storageFailurePresentation,
  syncPreparingMessage, taskDetailFullLabel, taskDetailLabel, taskFailureDetails,
  taskFailureSummary, taskProgressInfo,
  type ApplicationStage,
} from './features/recovery/task-ui';
import { buildResourceCategory, formatTime, weekdays } from './features/applications/application-primitives';
import {
  COLUMN_FILTER_PREFIX, ErrorDetailModalFrame, listToolbarQueryFields, matchesColumnFilterToken,
  namespacesFromPayload, parseColumnFilterToken, recoveryCompletedTargetLabel,
  recoveryCompletedTargetTitle, restorePointDisplayLabel, taskRestorePointId,
} from './features/applications/application-support';
import {
  latestTaskForRestorePoint, restorePointIsScheduled, restorePointListStatus,
  restorePointNamespaces, taskMatchesRestorePoint, taskStatusLabel,
} from './features/restore-points/restore-point-support';
import { validateFrontendModules, type ExtensionViewId, type HyperCDRFrontendModule } from './app/extensions';

const lazyWithUpgradeRecovery = <T extends React.ComponentType<any>>(loader: () => Promise<{ default: T }>) => React.lazy(async () => {
  const reloadKey = 'hypercdr.lazy-module-reload';
  try {
    const loaded = await loader();
    window.sessionStorage.removeItem(reloadKey);
    return loaded;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    const isStaleBuild = /dynamically imported module|importing a module script failed|failed to fetch/i.test(message);
    if (isStaleBuild && window.sessionStorage.getItem(reloadKey) !== '1') {
      window.sessionStorage.setItem(reloadKey, '1');
      window.location.reload();
      return new Promise<never>(() => undefined);
    }
    window.sessionStorage.removeItem(reloadKey);
    throw error;
  }
});

const FailbackPage = lazyWithUpgradeRecovery(() => import('./features/failback/failback-page'));
const LazyEmailSettingsPage = lazyWithUpgradeRecovery(() => import('./features/settings/email-settings-page'));
const LazyUpgradeManagementPage = lazyWithUpgradeRecovery(() => import('./features/upgrades/upgrade-management-page'));
const LazyCommunityUserManagementPage = lazyWithUpgradeRecovery(() => import('./features/users/community-user-management-page'));
const LazyOperationsCenterPage = lazyWithUpgradeRecovery(() => import('./features/operations/operations-center-page'));
const LazyActivityLogPage = lazyWithUpgradeRecovery(() => import('./features/operations/activity-log-page'));
const LazyDiagnosticLogsPage = lazyWithUpgradeRecovery(() => import('./features/operations/diagnostic-logs-page'));
const LazySupportBundlePage = lazyWithUpgradeRecovery(() => import('./features/operations/support-bundle-page'));
const LazyProfilePage = lazyWithUpgradeRecovery(() => import('./features/profile/profile-page'));
const LazyTagManagementPage = lazyWithUpgradeRecovery(() => import('./features/tags/tag-management-page'));
const LazyPolicyPage = lazyWithUpgradeRecovery(() => import('./features/policies/policy-page'));
const LazyStoragePage = lazyWithUpgradeRecovery(() => import('./features/storage/storage-page'));
const LazyClusterPage = lazyWithUpgradeRecovery(() => import('./features/clusters/cluster-page'));
const LazyOverviewPage = lazyWithUpgradeRecovery(() => import('./features/dashboard/overview-page').then(module => ({ default: module.OverviewPage })));
const LazyApplicationDrPage = lazyWithUpgradeRecovery(() => import('./features/applications/application-dr-page'));
const LazyRestorePointPage = lazyWithUpgradeRecovery(() => import('./features/restore-points/restore-point-page'));
const LazyBackupRecoveryTaskPage = lazyWithUpgradeRecovery(() => import('./features/tasks/backup-recovery-task-page'));

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
  | 'activity'
  | 'logs'
  | 'support_bundle'
  | 'tags'
  | 'users'
  | 'tenants'
  | 'email_settings'
  | 'upgrades'
  | 'profile'
  | ExtensionViewId;

type TopModule = 'overview' | 'dr' | 'config' | 'ops' | 'monitor' | 'settings';

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
      operations: ['Operations Center', 'Monitor platform health, active DR operations, and current issues'],
      activity: ['Activity Log', 'Review administrator actions and their results'],
      logs: ['Diagnostic Logs', 'Search platform and managed-cluster diagnostic logs'],
      support_bundle: ['Support Bundle', 'Collect a comprehensive offline troubleshooting package'],
      tags: ['Tag Management', 'Create and maintain reusable application tags'],
      users: ['User Management', 'Create and maintain platform users'],
      tenants: ['Tenant Management', 'Create and maintain isolated tenants'],
      email_settings: ['Email Settings', 'Configure password recovery email delivery'],
      upgrades: ['Platform Upgrade', 'Check releases and perform a manual blue-green upgrade'],
      profile: ['Basic Information', 'View and update your account'],
      login: ['', ''],
      dashboard: ['', ''],
    },
  },
};

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

const initialClusters: Cluster[] = [];

const initialStorage: StorageRepo[] = [];

const initialPolicies: PolicyItem[] = [];

const initialTags: TagItem[] = [];

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
  accountName?: string;
  accountKey?: string;
  serviceAccountKey?: string;
};
type ApiAgentToken = {
  id: string;
  token: string;
  expiresAt: string;
  prepareNodeCommand?: string;
  installCommand: string;
};

type ApiUnregisterPrecheck = {
  clusterId: string;
  agentOnline: boolean;
  defaultCluster: boolean;
  sourcePlanCount: number;
  targetPlanCount: number;
  restorePointCount: number;
  storageRepositoryIds: string[];
  activeTaskCount: number;
  activeTaskTypes: string[];
  unregisterActive: boolean;
  objectStorageNeeded: boolean;
  stage: string;
  allowed: boolean;
  blockers: string[];
};
type ApiCaptcha = {
  id: string;
  image: string;
  expiresAt: string;
};
type ApiAuthConfig = { challengeMode?: 'image' | 'turnstile'; turnstileSiteKey?: string };
declare global { interface Window { turnstile?: { ready?: (callback: () => void) => void; render: (element: HTMLElement, options: { sitekey: string; language?: string; size?: 'flexible'; theme?: 'dark'; callback: (token: string) => void; 'expired-callback'?: () => void; 'error-callback'?: () => void }) => string; reset: (id?: string) => void; remove?: (id?: string) => void }; } }
type AuthFlow = 'login' | 'forgot' | 'reset';

type ClusterTaskLog = {
  task: ApiTask;
  events: ApiTaskEvent[];
  loading: boolean;
};

const NAV_VIEW_KEY = 'hypercdr.nav.view';
const ACCOUNT_EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
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
  'tags',
  'users',
  'tenants',
  'email_settings',
  'upgrades',
  'profile',
  'operations',
  'activity',
  'logs',
  'support_bundle',
]);

const PLATFORM_DATA_VIEWS = new Set<View>([
  'dashboard',
  'applications',
  'dr_tasks',
  'failback',
  'clusters',
  'storage',
  'policies',
  'restore_points',
  'tags',
]);

function isRestorableView(view: View) {
  return RESTORABLE_VIEWS.has(view) || view.startsWith('extension:');
}

function readStoredView(): View | null {
  try {
    const value = localStorage.getItem(NAV_VIEW_KEY) as View | null;
    return value && isRestorableView(value) ? value : null;
  } catch {
    return null;
  }
}

function writeStoredView(view: View) {
  if (!isRestorableView(view)) return;
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

function isAgentTokenUsable(token: ApiAgentToken | null) {
  if (!token?.installCommand) return false;
  const expiresAt = Date.parse(token.expiresAt);
  // The command must remain valid throughout cluster-side preflight and
  // installation. First-time image pulls can legitimately take minutes.
  return Number.isNaN(expiresAt) || expiresAt > Date.now() + 10 * 60_000;
}

function shortDigest(digest?: string): string {
  if (!digest) return '';
  const cleaned = digest.replace(/^sha256:/, '');
  return cleaned.length > 12 ? cleaned.slice(0, 12) : cleaned;
}


function formatLastSeen(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return 'unknown';
  const timestamp = date.getTime();
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
let userTimeZone = browserTimeZone;
const availableTimeZones: string[] = (() => {
  const supportedValuesOf = (Intl as any).supportedValuesOf as ((key: string) => string[]) | undefined;
  const zones = supportedValuesOf ? supportedValuesOf('timeZone') : ['UTC', 'Asia/Shanghai', 'Asia/Tokyo', 'Europe/London', 'America/New_York', 'America/Los_Angeles'];
  return Array.from(new Set([browserTimeZone, 'UTC', ...zones]));
})();

function parseUTCInstant(value?: string): Date | null {
  if (!value) return null;
  const normalized = value.trim();
  if (!normalized) return null;
  // API timestamps are UTC instants. Keep compatibility with legacy values
  // that omitted the zone instead of letting browsers interpret them as local.
  const explicitZone = /(?:z|[+-]\d{2}:?\d{2})$/i.test(normalized);
  const timestamp = new Date(explicitZone || !normalized.includes('T') ? normalized : `${normalized}Z`);
  return Number.isNaN(timestamp.getTime()) ? null : timestamp;
}

function formatDateTime(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '-';
  return date.toLocaleString(undefined, {
    timeZone: userTimeZone,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function formatLocalDateTime(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '';
  const parts = new Intl.DateTimeFormat(undefined, {
    timeZone: userTimeZone,
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

function formatLocalDateKey(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '';
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: userTimeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value || '';
  return `${part('year')}-${part('month')}-${part('day')}`;
}

function userTimeZoneLabel(timeZone = userTimeZone) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    timeZoneName: 'longOffset',
  }).formatToParts(new Date());
  const offset = parts.find(part => part.type === 'timeZoneName')?.value || 'GMT';
  return `My Time Zone: ${offset} (${timeZone})`;
}

function timeZoneOptionLabel(timeZone: string, date = new Date()) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    timeZoneName: 'longOffset',
  }).formatToParts(date);
  const rawOffset = parts.find(part => part.type === 'timeZoneName')?.value || 'GMT';
  const offset = rawOffset === 'GMT' ? 'UTC+00:00' : rawOffset.replace(/^GMT/, 'UTC');
  return `${timeZone} (${offset})`;
}

function taskStatusClass(status?: string) {
  if (status === 'succeeded') return 'text-emerald-600';
  if (status === 'failed') return 'text-rose-600';
  if (['running', 'accepted', 'dispatched', 'queued'].includes(status || '')) return 'text-blue-600';
  return 'text-slate-500';
}


function isS3CompatibleType(type: string) {
  return ['s3-compatible', 's3 compatible'].includes(type.toLowerCase());
}

function buildStorageRepositoryInput(repo: StorageRepo): StorageRepositoryInput {
  const config = repo.config || {};
  const isCompatible = isS3CompatibleType(repo.type);
  const isAzure = repo.type === 'Azure';
  const isGCS = repo.type === 'Google Cloud' || repo.type === 'GCS';
  const azureDomain = String(config.blobDomain || 'blob.core.windows.net').replace(/^https?:\/\//, '');
  const endpoint = String(isAzure ? `${String(config.accountName || '')}.${azureDomain}` : config.endpoint || repo.endpoint || '');
  const bucket = String(isAzure ? config.container || repo.bucket || '' : config.bucket || repo.bucket || '');
  const rawRegion = String(config.region || repo.region || '').trim();
  const region = ['n/a', 'na', '-'].includes(rawRegion.toLowerCase()) ? '' : rawRegion;
  const accessKey = String(config.accessKey || '');
  const secretKey = String(config.secretKey || '');
  const payloadConfig: Record<string, string | boolean> = {};
  if (config.urlStyle) payloadConfig.urlStyle = String(config.urlStyle);
  if (config.prefix) payloadConfig.prefix = String(config.prefix);
  if (isAzure && config.accountName) payloadConfig.storageAccount = String(config.accountName);
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
    accountName: isAzure ? String(config.accountName || '') : undefined,
    accountKey: isAzure ? String(config.accountKey || '') : undefined,
    serviceAccountKey: isGCS ? String(config.serviceAccountKey || '') : undefined,
  };
}

type ScheduleParts = { hour: number; minute: number; weekDay: number; monthDay: number };

function datePartsInTimeZone(date: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).formatToParts(date);
  const value = (type: string) => Number(parts.find(part => part.type === type)?.value || 0);
  return { year: value('year'), month: value('month'), day: value('day'), hour: value('hour') % 24, minute: value('minute'), second: value('second') };
}

function zonedWallTimeToUTC(year: number, month: number, day: number, hour: number, minute: number, timeZone: string) {
  const desired = Date.UTC(year, month - 1, day, hour, minute, 0);
  let result = new Date(desired);
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const actual = datePartsInTimeZone(result, timeZone);
    const actualWall = Date.UTC(actual.year, actual.month - 1, actual.day, actual.hour, actual.minute, 0);
    const correction = desired - actualWall;
    if (correction === 0) break;
    result = new Date(result.getTime() + correction);
  }
  return result;
}

function scheduleDisplayToUTC(policy: Pick<PolicyItem, 'type' | 'hour' | 'minute' | 'weekDay' | 'monthDay'>, now = new Date()): ScheduleParts {
  if (policy.type === 'interval') return { hour: policy.hour, minute: policy.minute, weekDay: policy.weekDay, monthDay: policy.monthDay };
  const localNow = datePartsInTimeZone(now, userTimeZone);
  let year = localNow.year;
  let month = localNow.month;
  let day = localNow.day;
  if (policy.type === 'weekly') {
    const currentWeekDay = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
    day += (policy.weekDay - currentWeekDay + 7) % 7;
  } else if (policy.type === 'monthly') {
    day = Math.min(policy.monthDay, new Date(Date.UTC(year, month, 0)).getUTCDate());
  }
  const utc = zonedWallTimeToUTC(year, month, day, policy.hour, policy.minute, userTimeZone);
  let utcMonthDay = utc.getUTCDate();
  if (policy.type === 'monthly') {
    const localMonthKey = year * 12 + month;
    const utcMonthKey = utc.getUTCFullYear() * 12 + utc.getUTCMonth() + 1;
    // Day 31 is the scheduler's "last day of month" representation. It keeps
    // local day 1 schedules stable when their UTC instant falls in the prior
    // month, whose actual final date varies between 28 and 31.
    if (utcMonthKey < localMonthKey) utcMonthDay = 31;
  }
  return { hour: utc.getUTCHours(), minute: utc.getUTCMinutes(), weekDay: utc.getUTCDay(), monthDay: utcMonthDay };
}

function scheduleUTCToDisplay(policy: Pick<ApiPolicy, 'scheduleType' | 'hour' | 'minute' | 'weekDay' | 'monthDay'>, now = new Date()): ScheduleParts {
  const type = policy.scheduleType;
  if (!['daily', 'weekly', 'monthly'].includes(type)) return { hour: policy.hour || 0, minute: policy.minute || 0, weekDay: policy.weekDay || 0, monthDay: policy.monthDay || 1 };
  let year = now.getUTCFullYear();
  let month = now.getUTCMonth();
  let day = now.getUTCDate();
  if (type === 'weekly') {
    day += ((policy.weekDay || 0) - now.getUTCDay() + 7) % 7;
  } else if (type === 'monthly') {
    day = Math.min(policy.monthDay || 1, new Date(Date.UTC(year, month + 1, 0)).getUTCDate());
  }
  const utc = new Date(Date.UTC(year, month, day, policy.hour || 0, policy.minute || 0));
  const local = datePartsInTimeZone(utc, userTimeZone);
  const localLastDay = new Date(Date.UTC(local.year, local.month, 0)).getUTCDate();
  const displayMonthDay = type === 'monthly' && local.day === localLastDay ? 31 : local.day;
  return { hour: local.hour, minute: local.minute, weekDay: new Date(Date.UTC(local.year, local.month - 1, local.day)).getUTCDay(), monthDay: displayMonthDay };
}

function mapPolicy(policy: ApiPolicy): PolicyItem {
  const scheduleType = ['daily', 'weekly', 'monthly'].includes(policy.scheduleType) ? policy.scheduleType as PolicyScheduleType : 'interval';
  const composition = ['manual', 'combined', 'schedule', 'retention'].includes(policy.composition)
    ? policy.composition as PolicyComposition
    : 'combined';
  const displaySchedule = scheduleUTCToDisplay(policy);
  return {
    id: policy.id,
    name: policy.name,
    composition,
    type: scheduleType,
    intervalValue: policy.intervalValue || 1,
    intervalUnit: policy.intervalUnit === 'minute' || policy.intervalUnit === 'minutes' ? 'minutes' : 'hours',
    hour: displaySchedule.hour,
    minute: displaySchedule.minute,
    weekDay: displaySchedule.weekDay,
    monthDay: displaySchedule.monthDay,
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

const topNav: Array<{ key: TopModule; view: View }> = [
  { key: 'overview', view: 'dashboard' },
  { key: 'dr', view: 'applications' },
  { key: 'config', view: 'clusters' },
  { key: 'ops', view: 'operations' },
  { key: 'settings', view: 'profile' },
];

function moduleForView(view: View): TopModule {
  if (view === 'dashboard') return 'overview';
  if (view === 'applications' || view === 'restore_points' || view === 'dr_tasks' || view === 'failback') return 'dr';
  if (view === 'clusters' || view === 'storage' || view === 'policies' || view === 'tags') return 'config';
  if (view === 'operations' || view === 'activity' || view === 'logs' || view === 'support_bundle') return 'ops';
  return 'settings';
}

function statusText(status: ClusterStatus) {
  if (status === 'healthy') return 'Healthy';
  if (status === 'syncing') return 'Syncing';
  return 'Alert';
}

function formatPolicyType(type: PolicyScheduleType) {
  if (type === 'interval') return 'Interval';
  if (type === 'daily') return 'Daily Backup';
  if (type === 'weekly') return 'Weekly Backup';
  return 'Monthly Backup';
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

function LanguageIndicator({
  locale,
  compact = false,
}: {
  locale: LocaleCode;
  compact?: boolean;
}) {
  const language = locales[locale];

  return (
    <span
      className={compact ? 'hbdr-language-switch hbdr-language-switch-compact' : 'hbdr-language-switch'}
      aria-label={`${language.languageLabel}: ${language.name}`}
    >
      <Languages size={14} />
      <span>{compact ? 'EN' : language.languageLabel}</span>
      <strong>{language.name}</strong>
    </span>
  );
}

function toggleListValue(values: string[], value: string) {
  return values.includes(value) ? values.filter(item => item !== value) : [...values, value];
}

function makeColumnFilterToken(field: string, value: string) {
  return `${COLUMN_FILTER_PREFIX}${encodeURIComponent(field)}:${encodeURIComponent(value.trim())}`;
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

export type HyperCDRAppProps = { modules?: HyperCDRFrontendModule[] };

type ApiProductInfo = {
  product?: string;
  edition?: string;
  license?: {
    mode?: string;
    status?: string;
    detail?: string;
  };
  capabilities?: Record<string, { enabled?: boolean }>;
};

export default function App({ modules = [] }: HyperCDRAppProps) {
  const extensionModules = useMemo(() => validateFrontendModules(modules), [modules]);
  // Enterprise-owned modules are an edition boundary even before capability
  // discovery finishes. Never flash the Community fallback while product-info
  // is still loading or temporarily unavailable.
  const hasEnterpriseAuditModule = useMemo(
    () => extensionModules.some(module => module.id === 'enterprise-audit'),
    [extensionModules],
  );
  const [authSession, setAuthSession] = useState<AuthSession | null>(() => readStoredAuthSession());
  const [productInfo, setProductInfo] = useState<ApiProductInfo | null>(null);
  const [productCapabilities, setProductCapabilities] = useState<Record<string, { enabled?: boolean }>>({});
  const clusterRegistrationAllowed = !hasEnterpriseAuditModule || productInfo?.license?.status === 'active';
  const [passwordChangeCompleted, setPasswordChangeCompleted] = useState(false);
  const [view, setView] = useState<View>(() => readStoredAuthSession() ? (readStoredView() || 'dashboard') : 'login');
  const [timeZonePreference, setTimeZonePreference] = useState(() => authSession?.user.timeZone || '');
  const [timeZoneDrawerOpen, setTimeZoneDrawerOpen] = useState(false);
  const [draftTimeZone, setDraftTimeZone] = useState(() => authSession?.user.timeZone || '');
  const [savingTimeZone, setSavingTimeZone] = useState(false);
  const effectiveTimeZone = timeZonePreference || browserTimeZone;
  userTimeZone = effectiveTimeZone;
  setSharedUserTimeZone(effectiveTimeZone);
  const timeZoneLabel = userTimeZoneLabel(effectiveTimeZone);
  const [loginEmail, setLoginEmail] = useState('');
  const [loginPassword, setLoginPassword] = useState('');
  const [loginPasswordVisible, setLoginPasswordVisible] = useState(false);
  const [loginCaptchaCode, setLoginCaptchaCode] = useState('');
  const [loginCaptcha, setLoginCaptcha] = useState<ApiCaptcha | null>(null);
  const [authConfig, setAuthConfig] = useState<ApiAuthConfig>({ challengeMode: 'image' });
  const [turnstileToken, setTurnstileToken] = useState('');
  const [turnstileRendered, setTurnstileRendered] = useState(false);
  const turnstileRef = useRef<HTMLDivElement | null>(null);
  const turnstileWidget = useRef<string | null>(null);
  const [loginError, setLoginError] = useState('');
  const [loginSubmitting, setLoginSubmitting] = useState(false);
  const [authFlow, setAuthFlow] = useState<AuthFlow>('login');
  const [authMessage, setAuthMessage] = useState('');
  const [passwordResetCompleted, setPasswordResetCompleted] = useState(false);
  const [confirmPassword, setConfirmPassword] = useState('');
  const [resetToken, setResetToken] = useState('');
  const locale: LocaleCode = 'en';
  const [theme, setTheme] = useState<'light' | 'dark'>(() => readStoredAuthSession()?.user.theme === 'dark' ? 'dark' : 'light');
  const [savingTheme, setSavingTheme] = useState(false);
  const [accountMenuOpen, setAccountMenuOpen] = useState(false);
  const [releaseNotesOpen, setReleaseNotesOpen] = useState(false);
  const [releaseNotesUnread, setReleaseNotesUnread] = useState(false);
  const accountMenuRef = useRef<HTMLDivElement | null>(null);
  const releaseNotesAdminAudience = authSession?.user.role === 'admin';

  useLayoutEffect(() => {
    document.documentElement.dataset.theme = view === 'login' || !authSession ? 'dark' : theme;
  }, [authSession, theme, view]);

  useEffect(() => {
    if (!authSession) {
      setProductInfo(null);
      setProductCapabilities({});
      return;
    }
    let cancelled = false;
    void apiGet<ApiProductInfo>('/api/v1/product-info')
      .then(info => {
        if (!cancelled) {
          setProductInfo(info);
          setProductCapabilities(info.capabilities || {});
        }
      })
      .catch(() => {
        if (!cancelled) {
          setProductInfo(null);
          setProductCapabilities({});
        }
      });
    return () => { cancelled = true; };
  }, [authSession]);

  useEffect(() => {
    if (authConfig.challengeMode !== 'turnstile' || !authConfig.turnstileSiteKey || authFlow !== 'login' || view !== 'login' || authSession || passwordChangeCompleted) return;
    let cancelled = false;
    const container = turnstileRef.current;
    let script: HTMLScriptElement | null = null;
    const render = () => {
      if (cancelled || !turnstileRef.current || !window.turnstile || turnstileWidget.current) return;
      const mount = () => { if (cancelled || !container || !container.isConnected || !window.turnstile || turnstileWidget.current) return;
      turnstileWidget.current = window.turnstile.render(container!, {
        language: 'en', size: 'flexible', theme: 'dark',
        sitekey: authConfig.turnstileSiteKey!, callback: token => { if (!cancelled) { setTurnstileRendered(true); setTurnstileToken(token); } },
        'expired-callback': () => { if (!cancelled) setTurnstileToken(''); }, 'error-callback': () => { if (!cancelled) setTurnstileToken(''); },
      });
      // The embedded widget owns its loading UI after rendering begins.
      setTurnstileRendered(true);
      };
      if (!cancelled) (window.turnstile.ready ? window.turnstile.ready(mount) : mount());
    };
    if (!window.turnstile) {
      script = document.querySelector<HTMLScriptElement>('script[data-hcdr-turnstile]');
      if (!script) {
      script = document.createElement('script'); script.dataset.hcdrTurnstile = 'true'; script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'; script.async = false; script.defer = false;
      document.head.appendChild(script);
      }
      script.addEventListener('load', render);
    } else {
      render();
    }
    return () => {
      cancelled = true;
      script?.removeEventListener('load', render);
      const widget = turnstileWidget.current;
      if (widget && window.turnstile?.remove) window.turnstile.remove(widget);
      turnstileWidget.current = null;
      // Turnstile owns an iframe inside the mount node. Clear it after remove()
      // so a remount after logout or a failed login cannot retain a stale frame.
      container?.replaceChildren();
      setTurnstileToken('');
      setTurnstileRendered(false);
    };
  }, [authConfig.challengeMode, authConfig.turnstileSiteKey, authFlow, view, authSession, passwordChangeCompleted]);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      apiGet<ApiAuthConfig>('/api/v1/auth/config'),
      apiGet<{ data?: { enabled?: boolean; configured?: boolean; site_key?: string } }>('/api/v1/auth/turnstile/config'),
    ]).then(([config, turnstile]) => {
      if (cancelled) return;
      const t = turnstile.data;
      setAuthConfig({ ...config, challengeMode: t?.enabled && t?.configured ? 'turnstile' : 'image', turnstileSiteKey: t?.site_key || '' });
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (!authSession) {
      setReleaseNotesUnread(false);
      return;
    }
    setReleaseNotesUnread(hasUnreadReleaseNotes(releaseNotesAdminAudience));
  }, [authSession, releaseNotesAdminAudience]);
  const [clusters, setClusters] = useState<Cluster[]>(initialClusters);
  const [liveClusters, setLiveClusters] = useState<Cluster[] | null>(null);
  const [clusterRegistrationState, setClusterRegistrationState] = useState<'loading' | 'register' | 'default' | 'ready' | 'error'>('loading');
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

  useEffect(() => {
    const token = authSession?.session.token;
    if (!token) return;
    let cancelled = false;
    void apiGet<AuthSession['user']>('/api/v1/auth/me').then(user => {
      if (cancelled) return;
      setAuthSession(current => {
        if (!current || current.session.token !== token) return current;
        const next = { ...current, user };
        writeStoredAuthSession(next);
        return next;
      });
      setTimeZonePreference(user.timeZone || '');
      setDraftTimeZone(user.timeZone || '');
      setTheme(user.theme === 'dark' ? 'dark' : 'light');
    }).catch(() => {
      // The shared API client handles expired sessions. Keep the cached session
      // for transient network failures and refresh it on the next page load.
    });
    return () => { cancelled = true; };
  }, [authSession?.session.token]);
  const [liveApiPolicies, setLiveApiPolicies] = useState<ApiPolicy[]>([]);
  const [liveApiPlans, setLiveApiPlans] = useState<ApiProtectionPlan[]>([]);
  const [liveApiApps, setLiveApiApps] = useState<ApiApplication[]>([]);
  const liveApiPlansRef = useRef<ApiProtectionPlan[]>([]);
  const liveApiAppsRef = useRef<ApiApplication[]>([]);
  const [liveAppTasks, setLiveAppTasks] = useState<Record<string, ApiTask>>({});
  const [liveRecoveryTasks, setLiveRecoveryTasks] = useState<Record<string, ApiTask>>({});
  const [restorePointNamespaceFilter, setRestorePointNamespaceFilter] = useState<string[]>([]);
  const [secondaryCollapsed, setSecondaryCollapsed] = useState(false);
  const [selectedCluster, setSelectedCluster] = useState<Cluster | null>(null);
  const [defaultClusterId, setDefaultClusterId] = useState<string | null>(null);
  const [clusterPickerOpen, setClusterPickerOpen] = useState(false);
  const [clusterMenuId, setClusterMenuId] = useState<string | null>(null);
  const [diagnosticTaskId, setDiagnosticTaskId] = useState('');
  const prefetchedAgentTokenRef = useRef<ApiAgentToken | null>(null);
  const prefetchingAgentTokenRef = useRef<Promise<ApiAgentToken | null> | null>(null);
  const agentTokenOwnerRef = useRef('');
  const refreshInFlightRef = useRef<Promise<Cluster[]> | null>(null);
  const refreshInFlightViewRef = useRef<View | null>(null);
  const refreshLastStartedAtRef = useRef(0);
  const refreshLastResultRef = useRef<Cluster[]>([]);
  const resourceSessionOwnerRef = useRef(authSession?.session.token || '');

  useEffect(() => {
    const owner = authSession?.session.token || '';
    if (!owner) {
      setClusterRegistrationState('loading');
      return;
    }
    let cancelled = false;
    setClusterRegistrationState('loading');
    void apiGet<ApiList<Pick<ApiCluster, 'id' | 'isDefault'>>>('/api/v1/clusters?view=summary')
      .then(response => {
        if (cancelled || resourceSessionOwnerRef.current !== owner) return;
        const items = listItems(response);
        setClusterRegistrationState(items.length === 0
          ? 'register'
          : items.some(cluster => cluster.isDefault) ? 'ready' : 'default');
      })
      .catch(() => {
        if (!cancelled && resourceSessionOwnerRef.current === owner) setClusterRegistrationState('error');
      });
    return () => { cancelled = true; };
  }, [authSession?.session.token]);

  const [appStage, setAppStage] = useState<'select' | 'config' | 'run'>('select');
  const [search, setSearch] = useState('');
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 2600);
    return () => window.clearTimeout(timer);
  }, [toast]);

  useEffect(() => {
    const resetFromURL = new URLSearchParams(window.location.search).get('reset_token');
    if (resetFromURL) {
      setResetToken(resetFromURL); setAuthFlow('reset');
      window.history.replaceState(null, '', window.location.pathname);
    }
  }, []);

  useEffect(() => {
    const preference = authSession?.user.timeZone || '';
    setTimeZonePreference(preference);
    setDraftTimeZone(preference);
  }, [authSession?.user.timeZone]);

  const saveTimeZone = async () => {
    if (!authSession) return;
    setSavingTimeZone(true);
    try {
      const user = await apiPatch<AuthSession['user']>('/api/v1/auth/me', {
        email: authSession.user.email,
        displayName: authSession.user.displayName || '',
        timeZone: draftTimeZone,
      });
      const next = { ...authSession, user };
      setAuthSession(next);
      writeStoredAuthSession(next);
      setTimeZonePreference(user.timeZone || '');
      userTimeZone = user.timeZone || browserTimeZone;
      setSharedUserTimeZone(user.timeZone || browserTimeZone);
      if (liveApiPolicies.length > 0) {
        const remappedPolicies = liveApiPolicies.map(mapPolicy);
        setPolicies(remappedPolicies);
        setLivePolicies(remappedPolicies);
      }
      setTimeZoneDrawerOpen(false);
      setToast(`Time zone changed to ${user.timeZone || browserTimeZone}`);
    } catch (error) {
      setToast(error instanceof Error ? error.message : 'Failed to update time zone');
    } finally {
      setSavingTimeZone(false);
    }
  };

  useEffect(() => {
    if (!accountMenuOpen) return;
    const closeOnOutsideClick = (event: MouseEvent) => {
      if (!accountMenuRef.current?.contains(event.target as Node)) setAccountMenuOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setAccountMenuOpen(false);
    };
    document.addEventListener('mousedown', closeOnOutsideClick);
    document.addEventListener('keydown', closeOnEscape);
    return () => {
      document.removeEventListener('mousedown', closeOnOutsideClick);
      document.removeEventListener('keydown', closeOnEscape);
    };
  }, [accountMenuOpen]);

  useEffect(() => {
    if (!selectedCluster?.id) return;
    try {
      localStorage.setItem(SELECTED_CLUSTER_KEY, selectedCluster.id);
    } catch {
      // Keep the current in-memory selection if localStorage is unavailable.
    }
  }, [selectedCluster?.id]);

  const requestAgentToken = useCallback(async (clusterType: 'native-kubernetes' | 'huaweicloud-cce' | 'openshift' = 'native-kubernetes') => apiPost<ApiAgentToken>('/api/v1/agent-tokens', {
    description: 'cluster registration from console',
    ttlSeconds: 1800,
    clusterType,
  }), []);

  const prefetchAgentToken = useCallback(() => {
    const owner = authSession?.session.token || '';
    if (agentTokenOwnerRef.current !== owner) {
      agentTokenOwnerRef.current = owner;
      prefetchedAgentTokenRef.current = null;
      prefetchingAgentTokenRef.current = null;
    }
    if (!owner) return null;
    if (isAgentTokenUsable(prefetchedAgentTokenRef.current) || prefetchingAgentTokenRef.current) return prefetchingAgentTokenRef.current;
    prefetchedAgentTokenRef.current = null;
    const request = requestAgentToken('native-kubernetes')
      .then(token => {
        if (agentTokenOwnerRef.current === owner) prefetchedAgentTokenRef.current = token;
        return token;
      })
      .catch(() => null)
      .finally(() => {
        if (prefetchingAgentTokenRef.current === request) prefetchingAgentTokenRef.current = null;
      });
    prefetchingAgentTokenRef.current = request;
    return request;
  }, [authSession?.session.token, requestAgentToken]);

  const takePrefetchedAgentToken = useCallback(() => {
    const owner = authSession?.session.token || '';
    if (!owner || agentTokenOwnerRef.current !== owner) {
      agentTokenOwnerRef.current = owner;
      prefetchedAgentTokenRef.current = null;
      prefetchingAgentTokenRef.current = null;
      return null;
    }
    const token = prefetchedAgentTokenRef.current;
    prefetchedAgentTokenRef.current = null;
    return isAgentTokenUsable(token) ? token : null;
  }, [authSession?.session.token]);

  const getAgentTokenForRegistration = useCallback(async (clusterType: 'native-kubernetes' | 'huaweicloud-cce' | 'openshift' = 'native-kubernetes') => {
    if (clusterType !== 'native-kubernetes') {
      const token = await requestAgentToken(clusterType);
      if (!isAgentTokenUsable(token)) throw new Error('agent token is unavailable');
      return token;
    }
    const prefetched = takePrefetchedAgentToken();
    if (prefetched) {
      void prefetchAgentToken();
      return prefetched;
    }
    const token = await (prefetchingAgentTokenRef.current || requestAgentToken('native-kubernetes'));
    if (!isAgentTokenUsable(token)) throw new Error('agent token is unavailable');
    void prefetchAgentToken();
    return token;
  }, [prefetchAgentToken, requestAgentToken, takePrefetchedAgentToken]);

  useEffect(() => {
    if (!authSession || view !== 'clusters' || !clusterRegistrationAllowed) return;
    void prefetchAgentToken();
  }, [authSession, clusterRegistrationAllowed, prefetchAgentToken, view]);

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
    // Preserve high-value messages such as session expiry while refreshing the
    // one-time captcha. Explicit user actions still clear stale login errors.
    void refreshLoginCaptcha(false);
  }, [authSession, refreshLoginCaptcha, view]);

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
    const turnstile = authConfig.challengeMode === 'turnstile';
    if (!loginEmail.trim() || !loginPassword || (turnstile ? !turnstileToken : !loginCaptchaCode.trim())) {
      setLoginError(turnstile
        ? 'Email, password, and human verification are required'
        : 'Email, password, and verification code are required');
      return;
    }
    if (authConfig.challengeMode !== 'turnstile' && !loginCaptcha?.id) {
      setLoginError('Verification code failed to load');
      return;
    }
    setLoginSubmitting(true);
    setLoginError('');
    try {
      const response = await apiPost<ApiLoginResponse>('/api/v1/auth/login', {
        email: loginEmail.trim(),
        password: loginPassword,
        captchaId: loginCaptcha?.id,
        captchaCode: loginCaptchaCode.trim(),
        turnstileToken: turnstileToken,
      });
      const nextSession: AuthSession = {
        ...response,
        signedInAt: new Date().toISOString(),
      };
      setAuthSession(nextSession);
      writeStoredAuthSession(nextSession);
      setTheme(nextSession.user.theme === 'dark' ? 'dark' : 'light');
      clearStoredView();
      if (!nextSession.user.mustChangePassword) {
        writeStoredView('dashboard');
        setView('dashboard');
      }
    } catch (error) {
      setLoginError(error instanceof Error ? error.message : 'Login failed');
      if (authConfig.challengeMode === 'turnstile') {
        setTurnstileToken('');
        if (turnstileWidget.current && window.turnstile?.reset) window.turnstile.reset(turnstileWidget.current);
      } else {
        await refreshLoginCaptcha(false);
      }
    } finally {
      setLoginSubmitting(false);
    }
  }, [authConfig.challengeMode, loginCaptcha?.id, loginCaptchaCode, loginEmail, loginPassword, loginSubmitting, refreshLoginCaptcha, turnstileToken]);

  const applyAuthFlow = useCallback((next: AuthFlow) => {
    setAuthFlow(next); setLoginError(''); setAuthMessage(''); setPasswordResetCompleted(false); setLoginPassword(''); setConfirmPassword(''); setLoginCaptchaCode('');
    if (next === 'login') void refreshLoginCaptcha(false);
  }, [refreshLoginCaptcha]);

  const switchAuthFlow = useCallback((next: AuthFlow) => {
    const url = next === 'login' ? window.location.pathname : `${window.location.pathname}?auth=${next}`;
    window.history.pushState({ authFlow: next }, '', url);
    applyAuthFlow(next);
  }, [applyAuthFlow]);

  useEffect(() => {
    const initialParams = new URLSearchParams(window.location.search);
    const flowFromURL = initialParams.get('auth');
    const tokenFromURL = initialParams.get('reset_token');
    if (tokenFromURL) setResetToken(tokenFromURL);
    if (flowFromURL === 'forgot' || flowFromURL === 'reset') applyAuthFlow(flowFromURL);
    const handleHistoryNavigation = (event: PopStateEvent) => {
      if (authSession) {
        const historyView = event.state?.view as View | undefined;
        if (historyView && isRestorableView(historyView)) {
          writeStoredView(historyView);
          setView(historyView);
          if (historyView === 'applications') setAppStage('select');
          setSearch('');
          setClusterMenuId(null);
        }
        return;
      }
      const queryFlow = new URLSearchParams(window.location.search).get('auth');
      const next = event.state?.authFlow || queryFlow || 'login';
      applyAuthFlow(next === 'forgot' || next === 'reset' ? next : 'login');
    };
    window.addEventListener('popstate', handleHistoryNavigation);
    return () => window.removeEventListener('popstate', handleHistoryNavigation);
  }, [applyAuthFlow, authSession]);

  useEffect(() => {
    if (!authSession || !isRestorableView(view)) return;
    window.history.replaceState({ ...window.history.state, view }, '', window.location.href);
  }, [authSession, view]);

  const submitForgotPassword = useCallback(async () => {
    if (!loginEmail.trim() || loginSubmitting) { if (!loginEmail.trim()) setLoginError('Email is required'); return; }
    if (!ACCOUNT_EMAIL_PATTERN.test(loginEmail.trim())) {
      setLoginError(loginEmail.trim().toLowerCase() === 'admin'
        ? 'The built-in admin account cannot be recovered by email. Contact the system administrator.'
        : 'Enter a valid email address');
      return;
    }
    setLoginSubmitting(true); setLoginError(''); setAuthMessage('');
    try {
      const result = await apiPost<{message: string; resetToken?: string}>('/api/v1/auth/forgot-password', { email: loginEmail.trim() });
      setAuthMessage(result.message);
    } catch (error) { setLoginError(error instanceof Error ? error.message : 'Reset request failed'); }
    finally { setLoginSubmitting(false); }
  }, [loginEmail, loginSubmitting]);

  const submitPasswordReset = useCallback(async () => {
    if (!resetToken.trim() || !loginPassword) { setLoginError('Reset token and new password are required'); return; }
    if (loginPassword !== confirmPassword) { setLoginError('Passwords do not match'); return; }
    setLoginSubmitting(true); setLoginError('');
    try {
      await apiPost<{message: string}>('/api/v1/auth/reset-password', { token: resetToken.trim(), password: loginPassword });
      setLoginPassword('');
      setConfirmPassword('');
      setPasswordResetCompleted(true);
    }
    catch (error) { setLoginError(error instanceof Error ? error.message : 'Password reset failed'); }
    finally { setLoginSubmitting(false); }
  }, [confirmPassword, loginPassword, resetToken, switchAuthFlow]);

  const clearTenantResourceState = useCallback(() => {
    refreshInFlightRef.current = null;
    refreshInFlightViewRef.current = null;
    refreshLastStartedAtRef.current = 0;
    refreshLastResultRef.current = [];
    prefetchedAgentTokenRef.current = null;
    prefetchingAgentTokenRef.current = null;
    agentTokenOwnerRef.current = '';
    setClusters([]);
    setLiveClusters(null);
    setStorage([]);
    setLiveStorage(null);
    setPolicies([]);
    setLivePolicies(null);
    setTags([]);
    setRestorePointCount(0);
    setLiveRestorePoints([]);
    setLiveApiClusters([]);
    setLiveApiStorageRepos([]);
    setLiveApiTasks([]);
    setLiveApiRestorePointViews([]);
    setLiveApiRestorePoints([]);
    setLiveApiPolicies([]);
    setLiveApiPlans([]);
    setLiveApiApps([]);
    setLiveAppTasks({});
    setLiveRecoveryTasks({});
    setSelectedCluster(null);
    setDefaultClusterId(null);
  }, []);

  useEffect(() => {
    const owner = authSession?.session.token || '';
    if (resourceSessionOwnerRef.current === owner) return;
    resourceSessionOwnerRef.current = owner;
    clearTenantResourceState();
  }, [authSession?.session.token, clearTenantResourceState]);

  const signOut = useCallback(() => {
    void apiPost('/api/v1/auth/logout', {}).catch(() => undefined);
    setAccountMenuOpen(false);
    resourceSessionOwnerRef.current = '';
    clearTenantResourceState();
    setAuthSession(null);
    clearStoredAuthSession();
    clearStoredView();
    setLoginPassword('');
    setLoginCaptchaCode('');
    setLoginError('');
    setView('login');
  }, [clearTenantResourceState]);

  const saveTheme = async (nextTheme: 'light' | 'dark') => {
    if (!authSession || savingTheme || theme === nextTheme) return;
    setSavingTheme(true);
    try {
      const user = await apiPatch<AuthSession['user']>('/api/v1/auth/me/theme', { theme: nextTheme });
      const nextSession = { ...authSession, user };
      setAuthSession(nextSession);
      writeStoredAuthSession(nextSession);
      setTheme(user.theme === 'dark' ? 'dark' : 'light');
      setAccountMenuOpen(false);
    } catch (error) {
      setToast(error instanceof Error ? error.message : 'Could not save appearance preference');
    } finally {
      setSavingTheme(false);
    }
  };

  useEffect(() => {
    const expireSession = () => {
      clearStoredAuthSession();
      setAccountMenuOpen(false);
      resourceSessionOwnerRef.current = '';
      clearTenantResourceState();
      setAuthSession(null);
      setLoginPassword('');
      setLoginCaptchaCode('');
      setLoginError('Your session has expired. Sign in again.');
      setView('login');
    };
    window.addEventListener(AUTH_EXPIRED_EVENT, expireSession);
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, expireSession);
  }, [clearTenantResourceState]);

  const [clusterTaskLogs, setClusterTaskLogs] = useState<Record<string, ClusterTaskLog[]>>({});
  const [activeClusterTaskIds, setActiveClusterTaskIds] = useState<Set<string>>(new Set());
  const clusterTaskLogsRef = useRef(clusterTaskLogs);
  clusterTaskLogsRef.current = clusterTaskLogs;

  useEffect(() => {
    if (!authSession || view !== 'clusters') {
      setActiveClusterTaskIds(new Set());
      return;
    }
    let cancelled = false;
    const loadTasks = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        const res = await apiGet<ApiList<ApiTask>>('/api/v1/tasks?view=summary&types=register,unregister,agent-upgrade,velero-upgrade&limit=100');
        const tasks = listItems(res);
        const clusterTasks = tasks
          .filter(task => ['register', 'unregister', 'agent-upgrade', 'velero-upgrade'].includes(task.type))
          .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''));
        if (cancelled) return;
        setClusterTaskLogs(prev => {
          const next: Record<string, ClusterTaskLog[]> = {};
          for (const task of clusterTasks) {
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
  }, [authSession, view]);

  useEffect(() => {
    if (!authSession || view === 'login') return;
    const ids = Array.from(activeClusterTaskIds);
    if (ids.length === 0) return;
    let cancelled = false;
    const loadEvents = async () => {
      if (document.visibilityState === 'hidden') return;
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
  }, [activeClusterTaskIds, authSession, view]);

  const refreshPlatformData = useCallback((targetView: View = view) => {
    const owner = authSession?.session.token || '';
    if (!owner || resourceSessionOwnerRef.current !== owner) return Promise.resolve([]);
    const now = Date.now();
    if (refreshInFlightRef.current && refreshInFlightViewRef.current === targetView) return refreshInFlightRef.current;
    if (refreshInFlightViewRef.current === targetView && refreshLastResultRef.current.length > 0 && now - refreshLastStartedAtRef.current < 1200) {
      return Promise.resolve(refreshLastResultRef.current);
    }
    refreshLastStartedAtRef.current = now;
    const request = (async () => {
      if (targetView === 'clusters' || targetView === 'dr_tasks' || targetView === 'restore_points' || targetView === 'failback') {
        const clusterRes = await apiGet<ApiList<ApiCluster>>('/api/v1/clusters');
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, []));
        setLiveApiClusters(apiClusters);
        // A cluster-only poll must not discard applications loaded on demand
        // for the open DR topology. Otherwise relationships briefly appear and
        // disappear again on the next 10-second cluster refresh.
        setLiveClusters(previous => apiClusters.map(cluster => mapCluster(
          cluster,
          previous?.find(item => item.id === cluster.id)?.apps || [],
        )));
        setClusters(previous => apiClusters.map(cluster => mapCluster(
          cluster,
          previous.find(item => item.id === cluster.id)?.apps || [],
        )));
        return nextClusters;
      }
      if (targetView === 'storage') {
        const [clusterRes, storageRes] = await Promise.all([
          apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
          apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
        ]);
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, []));
        const apiStorage = listItems(storageRes);
        const nextStorage = apiStorage.map(mapStorageRepo);
        setLiveApiClusters(apiClusters); setLiveClusters(nextClusters); setClusters(nextClusters);
        setLiveApiStorageRepos(apiStorage); setLiveStorage(nextStorage); setStorage(nextStorage);
        return nextClusters;
      }
      if (targetView === 'policies') {
        const policyRes = await apiGet<ApiList<ApiPolicy>>('/api/v1/policies');
        if (resourceSessionOwnerRef.current !== owner) return [];
        const nextPolicies = listItems(policyRes).map(mapPolicy);
        setLiveApiPolicies(listItems(policyRes)); setLivePolicies(nextPolicies); setPolicies(nextPolicies);
        return refreshLastResultRef.current;
      }
      if (targetView === 'tags') {
        const [clusterRes, appRes, tagRes] = await Promise.all([
          apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
          apiGet<ApiList<ApiApplication>>('/api/v1/applications?view=summary'),
          apiGet<ApiList<TagItem>>('/api/v1/tags'),
        ]);
        if (resourceSessionOwnerRef.current !== owner) return [];
        const apiClusters = listItems(clusterRes); const apiApps = listItems(appRes);
        const nextClusters = apiClusters.map(cluster => mapCluster(cluster, mapApps(apiApps.filter(app => app.clusterId === cluster.id), [], [], [], apiClusters)));
        setTags(listItems(tagRes)); setLiveApiApps(apiApps); setLiveApiClusters(apiClusters); setLiveClusters(nextClusters); setClusters(nextClusters);
        return nextClusters;
      }
      const clusterRequest = apiGet<ApiList<ApiCluster>>('/api/v1/clusters');
      void clusterRequest.then(clusterRes => {
        if (resourceSessionOwnerRef.current !== owner) return;
        const apiClusters = listItems(clusterRes);
        setLiveApiClusters(apiClusters);
        setLiveClusters(previous => apiClusters.map(cluster => mapCluster(
          cluster,
          previous?.find(item => item.id === cluster.id)?.apps || [],
        )));
      }).catch(() => {
        // The complete refresh below keeps the previously rendered data visible.
      });
      const [clusterRes, appRes, storageRes, policyRes, planRes, taskRes, tagRes, restorePointRes] = await Promise.all([
        clusterRequest,
        apiGet<ApiList<ApiApplication>>('/api/v1/applications'),
        apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories'),
        apiGet<ApiList<ApiPolicy>>('/api/v1/policies'),
        apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
        apiGet<ApiList<ApiTask>>('/api/v1/tasks?view=summary&types=backup,restore,drill,takeover,storage-sync,schedule-sync,protection-cleanup&limit=500'),
        apiGet<ApiList<TagItem>>('/api/v1/tags'),
        apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points?view=summary&pageSize=500'),
      ]);
      if (resourceSessionOwnerRef.current !== owner) return [];
      const apiClusters = listItems(clusterRes);
      const apiApps = listItems(appRes);
      const apiStorage = listItems(storageRes);
      const apiPolicies = listItems(policyRes);
      const apiPlans = listItems(planRes);
      const apiRestorePoints = listItems(restorePointRes).map(mapRestorePoint);
      const apiTasks = await includePointedTasks(listItems(taskRes), apiPlans);
      setTags(listItems(tagRes));
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
        setLiveAppTasks(previous => mergeTaskMapKeepingActive(previous, nextAppTasks));
        setLiveRecoveryTasks(previous => mergeTaskMapKeepingActive(previous, nextRecoveryTasks));
        setSelectedCluster(prev => {
          if (prev && nextClusters.some(cluster => cluster.id === prev.id)) {
            return nextClusters.find(cluster => cluster.id === prev.id) || nextClusters[0] || null;
          }
          let storedSelectedId = '';
          try {
            storedSelectedId = localStorage.getItem(SELECTED_CLUSTER_KEY) || '';
          } catch {
            // localStorage may be unavailable in private contexts.
          }
          const apiDefault = nextClusters.find(cluster => cluster.isDefault);
          return nextClusters.find(cluster => cluster.id === storedSelectedId)
            || apiDefault
            || nextClusters[0]
            || null;
        });
        setDefaultClusterId(() => {
          const apiDefault = nextClusters.find(cluster => cluster.isDefault);
          return apiDefault?.id || null;
        });
      }
      return nextClusters;
    })().finally(() => {
      if (refreshInFlightRef.current === request) refreshInFlightRef.current = null;
      if (refreshInFlightViewRef.current === targetView) refreshInFlightViewRef.current = null;
    });
    refreshInFlightRef.current = request;
    refreshInFlightViewRef.current = targetView;
    return request;
  }, [authSession?.session.token, view]);

  const loadClusterTopology = useCallback(async () => {
    const [clusterRes, appRes, planRes] = await Promise.all([
      apiGet<ApiList<ApiCluster>>('/api/v1/clusters'),
      apiGet<ApiList<ApiApplication>>('/api/v1/applications?view=summary'),
      apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
    ]);
    const apiClusters = listItems(clusterRes);
    const apiApps = listItems(appRes);
    const apiPlans = listItems(planRes);
    const nextClusters = apiClusters.map(cluster => mapCluster(cluster, mapApps(
      apiApps.filter(app => app.clusterId === cluster.id), apiPlans, [], [], apiClusters,
    )));
    setLiveApiClusters(apiClusters);
    setLiveApiApps(apiApps);
    setLiveApiPlans(apiPlans);
    setLiveClusters(nextClusters);
    setClusters(nextClusters);
  }, []);

  useEffect(() => {
    liveApiAppsRef.current = liveApiApps;
  }, [liveApiApps]);
  useEffect(() => {
    liveApiPlansRef.current = liveApiPlans;
  }, [liveApiPlans]);

  useEffect(() => {
    if (!authSession || !PLATFORM_DATA_VIEWS.has(view)) return;
    let cancelled = false;
    const realtimeViews = new Set<View>(['dashboard', 'applications', 'clusters']);
    const loadPlatformData = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        await refreshPlatformData(view);
      } catch {
        if (!cancelled) {
          // Keep the current state visible if the backend is temporarily unavailable.
        }
      }
    };
    const loadApplicationActivity = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        // Task identity is owned by the persisted plan pointers. A recovery
        // can be submitted by another browser or directly through the API, so
        // poll the lightweight plan list in the same snapshot as tasks. Using
        // a stale latestRecoveryTaskId makes the row keep rendering the prior
        // drill until a full-page refresh.
        const [taskRes, restorePointRes, planRes] = await Promise.all([
          apiGet<ApiList<ApiTask>>('/api/v1/tasks?view=summary&types=backup,restore,drill,takeover,storage-sync,schedule-sync,protection-cleanup&limit=500'),
          apiGet<ApiList<ApiRestorePoint>>('/api/v1/restore-points?view=summary&pageSize=500'),
          apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans'),
        ]);
        if (cancelled) return;
        const apiPlans = listItems(planRes);
        const apiTasks = await includePointedTasks(listItems(taskRes), apiPlans);
        const apiRestorePoints = listItems(restorePointRes);
        const apiRestorePointViews = apiRestorePoints.map(mapRestorePoint);
        setLiveApiTasks(apiTasks);
        setLiveApiPlans(apiPlans);
        const nextAppTasks = buildAppTaskMap(apiTasks, liveApiAppsRef.current, ['backup'], apiRestorePointViews, apiPlans);
        const nextRecoveryTasks = buildAppTaskMap(apiTasks, liveApiAppsRef.current, ['restore', 'drill', 'takeover'], apiRestorePointViews, apiPlans);
        setLiveAppTasks(previous => mergeTaskMapKeepingActive(previous, nextAppTasks));
        setLiveRecoveryTasks(previous => mergeTaskMapKeepingActive(previous, nextRecoveryTasks));
        setRestorePointCount(apiRestorePointViews.length);
        setLiveRestorePoints(apiRestorePointViews);
        setLiveApiRestorePointViews(apiRestorePointViews);
        setLiveApiRestorePoints(apiRestorePoints);
      } catch {
        // Keep the current application state visible if a status poll fails.
      }
    };
    loadPlatformData();
    const shouldPoll = realtimeViews.has(view);
    const pollIntervalMs = view === 'applications' ? 3000 : 10000;
    const poll = view === 'applications' ? loadApplicationActivity : loadPlatformData;
    const timer = shouldPoll ? window.setInterval(poll, pollIntervalMs) : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [authSession, refreshPlatformData, view]);

  const visibleExtensionModules = useMemo(() => extensionModules.filter(module => authSession && (!module.isVisible || module.isVisible({ currentUser: authSession.user, capabilities: productCapabilities }))), [authSession, extensionModules, productCapabilities]);
  const activeExtension = visibleExtensionModules.find(module => module.view === view);
  const activeModule = activeExtension?.navigation.group === 'operations' ? 'ops' : activeExtension ? 'settings' : moduleForView(view);
  const language = locales[locale];
  const defaultCluster = useMemo(
    () => clusters.find(cluster => cluster.isDefault) || null,
    [clusters],
  );
  const defaultWorkspaceCluster = defaultCluster || clusters[0] || null;
  const workspaceCluster = selectedCluster || defaultWorkspaceCluster;
  const dashboardCluster = workspaceCluster;
  const dashboardApps = dashboardCluster?.apps || [];
  const protectedApps = dashboardApps.filter(app => app.isProtected).length;
  const activeRestorePointCount = dashboardCluster ? restorePointCount : 0;
  const drClusters = liveClusters ?? clusters;
  const drSelectedCluster = selectedCluster ? drClusters.find(cluster => cluster.id === selectedCluster.id) || null : null;
  const drDefaultCluster = drClusters.find(cluster => cluster.isDefault) || null;
  const drWorkspaceCluster = liveClusters
    ? (drSelectedCluster || drDefaultCluster)
    : workspaceCluster;
  const drActiveApps = drWorkspaceCluster?.apps || [];
  const drStorage = liveStorage ?? storage;
  const drPolicies = livePolicies ?? policies;
  const onboarding: 'register' | 'default' | 'ready' | 'loading' = liveClusters === null
    ? (clusterRegistrationState === 'error' ? 'loading' : clusterRegistrationState)
    : liveClusters.length === 0
      ? 'register'
      : (liveClusters.some(cluster => cluster.isDefault) ? 'ready' : 'default');
  const onboardingMessage = onboarding === 'register'
    ? 'Register a cluster before using this section'
    : onboarding === 'default'
      ? 'Set a default cluster before using this section'
      : '';
  const navBlockedViews: Set<View> = new Set<View>([
    'applications', 'failback', 'storage', 'policies', 'restore_points', 'dr_tasks', 'tags',
  ]);


  const secondaryNav = useMemo(() => {
    if (view === 'profile') return null;
    if (activeModule === 'overview') return null;
    if (activeModule === 'dr') {
      return {
        title: 'DR',
        items: [
          { label: 'Application DR', desc: 'Select applications, configure policies, and start DR', view: 'applications' as View, icon: Layers },
          { label: 'Restore Points', desc: 'Browse recovery points and launch drill or takeover', view: 'restore_points' as View, icon: DatabaseBackup },
          { label: 'Backup & Recovery Tasks', desc: 'Audit backup, drill, takeover, and restore task records', view: 'dr_tasks' as View, icon: ListChecks },
        ],
      };
    }
    if (activeModule === 'config') {
      return {
        title: 'Configuration',
        items: [
          { label: 'Clusters', desc: 'Registration, default cluster, and agent status', view: 'clusters' as View, icon: Server },
          { label: 'Storage', desc: 'Maintain shared restore-point repositories across clusters', view: 'storage' as View, icon: HardDrive },
          { label: 'Policies', desc: 'Maintain application protection plans and recovery targets', view: 'policies' as View, icon: ShieldCheck },
          { label: 'Tag Management', desc: 'Create tags and reuse them in DR lists', view: 'tags' as View, icon: Tags },
        ],
      };
    }
    if (activeModule === 'ops') {
      return {
        title: 'Operations',
        items: [
          { label: 'Operations Center', desc: 'Platform health, active operations, and current issues', view: 'operations' as View, icon: Activity },
          ...(!hasEnterpriseAuditModule && !productCapabilities.advancedAudit?.enabled ? [{ label: 'Activity Log', desc: 'Review administrator actions and results', view: 'activity' as View, icon: ClipboardList }] : []),
          ...visibleExtensionModules.filter(module => module.navigation.group === 'operations').map(module => ({ label: module.navigation.label, desc: module.navigation.description, view: module.view as View, icon: module.navigation.icon })),
          { label: 'Diagnostic Logs', desc: 'Search platform and managed-cluster logs', view: 'logs' as View, icon: Terminal },
          { label: 'Support Bundle', desc: 'Collect a comprehensive troubleshooting package', view: 'support_bundle' as View, icon: FileArchive },
        ],
      };
    }
    return {
      title: 'Settings',
      items: productCapabilities.advancedIdentity?.enabled
        ? [
            ...visibleExtensionModules.filter(module => module.navigation.group === 'settings').map(module => ({ label: module.navigation.label, desc: module.navigation.description, view: module.view as View, icon: module.navigation.icon })),
            ...(authSession?.user.systemAdmin ? [{ label: 'Platform Upgrade', desc: 'Check releases and perform a manual blue-green upgrade', view: 'upgrades' as View, icon: ArrowUpCircle }, { label: 'Email Settings', desc: 'Configure password recovery email delivery', view: 'email_settings' as View, icon: Mail }] : []),
          ]
        : [
            ...(authSession?.user.systemAdmin ? [{ label: 'User Management', desc: 'Manage the built-in Community administrator', view: 'users' as View, icon: User }] : []),
            ...visibleExtensionModules.filter(module => module.navigation.group === 'settings').map(module => ({ label: module.navigation.label, desc: module.navigation.description, view: module.view as View, icon: module.navigation.icon })),
            ...(authSession?.user.systemAdmin ? [{ label: 'Platform Upgrade', desc: 'Check releases and perform a manual blue-green upgrade', view: 'upgrades' as View, icon: ArrowUpCircle }, { label: 'Email Settings', desc: 'Configure password recovery email delivery', view: 'email_settings' as View, icon: Mail }] : []),
          ],
    };
  }, [activeModule, authSession?.user.systemAdmin, hasEnterpriseAuditModule, productCapabilities.advancedAudit?.enabled, productCapabilities.advancedIdentity?.enabled, visibleExtensionModules, view]);

  const openView = (nextView: View, options: { preserveSelectedCluster?: boolean; diagnosticTaskId?: string } = {}) => {
    if (!options.preserveSelectedCluster && (nextView === 'dashboard' || nextView === 'applications' || nextView === 'failback')) {
      const target = defaultWorkspaceCluster;
      if (target) setSelectedCluster(target);
    }
    if (nextView !== view) {
      window.history.pushState({ ...window.history.state, view: nextView }, '', window.location.href);
    }
    writeStoredView(nextView);
    setView(nextView);
    setDiagnosticTaskId(nextView === 'logs' ? options.diagnosticTaskId || '' : '');
    if (nextView === 'applications') {
      setAppStage('select');
    }
    setSearch('');
    setClusterMenuId(null);
  };

  const setDefaultCluster = async (cluster: Cluster, event?: React.MouseEvent) => {
    event?.stopPropagation();
    try {
      const updated = await apiPost<ApiCluster>(`/api/v1/clusters/${cluster.id}/default`, {});
      setDefaultClusterId(updated.isDefault ? updated.id : null);
      setSelectedCluster(prev => prev?.id === updated.id ? { ...prev, isDefault: updated.isDefault } : cluster);
      await refreshPlatformData();
      localStorage.setItem(SELECTED_CLUSTER_KEY, cluster.id);
    } catch {
      setToast('Failed to persist default cluster');
    }
  };

  const unregisterCluster = async (cluster: Cluster, event?: React.MouseEvent, deleteBackupData = false): Promise<ApiTask> => {
    event?.stopPropagation();
    const result = await apiPost<ApiTaskResponse>(`/api/v1/clusters/${cluster.id}/unregister`, {
      deleteVelero: true,
      deleteNamespace: true,
      deleteBackupData,
      reason: 'requested from platform cluster page',
    });
    const task = 'task' in result ? result.task : result;
    const warning = 'warning' in result ? result.warning : undefined;
    setToast(warning || `${cluster.name} unregister task dispatched`);
    return task;
  };

  const updateAppTags = (clusterId: string | null, appNames: string[], updater: (currentTags: string[]) => string[]) => {
    if (!clusterId || appNames.length === 0) return;
	const targets = liveApiApps.filter(app => app.clusterId === clusterId && appNames.includes(app.namespace || app.name));
	void Promise.all(targets.map(app => apiPut(`/api/v1/applications/${app.id}/tags`, { tagIds: updater(app.tags || []) }))).catch(error => {
	  setToast(error instanceof Error ? error.message : 'Failed to update application tags');
	  void refreshPlatformData();
	});
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

  if (passwordChangeCompleted) {
    return <PasswordChangeSuccess onContinue={() => { setPasswordChangeCompleted(false); setView('login'); }} />;
  }

  if (authSession?.user.mustChangePassword) {
    return <RequiredPasswordChange session={authSession} onChanged={() => { clearStoredAuthSession(); setAuthSession(null); setLoginPassword(''); setLoginCaptchaCode(''); setPasswordChangeCompleted(true); setView('login'); }} onSignOut={signOut} />;
  }

  if (view === 'login' && authFlow !== 'login') {
    return (
      <PasswordRecoveryPage
        flow={authFlow}
        email={loginEmail}
        setEmail={setLoginEmail}
        password={loginPassword}
        setPassword={setLoginPassword}
        confirmation={confirmPassword}
        setConfirmation={setConfirmPassword}
        resetToken={resetToken}
        error={loginError}
        message={authMessage}
        busy={loginSubmitting}
        completed={passwordResetCompleted}
        onForgot={() => void submitForgotPassword()}
        onReset={() => void submitPasswordReset()}
        onBack={() => switchAuthFlow('login')}
      />
    );
  }

  if (view === 'login') {
    return (
      <div className="premium-login min-h-screen">
        <div className="relative flex min-h-screen items-center justify-center px-10">
          <div className="w-full max-w-md">
            <div className="mb-8">
              <div className="flex items-center justify-between gap-3">
              <div className="hbdr-login-top-brand" aria-label="HyperCDR">
                <span className="hbdr-login-brand-one">Hyper</span>
                <span className="hbdr-login-brand-pro">CDR</span>
              </div>
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
                <span>{authFlow === 'login' ? 'Welcome to HyperCDR' : authFlow === 'forgot' ? 'Forgot your password?' : 'Set a new password'}</span>
                <LanguageIndicator locale={locale} compact />
              </h2>
              {authFlow !== 'login' && <p className="hbdr-auth-description">{authFlow === 'forgot' ? 'Enter your registered email address and we will send a reset link.' : 'Choose a new password for your account.'}</p>}
              <div className="hbdr-login-form grid gap-4">
                <label className="hbdr-login-field">
                  <User size={15} />
                  <input
                    placeholder="Email Address"
                    value={loginEmail}
                    onChange={event => setLoginEmail(event.target.value)}
                    autoComplete="username"
                    autoFocus={authFlow === 'login'}
                  />
                </label>
                {(authFlow === 'login' || authFlow === 'reset') && <div className="hbdr-login-field hbdr-login-password-field">
                  <ShieldCheck size={15} />
                  <input
                    placeholder={authFlow === 'reset' ? 'New Password' : 'Password'}
                    type={loginPasswordVisible ? 'text' : 'password'}
                    value={loginPassword}
                    onChange={event => setLoginPassword(event.target.value)}
                    onKeyDown={event => {
                      if (event.key === 'Enter') void (authFlow === 'reset' ? submitPasswordReset() : submitLogin());
                    }}
                    autoComplete={authFlow === 'reset' ? 'new-password' : 'current-password'}
                  />
                  <button
                    type="button"
                    tabIndex={-1}
                    className="hbdr-login-eye"
                    aria-label={loginPasswordVisible ? 'Hide password' : 'Show password'}
                    title={loginPasswordVisible ? 'Hide password' : 'Show password'}
                    onClick={() => setLoginPasswordVisible(visible => !visible)}
                  >
                    {loginPasswordVisible ? <EyeOff size={15} /> : <Eye size={15} />}
                  </button>
                </div>}
                {authFlow === 'reset' && <label className="hbdr-login-field">
                  <ShieldCheck size={15} />
                  <input placeholder="Confirm Password" type="password" value={confirmPassword} onChange={event => setConfirmPassword(event.target.value)} autoComplete="new-password" />
                </label>}
              </div>
              {authFlow === 'reset' && !resetToken && <div className="hbdr-login-error">This password reset link is incomplete. Request a new link and try again.</div>}
              {authFlow === 'login' && authConfig.challengeMode === 'turnstile' && <div className="hbdr-login-turnstile" aria-label="Cloudflare human verification">
                <div ref={turnstileRef} />
              </div>}
              {authFlow === 'login' && authConfig.challengeMode !== 'turnstile' && <div className="hbdr-login-captcha-code" aria-label="Verification code">
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
                <button type="button" tabIndex={-1} className="hbdr-login-captcha-image" aria-label="Refresh verification code" title="Refresh verification code" onClick={() => void refreshLoginCaptcha()}>
                  {loginCaptcha?.image ? <img src={loginCaptcha.image} alt="Verification code" /> : <span>----</span>}
                </button>
              </div>}
              {loginError && <div className="hbdr-login-error">{loginError}</div>}
              {authMessage && <div className="hbdr-auth-success">{authMessage}</div>}
              <button
                className="w-full bg-blue-600 text-white"
                disabled={loginSubmitting}
                onClick={() => void (authFlow === 'login' ? submitLogin() : authFlow === 'forgot' ? submitForgotPassword() : submitPasswordReset())}
              >
                {loginSubmitting ? 'Please wait...' : authFlow === 'login' ? 'Sign In' : authFlow === 'forgot' ? 'Send Reset Instructions' : 'Update Password'}
              </button>
              {authFlow === 'login' ? <>
                <div className="hbdr-login-forgot"><button type="button" onClick={() => switchAuthFlow('forgot')}>Forgot password?</button></div>
                <p className="hbdr-login-eula">By continuing, you agree to our Terms and Privacy Policy.</p>
              </> : <div className="hbdr-login-signup hbdr-auth-back">Already have an account? <button type="button" onClick={() => switchAuthFlow('login')}>Back to Sign In</button></div>}
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
      <aside className={`premium-sidebar w-64 bg-white border-r border-slate-200 flex flex-col ${accountMenuOpen ? 'z-[200]' : 'z-20'}`}>
        <div>
          <div>
            <div><HyperCDRLogoMark className="h-7 w-7" /></div>
            <h1>HyperCDR</h1>
          </div>
          <nav>
            {topNav.filter(item => item.key !== 'settings' || authSession?.user.role === 'admin').map(item => {
              const requiresOnboarding = ['dr', 'ops', 'config'].includes(item.key) && onboarding !== 'ready';
              const isConfig = item.key === 'config';
              const showBadge = isConfig && onboarding === 'register';
              const showCounter = isConfig && onboarding === 'default';
              return (
                <button
                  key={item.key}
                  onClick={() => openView(item.key === 'settings'
                    ? (authSession?.user.systemAdmin
                      ? (productCapabilities.advancedIdentity?.enabled
                        ? (visibleExtensionModules.find(module => module.navigation.group === 'settings')?.view as View | undefined) || 'users'
                        : 'users')
                      : (visibleExtensionModules.find(module => module.navigation.group === 'settings')?.view as View | undefined) || 'profile')
                    : item.view)}
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
          <button type="button" onClick={() => { setDraftTimeZone(timeZonePreference); setTimeZoneDrawerOpen(true); }} className="hbdr-timezone-button hbdr-top-tooltip" data-tooltip={`Timezone · ${timeZoneLabel}`} aria-label={`Current timezone: ${timeZoneLabel}`}><span className="hbdr-timezone-short">{timeZoneOptionLabel(userTimeZone).match(/\(([^)]+)\)/)?.[1] || 'UTC+00:00'}</span></button>
          <span className="hbdr-top-tooltip hbdr-language-tooltip" data-tooltip="Interface language: English">
            <LanguageIndicator locale={locale} compact />
          </span>
          <div className="hbdr-account" ref={accountMenuRef}>
            <button
              type="button"
              className={`hbdr-top-user ${accountMenuOpen ? 'is-open' : ''}`}
              onClick={() => setAccountMenuOpen(open => !open)}
              aria-label="Open account menu"
              aria-haspopup="menu"
              aria-expanded={accountMenuOpen}
            >
              <span className="hbdr-top-user-avatar"><User size={15} /></span>
              <span>{authSession?.user.email || 'admin'}</span>
              <ChevronDown size={13} />
            </button>
            {accountMenuOpen && (
              <div className="hbdr-account-menu" role="menu">
                <div className="hbdr-account-summary">
                  <span><User size={18} /></span>
                  <div>
                    <strong>{authSession?.user.email || 'admin'}</strong>
                    <small>{authSession?.user.role || 'User'}</small>
                  </div>
                </div>
                <button type="button" role="menuitem" onClick={() => { setAccountMenuOpen(false); openView('profile'); }}>
                  <Settings size={16} />
                  <span>Basic Information</span>
                </button>
                <div className="hbdr-account-theme-options" role="group" aria-label="Appearance">
                  <span className="hbdr-account-theme-label">Appearance</span>
                  <div>
                    <button type="button" role="menuitemradio" aria-checked={theme === 'light'} disabled={savingTheme} onClick={() => void saveTheme('light')}><Sun size={14} />Light{theme === 'light' && <Check size={13} />}</button>
                    <button type="button" role="menuitemradio" aria-checked={theme === 'dark'} disabled={savingTheme} onClick={() => void saveTheme('dark')}><Moon size={14} />Dark{theme === 'dark' && <Check size={13} />}</button>
                  </div>
                </div>
                <button type="button" role="menuitem" onClick={() => {
                  setAccountMenuOpen(false);
                  setReleaseNotesOpen(true);
                  markReleaseNotesViewed(releaseNotesAdminAudience);
                  setReleaseNotesUnread(false);
                }}>
                  <Bell size={16} />
                  <span>Release notes</span>
                  {releaseNotesUnread && <i className="hbdr-release-notes-unread" aria-label="Unread release notes" />}
                </button>
                <button type="button" role="menuitem" className="hbdr-account-signout" onClick={signOut}>
                  <LogOut size={16} />
                  <span>Sign out</span>
                </button>
              </div>
            )}
          </div>
        </div>
      </aside>

      <main className="flex flex-1 overflow-hidden">
        {secondaryNav && (
          <aside className={`hbdr-secondary-sidebar ${activeModule === 'dr' ? 'hbdr-dr-sidebar' : ''} ${secondaryCollapsed ? 'is-collapsed' : ''}`}>
            <div className="hbdr-secondary-title">
              <span>{activeModule === 'dr' ? 'Disaster Recovery' : language.secondaryTitles[activeModule]}</span>
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
                const availableBeforeClusterRegistration = onboarding === 'register' && (item.view === 'storage' || item.view === 'policies');
                const blocked = navBlockedViews.has(item.view) && onboarding !== 'ready' && item.view !== 'clusters' && !availableBeforeClusterRegistration;
                const disabled = blocked;
                return (
                  <button
                    key={item.view}
                    data-view={item.view}
                    onClick={() => { if (disabled) { setToast(onboardingMessage); openView('clusters'); return; } openView(item.view); }}
                    disabled={disabled}
                    title={disabled ? onboardingMessage : item.label}
                    className={`${view === item.view ? 'hbdr-secondary-active' : ''} ${disabled ? 'cursor-not-allowed opacity-50 hover:bg-transparent' : ''}`}
                  >
                    <span className="hbdr-secondary-icon" aria-hidden="true"><item.icon size={16} strokeWidth={1.8} /></span>
                    <span>
                      <strong>{item.label}</strong>
                      <small>{item.desc}</small>
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
                <React.Suspense fallback={<PageLoadFallback />}><LazyOverviewPage
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
                  productInfo={productInfo}
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
                /></React.Suspense>
              </motion.div>
            )}

            {view === 'applications' && (onboarding !== 'ready' ? onboardingGate : (
              <motion.div key="applications" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-app-page">
                <PageTitleBar>
                  <div className="min-w-0">
                    <h3 className="hbdr-app-workspace-title">Application DR</h3>
                    <p className="hbdr-app-workspace-desc">Select applications, configure policies, and start DR.</p>
                  </div>
                  <div className="hbdr-dashboard-design-cluster">
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
                </PageTitleBar>
                <React.Suspense fallback={<PageLoadFallback />}><LazyApplicationDrPage
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
                  platformTasks={liveApiTasks}
                /></React.Suspense>
              </motion.div>
            ))}

            {view === 'failback' && (onboarding !== 'ready' ? onboardingGate : <React.Suspense fallback={<PageLoadFallback />}><FailbackPage toast={setToast} /></React.Suspense>)}
            {view === 'clusters' && (
              <React.Suspense fallback={<PageLoadFallback />}><LazyClusterPage
                clusters={liveClusters ?? clusters}
                loading={liveClusters === null}
                protectionPlans={liveApiPlans}
                onLoadTopology={loadClusterTopology}
                canUpgrade={authSession?.user.role === 'admin'}
                defaultClusterId={defaultClusterId}
                clusterMenuId={clusterMenuId}
                setClusterMenuId={setClusterMenuId}
                setSelectedCluster={setSelectedCluster}
                setDefaultCluster={setDefaultCluster}
                unregisterCluster={unregisterCluster}
                onRenameCluster={(clusterId, name) => {
                  const patchCluster = (cluster: Cluster) => cluster.id === clusterId ? { ...cluster, name } : cluster;
                  setClusters(prev => prev.map(patchCluster));
                  setLiveClusters(prev => prev ? prev.map(patchCluster) : prev);
                  setSelectedCluster(prev => prev ? patchCluster(prev) : prev);
                }}
                onUpgradeCluster={async (clusterId) => {
                  const task = await apiPost<ApiTask>(`/api/v1/clusters/${clusterId}/agent/upgrade`, {});
                  setClusterTaskLogs(prev => ({ ...prev, [clusterId]: [{ task, events: [], loading: true }, ...(prev[clusterId] || []).filter(log => log.task.id !== task.id)] }));
                  setActiveClusterTaskIds(prev => new Set(prev).add(task.id));
                  const markUpgrading = (cluster: Cluster) => cluster.id === clusterId ? {
                    ...cluster,
                    agentUpgradeStatus: 'upgrading',
                  } : cluster;
                  setClusters(prev => prev.map(markUpgrading));
                  setLiveClusters(prev => prev ? prev.map(markUpgrading) : prev);
                  setSelectedCluster(prev => prev ? markUpgrading(prev) : prev);
                  return task;
                }}
                onUpgradeVelero={async (clusterId) => {
                  const task = await apiPost<ApiTask>(`/api/v1/clusters/${clusterId}/velero/upgrade`, {});
                  setClusterTaskLogs(prev => ({ ...prev, [clusterId]: [{ task, events: [], loading: true }, ...(prev[clusterId] || []).filter(log => log.task.id !== task.id)] }));
                  setActiveClusterTaskIds(prev => new Set(prev).add(task.id));
                  const markUpgrading = (cluster: Cluster) => cluster.id === clusterId ? { ...cluster, veleroUpgradeStatus: 'upgrading' } : cluster;
                  setClusters(prev => prev.map(markUpgrading));
                  setLiveClusters(prev => prev ? prev.map(markUpgrading) : prev);
                  setSelectedCluster(prev => prev ? markUpgrading(prev) : prev);
                  return task;
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
                registrationAllowed={clusterRegistrationAllowed}
                openLicenseManagement={() => openView('extension:enterprise-license')}
                toast={setToast}
              /></React.Suspense>
            )}
            {view === 'storage' && (onboarding !== 'ready' && onboarding !== 'register' ? onboardingGate : <React.Suspense fallback={<PageLoadFallback />}><LazyStoragePage storage={liveStorage ?? storage} clusters={liveClusters ?? clusters} onStorageCreated={(repo) => {
              setLiveStorage(prev => prev ? [repo, ...prev.filter(item => item.id !== repo.id)] : [repo]);
              setStorage(prev => [repo, ...prev.filter(item => item.id !== repo.id)]);
            }} /></React.Suspense>)}
            {view === 'policies' && (onboarding !== 'ready' && onboarding !== 'register' ? onboardingGate : <React.Suspense fallback={<PageLoadFallback />}><LazyPolicyPage policies={policies} setPolicies={setPolicies} /></React.Suspense>)}
            {view === 'restore_points' && (onboarding !== 'ready' ? onboardingGate : (
              <React.Suspense fallback={<PageLoadFallback />}><LazyRestorePointPage
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
              /></React.Suspense>
            ))}
            {view === 'dr_tasks' && (onboarding !== 'ready' ? onboardingGate : (
              <React.Suspense fallback={<PageLoadFallback />}><LazyBackupRecoveryTaskPage
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
              /></React.Suspense>
            ))}
            {view === 'operations' && <React.Suspense fallback={<PageLoadFallback />}><LazyOperationsCenterPage toast={setToast} openLogs={(taskId) => openView('logs', { diagnosticTaskId: taskId })} openClusters={() => openView('clusters')} /></React.Suspense>}
            {view === 'activity' && !hasEnterpriseAuditModule && !productCapabilities.advancedAudit?.enabled && <React.Suspense fallback={<PageLoadFallback />}><LazyActivityLogPage /></React.Suspense>}
            {view === 'logs' && authSession && <React.Suspense fallback={<PageLoadFallback />}><LazyDiagnosticLogsPage currentUser={authSession.user} toast={setToast} advancedTenancy={productCapabilities.advancedTenancy?.enabled === true} initialTaskId={diagnosticTaskId} /></React.Suspense>}
            {view === 'support_bundle' && authSession && <React.Suspense fallback={<PageLoadFallback />}><LazySupportBundlePage toast={setToast} /></React.Suspense>}
            {view === 'tags' && (onboarding !== 'ready' ? onboardingGate : <React.Suspense fallback={<PageLoadFallback />}><LazyTagManagementPage tags={tags} setTags={setTags} clusters={clusters} setClusters={setClusters} toast={setToast} /></React.Suspense>)}
            {view === 'email_settings' && authSession?.user.systemAdmin && <React.Suspense fallback={<PageLoadFallback />}><LazyEmailSettingsPage currentUser={authSession.user} toast={setToast} /></React.Suspense>}
            {view === 'upgrades' && authSession?.user.systemAdmin && <React.Suspense fallback={<PageLoadFallback />}><LazyUpgradeManagementPage isAdmin={authSession.user.systemAdmin} toast={setToast} refreshPlatformData={() => refreshPlatformData()} /></React.Suspense>}
            {view === 'users' && authSession?.user.systemAdmin && !productCapabilities.advancedIdentity?.enabled && <React.Suspense fallback={<PageLoadFallback />}><LazyCommunityUserManagementPage currentUser={authSession.user} toast={setToast} /></React.Suspense>}
            {view === 'profile' && authSession && <React.Suspense fallback={<PageLoadFallback />}><LazyProfilePage session={authSession} setSession={next => { setAuthSession(next); writeStoredAuthSession(next); }} toast={setToast} /></React.Suspense>}
            {authSession && visibleExtensionModules.map(module => view === module.view ? <React.Suspense key={module.id} fallback={<PageLoadFallback />}><module.component currentUser={authSession.user} clusters={liveApiClusters} toast={setToast} /></React.Suspense> : null)}
          </AnimatePresence>
        </section>
      </main>

      <AnimatePresence>
        {timeZoneDrawerOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => !savingTimeZone && setTimeZoneDrawerOpen(false)} />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-timezone-drawer" role="dialog" aria-modal="true" aria-label="My Time Zone">
              <div className="hbdr-filter-drawer-head">
                <div><h3>My Time Zone</h3><p>Times and restore point names use this time zone.</p></div>
                <button type="button" disabled={savingTimeZone} onClick={() => setTimeZoneDrawerOpen(false)} aria-label="Close time zone settings"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body">
                <div className="hbdr-timezone-current"><span>Current display</span><strong>{userTimeZoneLabel(draftTimeZone || browserTimeZone)}</strong></div>
                <label className="hbdr-timezone-option"><input type="radio" name="time-zone" checked={draftTimeZone === ''} onChange={() => setDraftTimeZone('')} /><span><strong>Follow browser</strong><small>{browserTimeZone}</small></span></label>
                <label className="hbdr-timezone-select-label">Specific time zone<select value={draftTimeZone} onChange={event => setDraftTimeZone(event.target.value)}><option value="">Follow browser — {timeZoneOptionLabel(browserTimeZone)}</option>{availableTimeZones.map(zone => <option key={zone} value={zone}>{timeZoneOptionLabel(zone)}</option>)}</select></label>
              </div>
              <div className="hbdr-filter-drawer-actions"><button type="button" disabled={savingTimeZone} onClick={() => setTimeZoneDrawerOpen(false)}>Cancel</button><button type="button" disabled={savingTimeZone} onClick={() => void saveTimeZone()}>{savingTimeZone ? 'Saving...' : 'Save'}</button></div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>

      <ReleaseNotesModal open={releaseNotesOpen} isAdmin={releaseNotesAdminAudience} onClose={() => setReleaseNotesOpen(false)} />

      {toast && (
        <div className="hbdr-toast fixed right-5 top-20 rounded border border-slate-200 bg-white px-4 py-3 text-sm font-bold text-slate-700 shadow-xl" role="status" aria-live="polite">
          {toast}
        </div>
      )}
    </div>
  );
}

function PageLoadFallback() {
  return <div className="flex min-h-48 items-center justify-center text-xs font-semibold text-slate-400">Loading page...</div>;
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

type ApiPlatformUser = ApiLoginResponse['user'];


function HyperCDRLogoMark({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 128 128" aria-hidden="true" className={className}>
      <defs>
        <linearGradient id="hcdr-sidebar-frame" x1="24" y1="28" x2="104" y2="100" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#ffcf3d" />
          <stop offset="0.48" stopColor="#67e8f9" />
          <stop offset="1" stopColor="#6d7cff" />
        </linearGradient>
        <filter id="hcdr-sidebar-glow" x="-25%" y="-25%" width="150%" height="150%">
          <feGaussianBlur stdDeviation="3" result="blur" />
          <feColorMatrix in="blur" type="matrix" values="0 0 0 0 0.35 0 0 0 0 0.68 0 0 0 0 1 0 0 0 0.45 0" />
          <feMerge><feMergeNode /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
      </defs>
      <path d="M64 18 101 39v50l-37 21-37-21V39l37-21Z" fill="rgba(7,10,18,0.84)" stroke="url(#hcdr-sidebar-frame)" strokeWidth="5" filter="url(#hcdr-sidebar-glow)" />
      <rect x="47" y="45" width="15" height="15" rx="4" fill="#67e8f9" />
      <rect x="66" y="45" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
      <rect x="47" y="64" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
      <rect x="66" y="64" width="15" height="15" rx="4" fill="#ffcf3d" />
    </svg>
  );
}


type ApiTenant = { id: string; name: string; description: string; status: 'active' | 'disabled'; userCount: number; clusterCount: number; createdAt: string; updatedAt: string };
