import { Lock } from 'lucide-react-native';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { AppState, View } from 'react-native';

import { authenticate } from '../../core/security/deviceAuth';
import { Button, Text } from '../../design/components';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { useLock } from '../../state/lock';
import { useSettings } from '../../state/settings';
import { shouldRelockOnResume, unlocksApp } from './lockLogic';

/**
 * Covers the app with a lock screen while app lock is on. Locks on cold start and after the app has been in the
 * background longer than the grace window; unlocks through the system biometric/PIN sheet.
 */
export function LockGate({ children }: { children: ReactNode }) {
  const c = useScheme();
  const loaded = useSettings((s) => s.loaded);
  const enabled = useSettings((s) => s.state.app.appLockEnabled);
  const unlocked = useLock((s) => s.unlocked);
  const setUnlocked = useLock((s) => s.setUnlocked);
  const prompting = useRef(false);
  const backgroundedAt = useRef<number | null>(null);
  const [noScreenLock, setNoScreenLock] = useState(false);

  const unlock = useCallback(async () => {
    if (prompting.current) return;
    prompting.current = true;
    try {
      const outcome = await authenticate(t('unlockReason'));
      setNoScreenLock(outcome === 'unavailable');
      if (unlocksApp(outcome)) setUnlocked(true);
    } finally {
      prompting.current = false;
    }
  }, [setUnlocked]);

  // An enabled lock asks at start and again after every relock; a disabled one never covers the app.
  useEffect(() => {
    if (loaded && enabled && !unlocked) void unlock();
  }, [loaded, enabled, unlocked, unlock]);

  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'background') backgroundedAt.current = Date.now();
      else if (s === 'active') {
        if (shouldRelockOnResume(enabled, backgroundedAt.current, Date.now())) setUnlocked(false);
        backgroundedAt.current = null;
      }
    });
    return () => sub.remove();
  }, [enabled, setUnlocked]);

  const covered = !loaded || (enabled && !unlocked);
  if (!covered) return <>{children}</>;
  return (
    <View style={{ flex: 1, backgroundColor: c.surface, alignItems: 'center', justifyContent: 'center', padding: Spacing.xl, gap: Spacing.md }}>
      {loaded ? (
        <>
          <Lock size={40} color={c.primary} />
          <Text variant="titleLarge">{t('appLockedTitle')}</Text>
          {noScreenLock ? <Text muted>{t('appLockNeedsScreenLock')}</Text> : null}
          <Button label={t('unlockButton')} onPress={() => void unlock()} />
        </>
      ) : null}
    </View>
  );
}
