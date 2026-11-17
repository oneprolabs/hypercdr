import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { listTaskEvents } from '../../api/tasks';
import type { AppItem } from '../clusters/types';
import { listItems, type ApiTask, type ApiTaskEvent } from '../recovery/types';
import { taskFailureSummary } from '../recovery/task-ui';
import { preferTaskSnapshot } from '../recovery/progress-snapshot';
import { isActiveTaskStatus } from '../recovery/task-status';
import type { createTaskDetailLoader } from '../recovery/task-detail-loader';

type TaskDetail = { app: AppItem; task: ApiTask; failure?: ReturnType<typeof taskFailureSummary> } | null;

// Own task-event refresh lifetime independently of the page's rendering and
// mutations. Cleanup prevents delayed responses from updating a closed page.
export function useDrTaskEvents({ syncTaskDetail, setSyncTaskDetail, namespaceDetailTaskId, drTaskEvents, setDrTaskEvents, setRefreshedTasks, taskDetailLoader }: {
  taskDetailLoader: ReturnType<typeof createTaskDetailLoader<ApiTask, Awaited<ReturnType<typeof listTaskEvents>>>>;
  setRefreshedTasks: Dispatch<SetStateAction<Record<string, ApiTask>>>;
  syncTaskDetail: TaskDetail;
  setSyncTaskDetail: Dispatch<SetStateAction<TaskDetail>>;
  namespaceDetailTaskId: string;
  drTaskEvents: Record<string, ApiTaskEvent[]>;
  setDrTaskEvents: Dispatch<SetStateAction<Record<string, ApiTaskEvent[]>>>;
}) {
  useEffect(() => {
    const taskId = syncTaskDetail?.task.id;
    if (!taskId) return;
    let cancelled = false;
    let loading = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refreshOpenTask = async () => {
      if (loading) return;
      loading = true;
      try {
        const [eventResult, latest] = await Promise.all([
          taskDetailLoader.events(taskId),
          taskDetailLoader.task(taskId),
        ]);
        if (cancelled) return;
        const nextEvents = listItems(eventResult);
        setDrTaskEvents(prev => {
          const current = prev[taskId] || [];
          const unchanged = current.length === nextEvents.length && current.every((event, index) => event.id === nextEvents[index]?.id);
          return unchanged ? prev : { ...prev, [taskId]: nextEvents };
        });
        if (latest) setRefreshedTasks(prev => ({ ...prev, [taskId]: preferTaskSnapshot(prev[taskId], latest) }));
        if (latest) setSyncTaskDetail(prev => {
          if (prev?.task.id !== taskId) return prev;
          if (preferTaskSnapshot(prev.task, latest) === prev.task) return prev;
          const currentSignature = JSON.stringify([prev.task.status, prev.task.progress, prev.task.errorCode, prev.task.errorMessage, prev.task.payload]);
          const nextSignature = JSON.stringify([latest.status, latest.progress, latest.errorCode, latest.errorMessage, latest.payload]);
          return currentSignature === nextSignature ? prev : { ...prev, task: latest, failure: undefined };
        });
        if (latest && isActiveTaskStatus(latest.status)) timer = setTimeout(refreshOpenTask, 2000);
      } catch {
        if (!cancelled) timer = setTimeout(refreshOpenTask, 5000);
        // Keep the last successful snapshot visible while the next live refresh retries.
      } finally {
        loading = false;
      }
    };
    void refreshOpenTask();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
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
