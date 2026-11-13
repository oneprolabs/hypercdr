import type { DrillCleanupCandidate } from './drill-cleanup-model';
import type { AbortRequestResult } from './force-abort-task-list';
import { TaskOperationTable } from './task-operation-table';

export function DrillCleanupTaskList({ candidates, selected, results, submitting, clusterName, onSelect, onRefresh }: {
  candidates: DrillCleanupCandidate[];
  selected: string[];
  results: Record<string, AbortRequestResult>;
  submitting: boolean;
  clusterName: (id: string) => string;
  onSelect: (id: string, checked: boolean) => void;
  onRefresh: () => void;
}) {
  return <TaskOperationTable mode="cleanup" rows={candidates.map(({ task, namespaces, blockedReason }) => ({ id: task.id, name: namespaces.join(', '), detail: `Drill ${task.status}`, cluster: clusterName(task.clusterId), time: task.createdAt, status: blockedReason || 'Not cleaned', blocked: Boolean(blockedReason) }))} selected={selected} results={results} submitting={submitting} onSelect={onSelect} onRefresh={onRefresh} />;
}
