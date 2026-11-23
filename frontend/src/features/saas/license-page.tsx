import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Copy, KeyRound, RefreshCw, Upload } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { ModalFrame } from '../../components/modal-frame';
import { formatLocalDateTime } from '../../lib/date-time';
import { SettingsHeader } from './organization-settings';
import './tenant-access.css';

type LicenseUsage = {
  tenantId: string;
  plan: 'free' | 'commercial';
  status: 'active' | 'expired';
  maxClusters: number;
  usedClusters: number;
  overLimit: boolean;
  canRegister: boolean;
  licenseId?: string;
  validUntil?: string;
};
type LicenseResponse = { usage: LicenseUsage; canActivate: boolean; activationConfigured: boolean };

export function LicensePage({ currentUser, toast }: FrontendModuleContext) {
  const [data, setData] = useState<LicenseResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [open, setOpen] = useState(false);
  const [raw, setRaw] = useState('');
  const [fileName, setFileName] = useState('');
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState('');
  const canManage = currentUser.role === 'admin' || currentUser.systemAdmin === true;

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try { setData(await apiGet<LicenseResponse>('/api/v1/saas/license')); }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'License information could not be loaded.'); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  async function activate(event: FormEvent) {
    event.preventDefault();
    if (busy || !raw.trim()) return;
    setBusy(true);
    setFormError('');
    try {
      const response = await apiPost<LicenseResponse>('/api/v1/saas/license', { license: raw.trim() });
      setData(response);
      setOpen(false);
      setRaw('');
      setFileName('');
      toast(`License activated. Your organization can manage ${response.usage.maxClusters} clusters.`);
    } catch (failure) {
      setFormError(failure instanceof Error ? failure.message : 'The license could not be activated.');
    } finally { setBusy(false); }
  }

  async function readFile(file: File | undefined) {
    if (!file) return;
    setFormError('');
    if (file.size > 16384) { setFormError('Select a license file smaller than 16 KiB.'); return; }
    setBusy(true);
    try {
      const text = await file.text();
      setRaw(text);
      setFileName(file.name);
    } catch { setFormError('The selected file could not be read. Select it again or paste the signed license.'); }
    finally { setBusy(false); }
  }

  const usage = data?.usage;
  return <section className="space-y-5 ee-organization-access">
    <SettingsHeader title="License & Usage" description="Cluster capacity is shared by all members of your organization. Role permissions are managed separately." icon={<KeyRound size={18} />} />
    <div className="ee-settings-panel">
      <header><div><h4>{currentUser.tenantName || 'Your organization'}</h4><p>One managed cluster is free. Activate a commercial license for additional clusters.</p></div><div className="hc-directory-row-actions"><button className="hbdr-dr-action-secondary" disabled={loading} onClick={() => void load()}><RefreshCw size={14} />Refresh</button>{canManage && <button className="hbdr-dr-action-primary" disabled={!data || loading || !data.activationConfigured} title={data && !data.activationConfigured ? 'The platform administrator must configure the license issuer public key first.' : undefined} onClick={() => { setRaw(''); setFileName(''); setFormError(''); setOpen(true); }}><Upload size={14} />{usage?.licenseId ? 'Update license' : 'Activate license'}</button>}</div></header>
      {error && <p className="ee-settings-error" role="alert">{error} Use Refresh to retry.</p>}
      {loading && !data ? <p role="status">Loading license information…</p> : usage && <>
        <dl className="hc-license-summary"><div><dt>Plan</dt><dd>{usage.plan === 'commercial' ? 'Commercial' : 'Free'}</dd><small>{usage.status === 'expired' ? 'Commercial license expired' : 'Active'}</small></div><div><dt>Managed clusters</dt><dd>{usage.usedClusters} / {usage.maxClusters}</dd><small>{usage.overLimit ? 'Over limit' : usage.canRegister ? `${usage.maxClusters - usage.usedClusters} available` : 'Limit reached'}</small></div><div><dt>Commercial expiry</dt><dd>{usage.validUntil ? formatLocalDateTime(usage.validUntil) : 'Not applicable'}</dd><small>Free access does not expire.</small></div></dl>
        {(usage.overLimit || usage.status === 'expired') && <p className="hc-access-note hc-access-note-warning" role="status">{usage.status === 'expired' ? 'The commercial license has expired. The free one-cluster limit now applies.' : 'The organization has more clusters than its current license allows.'} Existing clusters and running protection or recovery tasks are not stopped. Renew your license or unregister unused clusters before adding another.</p>}
        {!data.activationConfigured && <p className="hc-access-note">Commercial activation is not configured on this platform. Contact the platform administrator to configure the license issuer public key.</p>}
        <dl className="ee-settings-details"><div className="ee-settings-detail ee-settings-detail-wide"><dt>Organization ID</dt><dd className="hc-directory-row-actions"><span className="hc-license-code">{usage.tenantId}</span><button className="hbdr-dr-action-secondary" aria-label="Copy organization ID" onClick={() => { void navigator.clipboard.writeText(usage.tenantId).then(() => toast('Organization ID copied')).catch(() => toast('Copy failed. Select and copy the organization ID manually.')); }}><Copy size={14} /></button></dd></div></dl>
        <p className="ee-invite-description">Online, offline and unregistering clusters count toward usage. Capacity is released only after unregistration completes. A license does not grant member permissions.</p>
      </>}
    </div>
    {open && <ModalFrame title={usage?.licenseId ? 'Update license' : 'Activate license'} subtitle="The license must be issued for this organization. The platform verifies its signature, capacity and validity before applying it." onClose={() => { if (!busy) setOpen(false); }}><form className="ee-invite-form" onSubmit={event => void activate(event)}>
      <label className="hc-license-file">License file<input type="file" accept=".json,.license,application/json" disabled={busy} onChange={event => void readFile(event.target.files?.[0])} /><small className="ee-invite-description">{fileName || 'Choose a signed license file, or paste its contents below.'}</small></label>
      <label>Signed license<textarea className="hc-license-code" rows={7} value={raw} disabled={busy} onChange={event => { setRaw(event.target.value); setFileName(''); setFormError(''); }} placeholder="Paste the signed license JSON" /></label>
      <p className="hc-access-note">The cluster limit is the total allowed, not additional capacity. Activating a license does not delete clusters or interrupt running tasks.</p>
      {formError && <p className="ee-settings-error" role="alert">{formError}</p>}
      <div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setOpen(false)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy || !raw.trim()}>{busy ? 'Validating…' : 'Activate license'}</button></div>
    </form></ModalFrame>}
  </section>;
}
