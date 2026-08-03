import { useEffect, useState } from 'react';
import { History, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiGet } from '../../api/client';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatLocalDateKey, formatLocalDateTime } from '../../lib/date-time';

type ApiList<T> = { items: T[] };
type ApiAuditLog = {
  id: string; actorId?: string; actor: string; action: string; resourceType: string;
  resourceId?: string; resourceName?: string; result: 'Success' | 'Failed' | string;
  message?: string; payload?: Record<string, unknown>; createdAt: string;
};
const listItems = <T,>(response: ApiList<T>) => response.items || [];

export default function RealOperationsPage() {
  const [selectedJob, setSelectedJob] = useState<string | null>(null);
  const [auditLogs, setAuditLogs] = useState<ApiAuditLog[]>([]);
  const [query,setQuery]=useState(''); const [statusFilter,setStatusFilter]=useState('all'); const [typeFilter,setTypeFilter]=useState('all'); const [dateFrom,setDateFrom]=useState(''); const [dateTo,setDateTo]=useState('');

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const response = await apiGet<ApiList<ApiAuditLog>>('/api/v1/audit-logs?limit=1000');
        if (!cancelled) setAuditLogs(listItems(response));
      } catch {
        // Keep the current audit list visible if one refresh fails.
      }
    };
    load();
    return () => {
      cancelled = true;
    };
  }, []);

  const selectedAudit = auditLogs.find(item => item.id === selectedJob) || null;
  const jobs = auditLogs.map(item => ({
    id: item.id,
    type: item.action,
    object: item.resourceName || [item.resourceType, item.resourceId].filter(Boolean).join(' · ') || item.resourceType,
    status: item.result || 'Success',
    user: item.actor || 'Unknown',
    time: formatLocalDateTime(item.createdAt),
    dateKey: formatLocalDateKey(item.createdAt),
  })).filter(job => (statusFilter==='all'||job.status===statusFilter)&&(typeFilter==='all'||job.type===typeFilter)&&(!dateFrom||job.dateKey>=dateFrom)&&(!dateTo||job.dateKey<=dateTo)&&(!query.trim()||[job.id,job.type,job.object,job.status,job.user].some(value=>String(value).toLowerCase().includes(query.trim().toLowerCase()))));
  const exportAudit=()=>{const escape=(value:unknown)=>`"${String(value??'').replace(/"/g,'""')}"`;const csv=[['Operation','Resource','Result','User','Time'],...jobs.map(job=>[job.type,job.object,job.status,job.user,job.time])].map(row=>row.map(escape).join(',')).join('\n');const blob=new Blob(['\uFEFF'+csv],{type:'text/csv;charset=utf-8'});const url=URL.createObjectURL(blob);const link=document.createElement('a');link.href=url;link.download=`hypercdr-operation-history-${new Date().toISOString().slice(0,10)}.csv`;link.click();URL.revokeObjectURL(url)};
  const operationColumns: HyperTableColumn<typeof jobs[number]>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="radio"
          checked={selectedJob === info.row.original.id}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedJob(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    { id: 'type', header: 'Operation', accessorFn: job => job.type, size: 190, minSize: 150, cell: info => <span className="font-black text-slate-900">{info.row.original.type}</span>, meta: { title: job => job.type } },
    { id: 'object', header: 'Resource', accessorFn: job => job.object, size: 300, minSize: 180, maxSize: 520, cell: info => info.row.original.object, meta: { title: job => job.object } },
    { id: 'status', header: 'Result', accessorFn: job => job.status, size: 130, minSize: 110, cell: info => <span className={info.row.original.status === 'Success' ? 'text-emerald-600' : 'text-rose-600'}>{info.row.original.status}</span>, meta: { title: job => job.status } },
    { id: 'user', header: 'User', accessorFn: job => job.user, size: 150, minSize: 120, cell: info => info.row.original.user, meta: { title: job => job.user } },
    { id: 'time', header: 'Time', accessorFn: job => job.time, size: 190, minSize: 150, maxSize: 280, cell: info => info.row.original.time, meta: { title: job => job.time } },
  ];

  return (
    <motion.div key="operations" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><History size={18} /></div>
            <div className="hbdr-history-page-title">
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">History</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Review user operations and their results.</p>
            </div>
          </div>
          <div />
        </div>
      </div>

      <div className="hbdr-dr-table-card hbdr-history-table-list">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group">
              <button type="button" onClick={exportAudit} className="hbdr-dr-action-primary">Export</button>
              <button type="button" disabled={!selectedJob} onClick={() => setSelectedJob(null)} className="hbdr-dr-more">Clear Selection</button>
            </div>
            <div className="hbdr-dr-query-group hbdr-history-query-group">
              <select aria-label="Filter by type" value={typeFilter} onChange={event => setTypeFilter(event.target.value)}>
                <option value="all">All types</option>
                {Array.from(new Set(auditLogs.map(item => item.action))).sort().map(action => <option key={action} value={action}>{action}</option>)}
              </select>
              <select aria-label="Filter by status" value={statusFilter} onChange={event => setStatusFilter(event.target.value)}>
                <option value="all">All statuses</option>
                <option value="Success">Success</option>
                <option value="Failed">Failed</option>
              </select>
              <label className="hbdr-history-date-field">
                <span>From</span>
                <input aria-label="From date" type="date" value={dateFrom} onChange={event => setDateFrom(event.target.value)} />
              </label>
              <label className="hbdr-history-date-field">
                <span>To</span>
                <input aria-label="To date" type="date" value={dateTo} onChange={event => setDateTo(event.target.value)} />
              </label>
              {(dateFrom || dateTo) && <button type="button" title="Clear dates" aria-label="Clear dates" onClick={() => { setDateFrom(''); setDateTo(''); }}><X size={16} /></button>}
              <label className="hbdr-dr-search">
                <input value={query} onChange={event => setQuery(event.target.value)} placeholder="Enter search text" />
              </label>
            </div>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={operationColumns}
          data={jobs}
          getRowId={row => row.id}
          onRowClick={row => setSelectedJob(row.id)}
          getRowClassName={row => selectedJob === row.id ? 'hbdr-dr-row-selected' : ''}
          emptyMessage="No user operation records available."
          className="hbdr-history-hyper-table"
        />
      </div>
      <AnimatePresence>
        {selectedAudit && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => setSelectedJob(null)} />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-history-detail-drawer" role="dialog" aria-modal="true" aria-label="Operation details">
              <div className="hbdr-filter-drawer-head">
                <div><h3>Operation Details</h3><p>Recorded user action and platform response.</p></div>
                <button type="button" onClick={() => setSelectedJob(null)} aria-label="Close operation details"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-resource-detail">
                <div className="hbdr-resource-detail-section">
                  <h4>Summary</h4>
                  <dl>
                    <div><dt>Operation</dt><dd>{selectedAudit.action}</dd></div>
                    <div><dt>Result</dt><dd className={selectedAudit.result === 'Success' ? 'text-emerald-600' : 'text-rose-600'}>{selectedAudit.result}</dd></div>
                    <div><dt>User</dt><dd>{selectedAudit.actor || 'Unknown'}</dd></div>
                    <div><dt>Time</dt><dd>{formatLocalDateTime(selectedAudit.createdAt)}</dd></div>
                  </dl>
                </div>
                <div className="hbdr-resource-detail-section">
                  <h4>Resource</h4>
                  <dl>
                    <div><dt>Type</dt><dd>{selectedAudit.resourceType}</dd></div>
                    <div><dt>Name</dt><dd>{selectedAudit.resourceName || '-'}</dd></div>
                    <div><dt>ID</dt><dd className="break-all font-mono">{selectedAudit.resourceId || '-'}</dd></div>
                  </dl>
                </div>
                {selectedAudit.message && <div className={`hbdr-resource-detail-section ${selectedAudit.result === 'Failed' ? 'text-rose-700' : ''}`}><h4>Details</h4><p className="whitespace-pre-wrap break-words text-xs leading-5">{selectedAudit.message}</p></div>}
              </div>
            </motion.aside>
          </>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
