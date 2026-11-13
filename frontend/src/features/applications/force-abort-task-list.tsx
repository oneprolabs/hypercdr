import type { ApiTask } from '../recovery/types';
import { forceAbortBlockedReason } from '../recovery/protection-cleanup-guard';
import { taskDetailLabel } from '../recovery/task-ui';
import { TaskOperationTable } from './task-operation-table';
import type { OperationResult } from './task-operation-model';

export type AbortRequestResult = OperationResult;

export function ForceAbortTaskList({ tasks, selected, results, submitting, describe, clusterName, onSelect, onRefresh }: {
  tasks: ApiTask[];
  selected: string[];
  results: Record<string, AbortRequestResult>;
  submitting: boolean;
  describe: (task: ApiTask) => string;
  clusterName: (id: string) => string;
  onSelect: (id: string, checked: boolean) => void;
  onRefresh: () => void;
}) {
  return <TaskOperationTable mode="abort" rows={tasks.map(task => ({ id: task.id, name: taskDetailLabel(task.type), detail: describe(task), cluster: clusterName(task.clusterId), time: task.startedAt && !task.startedAt.startsWith('0001-') ? task.startedAt : task.createdAt, status: forceAbortBlockedReason(task) || task.status, blocked: Boolean(forceAbortBlockedReason(task)) }))} selected={selected} results={results} submitting={submitting} onSelect={onSelect} onRefresh={onRefresh} />;
}
