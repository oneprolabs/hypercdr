import { useState } from 'react';
import { Download, FileArchive } from 'lucide-react';
import { apiPost } from '../../api/client';
import { apiHeaders } from '../../api/client';

type Bundle = { name: string; downloadUrl: string; size: number; expiresAt: string };
export default function SupportBundlePage({ toast }: { toast: (message: string) => void }) {
  const [description, setDescription] = useState('');
  const [reproducible, setReproducible] = useState('');
  const [busy, setBusy] = useState(false);
  const [bundle, setBundle] = useState<Bundle | null>(() => { try { const raw = sessionStorage.getItem('hcdr.supportBundle'); return raw ? JSON.parse(raw) as Bundle : null; } catch { return null; } });
  const [screenshot, setScreenshot] = useState('');
  const create = async () => {
    if (!description.trim()) { toast('Please describe the problem before collecting diagnostics.'); return; }
    setBusy(true);
    try { const result = await apiPost<Bundle>('/api/v1/support-bundles', { description, reproducible, screenshotBase64: screenshot, sinceHours: 24 }); setBundle(result); sessionStorage.setItem('hcdr.supportBundle', JSON.stringify(result)); toast('Support bundle generated.'); }
    catch (e) { toast(`Failed to generate support bundle: ${e instanceof Error ? e.message : 'unknown error'}`); }
    finally { setBusy(false); }
  };
  const download = async () => {
    if (!bundle) return;
    const response = await fetch(bundle.downloadUrl, { headers: apiHeaders() });
    if (!response.ok) { toast('Download failed. Please generate the bundle again.'); return; }
    const blob = await response.blob(); const url = URL.createObjectURL(blob);
    const a = document.createElement('a'); a.href = url; a.download = bundle.name; a.click(); URL.revokeObjectURL(url);
  };
  return <div className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600"><FileArchive size={18}/></div><div><h3 className="text-sm font-black text-slate-900">Support Bundle</h3><p className="mt-1 text-[11px] text-slate-400">Collect comprehensive platform, cluster, storage, and OADP diagnostics for offline troubleshooting.</p></div></div></div>
    <section className="hbdr-dr-table-card p-5 space-y-4"><div><label className="text-xs font-bold text-slate-700">Problem description <span className="text-rose-600">*</span></label><textarea value={description} onChange={e=>setDescription(e.target.value)} rows={5} className="mt-2 w-full rounded-lg border border-slate-200 p-3 text-sm" placeholder="Describe what happened, when it happened, the cluster and task ID involved..."/></div><div><label className="text-xs font-bold text-slate-700">Can the problem be reproduced?</label><input value={reproducible} onChange={e=>setReproducible(e.target.value)} className="mt-2 w-full rounded-lg border border-slate-200 p-3 text-sm" placeholder="For example: yes, when starting a Drill"/></div><div><label className="text-xs font-bold text-slate-700">Exception screenshot (optional)</label><div className="mt-2 flex flex-wrap items-start gap-3"><label className="inline-flex cursor-pointer items-center rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"><span>Choose image</span><input type="file" accept="image/png,image/jpeg" className="sr-only" onChange={e=>{const file=e.target.files?.[0]; if(!file)return; if(file.size>10*1024*1024){toast('Screenshot must be smaller than 10 MB');return;} const reader=new FileReader(); reader.onload=()=>setScreenshot(String(reader.result||'')); reader.readAsDataURL(file);}} /></label>{screenshot&&<div className="flex items-center gap-3 rounded-lg border border-slate-200 bg-slate-50 p-2"><img src={screenshot} alt="Selected exception screenshot preview" className="h-16 w-24 rounded object-cover"/><div className="text-xs text-slate-600">Screenshot selected</div><button type="button" className="text-xs font-semibold text-rose-600" onClick={()=>setScreenshot('')}>Remove</button></div>}</div></div><p className="text-xs leading-5 text-slate-500">The bundle includes comprehensive runtime state and recent logs. Credentials, tokens, kubeconfig contents, Secret data, and business data are excluded.</p><button type="button" onClick={()=>void create()} disabled={busy} className="hbdr-dr-action-primary">{busy?'Collecting…':'Collect diagnostics'}</button>{bundle&&<div className="rounded-lg border border-emerald-100 bg-emerald-50 p-4 text-sm"><p className="font-semibold text-emerald-800">Bundle ready: {bundle.name}</p><button type="button" className="mt-2 inline-flex items-center gap-2 text-blue-700 underline" onClick={()=>void download()}><Download size={14}/>Download bundle</button></div>}</section>
  </div>;
}
