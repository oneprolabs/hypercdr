import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';

type GuideMode = 'command' | 'platform-direct';
const directory = '/etc/hypercdr/kubeconfigs';

function CommandBlock({ label, command }: { label: string; command: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };
  return <div className="hbdr-kubeconfig-command">
    <div className="hbdr-kubeconfig-command-head">
      <span>{label}</span>
      <button type="button" onClick={() => void copy()} aria-label={`Copy ${label}`}>
        {copied ? <Check size={13} /> : <Copy size={13} />}{copied ? 'Copied' : 'Copy'}
      </button>
    </div>
    <pre>{command}</pre>
  </div>;
}

export function ClusterKubeconfigHelp({ type, mode }: { type: RegistrationType; mode: GuideMode }) {
  const commandMode = mode === 'command';
  return <div className="hbdr-register-kubeconfig-guide">
    {type === 'native-kubernetes' && <>
      <p>On a control-plane or administration host, use a context with cluster-admin permissions.</p>
      <CommandBlock label="Check context and permissions" command={'kubectl config current-context\nkubectl auth can-i create clusterroles.rbac.authorization.k8s.io'} />
      <p>The permission check must print <code>yes</code>.</p>
      <p>Export the active context to a portable file.</p>
      <CommandBlock label="Export kubeconfig" command={'kubectl config view --raw --minify --flatten > hypercdr-native-kubeconfig.yaml\nchmod 600 hypercdr-native-kubeconfig.yaml'} />
      {!commandMode && <p>Upload <code>hypercdr-native-kubeconfig.yaml</code> in the next step.</p>}
    </>}
    {type === 'huaweicloud-cce' && <>
      <p>In Huawei Cloud CCE, open <strong>Clusters → target cluster → Cluster Connection / kubectl Access</strong>. Download a cluster-admin kubeconfig.</p>
      {commandMode ? <p>Copy the downloaded file to the Linux host where you will run the installer, and name it <code>cce-kubeconfig.yaml</code>.</p> : <p>Upload the downloaded YAML file in the next step and select its target context.</p>}
    </>}
    {type === 'openshift' && <>
      <p>In the OpenShift Web Console, choose <strong>Copy login command</strong> from the user menu. Run it on a Linux administration host with the <code>oc</code> CLI.</p>
      <CommandBlock label="Check login and permissions" command={'oc whoami\noc auth can-i create clusterroles.rbac.authorization.k8s.io'} />
      <p>The permission check must print <code>yes</code>.</p>
      <p>Export the active context to a portable file.</p>
      <CommandBlock label="Export kubeconfig" command={'oc config view --raw --minify --flatten > hypercdr-openshift-kubeconfig.yaml\nchmod 600 hypercdr-openshift-kubeconfig.yaml'} />
      {!commandMode && <p>Upload <code>hypercdr-openshift-kubeconfig.yaml</code> in the next step.</p>}
    </>}
    {commandMode && <>
      <p>Create the protected directory on this Linux host.</p>
      <CommandBlock label="Create kubeconfig directory" command={`sudo install -d -m 700 ${directory}`} />
      <p>Move the file into that directory with private permissions.</p>
      <CommandBlock label="Save kubeconfig for registration" command={type === 'huaweicloud-cce'
        ? `sudo install -m 600 ./cce-kubeconfig.yaml ${directory}/cce-kubeconfig.yaml`
        : type === 'openshift'
          ? `sudo install -m 600 ./hypercdr-openshift-kubeconfig.yaml ${directory}/openshift-kubeconfig.yaml`
          : `sudo install -m 600 ./hypercdr-native-kubeconfig.yaml ${directory}/native-kubeconfig.yaml`} />
      <p>Open a root shell with <code>sudo -i</code>, then run the install command in the next step. It lists the valid files in <code>{directory}</code> so you can choose one; if a file has several contexts, it asks you to choose a context too.</p>
    </>}
    <p className="hbdr-register-kubeconfig-security">Kubeconfig files contain administrator credentials. Keep them private and remove copies you no longer need.</p>
  </div>;
}
