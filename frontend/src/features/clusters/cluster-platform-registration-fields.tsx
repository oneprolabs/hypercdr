import React from 'react';
import { CheckCircle2, RefreshCw, Upload } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';
function RegistrationStep({number,title,description,children}:{number:number;title:string;description?:string;children?:React.ReactNode}) {
  return <section className="flex gap-3.5">
    <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white shadow-sm shadow-blue-200">{number}</div>
    <div className="min-w-0 flex-1 pb-1">
      <h3 className="text-sm font-bold leading-7 text-slate-900">{title}</h3>
      {description && <p className="mt-0.5 text-xs leading-5 text-slate-500">{description}</p>}
      {children && <div className="mt-3">{children}</div>}
    </div>
  </section>;
}
type UploadSession={id:string;contexts:Array<{name:string;apiServer:string;isCurrent?:boolean}>;fingerprint?:string;expiresAt?:string};
type Inspection={clusterName:string;serverVersion?:string;nodeCount?:number;defaultStorageClass?:string;storageClasses:string[];gates:Array<{id:string;label:string;status:string;detail:string}>};
type RegistrationTask={status:string;progress?:number;errorMessage?:string};
export function ClusterPlatformRegistrationFields({registrationType,cceUpload,cceContext,cceUploadLoading,cceUploadError,cceInspection,cceInspectionLoading,cceStorageClass,cceRegistrationTask,onOpenGuide,onUpload,onContextChange,onInspect,onStorageClassChange,onRegister,onCancel,compact=true}:{registrationType:RegistrationType;cceUpload:UploadSession|null;cceContext:string;cceUploadLoading:boolean;cceUploadError:string;cceInspection:Inspection|null;cceInspectionLoading:boolean;cceStorageClass:string;cceRegistrationTask:RegistrationTask|null;onOpenGuide:()=>void;onUpload:(file:File|undefined)=>void;onContextChange:(context:string)=>void;onInspect:()=>void;onStorageClassChange:(value:string)=>void;onRegister:()=>void;onCancel:()=>void;compact?:boolean}){
 return                   <div className="space-y-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
                    <RegistrationStep number={1} title={registrationType === 'huaweicloud-cce' ? 'Upload CCE kubeconfig' : registrationType === 'openshift' ? 'Upload OpenShift kubeconfig' : 'Upload kubeconfig'} description={compact ? undefined : 'The credential is encrypted in transit and used only for this registration attempt.'}>
                      {!compact && <div className="mb-3 flex items-start gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-[11px] leading-4 text-emerald-800">
                        <CheckCircle2 size={14} className="mt-0.5 shrink-0" />
                        <span><strong className="block">Temporary file · automatically deleted</strong>The uploaded kubeconfig is permanently deleted when registration succeeds or fails, when you cancel, or when the temporary session expires. No manual cleanup is required.</span>
                      </div>}
                      <label className={`flex min-h-24 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed px-4 text-center transition ${cceUploadLoading ? 'cursor-wait border-blue-200 bg-blue-50' : 'border-slate-300 bg-slate-50 hover:border-blue-300 hover:bg-blue-50/40'}`}>
                        <Upload size={20} className={cceUploadLoading ? 'animate-pulse text-blue-600' : 'text-slate-400'} />
                        <span className="mt-2 text-xs font-bold text-slate-700">{cceUploadLoading ? 'Validating kubeconfig…' : cceUpload ? 'Replace kubeconfig' : 'Choose YAML or JSON kubeconfig'}</span>
                        {!compact && <span className="mt-1 text-[11px] text-slate-500">Maximum 1 MiB. External credential plugins are not executed.</span>}
                        <input type="file" className="sr-only" accept=".yaml,.yml,.json,application/yaml,application/json" disabled={cceUploadLoading} onChange={event => void onUpload(event.target.files?.[0])} />
                      </label>
                      <button type="button" onClick={onOpenGuide} className="mt-2 inline-flex items-center gap-1 text-xs font-bold text-blue-700 underline decoration-blue-200 underline-offset-4 hover:text-blue-800">How do I get {registrationType === 'openshift' ? 'an OpenShift' : registrationType === 'huaweicloud-cce' ? 'a Huawei Cloud CCE' : 'a Native Kubernetes'} kubeconfig?</button>
                      {cceUploadError && <p role="alert" className="mt-3 rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-xs font-medium leading-5 text-rose-700">{cceUploadError}</p>}
                    </RegistrationStep>
                    {(!compact || cceUpload) && <RegistrationStep number={2} title="Select cluster context" description={compact ? undefined : 'Confirm the exact cluster that HyperCDR may inspect. No cluster resources are changed at this stage.'}>
                      {cceUpload ? <>
                        <select value={cceContext} onChange={event => onContextChange(event.target.value)} className="h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm font-semibold text-slate-800 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100">
                          <option value="" disabled>Select a context</option>
                          {cceUpload.contexts.map(context => <option key={context.name} value={context.name}>{context.name} · {context.apiServer}</option>)}
                        </select>
                        {!compact && <div className="mt-3 grid grid-cols-2 gap-3 rounded-lg bg-slate-50 p-3 text-[11px] text-slate-500">
                          <div><span className="block font-bold uppercase tracking-wide text-slate-400">Credential fingerprint</span><span className="mt-1 block truncate font-mono text-slate-700" title={cceUpload.fingerprint}>{cceUpload.fingerprint}</span></div>
                          <div><span className="block font-bold uppercase tracking-wide text-slate-400">Session expires</span><span className="mt-1 block text-slate-700">{cceUpload.expiresAt ? new Date(cceUpload.expiresAt).toLocaleTimeString() : 'Temporary session'}</span></div>
                        </div>}
                      </> : <div className="flex min-h-10 items-center rounded-lg border border-dashed border-slate-200 bg-slate-50 px-3 text-xs font-medium text-slate-400">Upload a kubeconfig to discover its available contexts.</div>}
                    </RegistrationStep>}
                    {(!compact || cceUpload) && <RegistrationStep number={3} title="Inspect and register" description={compact ? undefined : 'Inspect identity, version, permissions, capacity, and StorageClass. After confirmation, an isolated preflight verifies network and image pulls before installation.'}>
                      <button type="button" onClick={() => void onInspect()} disabled={!cceContext || cceUploadLoading || cceInspectionLoading} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-xs font-bold text-white shadow-sm transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-slate-300">{cceInspectionLoading && <RefreshCw size={13} className="animate-spin" />}{cceInspectionLoading ? 'Inspecting cluster…' : 'Inspect cluster'}</button>
                      {cceInspection && <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 rounded-xl border border-emerald-100 bg-emerald-50/60 p-3 text-xs">
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Cluster</span><strong className="mt-0.5 block text-slate-800">{cceInspection.clusterName}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Kubernetes</span><strong className="mt-0.5 block text-slate-800">{cceInspection.serverVersion || '—'}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">Worker nodes</span><strong className="mt-0.5 block text-slate-800">{cceInspection.nodeCount ?? '—'}</strong></div>
                        <div><span className="block text-[10px] font-bold uppercase tracking-wide text-emerald-600">StorageClass</span><strong className="mt-0.5 block text-slate-800">{cceInspection.defaultStorageClass || 'Selection required'}</strong></div>
                      </div>}
                      {cceInspection?.gates?.length > 0 && (!compact || cceInspection.gates.some(gate => gate.status !== 'passed')) && <div className="mt-3 divide-y divide-slate-100 rounded-xl border border-slate-200 bg-white px-3">
                        {cceInspection.gates.filter(gate => !compact || gate.status !== 'passed').map(gate => <div key={gate.id} className="flex items-start gap-2.5 py-2.5">
                          <span className={`mt-1 h-2 w-2 shrink-0 rounded-full ${gate.status === 'passed' ? 'bg-emerald-500' : gate.status === 'warning' ? 'bg-amber-500' : 'bg-blue-400'}`} />
                          <div className="min-w-0"><strong className="block text-xs text-slate-800">{gate.label}</strong><span className="mt-0.5 block text-[11px] leading-4 text-slate-500">{gate.detail}</span></div>
                        </div>)}
                      </div>}
                      {cceInspection && !cceRegistrationTask && <div className="mt-3 flex items-end gap-3">
                        <label className="min-w-0 flex-1 text-[10px] font-bold uppercase tracking-wide text-slate-500">StorageClass
                          <select value={cceStorageClass} onChange={event => onStorageClassChange(event.target.value)} className="mt-1 h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-xs font-semibold normal-case tracking-normal text-slate-800 outline-none focus:border-blue-500">
                            <option value="" disabled>Select a StorageClass</option>
                            {cceInspection.storageClasses.map(name => <option key={name} value={name}>{name}{name === cceInspection.defaultStorageClass ? ' (default)' : ''}</option>)}
                          </select>
                        </label>
                        <button type="button" onClick={() => void onRegister()} disabled={!cceStorageClass || cceInspectionLoading} className="h-9 rounded-lg bg-emerald-600 px-4 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 disabled:cursor-not-allowed disabled:bg-slate-300">Register cluster</button>
                      </div>}
                      {cceRegistrationTask && <div className={`mt-3 rounded-xl border px-3 py-3 text-xs ${cceRegistrationTask.status === 'failed' ? 'border-rose-100 bg-rose-50 text-rose-700' : cceRegistrationTask.status === 'succeeded' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : cceRegistrationTask.status === 'canceled' ? 'border-slate-200 bg-slate-50 text-slate-600' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>
                        <div className="flex items-center justify-between gap-3"><strong>{cceRegistrationTask.status === 'succeeded' ? 'Registration completed' : cceRegistrationTask.status === 'failed' ? 'Registration failed' : cceRegistrationTask.status === 'canceled' ? 'Registration canceled' : 'Registration in progress'}</strong><span className="tabular-nums">{cceRegistrationTask.status === 'succeeded' ? '100%' : cceRegistrationTask.status === 'failed' ? 'Stopped' : cceRegistrationTask.status === 'canceled' ? 'Canceled' : `${cceRegistrationTask.progress || 0}%`}</span></div>
                        <p className="mt-1 leading-5">{cceRegistrationTask.status === 'failed' ? cceRegistrationTask.errorMessage || 'The executor reported a registration failure.' : cceRegistrationTask.status === 'succeeded' ? 'Agent registration was confirmed and the temporary kubeconfig was destroyed.' : cceRegistrationTask.status === 'canceled' ? 'Installation stopped, rollback completed, and the temporary kubeconfig was destroyed.' : 'Preflight, installation, and agent readiness are being verified. You may keep this drawer open.'}</p>
                        {['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(cceRegistrationTask.status) && <button type="button" onClick={() => void onCancel()} disabled={cceInspectionLoading || cceRegistrationTask.status === 'canceling'} className="mt-2 rounded-lg border border-current px-3 py-1.5 text-[11px] font-bold transition hover:bg-white/60 disabled:cursor-wait disabled:opacity-60">{cceRegistrationTask.status === 'canceling' ? 'Canceling and rolling back…' : 'Cancel registration'}</button>}
                      </div>}
                    </RegistrationStep>}
                  </div>;
}
