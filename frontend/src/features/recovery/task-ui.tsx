import React from 'react';
import { Boxes, Calendar, ClipboardList, Clock, FileCog, Gauge, HardDrive, KeyRound, MoreVertical, Network, RefreshCw, Settings2, User } from 'lucide-react';
import { formatLocalDateTime } from '../../lib/date-time';
import type { AppItem, ResourceCategoryKey, ResourceKindSummary, ResourceRef } from '../clusters/types';
import type { ApiRestorePoint, ApiTask, ApiTaskEvent, VolumeProgressInfo } from './types';
import { isActiveTaskStatus, isFailedStatus, taskHasWarning } from './task-status';

export function formatBytes(bytes: number): string {
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

export function numberFromUnknown(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number(value.trim());
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

export function recordFromUnknown(value: unknown): Record<string, any> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, any> : {};
}

export function restorePointOriginalSize(point: ApiRestorePoint): { label: string; title: string; bytes: number } {
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

export function formatSignedBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const prefix = bytes < 0 ? '-' : '';
  return `${prefix}${formatBytes(Math.abs(bytes))}`;
}

export function restorePointStorageSize(point: ApiRestorePoint): { label: string; title: string; bytes: number } {
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

export function formatBytesPerSecond(bytes: number): string {
  if (!bytes || bytes < 0) return '';
  return `${formatBytes(bytes)}/s`;
}

export function formatEta(seconds: number): string {
  if (!seconds || seconds < 1) return '';
  if (seconds < 60) return `${Math.round(seconds)}s left`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${Math.round(seconds % 60)}s left`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m left`;
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '0.00';
  return Math.max(0, Math.min(100, value)).toFixed(2);
}

export function latestVolumeProgress(events?: ApiTaskEvent[]): VolumeProgressInfo | null {
  if (!events?.length) return null;
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const progress = events[index]?.payload?.velero?.volumeProgress;
    if (!progress || typeof progress !== 'object') continue;
    return {
      operation: typeof progress.operation === 'string' ? progress.operation : undefined,
      bytesDone: Number(progress.bytesDone || 0),
      totalBytes: Number(progress.totalBytes || 0),
	  incrementalBytes: Number(progress.incrementalBytes || 0),
	  incrementalCount: Number(progress.incrementalCount || 0),
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

export function taskProgressInfo(task: ApiTask, events?: ApiTaskEvent[]): VolumeProgressInfo | null {
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
  if (['succeeded', 'failed', 'canceled', 'cancelled'].includes(task.status)) {
    return null;
  }
  const volume = latestVolumeProgress(events);
  if (!volume || !volume.knownTotal || !volume.allTotalsKnown || volume.totalBytes <= 0) {
    return null;
  }
  return volume;
}

export function hasTaskEventReason(events: ApiTaskEvent[] | undefined, reasons: string[]): boolean {
  if (!events?.length) return false;
  const allowed = new Set(reasons);
  return events.some(event => allowed.has(event.reason));
}

export function latestTaskMessage(events: ApiTaskEvent[] | undefined, fallback: string): string {
  return events?.at(-1)?.message || fallback;
}

export function eventRestoreResultErrors(event?: ApiTaskEvent | null): string[] {
  const restoreResults = event?.payload?.velero?.restoreResults || event?.payload?.restoreResults;
  const errors = restoreResults?.errors;
  return Array.isArray(errors) ? errors.map(error => String(error)).filter(Boolean) : [];
}

export function taskFailureDetails(task: ApiTask, events?: ApiTaskEvent[]): string[] {
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

export type ErrorMessageDefinition = {
  code: string;
  aliases: string[];
  title: string;
  description: string;
  detail: string;
  match?: (message: string) => boolean;
};

export const ERROR_MESSAGE_CATALOG: ErrorMessageDefinition[] = [
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
    code: '120005',
    aliases: ['BSL_UNAVAILABLE'],
    title: 'Backup storage location is unavailable',
    description: 'Velero could not validate the configured BackupStorageLocation.',
    detail: 'Review the displayed reason. Verify the ObjectStore plugin, endpoint, bucket, credentials, TLS trust, and network access, then use the reconfigure icon to retry.',
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
    aliases: ['RESTORE_FAILED', 'RESTORE_SUBMIT_FAILED'],
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
  {
    code: '140005',
    aliases: ['RESTORE_WORKLOAD_IMAGE_PULL_FAILED'],
    title: 'Restored workload image pull failed',
    description: 'A restored Pod cannot start because its container image could not be pulled.',
    detail: 'Verify that the image exists in the configured registry, the restored image reference includes the correct registry port and project, registry CA trust is installed on every target node, and image pull credentials are available.',
  },
  {
    code: '140006',
    aliases: ['RESTORE_VOLUME_DEPENDENCY_MISSING'],
    title: 'Volume restore dependency is missing',
    description: 'Volume data restoration cannot start because a required persistent volume claim is missing.',
    detail: 'Retry the drill with Replace conflict handling after the previous target namespace is fully removed. Verify that the target StorageClass and provisioner are healthy and that the restored PVC is created before the workload Pod starts.',
  },
  {
    code: '140007',
    aliases: ['RESTORE_WORKLOAD_CRASH_LOOP'],
    title: 'Restored workload is crash looping',
    description: 'A restored container repeatedly exited and the application could not become ready.',
    detail: 'Review the restored Pod logs, last container exit reason, restored volume permissions, secrets, configuration, and application startup requirements.',
  },
  {
    code: '140008',
    aliases: ['RESTORE_STALE_STATE_CLEANUP_TIMEOUT'],
    title: 'Previous restore cleanup timed out',
    description: 'The target cluster could not remove stale Velero restore state before starting this recovery.',
    detail: 'Inspect deleting Velero Restore and PodVolumeRestore objects on the target cluster. Resolve stuck finalizers or controllers, confirm the stale objects are removed, and then retry the drill.',
    match: message => message.toLowerCase().includes('timed out waiting for stale restore state to be deleted'),
  },
];

export const ERROR_MESSAGE_BY_CODE = new Map(ERROR_MESSAGE_CATALOG.map(item => [item.code, item]));

export const ERROR_CODE_BY_ALIAS = new Map(ERROR_MESSAGE_CATALOG.flatMap(item => item.aliases.map(alias => [alias, item.code] as const)));

export function taskFailureText(task: ApiTask, events?: ApiTaskEvent[]): string {
  const details = taskFailureDetails(task, events);
  return details.length > 0 ? summarizeTaskFailure(details[0]) : 'View details';
}

export function taskFailureSummary(task: ApiTask, events?: ApiTaskEvent[]): { code: string; title: string; description: string; fullText: string } {
  const details = taskFailureDetails(task, events);
  const firstDetail = details[0] || task.errorMessage || task.errorCode || 'Task failed';
  const code = resolveErrorCode(task.errorCode, firstDetail);
  const definition = errorMessageDefinition(code);
  const taskLabel = taskDetailLabel(task.type);
  const unmatched = code === '100000';
  const originalCode = String(task.errorCode || '').trim();
  const fullText = [
    originalCode && !/^\d{6}$/.test(originalCode) ? `Original error code: ${originalCode}` : '',
    definition.detail,
    ...(details.length > 0 ? details : [firstDetail]),
  ].filter(Boolean).join('\n');
  return {
    code,
    title: unmatched ? `${taskLabel} failed` : definition.title,
    description: unmatched
      ? `The ${taskLabel.toLowerCase()} task failed. Open details to review the reported cause.`
      : definition.description,
    fullText,
  };
}

export function normalizeErrorCode(code?: string): string {
  const raw = String(code || '').trim();
  if (/^\d{6}$/.test(raw)) return raw;
  const normalized = raw.replace(/[^a-zA-Z0-9_:-]+/g, '_').replace(/^_+|_+$/g, '').toUpperCase();
  const mapped = ERROR_CODE_BY_ALIAS.get(normalized);
  return mapped || '100000';
}

export function resolveErrorCode(rawCode: string | undefined, message: string): string {
  const normalized = normalizeErrorCode(rawCode);
  const matched = ERROR_MESSAGE_CATALOG.find(item => item.match?.(message));
  if (matched) return matched.code;
  if (rawCode && normalized !== '100000') return normalized;
  return normalized;
}

export function errorMessageDefinition(code: string): ErrorMessageDefinition {
  return ERROR_MESSAGE_BY_CODE.get(code) || ERROR_MESSAGE_BY_CODE.get('100000')!;
}

export function volumeFailureDetails(payload: any): string[] {
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

export function humanizeBackupFailureMessage(message: string): string {
  if (message.includes('repository not initialized in the provided storage')) {
    return `${message}. Kopia repository is missing or not initialized. Reconfigure or retry the BackupStorageLocation for this cluster, then run sync again. If the object storage kopia directory was deleted manually, the repository must be initialized again first.`;
  }
  return message;
}

export function summarizeTaskFailure(message: string): string {
  if (!message) return 'View details';
  if (message.includes('repository not initialized in the provided storage')) return 'Kopia repository is not initialized';
  if (message.includes('backup repository connection')) return 'Backup repository connection failed';
  if (message.includes('data path backup failed')) return 'Volume data backup failed';
  if (message.includes('PartiallyFailed')) return 'Backup partially failed';
  if (message.length > 96) return `${message.slice(0, 93).trim()}...`;
  return message;
}

export function TaskErrorStatus({
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
  const stopTableEvent = (event: React.SyntheticEvent<HTMLElement>) => {
    event.stopPropagation();
  };
  const content = (
    <span className="hbdr-dr-task-error-title">
      <strong><b>[{errorCode}]</b> {title}</strong>
    </span>
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

export function TaskErrorDetailBlock({
  failure,
  details,
  onRetry,
}: {
  failure: { code: string; title: string; description: string; fullText: string };
  details?: string[];
  onRetry?: () => void;
}) {
  const fullDetails = details && details.length > 0 ? details : failure.fullText ? [failure.fullText] : [];
  const definition = errorMessageDefinition(failure.code);
  const possibleCause = productTaskMessage(fullDetails[0] || failure.description || 'No specific cause was reported.');
  const solution = taskFailureSolution(failure.code, fullDetails, definition.detail);
  return (
    <div className="hbdr-task-detail-error">
      <header>
        <small>Error summary</small>
        <strong>[{failure.code}] {failure.title}</strong>
        <span>{productTaskMessage(failure.description)}</span>
      </header>
      <div className="hbdr-task-detail-error-sections">
        <section>
          <b>Possible cause</b>
          <p>{possibleCause}</p>
        </section>
        <section>
          <b>Solution</b>
          <p>{solution}</p>
        </section>
        <section>
          <b>Technical details</b>
          {fullDetails.length > 0 ? (
            fullDetails.map((detail, index) => <p key={`${index}-${detail}`}>{detail}</p>)
          ) : (
            <p>No detailed error was reported by the task.</p>
          )}
        </section>
      </div>
      {onRetry && <div className="hbdr-task-detail-error-actions"><button type="button" onClick={onRetry}><RefreshCw size={13} />Retry recovery</button></div>}
    </div>
  );
}

function taskFailureSolution(code: string, details: string[], fallback: string): string {
  const evidence = details.join(' ').toLowerCase();
  if (code === '140005') {
    if (/timeout|timed out|i\/o timeout|connection refused|no route|dial tcp/.test(evidence)) {
      return 'Verify outbound DNS and TCP 443 access from every target worker node to the registry shown in the technical details. If direct access is blocked, map the image to an internal registry in Advanced options and retry.';
    }
    if (/unauthorized|authentication required|denied|pull access/.test(evidence)) {
      return 'Create or copy an imagePullSecret in the target namespace, attach it to the restored ServiceAccount or Pod template, verify registry permission, and retry.';
    }
    if (/not found|manifest unknown/.test(evidence)) {
      return 'Confirm that the exact repository and tag or digest exists. Map it to an available target-registry image in Advanced options, then retry.';
    }
  }
  return productTaskMessage(fallback);
}

export function TaskProcessTimeline({ task, events }: { task: ApiTask; events: ApiTaskEvent[] }) {
  const terminal = !isActiveTaskStatus(task.status);
  const recoveryStages = taskRecoveryStages(task, events);
  return (
    <div className="hbdr-task-detail-section">
      <div className="hbdr-task-detail-section-title">
        <strong>Execution process</strong>
        <span>{terminal ? `${events.length} records` : `Live · ${events.length} records`}</span>
      </div>
      {recoveryStages.length > 0 && (
        <div className="hbdr-recovery-stage-list" aria-label="Recovery stages">
          {recoveryStages.map(stage => (
            <section key={stage.id} className={`is-${stage.status}`}>
              <i aria-hidden="true" />
              <div>
                <strong>{stage.name}</strong>
                {stage.message && <span>{productTaskMessage(stage.message)}</span>}
                {stage.evidence.length > 0 && <ul>{stage.evidence.map((item, index) => <li key={`${stage.id}-${index}`}>{item}</li>)}</ul>}
              </div>
              <em>{stage.status.replace(/_/g, ' ')}</em>
            </section>
          ))}
        </div>
      )}
      <div className="hbdr-task-detail-events" aria-live="polite">
        {events.length > 0 ? events.map(event => {
          const errors = eventRestoreResultErrors(event);
          const eventMessage = taskProcessEventMessage(event);
          return (
            <section key={event.id} className="hbdr-task-log-entry">
              <div className="hbdr-task-log-line" title={`${formatLocalDateTime(event.createdAt) || '-'} · ${eventMessage}`}>
                <time>{formatLocalDateTime(event.createdAt) || '-'}</time>
                <p className={event.level === 'error' ? 'is-error' : ''}>{eventMessage}</p>
              </div>
              {errors.length > 0 && <ul>{errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}</ul>}
            </section>
          );
        }) : <p className="hbdr-task-detail-empty">Waiting for task events...</p>}
      </div>
    </div>
  );
}

type TaskRecoveryStage = {
  id: string;
  name: string;
  status: string;
  message: string;
  evidence: string[];
};

function taskRecoveryStages(task: ApiTask, events: ApiTaskEvent[]): TaskRecoveryStage[] {
  const candidates: unknown[] = [task.payload?.recoveryStages, task.payload?.velero?.recoveryStages];
  [...events].reverse().forEach(event => {
    candidates.push(event.payload?.recoveryStages, event.payload?.velero?.recoveryStages);
  });
  const raw = candidates.find(value => Array.isArray(value)) as Array<Record<string, unknown>> | undefined;
  if (!raw) return [];
  return raw.map((stage, index) => {
    const status = String(stage.status || 'pending').trim().toLowerCase().replace(/\s+/g, '_');
    const evidenceValue = stage.evidence;
    const evidence = Array.isArray(evidenceValue)
      ? evidenceValue.map(item => typeof item === 'string' ? item : JSON.stringify(item)).filter(Boolean)
      : evidenceValue ? [typeof evidenceValue === 'string' ? evidenceValue : JSON.stringify(evidenceValue)] : [];
    return {
      id: String(stage.id || `stage-${index}`),
      name: String(stage.name || stage.id || `Stage ${index + 1}`),
      status,
      message: String(stage.message || ''),
      evidence,
    };
  });
}

export function taskProcessEventMessage(event: ApiTaskEvent): string {
  const message = String(event.message || '').trim();
  const messages: Record<string, string> = {
    storage_preflight_started: 'Storage readiness check started.',
    storage_preflight_succeeded: 'Storage readiness check completed successfully.',
    storage_preflight_skipped: message || 'Storage is already configured; readiness check skipped.',
    dispatched: 'Task was dispatched to the cluster agent.',
    accepted: 'Cluster agent accepted the task and started processing.',
    backup_completed: 'Backup and restore point creation completed successfully.',
    restore_completed: 'Resource and volume data restoration completed successfully.',
    application_readiness_check_started: 'Restored application readiness validation started.',
    application_ready: 'Restored application readiness validation completed successfully.',
    completed: message || 'Task completed successfully.',
  };
  return productTaskMessage(messages[event.reason] || message || `${String(event.reason || 'Task event').replace(/_/g, ' ')}.`);
}

export function productTaskMessage(message: string): string {
  const cleaned = String(message || '')
    .replace(/\bHyperCDR Agent\s+/gi, '')
    .replace(/\bVelero\s+/gi, '')
    .trim();
  return cleaned ? cleaned.charAt(0).toUpperCase() + cleaned.slice(1) : cleaned;
}

export function TaskFinalResult({ task, events }: { task: ApiTask; events: ApiTaskEvent[] }) {
  if (isActiveTaskStatus(task.status)) return null;
  const failed = isFailedStatus(task.status);
  const warning = taskHasWarning(task);
  const lastEvent = events.at(-1);
  const failure = failed || warning ? taskFailureSummary(task, events) : null;
  const title = failed
    ? `[${failure?.code || normalizeErrorCode(task.errorCode)}] ${failure?.title || 'Task failed'}`
    : warning
      ? `Completed with warning · ${failure?.title || 'Review task details'}`
      : `${taskDetailLabel(task.type)} completed successfully`;
  const message = productTaskMessage(failed || warning
    ? failure?.description || task.errorMessage || lastEvent?.message || 'The task did not complete successfully.'
    : lastEvent?.message || 'All task stages completed successfully.');
  return (
    <div className={`hbdr-task-final-result ${failed ? 'is-failed' : warning ? 'is-warning' : 'is-succeeded'}`}>
      <small>Final result</small>
      <strong>{title}</strong>
      <span>{message}</span>
    </div>
  );
}

export function syncPreparingMessage(events: ApiTaskEvent[] | undefined): string {
  const storageStarted = hasTaskEventReason(events, ['storage_preflight_started']);
  const storageDoneOrSkipped = hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
  if (storageStarted && !storageDoneOrSkipped) {
    return 'Configuring storage...';
  }
  return 'Dispatching sync task...';
}

export function recoveryActionText(taskType: string): { label: string; dispatching: string; running: string; complete: string } {
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

export function taskDetailLabel(taskType: string): string {
  const normalized = (taskType || '').toLowerCase();
  if (normalized === 'drill') return 'Drill';
  if (normalized === 'takeover') return 'Takeover';
  if (normalized === 'restore') return 'Restore';
  if (normalized === 'retention-cleanup') return 'Cleanup';
  if (normalized === 'protection-cleanup') return 'DR Cleanup';
  if (normalized === 'storage-sync') return 'Storage Setup';
  if (normalized === 'backup-cancel') return 'Cancel Sync';
  if (normalized.includes('backup') || normalized.includes('sync')) return 'Sync';
  return 'Task';
}

export function taskDetailFullLabel(taskType: string): string {
  return String(taskType || '').toLowerCase() === 'retention-cleanup' ? 'Retention Cleanup' : taskDetailLabel(taskType);
}

export function taskOrigin(task: ApiTask): { label: 'Manual' | 'Scheduled' | 'System'; tone: 'manual' | 'scheduled' | 'system' } {
  const taskType = String(task.type || '').toLowerCase();
  const trigger = String(task.payload?.trigger || '').toLowerCase();
  if (trigger === 'scheduled' || task.payload?.scheduled === true || taskType === 'schedule-sync') {
    return { label: 'Scheduled', tone: 'scheduled' };
  }
  if (['retention-cleanup', 'protection-cleanup', 'storage-sync'].includes(taskType)) {
    return { label: 'System', tone: 'system' };
  }
  return { label: 'Manual', tone: 'manual' };
}

export function TaskOriginLabel({ task }: { task: ApiTask }) {
  const origin = taskOrigin(task);
  const Icon = origin.tone === 'scheduled' ? Calendar : origin.tone === 'system' ? Settings2 : User;
  return <i className={`hbdr-task-origin is-${origin.tone}`}><Icon size={12} />{origin.label}</i>;
}

export function recoveryPreparingMessage(events: ApiTaskEvent[] | undefined, taskType: string): string {
  const text = recoveryActionText(taskType);
  const storageStarted = hasTaskEventReason(events, ['storage_preflight_started']);
  const storageDoneOrSkipped = hasTaskEventReason(events, ['storage_preflight_skipped', 'storage_preflight_succeeded', 'dispatched', 'accepted', 'dispatch_waiting_agent', 'dispatch_failed']);
  if (storageStarted && !storageDoneOrSkipped) {
    return 'Configuring storage...';
  }
  return text.dispatching;
}

export function formatAge(seconds?: number): string {
  if (!seconds || seconds < 0) return '-';
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  if (days > 0) return `${days}d`;
  if (hours > 0) return `${hours}h`;
  if (minutes > 0) return `${minutes}m`;
  return `${Math.floor(seconds)}s`;
}

export function compactList(values?: string[], empty = '-'): string {
  if (!values || values.length === 0) return empty;
  return values.join(', ');
}

export function kubectlColumnsForKind(kind?: string): string[] {
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

export function resourceColumnValue(resource: ResourceRef, item: ResourceKindSummary, column: string, namespace: string): string {
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

export function resourcePrimaryStatus(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
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

export function resourceInventoryDetails(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string[] {
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

export function resourceInventoryTitle(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
  const lines = [
    `${item.kind}/${resource.name}`,
    `Namespace: ${resource.namespace || namespace}`,
    `Status: ${resourcePrimaryStatus(resource, item, namespace)}`,
    ...resourceInventoryDetails(resource, item, namespace),
    `Age: ${formatAge(resource.ageSeconds)}`,
  ];
  return lines.filter(Boolean).join('\n');
}

export function resourceInventoryDetailLimit(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string[] {
  const details = resourceInventoryDetails(resource, item, namespace);
  const kind = (item.kind || '').toLowerCase();
  if (kind === 'persistentvolumeclaim') return details.slice(0, 4);
  if (kind === 'deployment' || kind === 'daemonset' || kind === 'statefulset') return details.slice(0, 3);
  return details.slice(0, 3);
}

export function resourceFactLabel(label: string): string {
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

export function resourceInventoryFacts(resource: ResourceRef, item: ResourceKindSummary, namespace: string): Array<{ label: string; value: string }> {
  return resourceInventoryDetailLimit(resource, item, namespace).map(detail => {
    const separator = detail.indexOf(':');
    if (separator === -1) return { label: 'Info', value: detail };
    return {
      label: resourceFactLabel(detail.slice(0, separator)),
      value: detail.slice(separator + 1).trim(),
    };
  });
}

export function resourceInventoryDetailText(resource: ResourceRef, item: ResourceKindSummary, namespace: string): string {
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

export function resourceGridTemplate(columns: string[]): string {
  return columns.map(column => {
    if (column === 'NAME') return 'minmax(160px, 1.2fr)';
    if (column === 'IMAGES') return 'minmax(280px, 2fr)';
    if (column === 'SELECTOR' || column === 'VOLUME') return 'minmax(220px, 1.6fr)';
    if (column === 'PORT(S)' || column === 'HOSTS') return 'minmax(150px, 1fr)';
    if (column === 'AGE' || column === 'DATA' || column === 'READY' || column === 'TYPE' || column === 'STATUS') return 'minmax(74px, max-content)';
    return 'minmax(118px, max-content)';
  }).join(' ');
}

export function mapApplicationStatus(status: string | undefined, isProtected: boolean): AppItem['status'] {
  if (isProtected) return 'Protected';
  const normalized = (status || '').trim().toLowerCase();
  if (normalized === 'active' || normalized === 'running') return 'Active';
  if (normalized === 'terminating') return 'Terminating';
  return 'Unknown';
}

export function normalizeNodeStatus(status: string | undefined): string {
  const normalized = (status || '').trim().toLowerCase();
  if (normalized === 'ready') return 'Ready';
  if (normalized === 'notready' || normalized === 'not-ready') return 'NotReady';
  return status || 'Unknown';
}

export type ApplicationStage = 'select' | 'config' | 'run';

export function stageOfApp(protectionStatus: string | undefined, isProtected: boolean): ApplicationStage {
  const ps = (protectionStatus || '').toLowerCase();
  if (ps === 'protected' || isProtected) return 'run';
  if (ps === 'pending_protection') return 'config';
  return 'select';
}

export function drStatusForPlan(status: string | undefined): { label: string; tone: 'ok' | 'progress' | 'warn' | 'muted'; title: string } {
  const normalized = (status || '').trim().toLowerCase();
  switch (normalized) {
	case 'ready':
    case 'active':
      return { label: 'Ready', tone: 'ok', title: 'Storage location and backup schedule are active' };
	case 'ready_with_warning':
    case 'active_with_warning':
      return { label: 'Ready', tone: 'warn', title: 'Source backup schedule is active. Target cluster storage needs attention; restore, drill, and takeover may be unavailable until BSL is reconfigured.' };
	case 'configuring':
    case 'activating_storage':
      return { label: 'Configuring...', tone: 'progress', title: 'DR configuration is being applied. Storage and schedule setup continue automatically.' };
	case 'configuration_failed':
    case 'storage_failed':
	  return { label: 'Configuration failed', tone: 'warn', title: 'DR configuration failed while preparing storage. Open error details to review the cause and retry.' };
    case 'activating_schedule':
      return { label: 'Configuring...', tone: 'progress', title: 'DR configuration is being applied. Storage is ready and schedule setup is continuing automatically.' };
    case 'schedule_failed':
	  return { label: 'Configuration failed', tone: 'warn', title: 'DR configuration failed while preparing the schedule. Open error details to review the cause and retry.' };
	case 'cleaning':
    case 'cleanup_running':
      return { label: 'Cleaning...', tone: 'progress', title: 'Protection resources are being cleaned. Restore points, task records, schedule, and backup data are being removed.' };
    case 'cleanup_failed':
      return { label: 'Cleanup failed', tone: 'warn', title: 'Protection resource cleanup failed. Check the latest cleanup task error and retry cleanup.' };
    case 'disabled':
      return { label: 'Disabled', tone: 'muted', title: 'Protection plan is disabled' };
    case 'pending_activation':
    case '':
      return { label: 'Configuring...', tone: 'progress', title: 'DR configuration has been saved and is being applied.' };
    default:
      return { label: 'Configuring...', tone: 'progress', title: status ? `DR configuration is being applied (${status}).` : 'DR configuration is being applied.' };
  }
}

export function storageFailurePresentation(task: ApiTask | undefined): { message: string; solution: string } | null {
  if (!task || task.status.toLowerCase() !== 'failed') return null;
  const raw = (task.errorMessage || task.errorCode || '').trim();
  if (raw.toLowerCase().includes('unable to locate objectstore plugin named velero.io/aws')) {
    return {
      message: 'AWS object storage plugin is missing; Velero cannot access S3/MinIO.',
      solution: 'Install velero-plugin-for-aws, confirm velero.io/aws is registered, then reconfigure the BSL.',
    };
  }
  const normalized = raw.toLowerCase();
  if (normalized.includes('authorizationqueryparameterserror') && normalized.includes('incorrect date format')) {
    return {
      message: 'Object storage authentication failed: the configured region is invalid.',
      solution: 'Set a valid S3 region, such as us-east-1, then retry.',
    };
  }
  const firstLine = raw.split('\n').map(line => line.trim()).find(Boolean);
  return {
    message: firstLine || 'BackupStorageLocation configuration failed.',
    solution: 'Correct the reported BSL, credentials, endpoint, bucket, plugin, or network problem, then reconfigure storage.',
  };
}

export function isProtectionPlanReady(status: string | undefined): boolean {
	return ['ready', 'ready_with_warning', 'active', 'active_with_warning'].includes((status || '').trim().toLowerCase());
}

export function canRetryDrActivation(status: string | undefined): boolean {
	return ['configuration_failed', 'pending_activation', 'storage_failed', 'schedule_failed'].includes((status || '').trim().toLowerCase());
}

export function isProtectionPlanCleaning(status: string | undefined): boolean {
	return ['cleaning', 'cleanup_running'].includes((status || '').trim().toLowerCase());
}

export const resourceCategoryMeta: Array<{ key: ResourceCategoryKey; label: string }> = [
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

export const resourceCategoryKeys = new Set<ResourceCategoryKey>(resourceCategoryMeta.map(item => item.key));

export const resourceCategoryIconMap: Record<ResourceCategoryKey, React.ComponentType<{ size?: number; className?: string }>> = {
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

export function shortResourceKind(kind: string): string {
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

export function resourceCategoryForKind(kind: string): ResourceCategoryKey {
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

export function mergeResourceItems(items: ResourceKindSummary[]): ResourceKindSummary[] {
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
