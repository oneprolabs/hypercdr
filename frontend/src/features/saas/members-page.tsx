import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { Ban, Edit2, MailPlus, RefreshCw, Trash2, Users } from 'lucide-react';
import { apiDelete, apiGet, apiPatch, apiPost } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { ModalFrame } from '../../components/modal-frame';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatLocalDateTime } from '../../lib/date-time';
import { SettingsHeader } from './organization-settings';
import type { OrganizationRole } from './roles-page';
import './saas-auth.css';
import './tenant-access.css';

type Member = { id: string; username: string; email: string; systemAdmin?: boolean; displayName: string; role: string; roleId?: string; roleName?: string; status: string };
type Invitation = { id: string; email: string; role: string; roleId?: string; roleName?: string; status: string; expiresAt: string };
const roleName = (role: { role: string; roleName?: string }) => role.roleName || (role.role === 'admin' ? 'Administrator' : 'Operator');

export function MembersPage({ currentUser, toast, organization }: FrontendModuleContext & { organization?: { id: string; name: string } }) {
  const [members, setMembers] = useState<Member[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [roles, setRoles] = useState<OrganizationRole[]>([]);
  const [tab, setTab] = useState<'members' | 'invitations'>('members');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [formError, setFormError] = useState('');
  const [show, setShow] = useState(false);
  const [editing, setEditing] = useState<Member | null>(null);
  const [removing, setRemoving] = useState<Member | null>(null);
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('operator');
  const [status, setStatus] = useState('active');
  const [busy, setBusy] = useState(false);
  const [mail, setMail] = useState(false);
  const loadVersion = useRef(0);
  const base = organization ? `/api/v1/saas/admin/organizations/${encodeURIComponent(organization.id)}` : '/api/v1/saas';
  const canEdit = currentUser.role === 'admin' || currentUser.systemAdmin === true;

  const load = useCallback(async () => {
    const version = ++loadVersion.current;
    setLoading(true);
    setError('');
    try {
      const [memberResponse, invitationResponse, roleResponse] = await Promise.all([
        apiGet<{ items: Member[] }>(`${base}/members`),
        canEdit ? apiGet<{ items: Invitation[]; emailConfigured: boolean }>(`${base}/invitations`) : Promise.resolve(null),
        canEdit ? apiGet<{ items: OrganizationRole[] }>(`${base}/roles`) : Promise.resolve(null),
      ]);
      if (version !== loadVersion.current) return;
      setMembers(memberResponse.items);
      setInvitations(invitationResponse?.items || []);
      setMail(invitationResponse?.emailConfigured || false);
      setRoles(roleResponse?.items || []);
    } catch (failure) {
      if (version === loadVersion.current) setError(failure instanceof Error ? failure.message : 'Organization members could not be loaded.');
    } finally {
      if (version === loadVersion.current) setLoading(false);
    }
  }, [base, canEdit]);

  useEffect(() => {
    setMembers([]);
    setInvitations([]);
    setRoles([]);
    void load();
    return () => { ++loadVersion.current; };
  }, [load]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setFormError('');
    try {
      if (editing) await apiPatch(`${base}/members/${encodeURIComponent(editing.id)}`, { roleId: role, status });
      else await apiPost(`${base}/invitations`, { email, roleId: role });
      setShow(false);
      toast(editing ? 'Member access updated' : 'Invitation sent');
      await load();
    } catch (failure) {
      setFormError(failure instanceof Error ? failure.message : 'The request could not be completed.');
    } finally { setBusy(false); }
  }

  async function removeMember() {
    if (!removing || busy) return;
    setBusy(true);
    setFormError('');
    try {
      await apiDelete(`${base}/members/${encodeURIComponent(removing.id)}`);
      setRemoving(null);
      toast('Account deleted');
      await load();
    } catch (failure) { setFormError(failure instanceof Error ? failure.message : 'The account could not be deleted.'); }
    finally { setBusy(false); }
  }

  const memberColumns: HyperTableColumn<Member>[] = [
    { id: 'member', header: 'Member', size: 210, cell: ({ row }) => row.original.displayName || row.original.username },
    { id: 'account', header: 'Login account', size: 240, cell: ({ row }) => <span className="whitespace-nowrap" title={row.original.username}>{row.original.username}</span> },
    { id: 'email', header: 'Bound email', size: 240, cell: ({ row }) => <span className="whitespace-nowrap" title={row.original.email}>{row.original.email || <span className="ee-bound-email-unbound">Not bound</span>}</span> },
    { id: 'role', header: 'Role', size: 180, cell: ({ row }) => row.original.systemAdmin ? 'Platform administrator' : <span title={roleName(row.original)}>{roleName(row.original)}</span> },
    { id: 'status', header: 'Status', size: 100, cell: ({ row }) => row.original.status === 'active' ? 'Active' : 'Disabled' },
    ...(canEdit ? [{ id: 'actions', header: 'Actions', size: 168, cell: ({ row }: { row: { original: Member } }) => {
      const member = row.original;
      if (member.id === currentUser.id) return <span className="ee-invite-description">Your account</span>;
      if (member.systemAdmin) return <span className="ee-invite-description">Protected account</span>;
      return <div className="hc-directory-row-actions"><button className="hbdr-dr-action-secondary" disabled={loading || busy || roles.length === 0} onClick={() => { setEditing(member); setRole(member.roleId || member.role); setStatus(member.status); setFormError(''); setShow(true); }}><Edit2 size={14} />Edit</button><button className="hbdr-dr-action-secondary" disabled={busy} title="Delete account" aria-label={`Delete account ${member.username}`} onClick={() => { setFormError(''); setRemoving(member); }}><Trash2 size={14} /></button></div>;
    } }] : []),
  ];
  const invitationColumns: HyperTableColumn<Invitation>[] = [
    { id: 'email', header: 'Email', size: 280, cell: ({ row }) => <span className="whitespace-nowrap" title={row.original.email}>{row.original.email}</span> },
    { id: 'role', header: 'Role', size: 180, cell: ({ row }) => roleName(row.original) },
    { id: 'status', header: 'Status', size: 110, cell: ({ row }) => row.original.status },
    { id: 'expiry', header: 'Expires', size: 180, cell: ({ row }) => formatLocalDateTime(row.original.expiresAt) },
    { id: 'actions', header: 'Actions', size: 140, cell: ({ row }) => row.original.status === 'pending' ? <button className="hbdr-dr-action-secondary" disabled={busy} onClick={() => {
      setBusy(true);
      void apiPost(`${base}/invitations/${encodeURIComponent(row.original.id)}/revoke`, {}).then(() => { toast('Invitation revoked'); return load(); }).catch(failure => setError(failure instanceof Error ? failure.message : 'Invitation could not be revoked.')).finally(() => setBusy(false));
    }}><Ban size={14} />Revoke</button> : null },
  ];
  const selectedRole = roles.find(item => item.id === role);

  return <section className="space-y-5 ee-organization-access">
    <SettingsHeader title="Members" description={`Members of ${organization?.name || currentUser.tenantName || 'your organization'}. Each account belongs to one organization and has one role.`} icon={<Users size={18} />} />
    <div className="ee-members-tabs" role="tablist" aria-label="Organization membership"><button role="tab" aria-selected={tab === 'members'} onClick={() => setTab('members')}>Members</button>{canEdit && <button role="tab" aria-selected={tab === 'invitations'} onClick={() => setTab('invitations')}>Invitations</button>}</div>
    <div className="ee-settings-panel"><div className="hbdr-dr-toolbar hc-access-toolbar"><button className="hbdr-dr-action-secondary" disabled={loading || busy} onClick={() => void load()}><RefreshCw size={14} />Refresh</button>{canEdit && <button className="hbdr-dr-action-primary" disabled={loading || busy || !mail || roles.length === 0} title={!mail ? 'Configure system SMTP to send invitations.' : undefined} onClick={() => { setEditing(null); setEmail(''); setRole(members.filter(member => !member.systemAdmin).length === 0 ? 'admin' : 'operator'); setFormError(''); setShow(true); }}><MailPlus size={14} />Invite member</button>}</div>
      {canEdit && !loading && !mail && <p className="ee-invite-description">System SMTP must be configured to send invitations.</p>}
      {error && <p role="alert" className="ee-settings-error">{error} Use Refresh to retry.</p>}
      {tab === 'members' ? <HyperTable columns={memberColumns} data={members} emptyMessage={loading ? 'Loading members…' : error ? 'Members could not be loaded.' : 'No members.'} getRowId={member => member.id} /> : <HyperTable columns={invitationColumns} data={invitations} emptyMessage={loading ? 'Loading invitations…' : 'No invitations yet.'} getRowId={invitation => invitation.id} />}
    </div>
    {show && <ModalFrame title={editing ? 'Edit member' : 'Invite member'} subtitle={editing ? 'Role changes apply to new requests. Tasks already running are not interrupted.' : 'The recipient joins this organization after verifying their email.'} onClose={() => { if (!busy) setShow(false); }}><form className="ee-invite-form" onSubmit={event => void submit(event)}>
      {editing ? <><label>Login account<input readOnly value={editing.username} /></label><label>Bound email<input readOnly value={editing.email} placeholder="Not bound" /></label></> : <label>Email<input type="email" required value={email} onChange={event => setEmail(event.target.value)} /></label>}
      <label>Role<select value={role} onChange={event => setRole(event.target.value)}>{roles.map(item => <option key={item.id} value={item.id}>{item.name}{item.builtIn ? ' (Built-in)' : ''}</option>)}</select></label>
      {selectedRole && <p className="hc-access-note">{selectedRole.description || `${selectedRole.permissions.length} business permissions. No organization management access.`}</p>}
      {editing && <label>Status<select value={status} onChange={event => setStatus(event.target.value)}><option value="active">Active</option><option value="disabled">Disabled</option></select></label>}
      {formError && <p role="alert" className="ee-settings-error">{formError}</p>}
      <div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setShow(false)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy || !selectedRole}>{busy ? 'Saving…' : editing ? 'Save changes' : 'Send invitation'}</button></div>
    </form></ModalFrame>}
    {removing && <ModalFrame title="Delete account" subtitle={`Organization: ${organization?.name || currentUser.tenantName}`} onClose={() => { if (!busy) setRemoving(null); }}><p className="ee-invite-description">Delete account <strong>{removing.username}</strong>? This permanently deletes the account and its sign-in identities and signs the user out immediately. Organization resources remain unchanged. This cannot be undone.</p>{formError && <p role="alert" className="ee-settings-error">{formError}</p>}<div className="ee-invite-actions"><button className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setRemoving(null)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy} onClick={() => void removeMember()}><Trash2 size={14} />{busy ? 'Deleting…' : 'Delete account'}</button></div></ModalFrame>}
  </section>;
}
