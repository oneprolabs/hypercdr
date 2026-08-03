import { useCallback, useEffect, useState } from 'react';
import { Building2, ChevronDown, Edit2, Trash2, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiGet, apiPatch, apiPost } from '../../api/client';
import { EditField } from '../../components/edit-field';
import { HyperTable, type HyperTableColumn } from '../../components/table';

type ApiList<T> = { items: T[] };
type ApiTenant = { id: string; name: string; description: string; status: 'active' | 'disabled'; userCount: number; clusterCount: number; createdAt: string; updatedAt: string };
const storeDefaultTenantId = '00000000-0000-0000-0000-000000000001';
const listItems = <T,>(response: ApiList<T>) => response.items || [];

export default function TenantPage({ toast }: { toast: (message: string) => void }) {
  const [tenants, setTenants] = useState<ApiTenant[]>([]);
  const [selectedTenant, setSelectedTenant] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [editing, setEditing] = useState<ApiTenant | 'new' | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [status, setStatus] = useState<'active' | 'disabled'>('active');
  const [busy, setBusy] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const load = useCallback(async () => setTenants(listItems(await apiGet<ApiList<ApiTenant>>('/api/v1/tenants'))), []);
  useEffect(() => { void load().catch(error => toast(error instanceof Error ? error.message : 'Failed to load tenants')); }, [load, toast]);
  const selected = tenants.find(item => item.id === selectedTenant);
  const openNew = () => { setEditing('new'); setName(''); setDescription(''); setStatus('active'); };
  const openEdit = () => { if (selected) { setEditing(selected); setName(selected.name); setDescription(selected.description ?? ''); setStatus(selected.status); } };
  const save = async () => { setBusy(true); try { if (editing === 'new') await apiPost('/api/v1/tenants', {name,description,status}); else if (editing) await apiPatch(`/api/v1/tenants/${editing.id}`, {name,description,status}); setEditing(null); await load(); toast('Tenant saved'); } finally { setBusy(false); } };
  const remove = async () => { if (!selected) return; setBusy(true); try { await apiDelete(`/api/v1/tenants/${selected.id}`); setSelectedTenant(null); await load(); toast('Tenant deleted'); } finally { setBusy(false); } };
  const visibleTenants = tenants.filter(item => [item.name,item.description,item.status].some(value=>String(value||'').toLowerCase().includes(query.trim().toLowerCase())));
  const tenantColumns: HyperTableColumn<ApiTenant>[] = [
    {
      id: 'select',
      header: '',
      cell: info => (
        <input
          type="checkbox"
          checked={selectedTenant === info.row.original.id}
          onClick={event => event.stopPropagation()}
          onChange={() => setSelectedTenant(current => current === info.row.original.id ? null : info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Tenant',
      accessorFn: tenant => tenant.name,
      size: 220,
      minSize: 200,
      maxSize: 520,
      cell: info => <p className="text-sm font-black text-slate-900">{info.row.original.name}</p>,
      meta: { title: tenant => tenant.name },
    },
    { id: 'description', header: 'Description', accessorFn: tenant => tenant.description || '', size: 320, minSize: 200, cell: info => <span className="block truncate text-xs font-medium text-slate-500">{info.row.original.description || '—'}</span>, meta: { title: tenant => tenant.description || 'No description' } },
    { id: 'users', header: 'Users', accessorFn: tenant => tenant.userCount, size: 120, minSize: 100, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.userCount}</span>, meta: { title: tenant => `${tenant.userCount}` } },
    { id: 'clusters', header: 'Clusters', accessorFn: tenant => tenant.clusterCount, size: 120, minSize: 100, cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.clusterCount}</span>, meta: { title: tenant => `${tenant.clusterCount}` } },
    { id: 'status', header: 'Status', accessorFn: tenant => tenant.status, size: 120, minSize: 100, cell: info => <span className={`w-fit rounded-full border px-2 py-1 text-[10px] font-black ${info.row.original.status === 'active' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>{info.row.original.status === 'active' ? 'Active' : 'Disabled'}</span>, meta: { title: tenant => tenant.status } },
  ];

  return (
    <motion.div key="tenants" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Building2 size={18}/></div><div><h3 className="text-sm font-black tracking-tight text-slate-900">Tenant Management</h3><p className="mt-0.5 text-[11px] font-medium text-slate-400">Create and maintain isolated resource tenants.</p></div></div>
          <div />
        </div>
      </div>
      <div className="hbdr-dr-table-card hbdr-user-management-table">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group">
              <button type="button" onClick={openNew} className="hbdr-dr-action-primary">New</button>
              <div className="relative">
                <button type="button" disabled={!selected} onClick={()=>setMoreOpen(value=>!value)} className="hbdr-dr-more">More <ChevronDown size={15} className={moreOpen?'rotate-180 transition-transform':'transition-transform'}/></button>
                <AnimatePresence>{moreOpen && selected && <><div className="fixed inset-0 z-30" onClick={()=>setMoreOpen(false)}/><motion.div initial={{opacity:0,y:8,scale:.96}} animate={{opacity:1,y:0,scale:1}} exit={{opacity:0,y:8,scale:.96}} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"><button onClick={()=>{openEdit();setMoreOpen(false)}} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Edit2 size={14}/>Edit</button><button disabled={selected.id===storeDefaultTenantId||selected.userCount>0||selected.clusterCount>0||busy} onClick={()=>{setMoreOpen(false);void remove().catch(error=>toast(error instanceof Error?error.message:'Failed to delete tenant'))}} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-300"><Trash2 size={14}/>Delete</button></motion.div></>}</AnimatePresence>
              </div>
            </div>
            <div className="hbdr-dr-query-group hbdr-user-query-group"><label className="hbdr-dr-search"><input value={query} onChange={event=>setQuery(event.target.value)} placeholder="Enter search text"/></label></div>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={tenantColumns}
          data={visibleTenants}
          getRowId={row => row.id}
          onRowClick={row => setSelectedTenant(current => current === row.id ? null : row.id)}
          getRowClassName={row => selectedTenant === row.id ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedTenant ? 1 : 0}
          emptyMessage="No tenants available."
        />
      </div>
      <AnimatePresence>{editing && <><motion.div className="hbdr-filter-drawer-backdrop" initial={{opacity:0}} animate={{opacity:1}} exit={{opacity:0}} onClick={()=>setEditing(null)} /><motion.aside className="hbdr-filter-drawer hbdr-user-management-drawer" initial={{x:34,opacity:0}} animate={{x:0,opacity:1}} exit={{x:34,opacity:0}} role="dialog" aria-modal="true" aria-label={editing==='new'?'New Tenant':'Edit Tenant'}><div className="hbdr-filter-drawer-head"><div><strong>{editing === 'new' ? 'New Tenant' : 'Edit Tenant'}</strong><span>{editing==='new'?'Create an isolated tenant for users and resources.':'Update tenant information and access status.'}</span></div><button onClick={()=>setEditing(null)} aria-label="Close tenant drawer"><X size={18}/></button></div><div className="hbdr-filter-drawer-body"><div className="space-y-4"><EditField label="Tenant Name" value={name} onChange={setName}/><label className="block text-xs font-semibold tracking-normal text-slate-600">Description<textarea value={description} onChange={event=>setDescription(event.target.value)} maxLength={500} rows={4} placeholder="Briefly describe this tenant" className="mt-1 w-full resize-none rounded-lg border border-slate-200 px-3 py-2.5 text-sm font-medium text-slate-700 outline-none transition focus:border-blue-400 focus:ring-2 focus:ring-blue-100" /></label><label className="block text-xs font-semibold tracking-normal text-slate-600">Status<select value={status} onChange={event=>setStatus(event.target.value as 'active'|'disabled')} className="mt-1 h-10 w-full rounded-lg border border-slate-200 px-3"><option value="active">Active</option><option value="disabled">Disabled</option></select></label><div className="hbdr-filter-drawer-actions mt-6"><button className="hbdr-dr-action-primary" disabled={!name.trim()||busy} onClick={()=>void save().catch(error=>toast(error instanceof Error?error.message:'Failed to save tenant'))}>{busy?'Saving...':'Save'}</button><button disabled={busy} onClick={()=>setEditing(null)}>Cancel</button></div></div></div></motion.aside></>}</AnimatePresence>
    </motion.div>
  );
}
