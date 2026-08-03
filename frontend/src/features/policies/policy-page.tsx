import React, { useEffect, useMemo, useState } from 'react';
import { Calendar, CheckCircle2, ChevronDown, Clock, Edit2, Eye, History, Layers, MoreVertical, RefreshCw, Search, ShieldCheck, Sun, Trash2, X, Zap } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiPatch, apiPost } from '../../api/client';
import ListToolbarControls, { listToolbarQueryFields, matchesColumnFilterToken, parseColumnFilterToken } from '../../components/list-toolbar-controls';
import { ModalFrame } from '../../components/modal-frame';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { getUserTimeZone, userTimeZoneLabel } from '../../lib/date-time';

type PolicyComposition='manual'|'combined'|'schedule'|'retention';
type PolicyScheduleType='interval'|'daily'|'weekly'|'monthly';
type PolicyItem={id:string;name:string;composition:PolicyComposition;type:PolicyScheduleType;intervalValue:number;intervalUnit:'minutes'|'hours';hour:number;minute:number;weekDay:number;monthDay:number;retention?:number;status:'Active'|'Disabled';bound:number};
type ApiPolicy={id:string;name:string;composition:string;scheduleType:string;intervalValue?:number;intervalUnit?:string;hour?:number;minute?:number;weekDay?:number;monthDay?:number;retentionCount?:number;status:string;boundCount:number};
type ScheduleParts={hour:number;minute:number;weekDay:number;monthDay:number};
const weekdays=['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
const formatTime=(hour:number,minute:number)=>`${String(hour).padStart(2,'0')}:${String(minute).padStart(2,'0')}`;
const formatPolicyComposition=(value:PolicyComposition)=>value==='manual'?'Manual':value==='schedule'?'Schedule Only':value==='retention'?'Retention Only':'Schedule + Retention';
const formatPolicyType=(value:PolicyScheduleType)=>value==='interval'?'Interval':value==='daily'?'Daily Backup':value==='weekly'?'Weekly Backup':'Monthly Backup';
const formatPolicyRetention=(policy:Pick<PolicyItem,'composition'|'retention'>)=>policy.composition==='manual'?'Not defined':policy.composition==='schedule'?'Platform default':`${policy.retention??0} copies`;
const formatPolicySchedule=(policy:Pick<PolicyItem,'composition'|'type'|'intervalValue'|'intervalUnit'|'hour'|'minute'|'weekDay'|'monthDay'>)=>policy.composition==='manual'?'Manual trigger':policy.composition==='retention'?'Not scheduled':policy.type==='interval'?`Every ${policy.intervalValue} ${policy.intervalUnit==='minutes'?'minutes':'hours'}`:policy.type==='daily'?`Every day ${formatTime(policy.hour,policy.minute)}`:policy.type==='weekly'?`Every week ${weekdays[policy.weekDay]} ${formatTime(policy.hour,policy.minute)}`:`Every month ${policy.monthDay} Day ${formatTime(policy.hour,policy.minute)}`;
const datePartsInTimeZone=(date:Date,timeZone:string)=>{const parts=new Intl.DateTimeFormat('en-US',{timeZone,year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).formatToParts(date);const value=(type:string)=>Number(parts.find(part=>part.type===type)?.value||0);return{year:value('year'),month:value('month'),day:value('day'),hour:value('hour')%24,minute:value('minute'),second:value('second')}};
const zonedWallTimeToUTC=(year:number,month:number,day:number,hour:number,minute:number,timeZone:string)=>{const desired=Date.UTC(year,month-1,day,hour,minute,0);let result=new Date(desired);for(let attempt=0;attempt<3;attempt++){const actual=datePartsInTimeZone(result,timeZone);const correction=desired-Date.UTC(actual.year,actual.month-1,actual.day,actual.hour,actual.minute,0);if(correction===0)break;result=new Date(result.getTime()+correction)}return result};
const scheduleDisplayToUTC=(policy:Pick<PolicyItem,'type'|'hour'|'minute'|'weekDay'|'monthDay'>,now=new Date()):ScheduleParts=>{if(policy.type==='interval')return{hour:policy.hour,minute:policy.minute,weekDay:policy.weekDay,monthDay:policy.monthDay};const timeZone=getUserTimeZone();const local=datePartsInTimeZone(now,timeZone);let{year,month,day}=local;if(policy.type==='weekly'){const current=new Date(Date.UTC(year,month-1,day)).getUTCDay();day+=(policy.weekDay-current+7)%7}else if(policy.type==='monthly')day=Math.min(policy.monthDay,new Date(Date.UTC(year,month,0)).getUTCDate());const utc=zonedWallTimeToUTC(year,month,day,policy.hour,policy.minute,timeZone);let monthDay=utc.getUTCDate();if(policy.type==='monthly'&&(utc.getUTCFullYear()*12+utc.getUTCMonth()+1)<year*12+month)monthDay=31;return{hour:utc.getUTCHours(),minute:utc.getUTCMinutes(),weekDay:utc.getUTCDay(),monthDay}};
const scheduleUTCToDisplay=(policy:ApiPolicy,now=new Date()):ScheduleParts=>{if(!['daily','weekly','monthly'].includes(policy.scheduleType))return{hour:policy.hour||0,minute:policy.minute||0,weekDay:policy.weekDay||0,monthDay:policy.monthDay||1};let year=now.getUTCFullYear(),month=now.getUTCMonth(),day=now.getUTCDate();if(policy.scheduleType==='weekly')day+=((policy.weekDay||0)-now.getUTCDay()+7)%7;else if(policy.scheduleType==='monthly')day=Math.min(policy.monthDay||1,new Date(Date.UTC(year,month+1,0)).getUTCDate());const local=datePartsInTimeZone(new Date(Date.UTC(year,month,day,policy.hour||0,policy.minute||0)),getUserTimeZone());const lastDay=new Date(Date.UTC(local.year,local.month,0)).getUTCDate();return{hour:local.hour,minute:local.minute,weekDay:new Date(Date.UTC(local.year,local.month-1,local.day)).getUTCDay(),monthDay:policy.scheduleType==='monthly'&&local.day===lastDay?31:local.day}};
const mapPolicy=(policy:ApiPolicy):PolicyItem=>{const display=scheduleUTCToDisplay(policy);return{id:policy.id,name:policy.name,composition:['manual','combined','schedule','retention'].includes(policy.composition)?policy.composition as PolicyComposition:'combined',type:['daily','weekly','monthly'].includes(policy.scheduleType)?policy.scheduleType as PolicyScheduleType:'interval',intervalValue:policy.intervalValue||1,intervalUnit:policy.intervalUnit==='minute'||policy.intervalUnit==='minutes'?'minutes':'hours',...display,retention:policy.retentionCount||0,status:policy.status==='disabled'?'Disabled':'Active',bound:policy.boundCount||0}};
function TimeSelector({hour,minute,onChange}:{hour:number;minute:number;onChange:(hour:number,minute:number)=>void}){return <div className="flex items-center gap-2"><select value={hour} onChange={event=>onChange(Number(event.target.value),minute)} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:24}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select><span className="font-bold text-slate-500">:</span><select value={minute} onChange={event=>onChange(hour,Number(event.target.value))} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:60}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select></div>}
function Info({label,value}:{label:string;value:string}){return <div className="rounded bg-slate-50 p-3"><p className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</p><p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p></div>}

