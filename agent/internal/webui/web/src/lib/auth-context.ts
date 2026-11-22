import { createContext, useContext } from 'react';

export interface AuthState {
  authenticated: boolean;
  ready: boolean;
  /** The session check failed for a reason other than the agent refusing the session: show a retry. */
  unavailable: boolean;
  retry: () => void;
  login: (username: string, password: string) => Promise<void>;
  pair: (pairingCode: string) => Promise<void>;
  register: (pairingCode: string, username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

export const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider');
  return ctx;
}
