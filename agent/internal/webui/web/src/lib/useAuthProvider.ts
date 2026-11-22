import { useState, useCallback, useEffect, useMemo } from 'react';
import { api, clearLegacyBrowserToken } from './api';
import type { AuthState } from './auth-context';
import { isSessionRefused } from './sessionStatus';

/**
 * Owns the web session state (signed in, checked, agent unreachable) and the actions that change it, and returns
 * them as one AuthState for AuthProvider to publish through context.
 */
export function useAuthProvider(): AuthState {
  const [authenticated, setAuthenticated] = useState(false);
  const [ready, setReady] = useState(false);
  const [unavailable, setUnavailable] = useState(false);

  // Only the agent's own 401/403 signs the user out. Any other failure leaves the session alone and shows a retry.
  const checkSession = useCallback(() => {
    return api.status()
      .then(() => { setAuthenticated(true); setUnavailable(false); })
      .catch((err: unknown) => {
        if (isSessionRefused(err)) {
          setAuthenticated(false);
          setUnavailable(false);
        } else {
          setUnavailable(true);
        }
      })
      .finally(() => setReady(true));
  }, []);

  useEffect(() => {
    clearLegacyBrowserToken();
    void checkSession();
  }, [checkSession]);

  const retry = useCallback(() => {
    setReady(false);
    void checkSession();
  }, [checkSession]);

  const settle = useCallback(() => {
    setAuthenticated(true);
    setUnavailable(false);
    setReady(true);
  }, []);

  const login = useCallback(
    async (username: string, password: string) => {
      await api.login(username, password);
      settle();
    },
    [settle],
  );
  const pair = useCallback(
    async (pairingCode: string) => {
      await api.pair(pairingCode);
      settle();
    },
    [settle],
  );
  const register = useCallback(
    async (pairingCode: string, username: string, password: string) => {
      await api.register(pairingCode, username, password);
      settle();
    },
    [settle],
  );
  // The session cookie lives on the agent: if the server call fails the login is still valid,
  // so stay signed in and let the caller tell the user instead of pretending they are out.
  const logout = useCallback(async () => {
    await api.logoutSession();
    setAuthenticated(false);
  }, []);

  // One object per auth change, so a parent re-render does not re-render every useAuth() consumer.
  return useMemo(
    () => ({ authenticated, ready, unavailable, retry, login, pair, register, logout }),
    [authenticated, ready, unavailable, retry, login, pair, register, logout],
  );
}
