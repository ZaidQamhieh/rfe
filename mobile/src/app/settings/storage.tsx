import { Image } from 'expo-image';
import { Database, Fingerprint, ShieldOff, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { View } from 'react-native';

import { useGuardedAction } from '../../core/useGuardedAction';
import { authenticate, deviceAuthState } from '../../core/security/deviceAuth';
import { humanizeError } from '../../features/pairing/pairingService';
import { useActiveHost } from '../../state/activeHost';
import type { Host } from '../../core/models/host';
import { Pressable, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { lockEnableBlocker } from '../../features/security/lockLogic';
import { InfoRow, NavRow, RowBadge, SettingsPage, SettingsSection, ToggleRow } from '../../features/settings/parts';
import { hostStore, listingCache } from '../../services';
import { useLock } from '../../state/lock';
import { useSettings } from '../../state/settings';

type Trusted = { host: Host; pin: string | null };

/** App lock, the certificates this app trusts (one pin per paired computer), and cached data. */
export default function StorageSecuritySettings() {
  const c = useScheme();
  const dialogs = useDialogs();
  const toast = useToast();
  const lockOn = useSettings((s) => s.state.app.appLockEnabled);
  const setApp = useSettings((s) => s.setApp);
  const setUnlocked = useLock((s) => s.setUnlocked);
  const [trusted, setTrusted] = useState<Trusted[] | null>(null);
  const [cached, setCached] = useState<number | null>(null);

  const fetchAll = useCallback(async () => {
    const hosts = await hostStore.listHosts();
    return {
      trusted: await Promise.all(hosts.map(async (host) => ({ host, pin: await hostStore.getPin(host.id) }))),
      cached: await listingCache.count(hosts.map((h) => h.id)),
    };
  }, []);
  const apply = useCallback((r: { trusted: Trusted[]; cached: number }) => {
    setTrusted(r.trusted);
    setCached(r.cached);
  }, []);
  const reload = useCallback(() => fetchAll().then(apply), [fetchAll, apply]);
  useEffect(() => {
    let live = true;
    void fetchAll().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [fetchAll, apply]);

  const toggleLock = async (next: boolean) => {
    if (next) {
      const { hasHardware, level } = await deviceAuthState();
      const blocked = lockEnableBlocker(hasHardware, level);
      if (blocked) return void toast.error(t(blocked === 'noHardware' ? 'appLockNoHardware' : 'appLockNoScreenLock'));
      // Prove the user can pass the check before it starts guarding the app.
      if ((await authenticate(t('appLockConfirmReason'))) !== 'success') return;
      setUnlocked(true);
    }
    await setApp('appLockEnabled', next);
  };

  const forget = async ({ host }: Trusted) => {
    const label = host.label || host.address;
    const ok = await dialogs.confirm({ title: t('forgetHostTitle', { host: label }), description: t('forgetHostMessage'), confirmLabel: t('forgetButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    await hostStore.removeHost(host.id);
    // The Files tab must not keep browsing a computer that was just forgotten (F-76).
    if (useActiveHost.getState().active?.host.id === host.id) useActiveHost.getState().setActive(null);
    toast.success(t('hostForgotten', { host: label }));
    await reload();
  };

  const clearCache = async () => {
    const hosts = await hostStore.listHosts();
    await Promise.all(hosts.map((h) => listingCache.evictHost(h.id)));
    await Image.clearDiskCache();
    await Image.clearMemoryCache();
    toast.success(t('cacheCleared'));
    await reload();
  };

  // A failed action shows a message instead of an unhandled rejection, and a second tap while one runs is ignored (F-175).
  const onActionError = (e: unknown) => toast.error(humanizeError(e));
  const guardedToggleLock = useGuardedAction(toggleLock, onActionError);
  const guardedForget = useGuardedAction(forget, onActionError);
  const guardedClearCache = useGuardedAction(clearCache, onActionError);

  return (
    <SettingsPage>
      <SettingsSection title={t('appLockTitle')}>
        <ToggleRow icon={Fingerprint} tint={c.primary} title={t('appLockTitle')} subtitle={t('appLockSubtitle')} value={lockOn} onChange={(v) => void guardedToggleLock(v)} />
      </SettingsSection>
      <SettingsSection title={t('trustedCertsSection')}>
        {trusted === null ? (
          <InfoRow icon={Database} title={t('cacheCalculating')} />
        ) : trusted.length === 0 ? (
          <InfoRow icon={ShieldOff} title={t('noTrustedCerts')} />
        ) : (
          trusted.map((x) => (
            <View key={x.host.id} style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 11 }}>
              <RowBadge icon={Fingerprint} tint={c.primary} />
              <View style={{ flex: 1 }}>
                <Text style={LumenType.name}>{x.host.label || x.host.address}</Text>
                <Text muted style={LumenType.meta}>{x.pin ? x.host.address : t('probeMissingPinHint')}</Text>
              </View>
              <Pressable onPress={() => void guardedForget(x)} accessibilityLabel={`${t('forgetButton')} ${x.host.label || x.host.address}`} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
                <Trash2 size={20} color={c.error} />
              </Pressable>
            </View>
          ))
        )}
      </SettingsSection>
      <SettingsSection title={t('cacheSection')}>
        <InfoRow icon={Database} tint={c.primary} title={t('cacheListingLabel')} subtitle={cached === null ? t('cacheCalculating') : t('cacheListingsSummary', { count: cached })} />
        <NavRow icon={Trash2} tint={c.error} title={t('cacheClearAll')} onPress={() => void guardedClearCache()} />
      </SettingsSection>
    </SettingsPage>
  );
}
