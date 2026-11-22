import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { useColorScheme } from 'react-native';

import type { ThemeMode } from '../core/settings/settings';
import { darkScheme, darkRoles, lightRoles, lightScheme, type Roles, type Scheme } from './tokens';

type Ctx = { scheme: Scheme; mode: ThemeMode };
const ThemeContext = createContext<Ctx>({ scheme: lightScheme, mode: 'system' });

/** The scheme for a chosen mode; `system` follows the phone's dark setting. The graphite accent is fixed. */
export function resolveScheme(mode: ThemeMode, systemDark: boolean): Scheme {
  if (mode === 'dark') return darkScheme;
  if (mode === 'light') return lightScheme;
  return systemDark ? darkScheme : lightScheme;
}

/** Provides the active scheme to the tree; re-resolves when the chosen mode or the phone's dark setting changes. */
export function ThemeProvider({ mode = 'system', children }: { mode?: ThemeMode; children: ReactNode }) {
  const systemDark = useColorScheme() === 'dark';
  const value = useMemo(() => ({ scheme: resolveScheme(mode, systemDark), mode }), [mode, systemDark]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export const useScheme = (): Scheme => useContext(ThemeContext).scheme;

/** Category colours for the active scheme. */
export const useRoles = (): Roles => (useScheme().dark ? darkRoles : lightRoles);
