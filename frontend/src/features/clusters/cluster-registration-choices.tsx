import { CheckCircle2, Cloud, Server } from 'lucide-react';
export type RegistrationType = 'native-kubernetes' | 'huaweicloud-cce' | 'openshift';
export type RegistrationMode = 'platform-direct' | 'command';
export function ClusterRegistrationChoices({registrationType,registrationMode,installLoading,onTypeChange,onModeChange,compact=true}:{registrationType:RegistrationType;registrationMode:RegistrationMode;installLoading:boolean;onTypeChange:(type:RegistrationType)=>void;onModeChange:(mode:RegistrationMode)=>void;compact?:boolean}) {
 return <div className={compact ? 'hbdr-cluster-type-panel' : undefined}>
                  <section className={compact ? 'hbdr-cluster-type-section' : undefined}>
                    <div className="mb-2"><strong className="text-xs uppercase tracking-wide text-slate-500">{compact ? 'Cluster type' : '1. Cluster type'}</strong>{!compact && <p className="mt-1 text-[11px] text-slate-500">Select the Kubernetes environment you want to register.</p>}</div>
                    <div className={`grid grid-cols-1 sm:grid-cols-3 ${compact ? 'hbdr-cluster-type-tabs' : 'gap-2'}`} role="radiogroup" aria-label="Cluster type">
                    {([
                      ['native-kubernetes', 'Native Kubernetes', 'Self-managed Kubernetes cluster', Server],
                      ['huaweicloud-cce', 'Huawei Cloud CCE', 'Huawei-managed Kubernetes service', Cloud],
                      ['openshift', 'OpenShift', 'Red Hat OpenShift 4.14 or 4.15 with OADP', Cloud],
                    ] as const).map(([value, label, description, Icon]) => <button key={value} type="button" role="radio" aria-label={compact ? label : undefined} aria-checked={registrationType === value} disabled={installLoading} onClick={() => { if (registrationType !== value) onTypeChange(value); }} className={compact ? `hbdr-cluster-type-tab ${registrationType === value ? 'is-active' : ''}` : `relative flex min-h-20 items-start gap-3 rounded-xl border p-3 text-left transition ${registrationType === value ? 'border-blue-300 bg-blue-50/70 ring-1 ring-blue-100' : 'border-slate-200 bg-white hover:border-blue-200 hover:bg-slate-50'}`}>
                      <span className={`mt-0.5 rounded-lg p-2 ${registrationType === value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-500'}`}><Icon size={16} /></span>
                      <span className="min-w-0"><strong className="block text-sm text-slate-800">{label}</strong>{!compact && <span className="mt-1 block text-[11px] leading-4 text-slate-500">{description}</span>}</span>
                      {registrationType === value && <CheckCircle2 size={15} className="absolute right-3 top-3 text-blue-600" />}
                    </button>)}
                    </div>
                  </section>
                  <section className={compact ? 'hbdr-cluster-method-section' : undefined}>
                  <div className="mb-2"><strong className="text-xs uppercase tracking-wide text-slate-500">{compact ? 'Install method' : '2. Registration method'}</strong>{!compact && <p className="mt-1 text-[11px] text-slate-500">Choose who will run the Agent installation.</p>}</div>
                  <div className={compact ? 'hbdr-cluster-method-tabs grid grid-cols-2' : 'grid grid-cols-2 gap-2 rounded-xl border border-slate-200 bg-slate-50 p-1.5'} role="radiogroup" aria-label="Registration method">
                    {([
                      ['platform-direct', 'Platform direct install', 'Upload a temporary kubeconfig'],
                      ['command', 'Run installation command', registrationType === 'native-kubernetes' ? 'Run on the control-plane node' : 'Use a Linux administration host'],
                    ] as const).map(([value, label, description]) => <button key={value} type="button" role="radio" aria-checked={registrationMode === value} onClick={() => onModeChange(value)} className={compact ? `hbdr-cluster-method-tab ${registrationMode === value ? 'is-active' : ''}` : `rounded-lg border px-3 py-2.5 text-left transition ${registrationMode === value ? 'border-blue-200 bg-white shadow-sm ring-1 ring-blue-100' : 'border-transparent text-slate-500 hover:bg-white/70'}`}>
                      <span className={`block text-sm font-bold ${registrationMode === value ? 'text-blue-700' : 'text-slate-700'}`}>{compact ? value === 'platform-direct' ? 'Install for me' : 'Run command' : label}</span>
                      {!compact && <span className="mt-0.5 block text-[11px] leading-4">{description}</span>}
                    </button>)}
                  </div>
                  {compact && <p className="mt-2 text-[11px] leading-5 text-slate-500">Use <strong>Install for me</strong> when HyperCDR can reach the cluster API and you can upload an administrator kubeconfig. Use <strong>Run command</strong> when installation must run from a host with cluster access.</p>}
                  </section>
 </div>;
}
