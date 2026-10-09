import React, { useState } from 'react';
import { AlertCircle, Check, CheckCircle2, KeyRound, Mail, X } from 'lucide-react';
import { EditField } from '../components/edit-field';
import { apiPost } from '../api/client';
import type { AuthSession, ApiLoginResponse } from './types';
type AuthFlow = 'login' | 'forgot' | 'reset';
type ApiPlatformUser = ApiLoginResponse['user'];

function PasswordValidation({
  password,
  confirmation,
}: {
  password: string;
  confirmation: string;
}) {
  const longEnough = password.length >= 8;
  const matches = confirmation.length > 0 && password === confirmation;
  const variety = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((pattern) =>
    pattern.test(password),
  ).length;
  const strength =
    password.length === 0
      ? 0
      : password.length >= 12 && variety >= 3
        ? 3
        : longEnough && variety >= 2
          ? 2
          : 1;
  const strengthLabel = ["Not entered", "Weak", "Good", "Strong"][strength];
  return (
    <div className="rounded-lg border border-slate-100 bg-slate-50 px-3 py-3">
      <div className="mb-2 flex items-center justify-between text-[11px] font-semibold">
        <span className="text-slate-500">Password strength</span>
        <span
          className={
            strength >= 3
              ? "text-emerald-600"
              : strength === 2
                ? "text-blue-600"
                : strength === 1
                  ? "text-amber-600"
                  : "text-slate-400"
          }
        >
          {strengthLabel}
        </span>
      </div>
      <div className="mb-3 grid grid-cols-3 gap-1">
        {[1, 2, 3].map((level) => (
          <span
            key={level}
            className={`h-1 rounded-full ${strength >= level ? (strength >= 3 ? "bg-emerald-500" : strength === 2 ? "bg-blue-500" : "bg-amber-400") : "bg-slate-200"}`}
          />
        ))}
      </div>
      <div className="grid gap-2 text-[11px] font-semibold sm:grid-cols-2">
        <span
          className={
            longEnough
              ? "flex items-center gap-1.5 text-emerald-600"
              : "flex items-center gap-1.5 text-slate-400"
          }
        >
          <CheckCircle2 size={13} />
          8–128 characters
        </span>
        <span
          className={
            matches
              ? "flex items-center gap-1.5 text-emerald-600"
              : confirmation
                ? "flex items-center gap-1.5 text-rose-600"
                : "flex items-center gap-1.5 text-slate-400"
          }
        >
          {matches ? <CheckCircle2 size={13} /> : <AlertCircle size={13} />}
          Passwords match
        </span>
      </div>
      <p className="mt-2 text-[10px] leading-4 text-slate-400">
        For a stronger password, combine uppercase and lowercase letters,
        numbers, and symbols.
      </p>
    </div>
  );
}

export function RequiredPasswordChange({
  session,
  onChanged,
  onSignOut,
}: {
  session: AuthSession;
  onChanged: () => void;
  onSignOut: () => void;
}) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [recoveryEmail, setRecoveryEmail] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const recoveryEmailRequired = session.user.systemAdmin;
  const recoveryEmailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(recoveryEmail.trim());
  const valid = currentPassword.length > 0 && (!recoveryEmailRequired || recoveryEmailValid) && newPassword.length >= 8 && newPassword !== currentPassword && newPassword === confirmPassword;
  const submit = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      await apiPost<ApiPlatformUser>('/api/v1/auth/change-password', { currentPassword, newPassword, recoveryEmail: recoveryEmail.trim() });
      onChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Password update failed');
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="min-h-screen bg-slate-50 px-5 py-10">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-lg items-center justify-center">
        <section className="w-full overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-xl shadow-slate-200/60">
          <div className="border-b border-slate-100 px-7 py-6">
            <div className="mb-5 flex h-11 w-11 items-center justify-center rounded-xl bg-blue-50 text-blue-600"><KeyRound size={20} /></div>
            <h1 className="text-xl font-black tracking-tight text-slate-900">Change your temporary password</h1>
            <p className="mt-2 text-sm leading-6 text-slate-500">For account security, set a new password before entering HyperCDR.</p>
            <p className="mt-3 text-xs font-semibold text-slate-400">Signed in as <span className="text-slate-600">{session.user.email}</span></p>
          </div>
          <div className="space-y-4 px-7 py-6">
            <EditField label="Current Password" type="password" value={currentPassword} onChange={setCurrentPassword} />
            {recoveryEmailRequired && <div>
              <EditField label="Recovery Email" type="email" value={recoveryEmail} onChange={setRecoveryEmail} />
              <p className="mt-1.5 text-xs leading-5 text-slate-500">Password reset instructions for the built-in administrator will be sent to this address.</p>
              {recoveryEmail && !recoveryEmailValid && <p className="mt-1 text-xs font-semibold text-rose-600">Enter a valid email address.</p>}
            </div>}
            <EditField label="New Password" type="password" value={newPassword} onChange={setNewPassword} />
            <EditField label="Confirm New Password" type="password" value={confirmPassword} onChange={setConfirmPassword} />
            <PasswordValidation password={newPassword} confirmation={confirmPassword} />
            {newPassword && currentPassword === newPassword && <p className="text-xs font-semibold text-rose-600">New password must be different from the temporary password.</p>}
            {error && <div className="rounded-lg border border-rose-100 bg-rose-50 px-3 py-2.5 text-xs font-semibold text-rose-700">{error}</div>}
          </div>
          <div className="flex items-center justify-between border-t border-slate-100 bg-slate-50/70 px-7 py-5">
            <button type="button" onClick={onSignOut} disabled={busy} className="text-xs font-bold text-slate-500 hover:text-slate-700">Sign out</button>
            <button type="button" onClick={() => void submit()} disabled={!valid || busy} className="hbdr-dr-action-primary">{busy ? 'Updating...' : 'Change Password'}</button>
          </div>
        </section>
      </div>
    </div>
  );
}

