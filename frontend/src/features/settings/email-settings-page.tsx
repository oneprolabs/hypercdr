import { useCallback, useEffect, useMemo, useState } from 'react';
import { CheckCircle2, ChevronDown, Edit3, Send, Settings2, Star, Trash2, X, XCircle } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiGet, apiPost, apiPut } from '../../api/client';
import type { ApiLoginResponse } from '../../auth/types';
import { EditField } from '../../components/edit-field';
import ListToolbarControls, { listToolbarQueryFields, matchesColumnFilterToken } from '../../components/list-toolbar-controls';
import { HyperTable } from '../../components/table/HyperTable';
import type { HyperTableColumn } from '../../components/table/types';

type Security = 'none' | 'starttls' | 'tls';
type TestStatus = 'not_tested' | 'succeeded' | 'failed';
type ApiEmailSettings = {
  id: string; name: string; isDefault: boolean; enabled: boolean; host: string; port: number;
  security: Security; username: string; passwordConfigured: boolean; senderName: string;
  senderEmail: string; lastTestStatus: TestStatus; lastTestedAt?: string; lastTestError?: string;
  updatedAt?: string;
};
type EmailSettingsDraft = ApiEmailSettings & { password: string };

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const emptyDraft = (): EmailSettingsDraft => ({
  id: '', name: '', isDefault: false, enabled: true, host: '', port: 587, security: 'starttls',
  username: '', passwordConfigured: false, password: '', senderName: 'HyperCDR', senderEmail: '',
  lastTestStatus: 'not_tested',
});
const errorMessage = (error: unknown, fallback: string) => error instanceof Error ? error.message : fallback;
const formatTestTime = (value?: string) => value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : 'Never';

function TestStatusBadge({ item }: { item: ApiEmailSettings }) {
  if (item.lastTestStatus === 'succeeded') return <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-1 text-[11px] font-bold text-emerald-700"><CheckCircle2 size={12} />Succeeded</span>;
  if (item.lastTestStatus === 'failed') return <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-2 py-1 text-[11px] font-bold text-rose-700"><XCircle size={12} />Failed</span>;
  return <span className="inline-flex rounded-full bg-slate-100 px-2 py-1 text-[11px] font-bold text-slate-500">Not tested</span>;
}

