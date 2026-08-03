import { useCallback, useEffect, useState } from 'react';
import { ChevronDown, Edit2, KeyRound, Trash2, User, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiGet, apiPatch, apiPost } from '../../api/client';
import type { ApiLoginResponse } from '../../auth/types';
import { EditField } from '../../components/edit-field';
import { ModalFrame } from '../../components/modal-frame';
import { PasswordValidation } from '../../components/password-validation';
import { HyperTable, type HyperTableColumn } from '../../components/table';

type ApiList<T> = { items:T[] };
type ApiPlatformUser = ApiLoginResponse['user'];
type ApiTenant = { id:string; name:string; status:'active'|'disabled' };
const listItems = <T,>(response:ApiList<T>) => response.items || [];
const ACCOUNT_EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export default function UserManagementPage({ currentUser, toast }: { currentUser: ApiLoginResponse['user']; toast: (message: string) => void }) {
  const canManageTenants = currentUser.systemAdmin === true || currentUser.email === "admin";
  const [users, setUsers] = useState<ApiPlatformUser[]>([]);
  const [tenants, setTenants] = useState<ApiTenant[]>([]);
  const [editing, setEditing] = useState<ApiPlatformUser | null>(null);
  const [creating, setCreating] = useState(false);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [role, setRole] = useState("operator");
  const [status, setStatus] = useState("active");
  const [tenantId, setTenantId] = useState(currentUser.tenantId);
  const [passwordTarget, setPasswordTarget] = useState<ApiPlatformUser | null>(
    null,
  );
  const [deleteTarget, setDeleteTarget] = useState<ApiPlatformUser | null>(
    null,
  );
  const [replacementPassword, setReplacementPassword] = useState("");
  const [replacementPasswordConfirm, setReplacementPasswordConfirm] =
    useState("");
  const [selectedUserIds, setSelectedUserIds] = useState<string[]>([]);
  const [userBulkMenuOpen, setUserBulkMenuOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [userActionBusy, setUserActionBusy] = useState(false);
  const load = useCallback(async () => {
    const result = await apiGet<ApiList<ApiPlatformUser>>("/api/v1/users");
    setUsers(listItems(result));
  }, []);
  useEffect(() => {
    void load().catch((error) =>
      toast(error instanceof Error ? error.message : "Failed to load users"),
    );
  }, [load, toast]);
  useEffect(() => { if (canManageTenants) void apiGet<ApiList<ApiTenant>>('/api/v1/tenants').then(result => setTenants(listItems(result))).catch(error => toast(error instanceof Error ? error.message : 'Failed to load tenants')); }, [canManageTenants, toast]);
  const openCreate = () => {
    setCreating(true);
    setEditing(null);
    setEmail("");
    setDisplayName("");
    setPassword("");
    setConfirmPassword("");
    setRole("operator");
    setStatus("active");
    setTenantId(canManageTenants ? '' : currentUser.tenantId);
  };
  const openEdit = (user: ApiPlatformUser) => {
    setCreating(false);
    setEditing(user);
    setEmail(user.email);
    setDisplayName(user.displayName || "");
    setPassword("");
    setRole(user.role);
    setStatus(user.status);
    setTenantId(user.tenantId);
  };
  const closeEditor = () => {
    setCreating(false);
    setEditing(null);
  };
  const save = async () => {
    setUserActionBusy(true);
    try {
      if (creating) await apiPost("/api/v1/users", { tenantId, email, displayName, password, role, status });
      else if (editing) await apiPatch(`/api/v1/users/${editing.id}`, { tenantId, email, displayName, role, status });
      closeEditor();
      await load();
      toast("User saved");
    } finally {
      setUserActionBusy(false);
    }
  };
  const resetPassword = async () => {
    if (!passwordTarget || replacementPassword !== replacementPasswordConfirm)
      return;
    setUserActionBusy(true);
    try {
      await apiPost(`/api/v1/users/${passwordTarget.id}/password`, { password: replacementPassword });
      setPasswordTarget(null);
      setReplacementPassword("");
      setReplacementPasswordConfirm("");
      toast("Password updated");
    } finally {
      setUserActionBusy(false);
    }
  };
  const deleteUser = async () => {
    if (!deleteTarget) return;
    setUserActionBusy(true);
    try {
      await apiDelete(`/api/v1/users/${deleteTarget.id}`);
      setDeleteTarget(null);
      setSelectedUserIds((current) => current.filter((id) => id !== deleteTarget.id));
      await load();
      toast("User deleted");
    } finally {
      setUserActionBusy(false);
    }
  };
  const filteredUsers = users.filter((user) =>
    [
      user.email,
      user.displayName,
      user.role,
      user.status,
      user.authProvider,
      user.tenantName,
    ].some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(query.trim().toLowerCase()),
    ),
  );
  const selectedUsers = users.filter((user) =>
    selectedUserIds.includes(user.id),
  );
  const singleSelectedUser =
    selectedUsers.length === 1 ? selectedUsers[0] : null;
  const toggleSelectedUser = (id: string) =>
    setSelectedUserIds((current) =>
      current.includes(id)
        ? current.filter((item) => item !== id)
        : [...current, id],
    );
  const userColumns: HyperTableColumn<ApiPlatformUser>[] = [
    {
      id: "select",
      header: "",
      size: 46,
      minSize: 46,
      maxSize: 52,
      enableSorting: false,
      enableResizing: false,
      cell: (info) => (
        <input
          type="checkbox"
          checked={selectedUserIds.includes(info.row.original.id)}
          onClick={(event) => event.stopPropagation()}
          onChange={() => toggleSelectedUser(info.row.original.id)}
          aria-label={`Select ${info.row.original.email}`}
        />
      ),
      meta: { align: "center" },
    },
    {
      id: "user",
      header: "User",
      accessorFn: (user) => user.displayName || user.email,
      size: 320,
      minSize: 220,
      cell: (info) => {
        const user = info.row.original;
        return (
          <div>
            <p className="text-xs font-black text-slate-900">
              {user.displayName || user.email}
            </p>
            <p className="mt-0.5 text-[11px] text-slate-400">
              {user.email}
              {user.email === "admin" ? " · Built-in account" : ""}
            </p>
          </div>
        );
      },
      meta: { title: (user) => user.displayName || user.email },
    },
    {
      id: "tenant",
      header: "Tenant",
      accessorFn: user => user.tenantName || tenants.find(item => item.id === user.tenantId)?.name || user.tenantId,
      size: 190,
      minSize: 150,
      cell: info => <span className="text-xs font-semibold text-slate-500">{info.row.original.tenantName || tenants.find(item => item.id === info.row.original.tenantId)?.name || 'Unknown tenant'}</span>,
      meta: { title: user => user.tenantName || tenants.find(item => item.id === user.tenantId)?.name || user.tenantId },
    },
    {
      id: "role",
      header: "Role",
      accessorFn: (user) => user.role,
      size: 150,
      minSize: 120,
      cell: (info) => (
        <span className="text-xs font-semibold capitalize text-slate-600">
          {info.row.original.systemAdmin ? 'System administrator' : info.row.original.role === 'admin' ? 'Administrator' : 'Operator'}
        </span>
      ),
      meta: { title: (user) => user.role },
    },
    {
      id: "status",
      header: "Status",
      accessorFn: (user) => user.status,
      size: 140,
      minSize: 110,
      cell: (info) => (
        <span
          className={`inline-flex rounded-full border px-2 py-1 text-[10px] font-bold capitalize ${info.row.original.status === "active" ? "border-emerald-100 bg-emerald-50 text-emerald-700" : "border-slate-200 bg-slate-50 text-slate-500"}`}
        >
          {info.row.original.status}
        </span>
      ),
      meta: { title: (user) => user.status },
    },
    {
      id: "provider",
      header: "Sign-in",
      accessorFn: (user) => user.authProvider || "password",
      size: 130,
      minSize: 110,
      cell: (info) => (
        <span className="text-xs font-semibold capitalize text-slate-500">
          {info.row.original.authProvider || "password"}
        </span>
      ),
      meta: { title: (user) => user.authProvider || "password" },
    },
  ];
  return (
    <motion.div
      key="users"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><User size={18} /></div>
            <div>
              <h3 className="text-sm font-black tracking-tight text-slate-900">User Management</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Create and maintain platform users.</p>
            </div>
          </div>
          <div />
        </div>
      </div>
      <div className="hbdr-dr-table-card hbdr-user-management-table">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group">
              <button
                type="button"
                onClick={openCreate}
                className="hbdr-dr-action-primary"
              >
                New
              </button>
              <div className="relative">
                <button
                  type="button"
                  disabled={!singleSelectedUser}
                  onClick={() => setUserBulkMenuOpen((open) => !open)}
                  className="hbdr-dr-more"
                >
                  More{" "}
                  <ChevronDown
                    size={15}
                    className={
                      userBulkMenuOpen
                        ? "rotate-180 transition-transform"
                        : "transition-transform"
                    }
                  />
                </button>
                <AnimatePresence>
                  {userBulkMenuOpen && singleSelectedUser && (
                    <>
                      <div
                        className="fixed inset-0 z-30"
                        onClick={() => setUserBulkMenuOpen(false)}
                      />
                      <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.96 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.96 }}
                        className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5"
                      >
                        <button
                          onClick={() => {
                            openEdit(singleSelectedUser);
                            setUserBulkMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"
                        >
                          <Edit2 size={14} />
                          Edit
                        </button>
                        <button
                          onClick={() => {
                            setPasswordTarget(singleSelectedUser);
                            setReplacementPassword("");
                            setReplacementPasswordConfirm("");
                            setUserBulkMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"
                        >
                          <KeyRound size={14} />
                          Reset Password
                        </button>
                        <button
                          disabled={singleSelectedUser.email === "admin"}
                          onClick={() => {
                            setDeleteTarget(singleSelectedUser);
                            setUserBulkMenuOpen(false);
                          }}
                          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-300"
                        >
                          <Trash2 size={14} />
                          Delete
                        </button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <div className="hbdr-dr-query-group hbdr-user-query-group">
              <label className="hbdr-dr-search">
                <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Enter search text" />
              </label>
            </div>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={userColumns}
          data={filteredUsers}
          getRowId={(user) => user.id}
          onRowClick={(user) => toggleSelectedUser(user.id)}
          getRowClassName={(user) =>
            selectedUserIds.includes(user.id) ? "hbdr-dr-row-selected" : ""
          }
          selectedCount={selectedUserIds.length}
          emptyMessage={
            query
              ? "No users match the current search."
              : "No users have been created."
          }
        />
      </div>
      <AnimatePresence>
        {(creating || editing) && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={closeEditor} />
            <motion.aside initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: "easeOut" }} className="hbdr-filter-drawer hbdr-user-management-drawer" role="dialog" aria-modal="true" aria-label={creating ? "New User" : "Edit User"}>
              <div className="hbdr-filter-drawer-head"><div><strong>{creating ? "New User" : "Edit User"}</strong><span>{creating ? "Create a platform account and assign its access role." : "Update account information, role, and access status."}</span></div><button type="button" onClick={closeEditor} aria-label="Close user drawer"><X size={18} /></button></div>
              <div className="hbdr-filter-drawer-body"><div className="space-y-4">
              <EditField label={editing?.email === "admin" ? "Account" : "Email Address"} value={email} onChange={setEmail} disabled={editing?.email === "admin"} />
              <EditField
                label="Display Name"
                value={displayName}
                onChange={setDisplayName}
              />
              <label className="block text-xs font-semibold tracking-normal text-slate-600">Tenant{canManageTenants && !editing?.systemAdmin ? <select value={tenantId} onChange={event=>setTenantId(event.target.value)} className="mt-1 h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm font-medium text-slate-800 outline-none transition-all hover:border-slate-300 focus:border-blue-500 focus:shadow-[0_0_0_4px_rgba(59,130,246,0.12)]"><option value="">Select a tenant</option>{tenants.filter(item=>item.status==='active'||item.id===tenantId).map(item=><option key={item.id} value={item.id}>{item.name}</option>)}</select> : <input value={editing?.tenantName || currentUser.tenantName || tenants.find(item=>item.id===tenantId)?.name || ''} disabled className="mt-1 h-10 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 text-sm font-medium text-slate-500" />}</label>
              {creating && (
                <>
                  <EditField
                    label="Password"
                    type="password"
                    value={password}
                    onChange={setPassword}
                  />
                  <EditField
                    label="Confirm Password"
                    type="password"
                    value={confirmPassword}
                    onChange={setConfirmPassword}
                  />
                  <PasswordValidation
                    password={password}
                    confirmation={confirmPassword}
                  />
                </>
              )}
              <label className="block text-xs font-semibold tracking-normal text-slate-600">
                Role
                <select
                  value={role}
                  disabled={editing?.email === "admin"}
                  onChange={(e) => setRole(e.target.value)}
                  className="mt-1 h-10 w-full rounded-lg border border-slate-200 px-3"
                >
                  <option value="operator">Operator</option>
                  <option value="admin">Administrator</option>
                </select>
              </label>
              {editing && (
                <label className="block text-xs font-semibold tracking-normal text-slate-600">
                  Status
                  <select
                    value={status}
                    disabled={editing.email === "admin"}
                    onChange={(e) => setStatus(e.target.value)}
                    className="mt-1 h-10 w-full rounded-lg border border-slate-200 px-3"
                  >
                    <option value="active">Active</option>
                    <option value="disabled">Disabled</option>
                  </select>
                </label>
              )}
              <div className="hbdr-filter-drawer-actions mt-6">
                <button
                  disabled={
                    userActionBusy ||
                    (editing?.email !== "admin" && !ACCOUNT_EMAIL_PATTERN.test(email.trim())) ||
                    (canManageTenants && editing?.email !== "admin" && !tenantId) ||
                    (creating &&
                      (password.length < 8 || password !== confirmPassword || !tenantId))
                  }
                  className="hbdr-dr-action-primary"
                  onClick={() =>
                    void save().catch((error) =>
                      toast(
                        error instanceof Error
                          ? error.message
                          : "Failed to save user",
                      ),
                    )
                  }
                >
                  {userActionBusy ? "Saving..." : "Save"}
                </button>
                <button disabled={userActionBusy} onClick={closeEditor}>Cancel</button>
              </div>
            </div></div></motion.aside>
          </>
        )}
        {passwordTarget && (
          <ModalFrame
            title="Reset Password"
            onClose={() => setPasswordTarget(null)}
          >
            <div className="space-y-4">
              <p className="text-sm text-slate-500">
                Set a new password for{" "}
                <strong className="text-slate-800">
                  {passwordTarget.email}
                </strong>
                .
              </p>
              <EditField
                label="New Password"
                type="password"
                value={replacementPassword}
                onChange={setReplacementPassword}
              />
              <EditField
                label="Confirm New Password"
                type="password"
                value={replacementPasswordConfirm}
                onChange={setReplacementPasswordConfirm}
              />
              <PasswordValidation
                password={replacementPassword}
                confirmation={replacementPasswordConfirm}
              />
              <div className="flex justify-end gap-2">
                <button onClick={() => setPasswordTarget(null)}>Cancel</button>
                <button
                  disabled={
                    userActionBusy ||
                    replacementPassword.length < 8 ||
                    replacementPassword !== replacementPasswordConfirm
                  }
                  className="hbdr-dr-action-primary"
                  onClick={() =>
                    void resetPassword().catch((error) =>
                      toast(
                        error instanceof Error
                          ? error.message
                          : "Password update failed",
                      ),
                    )
                  }
                >
                  {userActionBusy ? "Resetting..." : "Reset Password"}
                </button>
              </div>
            </div>
          </ModalFrame>
        )}
        {deleteTarget && (
          <ModalFrame title="Delete User" onClose={() => setDeleteTarget(null)}>
            <div className="space-y-5">
              <div className="rounded-xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Delete <strong>{deleteTarget.email}</strong>? This user will
                immediately lose platform access.
              </div>
              <div className="flex justify-end gap-2">
                <button onClick={() => setDeleteTarget(null)}>Cancel</button>
                <button
                  disabled={userActionBusy}
                  className="rounded-lg bg-rose-600 px-4 py-2 text-xs font-bold text-white"
                  onClick={() =>
                    void deleteUser().catch((error) =>
                      toast(
                        error instanceof Error
                          ? error.message
                          : "Delete failed",
                      ),
                    )
                  }
                >
                  {userActionBusy ? "Deleting..." : "Delete User"}
                </button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
