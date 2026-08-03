import { ShieldCheck } from 'lucide-react';

type ClusterProtection = { apps:Array<{isProtected:boolean}> };

export function getProtectedAppCount(cluster:ClusterProtection) {
  return cluster.apps.filter(app=>app.isProtected).length;
}

export function isClusterProtected(cluster:ClusterProtection) {
  return getProtectedAppCount(cluster)>0;
}

export function ProtectionBadge({cluster}:{cluster:ClusterProtection}) {
  const protectedCount=getProtectedAppCount(cluster);
  const cls=protectedCount>0?'border-emerald-100 bg-emerald-50 text-emerald-700':'border-slate-200 bg-slate-50 text-slate-500';
  return <span title={protectedCount>0?`Protected ${protectedCount}/${cluster.apps.length} applications`:'No protected applications'} className={`cluster-protection-badge inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-semibold ${cls}`}><ShieldCheck size={12}/>{protectedCount>0?'Protected':'Unprotected'}</span>;
}

export function Metric({label,value,success,onClick}:{label:string;value:string|number;success?:boolean;onClick?:()=>void}) {
  const content=<><p className={`${success?'text-emerald-700':'text-slate-400'} text-[10px] font-medium tracking-wide`}>{label}</p><p className={`mt-0.5 text-[15px] font-semibold ${success?'text-emerald-600':'text-slate-900'}`}>{value}</p></>;
  const className=`cluster-metric-tile ${success?'cluster-metric-tile-success':''} ${onClick?'cluster-metric-tile-clickable':''} rounded-lg bg-slate-50/70 px-2.5 py-2`;
  return onClick?<button type="button" className={className} onClick={event=>{event.stopPropagation();onClick()}}>{content}</button>:<div className={className}>{content}</div>;
}
