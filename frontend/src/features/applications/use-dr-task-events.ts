import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react';
import { getTask, listTaskEvents } from '../../api/tasks';
import type { AppItem } from '../clusters/types';
import { listItems, type ApiTask, type ApiTaskEvent } from '../recovery/types';
import { taskFailureSummary } from '../recovery/task-ui';

type TaskDetail = { app: AppItem; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null;

// Own task-event refresh lifetime independently of the page's rendering and
// mutations. Cleanup prevents delayed responses from updating a closed page.
export function useDrTaskEvents({ displayedDrTaskKey, hasActiveDisplayedTask, refreshPlatformData, syncTaskDetail, setSyncTaskDetail, namespaceDetailTaskId, drTaskEvents, setDrTaskEvents }: {
  displayedDrTaskKey: string;
  hasActiveDisplayedTask: boolean;
  refreshPlatformData: () => Promise<unknown>;
  syncTaskDetail: TaskDetail;
  setSyncTaskDetail: Dispatch<SetStateAction<TaskDetail>>;
  namespaceDetailTaskId: string;
  drTaskEvents: Record<string, ApiTaskEvent[]>;
  setDrTaskEvents: Dispatch<SetStateAction<Record<string, ApiTaskEvent[]>>>;
}) {
  const refreshedTerminalEventIdsRef = useRef(new Set<string>());
  useEffect(() => {
    const ids = displayedDrTaskKey ? displayedDrTaskKey.split('|').filter(Boolean) : [];
    if (ids.length === 0) return;
    let cancelled = false;
    const loadEvents = async () => {
      const entries = await Promise.all(ids.map(async taskId => {
        try {
          const res = await listTaskEvents(taskId);
          return [taskId, listItems(res)] as const;
        } catch {
          return [taskId, null] as const;
        }
      }));
      if (cancelled) return;
      setDrTaskEvents(prev => {
        const next = { ...prev };
        let changed = false;
        for (const [taskId, events] of entries) {
          if (!events) continue;
          const current = prev[taskId] || [];
          const unchanged = current.length === events.length && current.every((event, index) => (
            event.id === events[index]?.id
            && event.level === events[index]?.level
            && event.message === events[index]?.message
          ));
          if (!unchanged) {
            next[taskId] = events;
            changed = true;
          }
        }
        return changed ? next : prev;
      });
      let observedNewTerminalEvent = false;
      for (const [, events] of entries) {
        for (const event of events || []) {
          if (!['completed', 'backup_completed', 'velero-schedule'].includes(event.reason)) continue;
          if (refreshedTerminalEventIdsRef.current.has(event.id)) continue;
          refreshedTerminalEventIdsRef.current.add(event.id);
          observedNewTerminalEvent = true;
        }
      }
      if (observedNewTerminalEvent) {
        void refreshPlatformData();
      }
    };
    loadEvents();
    const timer = hasActiveDisplayedTask ? window.setInterval(loadEvents, 2000) : undefined;
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [displayedDrTaskKey, hasActiveDisplayedTask, refreshPlatformData]);
  useEffect(() => {
    const taskId = syncTaskDetail?.task.id;
    if (!taskId) return;
    let cancelled = false;
    const refreshOpenTask = async () => {
      try {
        const [eventResult, latest] = await Promise.all([
          listTaskEvents(taskId),
          getTask(taskId),
        ]);
        if (cancelled) return;
        const nextEvents = listItems(eventResult);
        setDrTaskEvents(prev => {
          const current = prev[taskId] || [];
          const unchanged = current.length === nextEvents.length && current.every((event, index) => event.id === nextEvents[index]?.id);
          return unchanged ? prev : { ...prev, [taskId]: nextEvents };
        });
        if (latest) setSyncTaskDetail(prev => {
          if (prev?.task.id !== taskId) return prev;
          const currentSignature = JSON.stringify([prev.task.status, prev.task.progress, prev.task.errorCode, prev.task.errorMessage, prev.task.payload]);
          const nextSignature = JSON.stringify([latest.status, latest.progress, latest.errorCode, latest.errorMessage, latest.payload]);
          return currentSignature === nextSignature ? prev : { ...prev, task: latest, failure: undefined };
        });
      } catch {
        // Keep the last successful snapshot visible while the next live refresh retries.
      }
    };
    void refreshOpenTask();
    const timer = window.setInterval(refreshOpenTask, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [syncTaskDetail?.task.id]);
  useEffect(() => {
    if (!namespaceDetailTaskId || drTaskEvents[namespaceDetailTaskId]) return;
    let cancelled = false;
    void listTaskEvents(namespaceDetailTaskId)
      .then(result => {
        if (!cancelled) setDrTaskEvents(prev => ({ ...prev, [namespaceDetailTaskId]: listItems(result) }));
      })
      .catch(() => {
        if (!cancelled) setDrTaskEvents(prev => ({ ...prev, [namespaceDetailTaskId]: [] }));
      });
    return () => {
      cancelled = true;
    };
  }, [namespaceDetailTaskId, drTaskEvents]);
}
