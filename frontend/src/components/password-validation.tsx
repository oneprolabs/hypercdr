import { AlertCircle, CheckCircle2 } from 'lucide-react';

export function PasswordValidation({ password, confirmation }: { password:string; confirmation:string }) {
  const longEnough=password.length>=8;
  const matches=confirmation.length>0&&password===confirmation;
  const variety=[/[a-z]/,/[A-Z]/,/\d/,/[^A-Za-z0-9]/].filter(pattern=>pattern.test(password)).length;
  const strength=password.length===0?0:password.length>=12&&variety>=3?3:longEnough&&variety>=2?2:1;
  const strengthLabel=['Not entered','Weak','Good','Strong'][strength];
  return <div className="rounded-lg border border-slate-100 bg-slate-50 px-3 py-3"><div className="mb-2 flex items-center justify-between text-[11px] font-semibold"><span className="text-slate-500">Password strength</span><span className={strength>=3?'text-emerald-600':strength===2?'text-blue-600':strength===1?'text-amber-600':'text-slate-400'}>{strengthLabel}</span></div><div className="mb-3 grid grid-cols-3 gap-1">{[1,2,3].map(level=><span key={level} className={`h-1 rounded-full ${strength>=level?(strength>=3?'bg-emerald-500':strength===2?'bg-blue-500':'bg-amber-400'):'bg-slate-200'}`}/>)}</div><div className="grid gap-2 text-[11px] font-semibold sm:grid-cols-2"><span className={longEnough?'flex items-center gap-1.5 text-emerald-600':'flex items-center gap-1.5 text-slate-400'}><CheckCircle2 size={13}/>8–128 characters</span><span className={matches?'flex items-center gap-1.5 text-emerald-600':confirmation?'flex items-center gap-1.5 text-rose-600':'flex items-center gap-1.5 text-slate-400'}>{matches?<CheckCircle2 size={13}/>:<AlertCircle size={13}/>}Passwords match</span></div><p className="mt-2 text-[10px] leading-4 text-slate-400">For a stronger password, combine uppercase and lowercase letters, numbers, and symbols.</p></div>;
}
