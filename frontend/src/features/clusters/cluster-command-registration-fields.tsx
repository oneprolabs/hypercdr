import React from 'react';
import { Check, CheckCircle2, RefreshCw } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';
function RegistrationStep({number,title,description,children}:{number:number;title:string;description:string;children?:React.ReactNode}) {
  return <section className="flex gap-3.5">
    <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white shadow-sm shadow-blue-200">{number}</div>
    <div className="min-w-0 flex-1 pb-1">
      <h3 className="text-sm font-bold leading-7 text-slate-900">{title}</h3>
      <p className="mt-0.5 text-xs leading-5 text-slate-500">{description}</p>
      {children && <div className="mt-3">{children}</div>}
    </div>
  </section>;
}
const selectCommandText=(element:HTMLElement)=>{const selection=window.getSelection();if(!selection)return;const range=document.createRange();range.selectNodeContents(element);selection.removeAllRanges();selection.addRange(range)};
export function ClusterCommandRegistrationFields({registrationType,prepareNodeCommand,installCommand,installLoading,installError,copied,caCopied,registryCACommandRef,installCommandRef,onCopyCA,onCopyInstall,onRegenerate,compact=true}:{registrationType:RegistrationType;prepareNodeCommand:string;installCommand:string;installLoading:boolean;installError:string|null;copied:boolean;caCopied:boolean;registryCACommandRef:React.RefObject<HTMLTextAreaElement|null>;installCommandRef:React.RefObject<HTMLTextAreaElement|null>;onCopyCA:()=>void;onCopyInstall:()=>void;onRegenerate:()=>void;compact?:boolean}) {
 if (compact) return <div className="hbdr-register-command-compact">
   {registrationType === 'huaweicloud-cce' && <p>Run on a Linux host with kubectl and a CCE kubeconfig.</p>}
   {registrationType === 'openshift' && <p>Run on a Linux administration host with cluster-admin access.</p>}
   {prepareNodeCommand && <section><div className="hbdr-register-command-head"><strong>Prepare cluster nodes</strong><button type="button" disabled={!prepareNodeCommand} onClick={onCopyCA}>{caCopied ? 'Copied' : 'Copy'}</button></div><pre aria-label="Registry CA command" onClick={event=>selectCommandText(event.currentTarget)}>{prepareNodeCommand}</pre><textarea ref={registryCACommandRef} readOnly value={prepareNodeCommand} className="sr-only" tabIndex={-1} aria-hidden="true" /></section>}
   <section><div className="hbdr-register-command-head"><strong>Install agent</strong><div className="hbdr-register-command-tools"><button type="button" className="hbdr-register-regenerate" disabled={installLoading} onClick={onRegenerate}><RefreshCw size={13} />{installLoading ? 'Generating…' : 'Regenerate command'}</button><button type="button" disabled={!installCommand || installLoading} onClick={onCopyInstall}>{copied ? 'Copied' : 'Copy'}</button></div></div>{installError ? <p role="alert">{installError}</p> : <pre aria-label="Install command" onClick={event=>selectCommandText(event.currentTarget)}>{installLoading ? 'Generating command…' : installCommand || 'Command unavailable. Retry generation.'}</pre>}<textarea ref={installCommandRef} readOnly value={installCommand} className="sr-only" tabIndex={-1} aria-hidden="true" /></section>
   {installCommand && !installError && <p>Run the command on a host with cluster access. HyperCDR registers the cluster after its agent connects.</p>}
 </div>;
 return <>
                                    <div className="space-y-5 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
                  {(() => {
                    let step = 0;
                    const nextStep = () => ++step;
                    return <>
                  {registrationType === 'huaweicloud-cce' && <>
                    <RegistrationStep number={nextStep()} title="Install kubectl" description="Install kubectl on the Linux host that will run the registration command." />
                    <RegistrationStep number={nextStep()} title="Download the CCE kubeconfig" description="Download the cluster credential from Huawei Cloud CCE and save it as ~/.kube/hypercdr-cce.yaml. The installer will detect it automatically." />
                  </>}
                  {prepareNodeCommand && <RegistrationStep number={nextStep()} title="Install the registry CA" description="Run this command on every Kubernetes node to trust the private image registry.">
                  <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - prepare node</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <pre className="hbdr-cluster-register-command min-w-0 flex-1 cursor-text whitespace-pre-wrap break-all font-mono text-[11px] leading-5 text-blue-300" aria-label="Registry CA command" onClick={event=>selectCommandText(event.currentTarget)}>{prepareNodeCommand}</pre>
                        <textarea ref={registryCACommandRef} readOnly value={prepareNodeCommand} className="sr-only" tabIndex={-1} aria-hidden="true" />
                      </div>
                    </div>
                    <button onClick={onCopyCA} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95">
                      {caCopied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {caCopied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  </RegistrationStep>}

                  <RegistrationStep
                    number={nextStep()}
                    title="Install HyperCDR agent"
                    description={registrationType === 'huaweicloud-cce' ? 'Run this command on the host with access to the CCE cluster. The installer will verify the kubeconfig before making changes.' : registrationType === 'openshift' ? 'Run this command on a Linux administration host with cluster-admin access to OpenShift. Do not modify RHCOS nodes.' : 'Log in to the Kubernetes control-plane node and run this command.'}
                  >
                  {!installError && <div className="relative">
                    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900 p-4 font-mono text-[11px] leading-5 text-blue-300 shadow-inner">
                      <div className="mb-2 flex items-center gap-2 border-b border-white/10 pb-2 opacity-50">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="h-2 w-2 rounded-full bg-amber-500" />
                        <span className="h-2 w-2 rounded-full bg-emerald-500" />
                        <span className="ml-2 font-sans tracking-wide">Terminal - install agent</span>
                      </div>
                      <div className="flex items-start gap-2">
                        <span className="text-white/30">$</span>
                        <pre className="hbdr-cluster-register-command min-w-0 flex-1 cursor-text whitespace-pre-wrap break-all font-mono text-[11px] leading-5 text-blue-300" aria-label="Install command" onClick={event=>selectCommandText(event.currentTarget)}>{installLoading ? 'Generating install command...' : installCommand}</pre>
                        <textarea ref={installCommandRef} readOnly value={installCommand} className="sr-only" tabIndex={-1} aria-hidden="true" />
                      </div>
                    </div>
                    <button disabled={installLoading || !installCommand} onClick={onCopyInstall} className="absolute right-3 top-3 flex items-center gap-2 rounded-lg bg-white/20 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-white backdrop-blur transition-all hover:bg-white/30 active:scale-95 disabled:cursor-wait disabled:opacity-60">
                      {copied ? <CheckCircle2 size={12} /> : <Check size={12} />}
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>}
                  {installError && <p className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-medium text-rose-700">{installError}</p>}
                  </RegistrationStep>

                  <RegistrationStep number={nextStep()} title="Connect the cluster" description="Run the install command. HyperCDR registers the cluster after its agent connects." />
                  </>;
                  })()}
                  </div>
                   </>;
}
