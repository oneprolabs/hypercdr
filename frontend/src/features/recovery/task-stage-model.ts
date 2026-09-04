import type { ApiTask, ApiTaskEvent } from './types';
import { isActiveTaskStatus, isFailedStatus } from './task-status.ts';

export type TaskStageStatus = 'completed' | 'in_progress' | 'failed' | 'not_started';

export type TaskStageGroup = {
  id: string;
  name: string;
  status: TaskStageStatus;
  events: ApiTaskEvent[];
  currentEventId?: string;
};

type TaskStageSnapshot = { id: string; name: string; status: string };

const RECOVERY_STAGE_DEFINITIONS = [
  { id: 'preparing_restore', name: 'Preparing Restore' },
  { id: 'restoring_resources', name: 'Restoring Kubernetes Resources' },
  { id: 'restoring_data', name: 'Restoring Persistent Data' },
  { id: 'waiting_for_workloads', name: 'Waiting for Workloads' },
  { id: 'application_validation', name: 'Validating Application' },
  { id: 'finalizing_drill', name: 'Finalizing Drill' },
];

const SYNC_STAGE_DEFINITIONS = [
  { id: 'preparing_backup', name: 'Preparing Backup' },
  { id: 'backing_up_data', name: 'Backing Up Data' },
  { id: 'creating_restore_point', name: 'Creating Restore Point' },
  { id: 'finalizing_sync', name: 'Finalizing Sync' },
];

const STORAGE_SYNC_STAGE_DEFINITIONS = [
  { id: 'preparing_storage', name: 'Preparing Storage Configuration' },
  { id: 'validating_storage', name: 'Validating Object Storage' },
  { id: 'finalizing_storage', name: 'Completing DR Configuration' },
];

export function keyTaskEvents(events: ApiTaskEvent[]): ApiTaskEvent[] {
  const result: ApiTaskEvent[] = [];
  let latestProgress: ApiTaskEvent | null = null;
  [...events].sort((left, right) => {
    const timeOrder = (Date.parse(left.createdAt || '') || 0) - (Date.parse(right.createdAt || '') || 0);
    return timeOrder || String(left.id || '').localeCompare(String(right.id || ''));
  }).forEach(event => {
    const reason = String(event.reason || '').toLowerCase();
    if (['progress', 'backup_progress', 'restore_progress'].includes(reason)) {
      latestProgress = latestProgress ? { ...event, id: latestProgress.id, createdAt: latestProgress.createdAt } : event;
      return;
    }
    if (latestProgress) {
      result.push(latestProgress);
      latestProgress = null;
    }
    const previous = result.at(-1);
    if (previous && previous.reason === event.reason && previous.message === event.message && previous.level === event.level) return;
    result.push(event);
  });
  if (latestProgress) result.push(latestProgress);
  return result;
}

export function latestVisibleTaskEvent(events: ApiTaskEvent[] | undefined): ApiTaskEvent | null {
  return keyTaskEvents(events || []).at(-1) || null;
}

function taskRecoveryStages(task: ApiTask, events: ApiTaskEvent[]): TaskStageSnapshot[] {
  const candidates: unknown[] = [task.payload?.recoveryStages, task.payload?.velero?.recoveryStages];
  [...events].reverse().forEach(event => candidates.push(event.payload?.recoveryStages, event.payload?.velero?.recoveryStages));
  const raw = candidates.find(value => Array.isArray(value)) as Array<Record<string, unknown>> | undefined;
  if (!raw) return [];
  return raw.map((stage, index) => ({
    id: String(stage.id || `stage-${index}`),
    name: String(stage.name || stage.id || `Stage ${index + 1}`),
    status: String(stage.status || 'pending').trim().toLowerCase().replace(/\s+/g, '_'),
  }));
}

function taskEventStageId(event: ApiTaskEvent, recovery: boolean, storageSync = false): string {
  const reason = String(event.reason || '').toLowerCase();
  const code = String(event.payload?.errorCode || event.payload?.code || '').toUpperCase();
  if (storageSync) {
    if (['completed', 'storage_sync_completed', 'storage_configured'].includes(reason)) return 'finalizing_storage';
    if (['accepted', 'progress', 'storage_validation_started', 'storage_validation_succeeded'].includes(reason) || reason.includes('validation') || reason.includes('backup_storage_location')) return 'validating_storage';
    return 'preparing_storage';
  }
  if (!recovery) {
    if (reason === 'backup_completed') return 'creating_restore_point';
    if (['finalizing', 'completed'].includes(reason)) return 'finalizing_sync';
    if (['progress', 'backup_progress'].includes(reason) || reason.includes('volume') || reason.includes('backup')) return 'backing_up_data';
    return 'preparing_backup';
  }
  // Recovery progress events all share a generic `progress` reason. Prefer
  // the authoritative stage snapshot carried by that exact event, otherwise
  // readiness polls are incorrectly grouped back under persistent data.
  const eventStages = [event.payload?.recoveryStages, event.payload?.velero?.recoveryStages]
    .find(value => Array.isArray(value)) as Array<Record<string, unknown>> | undefined;
  const snapshotStage = eventStages?.find(stage => ['running', 'in_progress', 'failed'].includes(String(stage.status || '').toLowerCase()));
  if (snapshotStage?.id) return String(snapshotStage.id);
  if (reason === 'application_ready' || reason.includes('validation')) return 'application_validation';
  if (reason === 'application_readiness_check_started' || reason.includes('readiness') || reason.includes('workload') || code.includes('WORKLOAD')) return 'waiting_for_workloads';
  if (['finalizing', 'completed'].includes(reason)) return 'finalizing_drill';
  if (['restore_progress', 'progress'].includes(reason) || reason.includes('volume') || reason.includes('data_transfer') || reason.includes('data_path') || reason === 'restore_velero_stalled' || code.includes('VOLUME') || code.includes('DATA_PATH')) return 'restoring_data';
  if (reason === 'restore_completed' || reason.includes('restore_resource') || reason.includes('restore_submit') || code.includes('RESTORE_SUBMIT')) return 'restoring_resources';
  return 'preparing_restore';
}

