import { useState } from 'react';
import { CheckCircle2, Edit2, KeyRound, Lock, User } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiPatch, apiPost } from '../../api/client';
import type { ApiLoginResponse, AuthSession } from '../../auth/types';
import { EditField } from '../../components/edit-field';
import { ModalFrame } from '../../components/modal-frame';
import { PasswordValidation } from '../../components/password-validation';
import { SearchBar } from '../../components/search-bar';

type ApiPlatformUser = ApiLoginResponse['user'];

export default function ProfilePage({
  session,
  setSession,
  toast,
}: {
  session: AuthSession;
  setSession: (session: AuthSession) => void;
  toast: (message: string) => void;
}) {
  const [displayName, setDisplayName] = useState(
    session.user.displayName || "",
  );
  const [email, setEmail] = useState(session.user.email);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmNewPassword, setConfirmNewPassword] = useState("");
  const [profileEditorOpen, setProfileEditorOpen] = useState(false);
  const [passwordEditorOpen, setPasswordEditorOpen] = useState(false);
  const save = async () => {
    const user = await apiPatch<ApiPlatformUser>("/api/v1/auth/me", {
      email,
      displayName,
    });
    setSession({ ...session, user });
    setProfileEditorOpen(false);
    toast("Basic information updated");
  };
  const change = async () => {
    if (newPassword !== confirmNewPassword) return;
    await apiPost("/api/v1/auth/change-password", {
      currentPassword,
      newPassword,
    });
    setCurrentPassword("");
    setNewPassword("");
    setConfirmNewPassword("");
    setPasswordEditorOpen(false);
    toast("Password updated");
  };
  const initial = (session.user.displayName || session.user.email || "U")
    .trim()
    .charAt(0)
    .toUpperCase();
  const passwordAccount =
    (session.user.authProvider || "password") === "password";
  return (
    <motion.div
      key="profile"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <SearchBar
        title="Basic Information"
        desc="Account profile and security settings."
      />
      <section className="hbdr-section-card overflow-hidden">
        <div className="flex items-center gap-2 border-b border-slate-100 px-5 py-4">
          <User size={18} className="text-blue-600" />
          <h3 className="text-sm font-black text-slate-900">
            Account Information
          </h3>
        </div>
        <div className="flex flex-col gap-6 p-6 md:flex-row md:items-center">
          <div className="flex h-24 w-24 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-blue-500 to-indigo-600 text-3xl font-black text-white shadow-lg shadow-blue-100">
            {initial}
          </div>
          <div className="grid min-w-0 flex-1 gap-x-12 gap-y-5 sm:grid-cols-2">
            <div>
              <span className="text-xs font-semibold text-slate-400">
                Display Name
              </span>
              <p className="mt-1 text-sm font-bold text-slate-800">
                {session.user.displayName || "Not set"}
              </p>
            </div>
            <div>
              <span className="text-xs font-semibold text-slate-400">
                Email Address
              </span>
              <p className="mt-1 text-sm font-bold text-slate-800">
                {session.user.email}
              </p>
            </div>
            <div>
              <span className="text-xs font-semibold text-slate-400">
                User ID
              </span>
              <p className="mt-1 break-all font-mono text-xs font-semibold text-slate-600">
                {session.user.id}
              </p>
            </div>
            <div>
              <span className="text-xs font-semibold text-slate-400">Role</span>
              <p className="mt-1 text-sm font-bold capitalize text-slate-800">
                {session.user.role}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => {
              setDisplayName(session.user.displayName || "");
              setEmail(session.user.email);
              setProfileEditorOpen(true);
            }}
            className="hbdr-dr-action-primary shrink-0"
          >
            <Edit2 size={14} />
            Edit
          </button>
        </div>
      </section>
      <section className="hbdr-section-card overflow-hidden">
        <div className="flex items-center gap-2 border-b border-slate-100 px-5 py-4">
          <Lock size={18} className="text-emerald-600" />
          <h3 className="text-sm font-black text-slate-900">
            Security Settings
          </h3>
        </div>
        <div className="flex flex-col gap-5 p-6 md:flex-row md:items-end">
          <div className="flex-1">
            <p className="text-sm font-bold text-slate-800">Login Password</p>
            <p className="mt-2 text-xs leading-5 text-slate-500">
              Use a strong, unique password and update it regularly to keep your
              account secure.
            </p>
            <div className="mt-5 flex items-center gap-2 text-xs font-bold text-emerald-600">
              <CheckCircle2 size={16} />
              {passwordAccount ? "Configured" : "Managed by Google"}
            </div>
          </div>
          {passwordAccount && (
            <button
              type="button"
              onClick={() => {
                setCurrentPassword("");
                setNewPassword("");
                setConfirmNewPassword("");
                setPasswordEditorOpen(true);
              }}
              className="hbdr-dr-action-primary shrink-0"
            >
              <KeyRound size={14} />
              Change Password
            </button>
          )}
        </div>
      </section>
      <AnimatePresence>
        {profileEditorOpen && (
          <ModalFrame
            title="Edit Basic Information"
            onClose={() => setProfileEditorOpen(false)}
          >
            <div className="space-y-4">
              <EditField
                label="Email Address"
                value={email}
                onChange={setEmail}
              />
              <EditField
                label="Display Name"
                value={displayName}
                onChange={setDisplayName}
              />
              <div className="flex justify-end gap-2">
                <button onClick={() => setProfileEditorOpen(false)}>
                  Cancel
                </button>
                <button
                  disabled={!email.trim()}
                  className="hbdr-dr-action-primary"
                  onClick={() =>
                    void save().catch((e) =>
                      toast(e instanceof Error ? e.message : "Update failed"),
                    )
                  }
                >
                  Save
                </button>
              </div>
            </div>
          </ModalFrame>
        )}
        {passwordEditorOpen && (
          <ModalFrame
            title="Change Password"
            onClose={() => setPasswordEditorOpen(false)}
          >
            <div className="space-y-4">
              <EditField
                label="Current Password"
                type="password"
                value={currentPassword}
                onChange={setCurrentPassword}
              />
              <EditField
                label="New Password"
                type="password"
                value={newPassword}
                onChange={setNewPassword}
              />
              <EditField
                label="Confirm New Password"
                type="password"
                value={confirmNewPassword}
                onChange={setConfirmNewPassword}
              />
              <PasswordValidation
                password={newPassword}
                confirmation={confirmNewPassword}
              />
              <div className="flex justify-end gap-2">
                <button onClick={() => setPasswordEditorOpen(false)}>
                  Cancel
                </button>
                <button
                  disabled={
                    !currentPassword ||
                    newPassword.length < 8 ||
                    newPassword !== confirmNewPassword
                  }
                  className="hbdr-dr-action-primary"
                  onClick={() =>
                    void change().catch((e) =>
                      toast(
                        e instanceof Error
                          ? e.message
                          : "Password update failed",
                      ),
                    )
                  }
                >
                  Change Password
                </button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