export default function EmailSettingsPage({ currentUser, toast }: { currentUser: ApiLoginResponse['user']; toast: (message: string) => void }) {
  const [items, setItems] = useState<ApiEmailSettings[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [draft, setDraft] = useState<EmailSettingsDraft | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ApiEmailSettings | null>(null);
  const [testTarget, setTestTarget] = useState<ApiEmailSettings | null>(null);
  const [recipient, setRecipient] = useState(currentUser.email === 'admin' ? '' : currentUser.email);
  const [busy, setBusy] = useState('');
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState(['host', 'port', 'security', 'username', 'senderName', 'senderEmail', 'isDefault', 'lastTestStatus']);
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [moreOpen, setMoreOpen] = useState(false);

  const load = useCallback(async () => {
    setLoadError('');
    try {
      const response = await apiGet<{ items: ApiEmailSettings[] }>('/api/v1/email-settings/configurations');
      setItems(response.items || []);
    } catch (error) {
      setLoadError(errorMessage(error, 'Failed to load SMTP configurations.'));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const selectedItem = selectedIds.length === 1 ? items.find(item => item.id === selectedIds[0]) : undefined;
  const columnOptions = useMemo(() => [
    { value: 'isDefault', label: 'Default', locked: true },
    { value: 'name', label: 'Configuration Name', locked: true },
    { value: 'host', label: 'SMTP Server' },
    { value: 'port', label: 'Port' },
    { value: 'security', label: 'Encryption' },
    { value: 'username', label: 'Username' },
    { value: 'senderName', label: 'Sender Name' },
    { value: 'senderEmail', label: 'Sender Email' },
    { value: 'lastTestStatus', label: 'Test Status' },
  ], []);
  const queryFields = useMemo(() => listToolbarQueryFields([], columnOptions, ['name', ...visibleColumns]), [columnOptions, visibleColumns]);
  const filteredItems = useMemo(() => {
    const value = query.trim().toLowerCase();
    return items.filter(item => {
      const fieldValue = queryField === 'port' ? String(item.port) : queryField === 'isDefault' ? (item.isDefault ? 'yes default' : 'no') : String(item[queryField as keyof ApiEmailSettings] ?? '');
      if (value && !fieldValue.toLowerCase().includes(value)) return false;
      if (activeTags.length && !activeTags.includes(item.security)) return false;
      return activeFilters.every(filter => matchesColumnFilterToken(filter, field => field === 'port' ? String(item.port) : field === 'isDefault' ? (item.isDefault ? 'yes default' : 'no') : String(item[field as keyof ApiEmailSettings] ?? '')));
    });
  }, [activeFilters, activeTags, items, query, queryField]);
  useEffect(() => { setSelectedIds(current => current.filter(id => items.some(item => item.id === id))); }, [items]);
  const toggleSelected = (id: string) => setSelectedIds(current => current.includes(id) ? current.filter(value => value !== id) : [...current, id]);
  const updateDraft = <K extends keyof EmailSettingsDraft>(key: K, value: EmailSettingsDraft[K]) => setDraft(current => current ? { ...current, [key]: value } : current);
  const draftValid = Boolean(draft && draft.name.trim() && draft.host.trim() && draft.port > 0 && draft.port <= 65535 && emailPattern.test(draft.senderEmail) && (draft.id || draft.password));

  const save = async () => {
    if (!draft) return;
    setBusy('save');
    try {
      const payload = { ...draft, name: draft.name.trim(), host: draft.host.trim(), senderEmail: draft.senderEmail.trim() };
      if (draft.id) await apiPut(`/api/v1/email-settings/configurations/${draft.id}`, payload);
      else await apiPost('/api/v1/email-settings/configurations', payload);
      toast(draft.id ? 'SMTP configuration updated.' : 'SMTP configuration created.');
      setDraft(null);
      await load();
    } catch (error) { toast(errorMessage(error, 'Failed to save SMTP configuration.')); }
    finally { setBusy(''); }
  };
  const runTest = async (item: ApiEmailSettings) => {
    if (!emailPattern.test(recipient)) { toast('Enter a valid recipient email address.'); return; }
    setBusy(`test:${item.id}`);
    try {
      await apiPost(`/api/v1/email-settings/configurations/${item.id}/test`, { recipient });
      toast(`Test email sent with ${item.name}.`);
      setTestTarget(current => current?.id === item.id ? { ...current, lastTestStatus: 'succeeded', lastTestedAt: new Date().toISOString(), lastTestError: '' } : current);
      await load();
    } catch (error) {
      const message = errorMessage(error, 'Failed to send test email.');
      toast(message);
      setTestTarget(current => current?.id === item.id ? { ...current, lastTestStatus: 'failed', lastTestedAt: new Date().toISOString(), lastTestError: message } : current);
      await load();
    }
    finally { setBusy(''); }
  };
  const setDefault = async (item: ApiEmailSettings) => {
    setBusy(`default:${item.id}`);
    try { await apiPost(`/api/v1/email-settings/configurations/${item.id}/default`, {}); toast(`${item.name} is now the default SMTP configuration.`); await load(); }
    catch (error) { toast(errorMessage(error, 'Failed to change the default SMTP configuration.')); }
    finally { setBusy(''); }
  };
  const remove = async () => {
    if (!deleteTarget) return;
    setBusy(`delete:${deleteTarget.id}`);
    try { await apiDelete(`/api/v1/email-settings/configurations/${deleteTarget.id}`); toast('SMTP configuration deleted.'); setDeleteTarget(null); await load(); }
    catch (error) { toast(errorMessage(error, 'Failed to delete SMTP configuration.')); }
    finally { setBusy(''); }
  };

  const columns = useMemo<HyperTableColumn<ApiEmailSettings>[]>(() => [
    { id: 'select', header: () => <input type="checkbox" aria-label="Select all SMTP configurations" checked={filteredItems.length > 0 && filteredItems.every(item => selectedIds.includes(item.id))} onChange={event => setSelectedIds(event.target.checked ? filteredItems.map(item => item.id) : [])} onClick={event => event.stopPropagation()} />, size: 48, enableSorting: false, enableResizing: false, cell: ({ row }) => <input type="checkbox" aria-label={`Select ${row.original.name}`} checked={selectedIds.includes(row.original.id)} onChange={() => toggleSelected(row.original.id)} onClick={event => event.stopPropagation()} /> },
    { accessorKey: 'isDefault', header: 'Default', size: 105, cell: ({ row }) => row.original.isDefault ? <span className="inline-flex items-center gap-1 font-bold text-blue-700"><Star size={12} fill="currentColor" />Yes</span> : 'No' },
    { accessorKey: 'name', header: 'Configuration Name', size: 190, cell: ({ row }) => <span className="truncate font-bold text-slate-800">{row.original.name}</span>, meta: { title: row => row.name } },
    ...(visibleColumns.includes('host') ? [{ accessorKey: 'host', header: 'SMTP Server', size: 190, cell: ({ row }: { row: { original: ApiEmailSettings } }) => <span className="font-medium text-slate-700">{row.original.host}</span>, meta: { title: (row: ApiEmailSettings) => row.host } } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('port') ? [{ accessorKey: 'port', header: 'Port', size: 90 } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('security') ? [{ accessorKey: 'security', header: 'Encryption', size: 120, cell: ({ row }: { row: { original: ApiEmailSettings } }) => row.original.security.toUpperCase() } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('username') ? [{ accessorKey: 'username', header: 'Username', size: 170, cell: ({ row }: { row: { original: ApiEmailSettings } }) => row.original.username || '-' } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('senderName') ? [{ accessorKey: 'senderName', header: 'Sender Name', size: 150, cell: ({ row }: { row: { original: ApiEmailSettings } }) => row.original.senderName || '-' } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('senderEmail') ? [{ accessorKey: 'senderEmail', header: 'Sender Email', size: 210, meta: { title: (row: ApiEmailSettings) => row.senderEmail } } as HyperTableColumn<ApiEmailSettings>] : []),
    ...(visibleColumns.includes('lastTestStatus') ? [{ accessorKey: 'lastTestStatus', header: 'Test Status', size: 130, cell: ({ row }: { row: { original: ApiEmailSettings } }) => <TestStatusBadge item={row.original} /> } as HyperTableColumn<ApiEmailSettings>] : []),
  ], [filteredItems, selectedIds, visibleColumns]);

  return <motion.div key="email-settings" initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Settings2 size={18} /></div><div><h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Email Settings</h3><p className="mt-0.5 text-[11px] font-medium text-slate-400">Manage SMTP delivery for notifications and password recovery.</p></div></div><div /></div></div>

    <section className="hbdr-dr-table-card"><div className="hbdr-dr-table-head"><div className="hbdr-dr-toolbar"><div className="hbdr-dr-action-group"><button type="button" className="hbdr-dr-action-primary" onClick={() => setDraft(emptyDraft())}>New</button><div className="relative"><button type="button" disabled={!selectedItem} onClick={() => setMoreOpen(current => !current)} className="hbdr-dr-more">More <ChevronDown size={15} className={moreOpen ? 'rotate-180 transition-transform' : 'transition-transform'} /></button><AnimatePresence>{moreOpen && selectedItem && <><div className="fixed inset-0 z-30" onClick={() => setMoreOpen(false)} /><motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"><button type="button" onClick={() => { setDraft({ ...selectedItem, password: '' }); setMoreOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Edit3 size={14} />Edit</button><button type="button" onClick={() => { setTestTarget(selectedItem); setMoreOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Send size={14} />Test</button><button type="button" disabled={selectedItem.isDefault} onClick={() => { void setDefault(selectedItem); setMoreOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Star size={14} />Set as Default</button><button type="button" disabled={selectedItem.isDefault} onClick={() => { setDeleteTarget(selectedItem); setMoreOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Trash2 size={14} />Delete</button></motion.div></>}</AnimatePresence></div></div><ListToolbarControls query={query} setQuery={setQuery} queryField={queryField} setQueryField={setQueryField} queryFields={queryFields} tags={[{ value: 'starttls', label: 'STARTTLS', count: items.filter(item => item.security === 'starttls').length }, { value: 'tls', label: 'TLS', count: items.filter(item => item.security === 'tls').length }, { value: 'none', label: 'None', count: items.filter(item => item.security === 'none').length }]} activeTags={activeTags} setActiveTags={setActiveTags} filters={[]} activeFilters={activeFilters} setActiveFilters={setActiveFilters} columns={columnOptions} visibleColumns={['name', ...visibleColumns]} setVisibleColumns={value => setVisibleColumns(current => { const next = typeof value === 'function' ? value(['name', ...current]) : value; return next.filter(column => column !== 'name'); })} onRefresh={() => { setLoading(true); setSelectedIds([]); void load(); }} /></div></div>
      {loadError ? <div className="flex min-h-40 flex-col items-center justify-center gap-3 p-6 text-sm text-rose-600"><span>{loadError}</span><button type="button" className="hbdr-dr-action-primary" onClick={() => { setLoading(true); void load(); }}>Retry</button></div> : loading ? <div className="flex min-h-40 items-center justify-center text-sm font-medium text-slate-400">Loading SMTP configurations...</div> : <HyperTable variant="page" density="comfortable" columns={columns} data={filteredItems} getRowId={item => item.id} onRowClick={item => toggleSelected(item.id)} getRowClassName={item => selectedIds.includes(item.id) ? 'hbdr-dr-row-selected' : ''} selectedCount={selectedIds.length} emptyMessage={query || activeTags.length || activeFilters.length ? 'No SMTP configurations match the current filters.' : 'No SMTP configurations have been created.'} resetPageOnDataChange />}
    </section>

    <AnimatePresence>{draft && <><motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => busy === '' && setDraft(null)} /><motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-label={draft.id ? 'Edit SMTP configuration' : 'Add SMTP configuration'}><div className="hbdr-filter-drawer-head"><div><strong>{draft.id ? 'Edit SMTP Configuration' : 'Add SMTP Configuration'}</strong><span>{draft.id ? 'Update delivery settings. Leave password blank to keep the saved password.' : 'Create a reusable SMTP delivery configuration.'}</span></div><button type="button" onClick={() => setDraft(null)} disabled={busy !== ''} aria-label="Close SMTP configuration drawer"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><section className="hbdr-advanced-filter-section"><h4>Configuration</h4><div className="hbdr-advanced-filter-box space-y-4"><EditField label="Configuration Name" value={draft.name} onChange={value => updateDraft('name', value)} placeholder="Primary SMTP" /><div className="grid grid-cols-2 gap-3"><EditField label="SMTP Server" value={draft.host} onChange={value => updateDraft('host', value)} placeholder="smtp.example.com" /><EditField label="Port" value={String(draft.port)} onChange={value => updateDraft('port', Number(value) || 0)} /></div><label className="block text-xs font-semibold text-slate-600">Encryption<select value={draft.security} onChange={event => updateDraft('security', event.target.value as Security)} className="mt-1 h-10 w-full rounded-lg border border-slate-200 px-3"><option value="starttls">STARTTLS</option><option value="tls">TLS</option><option value="none">None</option></select></label><EditField label="Username" value={draft.username} onChange={value => updateDraft('username', value)} /><EditField label="Password" type="password" value={draft.password} onChange={value => updateDraft('password', value)} placeholder={draft.passwordConfigured ? 'Configured — leave blank to keep' : 'Enter SMTP password'} /><EditField label="Sender Name" value={draft.senderName} onChange={value => updateDraft('senderName', value)} /><EditField label="Sender Email" value={draft.senderEmail} onChange={value => updateDraft('senderEmail', value)} placeholder="noreply@example.com" /></div></section></div><div className="hbdr-filter-drawer-actions"><button type="button" disabled={!draftValid || busy !== ''} onClick={() => void save()}>{busy === 'save' ? 'Saving...' : draft.id ? 'Save Changes' : 'Create Configuration'}</button><button type="button" disabled={busy !== ''} onClick={() => setDraft(null)}>Cancel</button></div></motion.aside></>}</AnimatePresence>

    <AnimatePresence>{testTarget && <><motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => busy === '' && setTestTarget(null)} /><motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-label="Test SMTP configuration"><div className="hbdr-filter-drawer-head"><div><strong>Test SMTP Configuration</strong><span>{testTarget.name} · {testTarget.host}:{testTarget.port}</span></div><button type="button" onClick={() => setTestTarget(null)} disabled={busy !== ''} aria-label="Close SMTP test drawer"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><section className="hbdr-advanced-filter-section"><h4>Test Email</h4><div className="hbdr-advanced-filter-box space-y-4"><EditField label="Recipient Email" value={recipient} onChange={setRecipient} placeholder="admin@example.com" />{testTarget.lastTestStatus !== 'not_tested' && <div className="rounded-xl border border-slate-200 bg-slate-50 p-3"><div className="flex items-center justify-between gap-3"><TestStatusBadge item={testTarget} /><span className="text-[11px] text-slate-400">{formatTestTime(testTarget.lastTestedAt)}</span></div>{testTarget.lastTestError && <p className="mt-2 break-words text-xs leading-5 text-rose-600">{testTarget.lastTestError}</p>}</div>}</div></section></div><div className="hbdr-filter-drawer-actions"><button type="button" disabled={busy !== '' || !emailPattern.test(recipient)} onClick={() => void runTest(testTarget)}>{busy === `test:${testTarget.id}` ? 'Sending...' : 'Send Test Email'}</button><button type="button" disabled={busy !== ''} onClick={() => setTestTarget(null)}>Cancel</button></div></motion.aside></>}</AnimatePresence>

    <AnimatePresence>{deleteTarget && <><motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={() => busy === '' && setDeleteTarget(null)} /><motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-label="Delete SMTP configuration"><div className="hbdr-filter-drawer-head"><div><strong>Delete SMTP Configuration</strong><span>This action cannot be undone.</span></div><button type="button" onClick={() => setDeleteTarget(null)} disabled={busy !== ''} aria-label="Close delete drawer"><X size={18} /></button></div><div className="hbdr-filter-drawer-body"><section className="hbdr-advanced-filter-section"><h4>Confirmation</h4><div className="hbdr-advanced-filter-box"><p className="text-sm leading-6 text-slate-600">Delete <strong className="text-slate-900">{deleteTarget.name}</strong>? It will no longer be available for email delivery.</p></div></section></div><div className="hbdr-filter-drawer-actions"><button type="button" disabled={busy !== ''} onClick={() => void remove()}>{busy === `delete:${deleteTarget.id}` ? 'Deleting...' : 'Delete Configuration'}</button><button type="button" disabled={busy !== ''} onClick={() => setDeleteTarget(null)}>Cancel</button></div></motion.aside></>}</AnimatePresence>
  </motion.div>;
}
