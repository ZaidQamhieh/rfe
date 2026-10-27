import { useState, useCallback, useEffect, type ReactNode } from 'react';
import { api, clearLegacyBrowserToken } from './api';
import { AuthContext } from './auth-context';

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authenticated, setAuthenticated] = useState(false);
  const [ready, setReady] = useState(false);
  const [agentName, setAgentName] = useState('');

  useEffect(() => {
    let active = true;
    clearLegacyBrowserToken();
    api.status()
      .then(() => { if (active) setAuthenticated(true); })
      .catch(() => { if (active) setAuthenticated(false); })
      .finally(() => { if (active) setReady(true); });
    return () => { active = false; };
  }, []);

  const settle = useCallback((res: { agentName: string }) => {
    setAgentName(res.agentName);
    setAuthenticated(true);
    setReady(true);
  }, []);

  const login = useCallback(
    async (username: string, password: string) => settle(await api.login(username, password)),
    [settle],
  );
  const pair = useCallback(
    async (pairingCode: string) => settle(await api.pair(pairingCode)),
    [settle],
  );
  const register = useCallback(
    async (pairingCode: string, username: string, password: string) =>
      settle(await api.register(pairingCode, username, password)),
    [settle],
  );
  const logout = useCallback(() => {
    api.logoutSession().catch(() => {});
    setAuthenticated(false);
  }, []);

  return (
    <AuthContext.Provider value={{ authenticated, ready, agentName, login, pair, register, logout }}>
      {children}
    </AuthContext.Provider>
  );
}
