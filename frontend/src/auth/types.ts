export type ApiLoginResponse = {
  user: {
    id: string;
    username?: string;
    emailVerifiedAt?: string;
    email: string;
    displayName?: string;
    role: string;
    roleId?: string;
    roleName?: string;
    permissions?: string[];
    status: string;
    authProvider?: string;
    timeZone?: string;
    theme?: 'light' | 'dark';
    tenantId: string;
    tenantName: string;
    systemAdmin?: boolean;
    mustChangePassword: boolean;
  };
  session: {
    token: string;
    expiresAt: string;
  };
};

export type AuthSession = ApiLoginResponse & {
  signedInAt: string;
};
