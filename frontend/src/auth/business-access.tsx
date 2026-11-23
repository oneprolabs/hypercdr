import { createContext, useContext, type ReactNode } from 'react';
import type { ApiLoginResponse } from './types';

type User = ApiLoginResponse['user'];
const BusinessAccessContext = createContext<User | undefined>(undefined);

export function userHasBusinessPermission(user: User | null | undefined, permission: string): boolean {
  if (!user) return false;
  if (user.systemAdmin || user.role === 'admin') return true;
  if (user.role !== 'operator') return false;
  if (!user.roleId || user.roleId === 'operator') return true;
  return user.permissions?.includes(permission) === true;
}

export function BusinessAccessProvider({ user, children }: { user?: User; children: ReactNode }) {
  return <BusinessAccessContext.Provider value={user}>{children}</BusinessAccessContext.Provider>;
}

export function useBusinessPermission(permission: string): boolean {
  const user = useContext(BusinessAccessContext);
  return user === undefined || userHasBusinessPermission(user, permission);
}
