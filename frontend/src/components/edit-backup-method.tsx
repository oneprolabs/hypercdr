import { useEffect, useRef, useState } from 'react';
import { LoaderCircle, ShieldCheck } from 'lucide-react';
import { snapshotPrerequisiteResult, snapshotPrerequisiteSummary, snapshotSetupGuidance, type DataProtectionSettings, type SnapshotPrerequisiteInventory } from '../features/recovery/data-protection-model';
import './edit-backup-method.css';

type Props = {
  open: boolean;
  scopeKey: string;
  namespaces: string[];
  value: DataProtectionSettings | undefined;
  onChange: (value: DataProtectionSettings) => void;
  onCheck?: () => Promise<SnapshotPrerequisiteInventory>;
  onValidityChange: (valid: boolean) => void;
};

export function EditBackupMethod(props: Props) {
  const [result, setResult] = useState<ReturnType<typeof snapshotPrerequisiteResult> | null>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const latest = useRef(props);
  latest.current = props;
  const scopeKey = JSON.stringify([props.scopeKey, [...new Set(props.namespaces)].sort()]);
  const [checkedScope, setCheckedScope] = useState('');
  const supported = checkedScope === scopeKey && result?.status === 'supported';
  const valid = props.value?.mode === 'live-filesystem' || (!props.value?.mode) || (props.value.mode === 'snapshot-crash-consistent' && supported);
  useEffect(() => { props.onValidityChange(valid); }, [valid, props.onValidityChange]);
  useEffect(() => {
    if (!props.open) return;
    let canceled = false;
    setResult(null);
    setError('');
    const check = async () => {
      try {
        if (!latest.current.onCheck) throw new Error('CSI detection is unavailable.');
        const inventory = await latest.current.onCheck();
        if (!canceled) {
          setResult(snapshotPrerequisiteResult(inventory, latest.current.namespaces, Date.now()));
          setCheckedScope(scopeKey);
        }
      } catch (failure) {
        if (!canceled) {
          setResult(snapshotPrerequisiteResult({ capabilities: [] }, latest.current.namespaces, Date.now()));
          setCheckedScope(scopeKey);
          setError(failure instanceof Error ? failure.message : 'CSI detection failed.');
        }
      }
    };
    void check();
    return () => { canceled = true; };
  }, [props.open, scopeKey, retry]);
  const summary = snapshotPrerequisiteSummary(result?.checks || []);
  const checking = !result || checkedScope !== scopeKey;
  return <section className="hbdr-config-section hbdr-protect-backup-method hbdr-edit-backup-method" aria-label="Backup method">
    <div className="hbdr-config-section-head"><ShieldCheck size={17} /><strong>Backup method</strong></div>
    <div className="hbdr-protect-wizard-options">
      <label className={`${props.value?.mode === 'snapshot-crash-consistent' ? 'is-selected' : ''} ${!supported ? 'is-unavailable' : ''}`}>
        <input type="radio" name="edit-backup-method" disabled={!supported} checked={props.value?.mode === 'snapshot-crash-consistent'} onChange={() => props.onChange({ mode: 'snapshot-crash-consistent' })} />
        <span><strong>CSI snapshots</strong><small>Crash-consistent snapshots. Apps keep running.</small></span>
      </label>
      <label className={!props.value?.mode || props.value.mode === 'live-filesystem' ? 'is-selected' : ''}>
        <input type="radio" name="edit-backup-method" checked={!props.value?.mode || props.value.mode === 'live-filesystem'} onChange={() => props.onChange({ mode: 'live-filesystem', riskAcknowledgement: 'live-filesystem-no-application-consistency-v1' })} />
        <span><strong>File-level backup</strong><small>Live files, no snapshots. Consistency is not guaranteed.</small></span>
      </label>
    </div>
    {!supported && (checking ? <div className="hbdr-protect-backup-status" role="status"><LoaderCircle size={14} className="animate-spin" />Checking CSI snapshot support…</div> : <>
      {error && <p role="alert">{error}</p>}
      <details className="hbdr-csi-volume-details" key={`${scopeKey}:${retry}`}>
        <summary>{summary.namespaces} namespaces · {summary.volumes} volumes: {summary.supported} supported · {summary.unsupported} unsupported{summary.failed > 0 && ` · ${summary.failed} detection failed`}{summary.unverifiedNamespaces > 0 && ` · ${summary.unverifiedNamespaces} namespaces not verified`}</summary>
        <div className="hbdr-csi-volume-groups"><table><caption className="sr-only">CSI snapshot support</caption><thead><tr><th>Namespace</th><th>PVC</th><th>Storage / CSI driver</th><th>Status</th><th>Reason / next step</th></tr></thead><tbody>
          {result?.checks.flatMap(check => check.volumes.length ? check.volumes.map(volume => <tr key={`${check.namespace}/${volume.name}`}>
            <td>{check.namespace}</td><td>{volume.name}</td><td>{volume.fields?.storageClass || '—'}<small>{volume.driver || 'No CSI driver'}</small></td>
            <td>{volume.fields?.status === 'configured' ? 'Supported' : volume.fields?.status === 'unsupported' ? 'Unsupported' : 'Detection failed'}</td>
            <td>{volume.fields?.reason || 'Volume support could not be verified.'}{volume.fields?.status !== 'configured' && <small>{snapshotSetupGuidance(volume)}</small>}</td>
          </tr>) : [<tr key={check.namespace}><td>{check.namespace}</td><td>—</td><td>—</td><td>{check.status === 'configured' ? 'No volumes' : 'Detection failed'}</td><td>{check.status === 'configured' ? 'No persistent volumes.' : check.error || error || 'Check Agent connectivity and permissions, then recheck.'}</td></tr>])}
        </tbody></table></div>
      </details>
      <div className="hbdr-protect-backup-status"><button type="button" onClick={() => setRetry(previous => previous + 1)}>Recheck</button><a href="https://kubernetes.io/docs/concepts/storage/volume-snapshots/" target="_blank" rel="noopener noreferrer">How to enable CSI snapshots? (Kubernetes docs)</a></div>
    </>)}
    {props.value?.mode === 'application-consistent' && <p role="alert">Select CSI snapshots or file-level backup. Application stop/freeze coordination is no longer supported.</p>}
    <p className="hbdr-edit-backup-method-note">Future syncs only. Existing recovery points are unchanged.</p>
  </section>;
}
