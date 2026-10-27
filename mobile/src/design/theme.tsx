import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { useColorScheme } from 'react-native';

import { schemeFromSeed } from './accent';
import { mix } from './color';
import { amoledScheme, darkScheme, darkRoles, lightRoles, lightScheme, type Roles, type Scheme } from './tokens';

export type ThemeMode = 'system' | 'light' | 'dark' | 'amoled';

type Ctx = { scheme: Scheme; mode: ThemeMode };
const ThemeContext = createContext<Ctx>({ scheme: lightScheme, mode: 'system' });

const AMOLED_SURFACES = {
  surface: amoledScheme.surface,
  surfaceContainerLowest: amoledScheme.surfaceContainerLowest,
  surfaceContainerLow: amoledScheme.surfaceContainerLow,
  surfaceContainer: amoledScheme.surfaceContainer,
} as const;

/** True black only applies to a dark theme: with the light theme chosen (or the system in light) the AMOLED switch does nothing. */
export function effectiveMode(mode: ThemeMode, amoled: boolean, systemDark: boolean): ThemeMode {
  if (!amoled || mode === 'light') return mode;
  return mode === 'dark' || mode === 'amoled' || systemDark ? 'amoled' : mode;
}

/** [seed] is an ARGB accent; null keeps the hand-picked default schemes. */
export function resolveScheme(mode: ThemeMode, systemDark: boolean, seed: number | null = null): Scheme {
  if (seed !== null) {
    const dark = mode === 'amoled' || mode === 'dark' || (mode === 'system' && systemDark);
    const derived = schemeFromSeed(seed, dark);
    return mode === 'amoled' ? { ...derived, ...AMOLED_SURFACES } : derived;
  }
  if (mode === 'amoled') return amoledScheme;
  if (mode === 'dark') return darkScheme;
  if (mode === 'light') return lightScheme;
  return systemDark ? darkScheme : lightScheme;
}

export function ThemeProvider({ mode = 'system', amoled = false, seed = null, children }: { mode?: ThemeMode; amoled?: boolean; seed?: number | null; children: ReactNode }) {
  const systemDark = useColorScheme() === 'dark';
  const value = useMemo(() => {
    const effective = effectiveMode(mode, amoled, systemDark);
    return { scheme: resolveScheme(effective, systemDark, seed), mode: effective };
  }, [mode, amoled, systemDark, seed]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export const useScheme = (): Scheme => useContext(ThemeContext).scheme;

/** Category colours for the active scheme. */
export const useRoles = (): Roles => (useScheme().dark ? darkRoles : lightRoles);

/** Tinted chip behind a role glyph: the role over the surface at 14% (light) or 17% (dark). */
export const roleTint = (role: string, scheme: Scheme): string => mix(role, scheme.surface, scheme.dark ? 0.17 : 0.14);
