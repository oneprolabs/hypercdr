import { useState } from 'react';
import { motion } from 'motion/react';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { SearchBar } from '../../components/search-bar';

export default function FailbackPage({ toast }: { toast: (msg: string) => void }) {
  const [selectedApp, setSelectedApp] = useState<string | null>(null);
  const rows: Array<{ name: string; source: string; target: string; point: string; status: string }> = [];
  const failbackColumns: HyperTableColumn<typeof rows[number]>[] = [
    { id: 'select', header: '', cell: info => <input type="radio" checked={selectedApp === info.row.original.name} onClick={event => event.stopPropagation()} onChange={() => setSelectedApp(info.row.original.name)} />, size: 42, minSize: 42, maxSize: 54, enableSorting: false, enableResizing: false, meta: { align: 'center' } },
    { id: 'name', header: 'Application', accessorFn: row => row.name, size: 270, minSize: 200, maxSize: 520, cell: info => <div><p className="text-sm font-black text-slate-900">{info.row.original.name}</p><p className="text-xs text-slate-400">Application-level DR runtime</p></div>, meta: { title: row => row.name } },
    { id: 'source', header: 'Source', accessorFn: row => row.source, size: 190, minSize: 150, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.source}</span>, meta: { title: row => row.source } },
    { id: 'target', header: 'Target', accessorFn: row => row.target, size: 210, minSize: 160, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.target}</span>, meta: { title: row => row.target } },
    { id: 'point', header: 'Restore Point', accessorFn: row => row.point, size: 170, minSize: 140, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.point}</span>, meta: { title: row => row.point } },
    { id: 'status', header: 'Status', accessorFn: row => row.status, size: 150, minSize: 120, cell: info => <span className="w-fit rounded-full border border-blue-100 bg-blue-50 px-2 py-1 text-[10px] font-black text-blue-700">{info.row.original.status}</span>, meta: { title: row => row.status } },
  ];

  return (
    <motion.div key="failback" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <SearchBar title="Failback" desc="Fail back taken-over applications to the production cluster." action="Refresh Status" onAction={() => toast('Failback status refreshed')} />
      <div className="rounded border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-2.5">
          <div><p className="text-sm font-black text-slate-900">Failback Applications</p><p className="mt-1 text-xs font-medium text-slate-400">{selectedApp ? `Selected ${selectedApp}` : 'Select a taken-over application'}</p></div>
          <button disabled={!selectedApp} onClick={() => toast(`${selectedApp} submitted failback precheck`)} className="hbdr-dr-action-primary">Start Failback Precheck</button>
        </div>
        <HyperTable variant="page" density="comfortable" columns={failbackColumns} data={rows} getRowId={row => row.name} onRowClick={row => setSelectedApp(row.name)} getRowClassName={row => selectedApp === row.name ? 'hbdr-dr-row-selected' : ''} emptyMessage="No failback applications available." />
      </div>
    </motion.div>
  );
}
