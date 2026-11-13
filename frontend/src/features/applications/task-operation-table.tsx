import { formatDateTime } from '../../lib/date-time';
import { operationOutcome, type OperationMode, type OperationResult } from './task-operation-model';
import './task-operation-table.css';

export type OperationRow = { id: string; name: string; detail?: string; cluster: string; time: string; status: string; blocked?: boolean };

export function TaskOperationTable({ mode, rows, selected, results, submitting, onSelect, onRefresh }: {
  mode: OperationMode; rows: OperationRow[]; selected: string[]; results: Record<string, OperationResult>; submitting: boolean;
  onSelect: (id: string, checked: boolean) => void; onRefresh: () => void;
}) {
  const started = Object.keys(results).length > 0;
  return <div className="hbdr-operation-list">
    {!rows.length ? <p className="hbdr-operation-empty">{mode === 'abort' ? 'No active tasks.' : 'No uncleaned drill resources.'}</p> : <table className="hbdr-operation-table" aria-label={mode === 'abort' ? 'Tasks to abort' : 'Drill resources to clean'}>
      <thead><tr><th aria-label="Selection" /><th>{mode === 'abort' ? 'Task / Application' : 'Namespace / Last drill'}</th><th>{mode === 'abort' ? 'Cluster' : 'Target cluster'}</th><th>Status</th></tr></thead>
      <tbody>{rows.map(row => {
        const result = results[row.id];
        const outcome = result ? operationOutcome(mode, result) : undefined;
        const error = outcome?.retryable ? result?.operationTask?.errorMessage || result?.message : '';
        return <tr key={row.id}>
          <td>{started ? result && <span className="hbdr-operation-mark" aria-label="Included in this operation">✓</span> : <input type="checkbox" aria-label={`Select ${row.name} ${row.cluster}`} checked={selected.includes(row.id)} disabled={submitting || row.blocked} onChange={event => onSelect(row.id, event.target.checked)} />}</td>
          <td><strong title={row.name}>{row.name}</strong>{row.detail && <span title={row.detail}>{row.detail}</span>}<time title={formatDateTime(row.time)}>{formatDateTime(row.time)}</time>
            {error && <details className="hbdr-operation-error"><summary>{outcome?.label} · View details</summary><p>{error}</p></details>}
          </td>
          <td title={row.cluster}>{row.cluster}</td>
          <td><span className={`hbdr-operation-status is-${outcome?.tone || 'neutral'}`} role="status">{outcome?.label || (started && !result ? 'Not selected' : row.status)}</span>
            {result?.unavailable && <button type="button" className="hbdr-operation-link" onClick={onRefresh}>Refresh status</button>}
            {result?.acceptedAt && outcome && !outcome.terminal && !result.unavailable && Date.now() - result.acceptedAt > 60000 && <small>Still processing</small>}
          </td>
        </tr>;
      })}</tbody>
    </table>}
    {started && <p className="hbdr-operation-note">Closing this window will not stop submitted operations.</p>}
  </div>;
}
