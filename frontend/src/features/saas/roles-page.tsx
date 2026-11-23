import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Edit2, Eye, Plus, RefreshCw, Shield, Trash2 } from 'lucide-react';
import { apiDelete, apiGet, apiPatch, apiPost } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { ModalFrame } from '../../components/modal-frame';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { SettingsHeader } from './organization-settings';
import './tenant-access.css';

export type OrganizationRole = {
  id: string;
  name: string;
  description: string;
  builtIn: boolean;
  permissions: string[];
  memberCount: number;
  pendingInvitationCount: number;
};

type Permission = { key: string; name: string; group: string; description: string };
type RoleResponse = { items: OrganizationRole[]; permissions: Permission[] };
type RoleDraft = { id?: string; name: string; description: string; permissions: string[]; builtIn?: boolean };

export function RolesPage({ currentUser, toast }: FrontendModuleContext) {
  const [roles, setRoles] = useState<OrganizationRole[]>([]);
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<RoleDraft | null>(null);
  const [removing, setRemoving] = useState<OrganizationRole | null>(null);
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState('');
  const canManage = currentUser.role === 'admin' || currentUser.systemAdmin === true;

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await apiGet<RoleResponse>('/api/v1/saas/roles');
      setRoles(response.items);
      setPermissions(response.permissions);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Roles could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  function togglePermission(key: string) {
    setDraft(current => current ? {
      ...current,
      permissions: current.permissions.includes(key) ? current.permissions.filter(value => value !== key) : [...current.permissions, key],
    } : null);
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!draft || draft.builtIn || busy) return;
    setBusy(true);
    setFormError('');
    try {
      const body = { name: draft.name.trim(), description: draft.description.trim(), permissions: draft.permissions };
      if (draft.id) await apiPatch(`/api/v1/saas/roles/${encodeURIComponent(draft.id)}`, body);
      else await apiPost('/api/v1/saas/roles', body);
      setDraft(null);
      toast(draft.id ? 'Role updated. New requests use the updated permissions.' : 'Role created. Assign it to members or invitations.');
      await load();
    } catch (failure) {
      setFormError(failure instanceof Error ? failure.message : 'The role could not be saved.');
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!removing || busy) return;
    setBusy(true);
    setFormError('');
    try {
      await apiDelete(`/api/v1/saas/roles/${encodeURIComponent(removing.id)}`);
      setRemoving(null);
      toast('Role deleted');
      await load();
    } catch (failure) {
      setFormError(failure instanceof Error ? failure.message : 'The role could not be deleted.');
    } finally {
      setBusy(false);
    }
  }

  const columns: HyperTableColumn<OrganizationRole>[] = [
    { id: 'name', header: 'Role', size: 240, minSize: 190, cell: ({ row }) => <div className="hc-role-name"><span title={row.original.name}>{row.original.name}</span><small title={row.original.description}>{row.original.description || 'Custom business permissions'}</small></div> },
    { id: 'source', header: 'Type', size: 130, cell: ({ row }) => row.original.builtIn ? 'Built-in · Read-only' : 'Organization' },
    { id: 'permissions', header: 'Access', size: 240, cell: ({ row }) => row.original.id === 'admin' ? 'All business & organization management' : row.original.id === 'operator' ? 'All business permissions' : `${row.original.permissions.length} of ${permissions.length} business permissions` },
    { id: 'members', header: 'Members', size: 100, cell: ({ row }) => row.original.memberCount },
    { id: 'invitations', header: 'Pending invites', size: 120, cell: ({ row }) => row.original.pendingInvitationCount },
    { id: 'actions', header: 'Actions', size: 150, cell: ({ row }) => {
      const role = row.original;
      const readOnly = role.builtIn || !canManage;
      const inUse = role.memberCount > 0 || role.pendingInvitationCount > 0;
      return <div className="hc-directory-row-actions">
        <button className="hbdr-dr-action-secondary" onClick={() => { setFormError(''); setDraft({ ...role, builtIn: readOnly }); }}><span aria-hidden="true">{readOnly ? <Eye size={14} /> : <Edit2 size={14} />}</span>{readOnly ? 'View' : 'Edit'}</button>
        {!readOnly && <button className="hbdr-dr-action-secondary" disabled={inUse} title={inUse ? 'Reassign members and revoke pending invitations first.' : `Delete ${role.name}`} aria-label={`Delete role ${role.name}`} onClick={() => { setFormError(''); setRemoving(role); }}><Trash2 size={14} /></button>}
      </div>;
    } },
  ];
  const groups = [...new Set(permissions.map(permission => permission.group))];

  return <section className="space-y-5 ee-organization-access">
    <SettingsHeader title="Roles & Permissions" description="Assign roles to members. Built-in roles are shared definitions; custom roles apply only to your organization." icon={<Shield size={18} />} />
    <div className="ee-settings-panel">
      <div className="hbdr-dr-toolbar hc-access-toolbar">
        <span className="ee-invite-description">Role changes affect new requests, not tasks already running.</span>
        <div className="hc-directory-row-actions"><button className="hbdr-dr-action-secondary" disabled={loading} onClick={() => void load()}><RefreshCw size={14} />Refresh</button>
          {canManage && <button className="hbdr-dr-action-primary" disabled={loading || permissions.length === 0} onClick={() => { setFormError(''); setDraft({ name: '', description: '', permissions: ['resources.view'] }); }}><Plus size={14} />Create role</button>}
        </div>
      </div>
      {error && <p className="ee-settings-error" role="alert">{error} Use Refresh to retry.</p>}
      <HyperTable columns={columns} data={roles} getRowId={role => role.id} emptyMessage={loading ? 'Loading roles…' : error ? 'Roles could not be loaded.' : 'No roles.'} />
    </div>
    {draft && <ModalFrame title={draft.builtIn ? 'Role permissions' : draft.id ? 'Edit role' : 'Create role'} subtitle={draft.builtIn ? 'Read-only role definition.' : 'Only members of this organization can use this role.'} onClose={() => { if (!busy) setDraft(null); }}>
      <form className="ee-invite-form hc-role-form" onSubmit={event => void save(event)}>
        <label>Role name<input required maxLength={64} readOnly={draft.builtIn} value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} placeholder="For example, Drill operator" /></label>
        <label>Description<textarea maxLength={500} rows={2} readOnly={draft.builtIn} value={draft.description} onChange={event => setDraft({ ...draft, description: event.target.value })} placeholder="Describe who should use this role" /></label>
        <div className="hc-permission-heading"><strong>Business permissions</strong><span>{draft.permissions.length} selected</span></div>
        {draft.id === 'admin' && <p className="hc-access-note">Administrator also manages organization information, members, roles and licenses. Platform administration is separate.</p>}
        {draft.id === 'operator' && <p className="hc-access-note">Operator has all business permissions, but cannot manage organization information, members, roles or licenses.</p>}
        <div className="hc-permission-groups">{groups.map(group => <fieldset key={group} disabled={draft.builtIn || busy}><legend>{group}</legend>{permissions.filter(permission => permission.group === group).map(permission => <label key={permission.key} className="hc-permission-option"><input type="checkbox" checked={draft.permissions.includes(permission.key)} onChange={() => togglePermission(permission.key)} /><span><strong>{permission.name}</strong><small>{permission.description}</small></span></label>)}</fieldset>)}</div>
        {!draft.builtIn && draft.permissions.length === 0 && <p className="hc-access-note">This role has no business access. Members can still sign in and manage their personal information.</p>}
        {formError && <p className="ee-settings-error" role="alert">{formError}</p>}
        <div className="ee-invite-actions"><button type="button" className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setDraft(null)}>{draft.builtIn ? 'Close' : 'Cancel'}</button>{!draft.builtIn && <button className="hbdr-dr-action-primary" disabled={busy || !draft.name.trim()}>{busy ? 'Saving…' : draft.id ? 'Save changes' : 'Create role'}</button>}</div>
      </form>
    </ModalFrame>}
    {removing && <ModalFrame title="Delete role" subtitle="This action cannot be undone." onClose={() => { if (!busy) setRemoving(null); }}><p className="ee-invite-description">Delete <strong>{removing.name}</strong>? Built-in roles and other organization roles remain unchanged.</p>{formError && <p className="ee-settings-error" role="alert">{formError}</p>}<div className="ee-invite-actions"><button className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setRemoving(null)}>Cancel</button><button className="hbdr-dr-action-primary" disabled={busy} onClick={() => void remove()}>{busy ? 'Deleting…' : 'Delete role'}</button></div></ModalFrame>}
  </section>;
}