export function PasswordChangeSuccess({ onContinue }: { onContinue: () => void }) {
  return (
    <div className="min-h-screen bg-slate-50 px-5 py-10">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-lg items-center justify-center">
        <section className="w-full overflow-hidden rounded-2xl border border-slate-200 bg-white text-center shadow-xl shadow-slate-200/60">
          <div className="px-8 py-9">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-emerald-50 text-emerald-600"><CheckCircle2 size={28} /></div>
            <h1 className="mt-5 text-xl font-black tracking-tight text-slate-900">Password changed successfully</h1>
            <p className="mx-auto mt-3 max-w-sm text-sm leading-6 text-slate-500">Your previous sessions have been signed out. Sign in again with your new password to continue.</p>
          </div>
          <div className="border-t border-slate-100 bg-slate-50/70 px-8 py-5">
            <button type="button" autoFocus onClick={onContinue} className="hbdr-dr-action-primary mx-auto">Go to Sign In</button>
          </div>
        </section>
      </div>
    </div>
  );
}

export function PasswordRecoveryPage({
  flow,
  email,
  setEmail,
  password,
  setPassword,
  confirmation,
  setConfirmation,
  resetToken,
  error,
  message,
  busy,
  completed,
  onForgot,
  onReset,
  onBack,
}: {
  flow: Exclude<AuthFlow, 'login'>;
  email: string;
  setEmail: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  confirmation: string;
  setConfirmation: (value: string) => void;
  resetToken: string;
  error: string;
  message: string;
  busy: boolean;
  completed: boolean;
  onForgot: () => void;
  onReset: () => void;
  onBack: () => void;
}) {
  const instructionsSent = flow === 'forgot' && Boolean(message);
  const passwordValid = password.length >= 8 && password.length <= 128 && password === confirmation;

  if (completed) {
    return (
      <div className="min-h-screen bg-slate-50 px-5 py-10">
        <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-lg items-center justify-center">
          <section className="w-full overflow-hidden rounded-2xl border border-slate-200 bg-white text-center shadow-xl shadow-slate-200/60">
            <div className="px-8 py-9">
              <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-emerald-50 text-emerald-600"><CheckCircle2 size={28} /></div>
              <h1 className="mt-5 text-xl font-black tracking-tight text-slate-900">Password reset successfully</h1>
              <p className="mx-auto mt-3 max-w-sm text-sm leading-6 text-slate-500">Your password has been updated and existing sessions have been signed out. Use your new password to sign in.</p>
            </div>
            <div className="border-t border-slate-100 bg-slate-50/70 px-8 py-5">
              <button type="button" autoFocus onClick={onBack} className="hbdr-dr-action-primary mx-auto">Go to Sign In</button>
            </div>
          </section>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 px-5 py-10">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-lg items-center justify-center">
        <section className="w-full overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-xl shadow-slate-200/60">
          <div className="border-b border-slate-100 px-7 py-6">
            <div className={`mb-5 flex h-11 w-11 items-center justify-center rounded-xl ${instructionsSent ? 'bg-emerald-50 text-emerald-600' : 'bg-blue-50 text-blue-600'}`}>
              {instructionsSent ? <Mail size={20} /> : <KeyRound size={20} />}
            </div>
            <h1 className="text-xl font-black tracking-tight text-slate-900">
              {instructionsSent ? 'Check your email' : flow === 'forgot' ? 'Forgot your password?' : 'Set a new password'}
            </h1>
            <p className="mt-2 text-sm leading-6 text-slate-500">
              {instructionsSent
                ? 'If the email address is registered, password reset instructions have been sent.'
                : flow === 'forgot'
                  ? 'Enter your registered email address. We will send you a secure reset link valid for 15 minutes.'
                  : 'Choose a new password for your HyperCDR account.'}
            </p>
          </div>

          {!instructionsSent && <div className="space-y-4 px-7 py-6">
            {flow === 'forgot' ? (
              <EditField label="Email Address" value={email} onChange={setEmail} placeholder="name@example.com" />
            ) : (
              <>
                {!resetToken && <div className="rounded-lg border border-rose-100 bg-rose-50 px-3 py-2.5 text-xs font-semibold text-rose-700">This reset link is incomplete or invalid. Request a new link and try again.</div>}
                <EditField label="New Password" type="password" value={password} onChange={setPassword} />
                <EditField label="Confirm New Password" type="password" value={confirmation} onChange={setConfirmation} />
                <PasswordValidation password={password} confirmation={confirmation} />
              </>
            )}
            {error && <div className="rounded-lg border border-rose-100 bg-rose-50 px-3 py-2.5 text-xs font-semibold text-rose-700">{error}</div>}
          </div>}

          <div className="flex items-center justify-between border-t border-slate-100 bg-slate-50/70 px-7 py-5">
            <button type="button" onClick={onBack} disabled={busy} className="text-xs font-bold text-slate-500 hover:text-slate-700">Back to Sign In</button>
            {!instructionsSent && <button
              type="button"
              onClick={flow === 'forgot' ? onForgot : onReset}
              disabled={busy || (flow === 'forgot' ? !email.trim() : !resetToken || !passwordValid)}
              className="hbdr-dr-action-primary"
            >
              {busy ? 'Please wait...' : flow === 'forgot' ? 'Send Reset Instructions' : 'Reset Password'}
            </button>}
          </div>
        </section>
      </div>
    </div>
  );
}

