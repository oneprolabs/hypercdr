import { useState } from 'react';
import { Download, FileArchive } from 'lucide-react';
import { apiPost } from '../../api/client';
import { apiHeaders } from '../../api/client';
import { formatLocalDateTime, getUserTimeZone } from '../../lib/date-time';

type Bundle = { name: string; downloadUrl: string; size: number; expiresAt: string; generatedAt?: string; timeZone?: string; description?: string; downloaded?: boolean; fresh?: boolean };
const readBundles=()=>{try{const many=localStorage.getItem('hcdr.supportBundles');if(many)return JSON.parse(many) as Bundle[];const old=sessionStorage.getItem('hcdr.supportBundle');return old?[JSON.parse(old) as Bundle]:[]}catch{return[]}};
const saveBundles=(items:Bundle[])=>{try{localStorage.setItem('hcdr.supportBundles',JSON.stringify(items.slice(0,20)))}catch{/* storage unavailable */}};
const formatSize=(size:number)=>size>=1024*1024?`${(size/1024/1024).toFixed(1)} MB`:`${Math.max(1,Math.round(size/1024))} KB`;
export default function SupportBundlePage({ toast }: { toast: (message: string) => void }) {
  const [description, setDescription] = useState('');
  const [reproducible, setReproducible] = useState('');
  const [busy, setBusy] = useState(false);
  const [bundles, setBundles] = useState<Bundle[]>(readBundles);
  const [screenshot, setScreenshot] = useState('');
  const create = async () => {
    if (!description.trim()) { toast('Please describe the problem before collecting diagnostics.'); return; }
    setBusy(true);
    try { const result = await apiPost<Bundle>('/api/v1/support-bundles', { description, reproducible, screenshotBase64: screenshot, sinceHours: 24, timeZone: getUserTimeZone() }); const next=[{...result,description:description.trim(),fresh:true},...bundles.map(item=>({...item,fresh:false}))].slice(0,20);setBundles(next);saveBundles(next);toast(`New support bundle generated: ${result.name}`); }
    catch (e) { toast(`Failed to generate support bundle: ${e instanceof Error ? e.message : 'unknown error'}`); }
    finally { setBusy(false); }
  };
  const download = async (bundle:Bundle) => {
    const response = await fetch(bundle.downloadUrl, { headers: apiHeaders() });
    if (!response.ok) { toast('Download failed. Please generate the bundle again.'); return; }
    const blob = await response.blob(); const url = URL.createObjectURL(blob);
    const a = document.createElement('a'); a.href = url; a.download = bundle.name; a.click(); URL.revokeObjectURL(url);const next=bundles.map(item=>item.name===bundle.name?{...item,downloaded:true,fresh:false}:item);setBundles(next);saveBundles(next);
  };
  return <div className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600"><FileArchive size={18}/></div><div><h3 className="text-sm font-black text-slate-900">Support Bundle</h3><p className="mt-1 text-[11px] text-slate-400">Collect comprehensive platform, cluster, storage, and OADP diagnostics for offline troubleshooting.</p></div></div></div>
    <section className="hbdr-dr-table-card p-5 space-y-4"><div><label className="text-xs font-bold text-slate-700">Problem description <span className="text-rose-600">*</span></label><textarea value={description} onChange={e=>setDescription(e.target.value)} rows={5} className="mt-2 w-full rounded-lg border border-slate-200 p-3 text-sm" placeholder="Describe what happened, when it happened, the cluster and task ID involved..."/></div><div><label className="text-xs font-bold text-slate-700">Can the problem be reproduced?</label><input value={reproducible} onChange={e=>setReproducible(e.target.value)} className="mt-2 w-full rounded-lg border border-slate-200 p-3 text-sm" placeholder="For example: yes, when starting a Drill"/></div><div><label className="text-xs font-bold text-slate-700">Exception screenshot (optional)</label><div className="mt-2 flex flex-wrap items-start gap-3"><label className="inline-flex cursor-pointer items-center rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"><span>Choose image</span><input type="file" accept="image/png,image/jpeg" className="sr-only" onChange={e=>{const file=e.target.files?.[0]; if(!file)return; if(file.size>10*1024*1024){toast('Screenshot must be smaller than 10 MB');return;} const reader=new FileReader(); reader.onload=()=>setScreenshot(String(reader.result||'')); reader.readAsDataURL(file);}} /></label>{screenshot&&<div className="flex items-center gap-3 rounded-lg border border-slate-200 bg-slate-50 p-2"><img src={screenshot} alt="Selected exception screenshot preview" className="h-16 w-24 rounded object-cover"/><div className="text-xs text-slate-600">Screenshot selected</div><button type="button" className="text-xs font-semibold text-rose-600" onClick={()=>setScreenshot('')}>Remove</button></div>}</div></div><p className="text-xs leading-5 text-slate-500">The bundle includes comprehensive runtime state and recent logs. Credentials, tokens, kubeconfig contents, Secret data, and business data are excluded.</p><button type="button" onClick={()=>void create()} disabled={busy} className="hbdr-dr-action-primary">{busy?'Collecting…':'Collect diagnostics'}</button></section>
    <section className="hbdr-dr-table-card"><div className="border-b border-slate-100 px-5 py-4"><h4 className="text-sm font-black text-slate-900">Generated bundles</h4><p className="mt-1 text-xs text-slate-500">Newest bundle first. File names use the current page time zone.</p></div><div className="divide-y divide-slate-100">{bundles.length===0?<p className="p-8 text-center text-xs text-slate-400">No support bundle has been generated in this browser.</p>:bundles.map(item=><div key={item.name} className={`flex items-center justify-between gap-4 px-5 py-4 ${item.fresh?'bg-emerald-50/60':''}`}><div className="min-w-0"><div className="flex items-center gap-2"><strong className="truncate text-xs text-slate-800">{item.name}</strong>{item.fresh&&<span className="rounded-full bg-emerald-100 px-2 py-0.5 text-[9px] font-bold text-emerald-700">Just generated</span>}</div><p className="mt-1 truncate text-[11px] text-slate-500">{item.generatedAt?formatLocalDateTime(item.generatedAt):'Historical record'} · {formatSize(item.size)}{item.description?` · ${item.description}`:''}</p></div><button type="button" disabled={item.downloaded} className="hbdr-dr-action-secondary shrink-0" onClick={()=>void download(item)}><Download size={14}/>{item.downloaded?'Downloaded':'Download'}</button></div>)}</div></section>
  </div>;
}