function normalizedStageStatus(status: string): TaskStageStatus {
  const value = String(status || '').toLowerCase();
  if (value === 'failed' || value === 'error') return 'failed';
  if (value === 'running' || value === 'in_progress') return 'in_progress';
  if (['succeeded', 'completed', 'skipped', 'not_applicable'].includes(value)) return 'completed';
  return 'not_started';
}

export function taskStageStatusLabel(status: TaskStageStatus): string {
  if (status === 'in_progress') return 'In progress';
  if (status === 'not_started') return 'Not started';
  return status.charAt(0).toUpperCase() + status.slice(1);
}

/** Timeline details are an execution history, so future stages stay hidden. */
export function reachedTaskStages(groups: TaskStageGroup[]): TaskStageGroup[] {
  return groups.filter(group => group.status !== 'not_started' || group.events.length > 0);
}

export function groupTaskEventsByStage(task: ApiTask, events: ApiTaskEvent[]): TaskStageGroup[] {
  const taskType = String(task.type || '').toLowerCase();
  const recovery = ['drill', 'restore', 'takeover'].includes(taskType);
  const storageSync = taskType === 'storage-sync';
  const taskEvents = keyTaskEvents(events.filter(event => !event.taskId || event.taskId === task.id));
  const currentEvent = taskEvents.at(-1);
  const snapshots = recovery ? taskRecoveryStages(task, taskEvents) : [];
  const definitions = recovery
    ? RECOVERY_STAGE_DEFINITIONS.map(definition => snapshots.find(stage => stage.id === definition.id) || definition)
    : storageSync ? STORAGE_SYNC_STAGE_DEFINITIONS : SYNC_STAGE_DEFINITIONS;
  const eventMap = new Map(definitions.map(definition => [definition.id, [] as ApiTaskEvent[]]));
  taskEvents.forEach(event => {
    const stageID = taskEventStageId(event, recovery, storageSync);
    (eventMap.get(stageID) || eventMap.get(definitions[0].id))?.push(event);
  });
  const snapshotCurrentStageID = recovery
    ? snapshots.find(stage => ['running', 'in_progress', 'failed'].includes(stage.status))?.id || ''
    : '';
  // The newest event for this task is the freshest execution signal.  Stage
  // snapshots can legitimately lag one polling cycle, so they must not move
  // the timeline backwards (which caused the status to flicker between
  // stages). Use the event stage first and only fall back to the snapshot when
  // no event identifies a stage.
  const currentStageID = (currentEvent ? taskEventStageId(currentEvent, recovery, storageSync) : '') || snapshotCurrentStageID;
  const taskFailed = isFailedStatus(task.status);
  const taskActive = isActiveTaskStatus(task.status);
  const taskSucceeded = !taskActive && !taskFailed && ['succeeded', 'completed', 'success'].includes(String(task.status || '').toLowerCase());
  return definitions.map((definition, index) => {
    const eventsForStage = eventMap.get(definition.id) || [];
    const snapshot = snapshots.find(stage => stage.id === definition.id);
    let status = normalizedStageStatus(snapshot?.status || '');
    if (!recovery) {
      const currentIndex = definitions.findIndex(item => item.id === currentStageID);
      status = taskSucceeded
        ? 'completed'
        : eventsForStage.length === 0 || index > currentIndex
          ? 'not_started'
          : index < currentIndex
            ? 'completed'
            : taskFailed
              ? 'failed'
              : taskActive
                ? 'in_progress'
                : 'completed';
    }
    if (definition.id === currentStageID) {
      if (taskFailed || currentEvent?.level === 'error' || String(currentEvent?.reason || '').toLowerCase().includes('failed')) status = 'failed';
      else if (taskActive) status = 'in_progress';
      else if (eventsForStage.length > 0) status = 'completed';
    }
    return { id: definition.id, name: String(definition.name), status, events: eventsForStage, currentEventId: definition.id === currentStageID ? currentEvent?.id : undefined };
  });
}
