import React, { useId, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';
import { ClusterKubeconfigHelp } from './cluster-kubeconfig-guide';
import { registrationCommand } from './registration-command';
const selectCommandText=(element:HTMLElement)=>{const selection=window.getSelection();if(!selection)return;const range=document.createRange();range.selectNodeContents(element);selection.removeAllRanges();selection.addRange(range)};
export function ClusterCommandRegistrationFields({registrationType,prepareNodeCommand,installCommand,installLoading,installError,copied,caCopied,registryCACommandRef,installCommandRef,onCopyCA,onCopyInstall,onRegenerate}:{registrationType:RegistrationType;prepareNodeCommand:string;installCommand:string;installLoading:boolean;installError:string|null;copied:boolean;caCopied:boolean;registryCACommandRef:React.RefObject<HTMLTextAreaElement|null>;installCommandRef:React.RefObject<HTMLTextAreaElement|null>;onCopyCA:()=>void;onCopyInstall:(command:string)=>void;onRegenerate:()=>void}) {
 const [customIP, setCustomIP] = useState(false);
 const [platformIP, setPlatformIP] = useState('');
 const [lastCopiedCommand, setLastCopiedCommand] = useState('');
 const addressId = useId();
 const errorId = useId();
 const formatted = registrationCommand(installCommand, customIP ? platformIP : undefined);
 return <div className="hbdr-register-command-compact">
   <div className="hbdr-register-guided-step"><span>1</span><div><h3>Prepare the installation environment</h3><ClusterKubeconfigHelp type={registrationType} mode="command" /></div></div>
   {prepareNodeCommand && <section><div className="hbdr-register-command-head"><strong><span className="hbdr-register-step-number">2</span>Prepare cluster nodes</strong><button type="button" disabled={!prepareNodeCommand} onClick={onCopyCA}>{caCopied ? 'Copied' : 'Copy'}</button></div><pre aria-label="Registry CA command" onClick={event=>selectCommandText(event.currentTarget)}>{prepareNodeCommand}</pre><textarea ref={registryCACommandRef} readOnly value={prepareNodeCommand} className="sr-only" tabIndex={-1} aria-hidden="true" /></section>}
   <section><div className="hbdr-register-command-head"><strong><span className="hbdr-register-step-number">{prepareNodeCommand ? 3 : 2}</span>Install agent</strong><div className="hbdr-register-command-tools"><button type="button" className="hbdr-register-regenerate" disabled={installLoading} onClick={onRegenerate}><RefreshCw size={13} />{installLoading ? 'Generating…' : 'Regenerate command'}</button><button type="button" disabled={!formatted.command || !!formatted.error || !!installError || installLoading} onClick={() => { setLastCopiedCommand(formatted.command); onCopyInstall(formatted.command); }}>{copied && lastCopiedCommand === formatted.command ? 'Copied' : 'Copy'}</button></div></div>{installError ? <p role="alert">{installError}</p> : <pre aria-label="Install command" onClick={event=>selectCommandText(event.currentTarget)}>{installLoading ? 'Generating command…' : formatted.command || (formatted.error ? 'Enter a valid platform IP to generate the command.' : 'Command unavailable. Retry generation.')}</pre>}<textarea ref={installCommandRef} readOnly value={formatted.command} className="sr-only" tabIndex={-1} aria-hidden="true" /></section>
   <div className="hbdr-register-platform-ip">
     <label className="hbdr-register-platform-ip-option"><input type="checkbox" checked={customIP} onChange={event => setCustomIP(event.target.checked)} /><span>Specify the platform IP this cluster connects to</span></label>
     {customIP && <div className="hbdr-register-platform-ip-field"><label htmlFor={addressId}>Platform IP</label><input id={addressId} type="text" value={platformIP} onChange={event => setPlatformIP(event.target.value)} placeholder="e.g. 192.168.8.149" spellCheck={false} autoComplete="off" aria-invalid={!!formatted.error} aria-describedby={formatted.error ? errorId : undefined} /></div>}
     {formatted.error && <p id={errorId} role="alert">{formatted.error}</p>}
   </div>
   {installCommand && !installError && <p>Run this command as the Linux user with cluster access. The script checks connectivity and administrator permissions, then asks you to confirm the target cluster before installation. Failed checks stop installation. HyperCDR registers the cluster after the agent connects.</p>}
 </div>;
}
