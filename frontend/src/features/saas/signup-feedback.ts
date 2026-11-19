export class PublicAuthError extends Error {
  code: string;
  retryAfterSeconds: number;

  constructor(message: string, code = '', retryAfterSeconds = 0) {
    super(message);
    this.code = code;
    this.retryAfterSeconds = Math.max(0, Number(retryAfterSeconds) || 0);
  }
}

export function signupFeedback(error: unknown) {
  const code = error instanceof PublicAuthError ? error.code : '';
  return {
    message: error instanceof Error ? error.message : 'The request could not be completed. Please try again.',
    code,
    cooldown: code === 'too_many_attempts' && error instanceof PublicAuthError ? Math.ceil(error.retryAfterSeconds) : 0,
    accountCreated: code === 'account_created_login_failed',
    showAccountLinks: ['email_already_registered', 'account_already_has_organization', 'account_created_login_failed'].includes(code),
    showNewLink: code === 'link_invalid',
  };
}

export function signupCooldownSeconds(deadline: number, now = Date.now()) {
  return Math.max(0, Math.ceil((deadline - now) / 1000));
}
