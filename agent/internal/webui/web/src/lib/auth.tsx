import type { ReactNode } from 'react';
import { AuthContext } from './auth-context';
import { useAuthProvider } from './useAuthProvider';

export function AuthProvider({ children }: { children: ReactNode }) {
  const value = useAuthProvider();
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
