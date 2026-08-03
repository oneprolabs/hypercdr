import { useCallback, useEffect, useState } from 'react';
import { Settings2 } from 'lucide-react';
import { motion } from 'motion/react';
import { apiGet, apiPost, apiPut } from '../../api/client';
import type { ApiLoginResponse } from '../../auth/types';
import { EditField } from '../../components/edit-field';

type ApiEmailSettings = { enabled:boolean; host:string; port:number; security:'none'|'starttls'|'tls'; username:string; passwordConfigured:boolean; senderName:string; senderEmail:string; updatedAt?:string };
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export default function EmailSettingsPage({ currentUser, toast }: { currentUser: ApiLoginResponse['user']; toast:(message:string)=>void }) {
  const [form,setForm]=useState<ApiEmailSettings>({enabled:true,host:'',port:587,security:'starttls',username:'',passwordConfigured:false,senderName:'HyperCDR',senderEmail:''});
  const [password,setPassword]=useState('');
  const [recipient,setRecipient]=useState(currentUser.email==='admin'?'':currentUser.email);
  const [busy,setBusy]=useState('');
  const load=useCallback(async()=>setForm(await apiGet<ApiEmailSettings>('/api/v1/email-settings')),[]);
  useEffect(()=>{void load().catch(error=>toast(error instanceof Error?error.message:'Failed to load email settings'))},[load,toast]);
  const update=<K extends keyof ApiEmailSettings>(key:K,value:ApiEmailSettings[K])=>setForm(current=>({...current,[key]:value}));
  const save=async()=>{setBusy('save');try{const saved=await apiPut<ApiEmailSettings>('/api/v1/email-settings',{...form,enabled:true,password});setForm(saved);setPassword('');toast('Email settings saved. Password recovery is ready to use.')}finally{setBusy('')}};
  const test=async()=>{setBusy('test');try{await apiPost('/api/v1/email-settings/test',{recipient});toast('Test email sent')}finally{setBusy('')}};
  return <motion.div key="email-settings" initial={{opacity:0}} animate={{opacity:1}} className="space-y-5">
    <div className="hbdr-page-hero"><div className="flex items-center gap-3"><div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Settings2 size={18}/></div><div><h3 className="text-sm font-black tracking-tight text-slate-900">Email Settings</h3><p className="mt-0.5 text-[11px] font-medium text-slate-400">Configure email delivery for password recovery.</p></div></div></div>
    <section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>SMTP Configuration</h3><p>Saving a valid configuration makes password recovery available automatically. Passwords are encrypted and never displayed.</p></div></div><div className="grid gap-4 p-5 md:grid-cols-2"><EditField label="SMTP Server" value={form.host} onChange={value=>update('host',value)} placeholder="smtp.example.com"/><EditField label="Port" value={String(form.port)} onChange={value=>update('port',Number(value)||0)}/><label className="block text-xs font-semibold tracking-normal text-slate-600">Encryption<select value={form.security} onChange={event=>update('security',event.target.value as ApiEmailSettings['security'])} className="mt-1 h-10 w-full rounded-lg border border-slate-200 px-3"><option value="starttls">STARTTLS</option><option value="tls">TLS</option><option value="none">None</option></select></label><EditField label="Username" value={form.username} onChange={value=>update('username',value)}/><EditField label="Password" type="password" value={password} onChange={setPassword} placeholder={form.passwordConfigured?'Configured — leave blank to keep':'Enter SMTP password'}/><EditField label="Sender Name" value={form.senderName} onChange={value=>update('senderName',value)}/><EditField label="Sender Email" value={form.senderEmail} onChange={value=>update('senderEmail',value)} placeholder="noreply@example.com"/></div><div className="flex justify-end border-t border-slate-100 px-5 py-4"><button disabled={busy!==''||form.port<1||form.port>65535} onClick={()=>void save().catch(error=>toast(error instanceof Error?error.message:'Failed to save email settings'))} className="hbdr-dr-action-primary">{busy==='save'?'Saving...':'Save'}</button></div></section>
    <section className="hbdr-section-card overflow-hidden"><div className="hbdr-section-toolbar"><div><h3>Test Email</h3><p>Verify connectivity and credentials before users request password recovery.</p></div></div><div className="flex flex-col gap-3 p-5 md:flex-row md:items-end"><div className="flex-1"><EditField label="Recipient Email" value={recipient} onChange={setRecipient} placeholder="admin@example.com"/></div><button disabled={busy!==''||!emailPattern.test(recipient)} onClick={()=>void test().catch(error=>toast(error instanceof Error?error.message:'Failed to send test email'))} className="hbdr-dr-action-primary">{busy==='test'?'Sending...':'Send Test Email'}</button></div></section>
  </motion.div>;
}
