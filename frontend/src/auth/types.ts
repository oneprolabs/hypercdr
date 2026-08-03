export type ApiLoginResponse = {
  user: {
    id: string;
    email: string;
    displayName?: string;
    role: string;
    status: string;
    authProvider?: string;
    timeZone?: string;
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
