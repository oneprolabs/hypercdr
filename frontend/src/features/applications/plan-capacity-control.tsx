import { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import { formatDateTime } from '../../lib/date-time';
import { formatBytes } from '../recovery/task-ui';

type CapacitySize = { totalBytes?: number; collectedAt?: string; status?: string; errorMessage?: string };
type CapacityTask = { id: string; status: string; errorMessage?: string };
type CapacityState = { size: CapacitySize | null; task: CapacityTask | null };
const running = (task: CapacityTask | null) => !!task && !['succeeded', 'failed', 'cancelled'].includes(task.status);

export function PlanCapacityControl({ planId }: { planId: string }) {
  const [state, setState] = useState<CapacityState>({ size: null, task: null });
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const path = `/api/v1/protection-plans/${encodeURIComponent(planId)}/capacity`;
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      try {
        const result = await apiGet<CapacityState>(path);
        if (cancelled) return;
        setState(result);
        setError('');
      } catch (reason) {
        if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason));
      } finally {
        if (!cancelled) { setLoading(false); timer = setTimeout(refresh, 5000); }
      }
    };
    void refresh();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [path]);
  const busy = loading || submitting || running(state.task);
  const refresh = async () => {
    if (busy) return;
    setSubmitting(true);
    setError('');
    try {
      const result = await apiPost<{ task: CapacityTask }>(`${path}/refresh`, {});
      setState(current => ({ ...current, task: result.task }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally { setSubmitting(false); }
  };
  const failure = error || (state.task?.status === 'failed' ? state.task.errorMessage || 'Capacity refresh failed.' : state.size?.status === 'failed' ? state.size.errorMessage || 'Capacity refresh failed.' : '');
  return <div className="hbdr-plan-capacity">
    <div className="hbdr-plan-capacity-row"><span>{typeof state.size?.totalBytes === 'number' ? `Used ${formatBytes(state.size.totalBytes)}` : 'Pending collection'}</span><button type="button" disabled={busy} onClick={() => void refresh()} aria-label="Refresh capacity" title={busy && !loading ? 'Refreshing capacity…' : 'Refresh capacity'} aria-busy={busy && !loading}><RefreshCw size={14} className={busy && !loading ? 'animate-spin' : ''} /></button></div>
    {state.size?.collectedAt && <span className="hbdr-plan-capacity-time">Updated {formatDateTime(state.size.collectedAt)}</span>}
    {failure && <span role="alert" className="text-rose-600">{failure}</span>}
  </div>;
}
