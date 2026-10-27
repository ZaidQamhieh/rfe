import { createContext, useContext } from 'react';

export interface AuthState {
  authenticated: boolean;
  ready: boolean;
  agentName: string;
  login: (username: string, password: string) => Promise<void>;
  pair: (pairingCode: string) => Promise<void>;
  register: (pairingCode: string, username: string, password: string) => Promise<void>;
  logout: () => void;
}

export const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider');
  return ctx;
}
