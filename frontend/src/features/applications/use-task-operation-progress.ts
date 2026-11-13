import { useEffect, useState, type Dispatch, type SetStateAction } from 'react';
import { getTask } from '../../api/tasks';
import { operationOutcome, taskFinished, type OperationMode, type OperationResult } from './task-operation-model';

export function useTaskOperationProgress(mode: OperationMode, results: Record<string, OperationResult>, setResults: Dispatch<SetStateAction<Record<string, OperationResult>>>, enabled: boolean) {
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    const pending = Object.entries(results).filter(([, result]) => !result.submitting && (!operationOutcome(mode, result).terminal || result.unavailable));
    if (!pending.length) return;
    const poll = async () => {
      const updates = await Promise.all(pending.map(async ([id, result]) => {
        try {
          const operationID = result.operationTask?.id;
          const [operationTask, targetTask] = await Promise.all([
            operationID ? getTask(operationID) : Promise.resolve(undefined),
            mode === 'abort' ? getTask(id) : Promise.resolve(undefined),
          ]);
          if (!operationID && mode === 'abort' && !taskFinished(targetTask)) {
            const abortID = targetTask?.payload?.abortTaskId;
            if (typeof abortID === 'string' && abortID) return [id, { ...result, operationTask: await getTask(abortID), targetTask, unavailable: false }] as const;
          }
          return [id, { ...result, operationTask, targetTask, unavailable: false, message: operationTask?.errorMessage || result.message }] as const;
        } catch {
          return [id, { ...result, unavailable: true }] as const;
        }
      }));
      if (disposed) return;
      setRefresh(0);
      setResults(previous => ({ ...previous, ...Object.fromEntries(updates) }));
    };
    timer = setTimeout(() => void poll(), refresh ? 100 : 3000);
    return () => { disposed = true; clearTimeout(timer); };
  }, [enabled, mode, results, setResults, refresh]);
  return () => setRefresh(value => value + 1);
}