export default function PolicyPage({ policies, setPolicies }: { policies: PolicyItem[]; setPolicies: React.Dispatch<React.SetStateAction<PolicyItem[]>> }) {
  const defaultPolicyForm = (): Omit<PolicyItem, 'id' | 'status' | 'bound'> => ({
    name: '',
    composition: 'combined',
    type: 'interval',
    intervalValue: 5,
    intervalUnit: 'minutes',
    hour: 0,
    minute: 0,
    weekDay: 0,
    monthDay: 1,
    retention: 7,
  });

  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState(['composition', 'schedule', 'retention', 'bound', 'status']);
  const [selectedPolicyIds, setSelectedPolicyIds] = useState<string[]>([]);
  const [policyBulkMenuOpen, setPolicyBulkMenuOpen] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [editingPolicyId, setEditingPolicyId] = useState<string | null>(null);
  const [policyForm, setPolicyForm] = useState(defaultPolicyForm());
  const [policyModalOpen, setPolicyModalOpen] = useState(false);
  const [deletePolicy, setDeletePolicy] = useState<PolicyItem | null>(null);
  const [detailPolicy, setDetailPolicy] = useState<PolicyItem | null>(null);
  const [policyError, setPolicyError] = useState<string | null>(null);
  const [savingPolicy, setSavingPolicy] = useState(false);
  const showScheduleConfig = policyForm.composition !== 'retention';
  const showRetentionConfig = policyForm.composition !== 'schedule';

  useEffect(() => {
    if (!menuId) return;
    const closeMenu = () => setMenuId(null);
    window.addEventListener('click', closeMenu);
    return () => window.removeEventListener('click', closeMenu);
  }, [menuId]);

  const policyQueryValue = (policy: PolicyItem, field: string) => {
    if (field === 'composition') return formatPolicyComposition(policy.composition);
    if (field === 'schedule') return formatPolicySchedule(policy);
    if (field === 'retention') return formatPolicyRetention(policy);
    if (field === 'bound') return `${policy.bound} applications`;
    if (field === 'status') return policy.status;
    if (field === 'type') return formatPolicyType(policy.type);
    return policy.name;
  };
  const policyMatchesFilter = (policy: PolicyItem, filter: string) => {
    if (filter === 'active') return policy.status === 'Active';
    if (filter === 'disabled') return policy.status !== 'Active';
    if (filter === 'bound') return policy.bound > 0;
    if (filter === 'unbound') return policy.bound === 0;
    return true;
  };
  const filteredPolicies = policies.filter(policy => {
    const keyword = query.trim().toLowerCase();
    const queryMatched = !keyword || policyQueryValue(policy, queryField).toLowerCase().includes(keyword);
    const tagsMatched = activeTags.length === 0 || activeTags.includes(policy.composition);
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => policyQueryValue(policy, field));
      return policyMatchesFilter(policy, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  });
  const policyColumns = [
    { value: 'composition', label: 'Schedule Type' },
    { value: 'schedule', label: 'Execution Plan' },
    { value: 'retention', label: 'Retained Copies' },
    { value: 'bound', label: 'Bound Apps' },
    { value: 'status', label: 'Status' },
  ];
  const policyQueryFields = listToolbarQueryFields([{ value: 'name', label: 'Policy Name' }], policyColumns, visibleColumns);
  const selectedPolicies = policies.filter(policy => selectedPolicyIds.includes(policy.id));
  const singleSelectedPolicy = selectedPolicies.length === 1 ? selectedPolicies[0] : null;
  const allVisiblePoliciesSelected = filteredPolicies.length > 0 && filteredPolicies.every(policy => selectedPolicyIds.includes(policy.id));

  const toggleSelectedPolicy = (policyId: string) => {
    setSelectedPolicyIds(prev => prev.includes(policyId) ? prev.filter(id => id !== policyId) : [...prev, policyId]);
  };

  const toggleVisiblePolicies = () => {
    setSelectedPolicyIds(prev => {
      const visibleIds = filteredPolicies.map(policy => policy.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };

  const policyTableColumns = useMemo<HyperTableColumn<PolicyItem>[]>(() => {
    const columns: HyperTableColumn<PolicyItem>[] = [
      {
        id: 'select',
        header: () => (
          <input
            type="checkbox"
            checked={allVisiblePoliciesSelected}
            onClick={event => event.stopPropagation()}
            onChange={toggleVisiblePolicies}
          />
        ),
        cell: info => (
          <input
            type="checkbox"
            checked={selectedPolicyIds.includes(info.row.original.id)}
            onClick={event => event.stopPropagation()}
            onChange={() => toggleSelectedPolicy(info.row.original.id)}
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
        header: 'Policy Name',
        accessorFn: policy => policy.name,
        size: 280,
        minSize: 210,
        maxSize: 540,
        cell: info => {
          const policy = info.row.original;
          return (
            <div className="hbdr-dr-name-cell">
              <div className="hbdr-dr-namespace-icon">
                <ShieldCheck size={18} />
              </div>
              <div>
                <p className="hbdr-dr-app-name">{policy.name}</p>
              </div>
            </div>
          );
        },
        meta: { title: policy => `${policy.name} (${policy.id})` },
      },
    ];
    const addColumn = (column: HyperTableColumn<PolicyItem>) => {
      if (visibleColumns.includes(column.id as string)) columns.push(column);
    };
    addColumn({
      id: 'composition',
      header: 'Schedule Type',
      accessorFn: policy => formatPolicyComposition(policy.composition),
      size: 190,
      minSize: 170,
      cell: info => <span className="hbdr-dr-policy">{formatPolicyComposition(info.row.original.composition)}</span>,
      meta: { title: policy => formatPolicyComposition(policy.composition) },
    });
    addColumn({
      id: 'schedule',
      header: 'Execution Plan',
      accessorFn: policy => formatPolicySchedule(policy),
      size: 210,
      minSize: 160,
      maxSize: 360,
      cell: info => formatPolicySchedule(info.row.original),
      meta: { title: policy => formatPolicySchedule(policy) },
    });
    addColumn({
      id: 'retention',
      header: 'Retained Copies',
      accessorFn: policy => policy.retention,
      size: 150,
      minSize: 130,
      cell: info => formatPolicyRetention(info.row.original),
      meta: { title: policy => formatPolicyRetention(policy) },
    });
    addColumn({
      id: 'bound',
      header: 'Bound Apps',
      accessorFn: policy => policy.bound,
      size: 138,
      minSize: 120,
      cell: info => `${info.row.original.bound} applications`,
      meta: { title: policy => `${policy.bound} applications` },
    });
    addColumn({
      id: 'status',
      header: 'Status',
      accessorFn: policy => policy.status,
      size: 116,
      minSize: 100,
      cell: info => <span className={info.row.original.status === 'Active' ? 'hbdr-dr-task-ok' : 'hbdr-dr-task-warn'}>{info.row.original.status === 'Active' ? 'ACTIVE' : 'DISABLED'}</span>,
      meta: { title: policy => policy.status },
    });
    return columns;
  }, [allVisiblePoliciesSelected, selectedPolicyIds, visibleColumns]);

  const openCreatePolicy = () => {
    setEditingPolicyId(null);
    setPolicyForm(defaultPolicyForm());
    setPolicyModalOpen(true);
  };

  const openEditPolicy = (policy: PolicyItem) => {
    setEditingPolicyId(policy.id);
    setPolicyForm({
      name: policy.name,
      composition: policy.composition || 'combined',
      type: policy.type,
      intervalValue: policy.intervalValue,
      intervalUnit: policy.intervalUnit,
      hour: policy.hour,
      minute: policy.minute,
      weekDay: policy.weekDay,
      monthDay: policy.monthDay,
      retention: policy.retention,
    });
    setPolicyModalOpen(true);
    setMenuId(null);
  };

  const closePolicyModal = () => {
    setPolicyModalOpen(false);
    setEditingPolicyId(null);
    setPolicyForm(defaultPolicyForm());
  };

  const savePolicy = async () => {
    if (!policyForm.name.trim()) return;
    const normalizedForm = {
      ...policyForm,
      intervalValue: Math.max(1, Number(policyForm.intervalValue) || 1),
      retention: Math.max(1, Number(policyForm.retention) || 1),
    };
    const utcSchedule = scheduleDisplayToUTC(normalizedForm);
    const input = {
      name: normalizedForm.name.trim(),
      composition: normalizedForm.composition,
      scheduleType: normalizedForm.type,
      intervalValue: normalizedForm.intervalValue,
      intervalUnit: normalizedForm.intervalUnit,
      hour: utcSchedule.hour,
      minute: utcSchedule.minute,
      weekDay: utcSchedule.weekDay,
      monthDay: utcSchedule.monthDay,
      retentionCount: normalizedForm.retention,
      retentionDays: 0,
      status: 'active',
    };
		setSavingPolicy(true);
		setPolicyError(null);
    try {
      const created = editingPolicyId
				? await apiPatch<ApiPolicy>(`/api/v1/policies/${editingPolicyId}`, input)
				: await apiPost<ApiPolicy>('/api/v1/policies', input);
      const mapped = mapPolicy(created);
      setPolicies(prev => prev.some(p => p.id === mapped.id) ? prev.map(p => p.id === mapped.id ? mapped : p) : [mapped, ...prev]);
      closePolicyModal();
    } catch (error) {
			setPolicyError(error instanceof Error ? error.message : 'Failed to save policy');
		} finally {
			setSavingPolicy(false);
    }
  };

  const togglePolicyStatus = (policy: PolicyItem) => {
    setPolicies(prev => prev.map(item => item.id === policy.id ? { ...item, status: item.status === 'Active' ? 'Disabled' : 'Active' } : item));
    setMenuId(null);
  };

  const confirmDeletePolicy = async () => {
    if (!deletePolicy) return;
		setSavingPolicy(true);
		setPolicyError(null);
		try {
			await apiDelete(`/api/v1/policies/${deletePolicy.id}`);
			setPolicies(prev => prev.filter(policy => policy.id !== deletePolicy.id));
			setSelectedPolicyIds(prev => prev.filter(id => id !== deletePolicy.id));
			setDeletePolicy(null);
		} catch (error) {
			setPolicyError(error instanceof Error ? error.message : 'Failed to delete policy');
		} finally {
			setSavingPolicy(false);
		}
  };

  return (
    <motion.div key="policies" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><ShieldCheck size={18} /></div>
            <div className="hbdr-policy-page-title">
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Policies</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Maintain application protection plans and recovery targets.</p>
            </div>
          </div>
          <div />
        </div>
      </div>

      {true ? (
        <>
        <div className="hbdr-dr-table-card hbdr-policy-table-list">
          <div className="hbdr-dr-table-head">
            <div className="hbdr-dr-toolbar">
              <div className="hbdr-dr-action-group">
                <button aria-label="Create DR policy" title="Create DR policy" onClick={openCreatePolicy} className="hbdr-dr-action-primary">New</button>
                <div className="relative">
                  <button disabled={selectedPolicies.length === 0} onClick={() => setPolicyBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                    More <ChevronDown size={15} className={policyBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                  </button>
                  <AnimatePresence>
                    {policyBulkMenuOpen && selectedPolicies.length > 0 && (
                      <>
                        <div className="fixed inset-0 z-30" onClick={() => setPolicyBulkMenuOpen(false)} />
                        <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; setDetailPolicy(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Eye size={14} />View</button>
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; openEditPolicy(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Edit2 size={14} />Edit</button>
                          <button disabled={!singleSelectedPolicy} onClick={() => { if (!singleSelectedPolicy) return; setDeletePolicy(singleSelectedPolicy); setPolicyBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Trash2 size={14} />Delete</button>
                        </motion.div>
                      </>
                    )}
                  </AnimatePresence>
                </div>
              </div>
              <ListToolbarControls
                query={query}
                setQuery={setQuery}
                queryField={queryField}
                setQueryField={setQueryField}
                queryFields={policyQueryFields}
                tags={[
                  { value: 'manual', label: 'Manual', count: policies.filter(policy => policy.composition === 'manual').length },
                  { value: 'combined', label: 'Schedule + Retention', count: policies.filter(policy => policy.composition === 'combined').length },
                  { value: 'schedule', label: 'Schedule Only', count: policies.filter(policy => policy.composition === 'schedule').length },
                  { value: 'retention', label: 'Retention Only', count: policies.filter(policy => policy.composition === 'retention').length },
                ]}
                activeTags={activeTags}
                setActiveTags={setActiveTags}
                filters={[
                  { value: 'active', label: 'Active', count: policies.filter(policy => policyMatchesFilter(policy, 'active')).length },
                  { value: 'disabled', label: 'Disabled', count: policies.filter(policy => policyMatchesFilter(policy, 'disabled')).length },
                  { value: 'bound', label: 'Bound to Apps', count: policies.filter(policy => policyMatchesFilter(policy, 'bound')).length },
                  { value: 'unbound', label: 'Not Bound', count: policies.filter(policy => policyMatchesFilter(policy, 'unbound')).length },
                ]}
                activeFilters={activeFilters}
                setActiveFilters={setActiveFilters}
                columns={policyColumns}
                visibleColumns={visibleColumns}
                setVisibleColumns={setVisibleColumns}
                onRefresh={() => {
                  setPolicies(prev => [...prev]);
                  setSelectedPolicyIds([]);
                }}
              />
            </div>
          </div>
          <HyperTable
            variant="page"
            density="comfortable"
            columns={policyTableColumns}
            data={filteredPolicies}
            getRowId={row => row.id}
            onRowClick={row => toggleSelectedPolicy(row.id)}
            getRowClassName={row => selectedPolicyIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
            selectedCount={selectedPolicyIds.length}
            emptyMessage={policies.length === 0 ? 'No DR policies yet' : 'No policies match the current filters'}
            className="hbdr-policy-hyper-table"
          />
        </div>
        <div className="hidden grid-cols-1 gap-4">
          {filteredPolicies.map(policy => (
            <div key={policy.id} className={`relative flex flex-col gap-5 overflow-visible rounded-2xl border border-slate-200 bg-white p-6 shadow-sm transition-all hover:border-slate-300 hover:shadow-md lg:flex-row lg:items-center lg:justify-between ${menuId === policy.id ? 'z-40' : 'z-0'}`}>
              <div className="flex min-w-0 items-center gap-5">
                <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl border border-violet-100 bg-violet-50 text-violet-600 shadow-sm"><ShieldCheck size={22} /></div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="truncate text-lg font-bold tracking-tight text-slate-900">{policy.name}</h3>
                    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold uppercase text-slate-500">{formatPolicyComposition(policy.composition)}</span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm font-medium text-slate-500">
                    <span className="flex items-center gap-1.5"><Clock size={14} className="text-slate-400" />{formatPolicySchedule(policy)}</span>
                    <span className="flex items-center gap-1.5"><History size={14} className="text-slate-400" />Retention: {formatPolicyRetention(policy)}</span>
                    <span className="flex items-center gap-1.5"><Layers size={14} className="text-slate-400" />Bound {policy.bound} applications</span>
                  </div>
                </div>
              </div>
              <div className="flex shrink-0 items-center justify-between gap-5 lg:justify-end">
                <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ${policy.status === 'Active' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>{policy.status === 'Active' ? 'Active' : 'Disabled'}</span>
                <div className="relative" data-policy-menu-root>
                  <button onClick={(event) => { event.stopPropagation(); setMenuId(menuId === policy.id ? null : policy.id); }} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700"><MoreVertical size={18} /></button>
                  <AnimatePresence>
                    {menuId === policy.id && (
                      <>
                        <div className="fixed inset-0 z-30" onClick={() => setMenuId(null)} />
                        <motion.div data-policy-menu-root onClick={(event) => event.stopPropagation()} initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute right-0 top-10 z-50 w-44 overflow-hidden rounded-xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5">
                          <button onClick={() => openEditPolicy(policy)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Edit2 size={14} />Edit Policy</button>
                          <button onClick={() => togglePolicyStatus(policy)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><RefreshCw size={14} />{policy.status === 'Active' ? 'Disable Policy' : 'Enable Policy'}</button>
                          <button onClick={() => { setDeletePolicy(policy); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete Policy</button>
                        </motion.div>
                      </>
                    )}
                  </AnimatePresence>
                </div>
              </div>
            </div>
          ))}
        </div>
        </>
      ) : (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-slate-200 bg-white p-16 text-center shadow-sm">
          <div className="mb-4 rounded-full border border-slate-100 bg-slate-50 p-4 text-slate-400"><Search size={28} /></div>
          <h4 className="text-sm font-bold text-slate-800">No matching DR policies</h4>
          <p className="mt-1 max-w-sm text-xs text-slate-400">No matching backup policies. Try another keyword or create a new policy.</p>
          {query && <button onClick={() => setQuery('')} className="mt-4 rounded-lg border border-blue-100 bg-blue-50 px-4 py-1.5 text-xs font-semibold text-blue-600 transition-colors hover:bg-blue-100">Reset Filters</button>}
        </div>
      )}

      <AnimatePresence>
		{policyError && <div className="rounded border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">{policyError}</div>}
		{detailPolicy && <ModalFrame title="Policy Details" onClose={()=>setDetailPolicy(null)}><div className="grid grid-cols-1 gap-3 md:grid-cols-2"><Info label="Name" value={detailPolicy.name}/><Info label="Status" value={detailPolicy.status}/><Info label="Schedule" value={formatPolicySchedule(detailPolicy)}/><Info label="Retention" value={formatPolicyRetention(detailPolicy)}/><Info label="Composition" value={formatPolicyComposition(detailPolicy.composition)}/><Info label="Bound Apps" value={`${detailPolicy.bound}`}/></div><div className="mt-5 flex justify-end gap-3"><button onClick={()=>setDetailPolicy(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600">Close</button><button onClick={()=>{openEditPolicy(detailPolicy);setDetailPolicy(null)}} className="rounded-xl bg-blue-600 px-6 py-2.5 text-sm font-bold text-white">Edit</button></div></ModalFrame>}
        {policyModalOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={closePolicyModal} className="hbdr-filter-drawer-backdrop" />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-policy-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>{editingPolicyId ? 'Edit DR Policy' : 'New DR Policy'}</strong>
                  <span>Define backup schedule and retention rules.</span>
                </div>
                <button type="button" onClick={closePolicyModal} aria-label="Close policy drawer"><X size={18} /></button>
              </div>

              <div className="hbdr-filter-drawer-body hbdr-policy-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Name</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <span className="text-[11px] font-black uppercase tracking-[0.2em] text-indigo-600">Policy Name</span>
                  <input type="text" value={policyForm.name} onChange={event => setPolicyForm({ ...policyForm, name: event.target.value })} className="w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm font-medium outline-none transition-all focus:border-indigo-500 focus:ring-4 focus:ring-indigo-100" placeholder="Example: Core workload-5minute rapid DR" />
                  </div>
                </section>

                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Composition</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid gap-2 md:grid-cols-3">
                    {[
                      { id: 'combined' as PolicyComposition, title: 'Schedule + Retention', badge: 'Recommended' },
                      { id: 'schedule' as PolicyComposition, title: 'Schedule Only', badge: 'Timing' },
                      { id: 'retention' as PolicyComposition, title: 'Retention Only', badge: 'Lifecycle' },
                    ].map(item => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => setPolicyForm({ ...policyForm, composition: item.id })}
                        aria-pressed={policyForm.composition === item.id}
                        className={`hbdr-policy-composition-card flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-left transition-all ${policyForm.composition === item.id ? 'border-indigo-500 bg-indigo-50 text-indigo-950 shadow-sm' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:bg-slate-50'}`}
                      >
                        <span>
                          <span className={`mb-1 inline-flex rounded-full px-2 py-0.5 text-[10px] font-black ${policyForm.composition === item.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}>{item.badge}</span>
                          <strong className="block text-sm font-black">{item.title}</strong>
                        </span>
                        <span className={`h-4 w-4 rounded-full border ${policyForm.composition === item.id ? 'border-indigo-600 bg-indigo-600' : 'border-slate-300 bg-white'}`} />
                      </button>
                    ))}
                  </div>
                  </div>
                </section>

                {showScheduleConfig && (
                <section className="hbdr-advanced-filter-section">
                  <h4>Schedule Type</h4>
                  <p className="hbdr-policy-timezone-note">Schedule times use {userTimeZoneLabel()}.</p>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid grid-cols-2 gap-2">
                    {[
                      { id: 'interval' as PolicyScheduleType, label: 'Interval', icon: Zap },
                      { id: 'daily' as PolicyScheduleType, label: 'Daily Backup', icon: Sun },
                      { id: 'weekly' as PolicyScheduleType, label: 'Weekly Backup', icon: Calendar },
                      { id: 'monthly' as PolicyScheduleType, label: 'Monthly Backup', icon: Layers },
                    ].map(type => {
                      const TypeIcon = type.icon;
                      return (
                        <button key={type.id} onClick={() => setPolicyForm({ ...policyForm, type: type.id })} className={`flex items-center gap-2 rounded-xl border-2 px-3 py-2 transition-all ${policyForm.type === type.id ? 'border-indigo-600 bg-indigo-50/50 shadow-sm' : 'border-slate-100 hover:border-slate-200'}`}>
                          <span className={`rounded-lg p-1.5 ${policyForm.type === type.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}><TypeIcon size={16} /></span>
                          <span className={`text-sm font-bold ${policyForm.type === type.id ? 'text-indigo-900' : 'text-slate-600'}`}>{type.label}</span>
                        </button>
                      );
                    })}
                  </div>

                  <AnimatePresence mode="wait">
                    <motion.div key={policyForm.type} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -10 }} className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                      {policyForm.type === 'interval' && (
                        <div className="flex flex-wrap items-center gap-3">
                          <span className="text-sm font-bold text-slate-700">Run every: </span>
                          <input type="number" min={1} value={policyForm.intervalValue} onChange={event => setPolicyForm({ ...policyForm, intervalValue: Number(event.target.value) })} className="w-20 rounded-lg border border-slate-200 bg-white px-3 py-2 text-center text-sm font-bold outline-none focus:border-indigo-500" />
                          <select value={policyForm.intervalUnit} onChange={event => setPolicyForm({ ...policyForm, intervalUnit: event.target.value as 'minutes' | 'hours' })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            <option value="minutes">minutes</option>
                            <option value="hours">hours</option>
                          </select>
                        </div>
                      )}
                      {policyForm.type === 'daily' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Run every:</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'weekly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Daily execution time:</span>
                          <select value={policyForm.weekDay} onChange={event => setPolicyForm({ ...policyForm, weekDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {weekdays.map((day, index) => <option key={day} value={index}>{day}</option>)}
                          </select>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'monthly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Every month</span>
                          <select value={policyForm.monthDay} onChange={event => setPolicyForm({ ...policyForm, monthDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {Array.from({ length: 31 }).map((_, index) => <option key={index + 1} value={index + 1}>{index + 1}</option>)}
                          </select>
                          <span className="text-sm font-bold text-slate-700">Day</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                    </motion.div>
                  </AnimatePresence>
                  </div>
                </section>
                )}

                {showRetentionConfig && (
                <section className="hbdr-advanced-filter-section">
                  <h4>Retention Policy</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                    <div className="flex max-w-[260px] items-center gap-3">
                      <input type="number" min={1} value={policyForm.retention} onChange={event => setPolicyForm({ ...policyForm, retention: Number(event.target.value) })} className="w-full rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-bold outline-none focus:border-indigo-500" />
                      <span className="text-xs font-bold uppercase text-slate-400">valid copies</span>
                    </div>
                  </div>
                  </div>
                </section>
                )}
              </div>

              <div className="hbdr-filter-drawer-actions hbdr-policy-drawer-actions">
                <button type="button" onClick={()=>void savePolicy()} disabled={!policyForm.name.trim() || savingPolicy}><CheckCircle2 size={15} />{savingPolicy ? 'Saving...' : editingPolicyId ? 'Update Policy' : 'Save Policy'}</button>
                <button type="button" onClick={closePolicyModal}>Cancel</button>
              </div>
            </motion.div>
          </>
        )}

        {deletePolicy && (
          <ModalFrame title="Delete Policy" onClose={() => setDeletePolicy(null)}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Confirm policy deletion <strong>{deletePolicy.name}</strong>? Bound applications will not be automatically migrated.
              </div>
              {policyError && <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">{policyError}</div>}
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Policy Name" value={deletePolicy.name} />
                <Info label="Schedule" value={formatPolicySchedule(deletePolicy)} />
                <Info label="Retained Copies" value={formatPolicyRetention(deletePolicy)} />
                <Info label="Bound Apps" value={`${deletePolicy.bound}`} />
              </div>
              <div className="flex justify-end gap-3">
                <button onClick={() => setDeletePolicy(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button onClick={()=>void confirmDeletePolicy()} disabled={savingPolicy} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700 active:scale-95 disabled:opacity-50">{savingPolicy ? 'Deleting...' : 'Delete'}</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
