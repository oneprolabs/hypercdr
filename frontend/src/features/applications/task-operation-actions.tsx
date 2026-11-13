import { operationOutcome, operationSummary, type OperationMode, type OperationResult } from './task-operation-model';

export function TaskOperationActions({ mode, results, selectedCount, rowCount, loading, submitting, onSubmit, onRetry, onClose, onViewTasks }: {
  mode: OperationMode; results: Record<string, OperationResult>; selectedCount: number; rowCount: number; loading: boolean; submitting: boolean;
  onSubmit: () => void; onRetry: () => void; onClose: () => void; onViewTasks: () => void;
}) {
  const started = Object.keys(results).length > 0;
  const retryCount = Object.values(results).filter(result => operationOutcome(mode, result).retryable).length;
  return <div className="hbdr-filter-drawer-actions">
    <span className="hbdr-operation-footer-summary" role="status">{started ? operationSummary(mode, results) : selectedCount ? `${selectedCount} selected` : ''}</span>
    {started && !submitting && <button type="button" onClick={onViewTasks}>View tasks</button>}
    <button type="button" onClick={onClose}>{started || (!rowCount && !loading) ? 'Close' : 'Cancel'}</button>
    {started ? submitting ? <button type="button" disabled>Submitting…</button> : retryCount > 0 && <button type="button" className="!bg-rose-600 !text-white" onClick={onRetry}>{`Retry Failed (${retryCount})`}</button>
      : (rowCount > 0 || loading) && <button type="button" disabled={loading || submitting || !selectedCount} className="!bg-rose-600 !text-white disabled:!bg-slate-100 disabled:!text-slate-400 disabled:cursor-not-allowed" onClick={onSubmit}>{submitting ? 'Submitting…' : `${mode === 'abort' ? 'Force Abort' : 'Clean Selected'}${selectedCount ? ` (${selectedCount})` : ''}`}</button>}
  </div>;
}
