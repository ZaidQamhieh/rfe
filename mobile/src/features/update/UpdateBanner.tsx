import { useRouter } from 'expo-router';
import { Download, X } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import { Pressable, Text } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { shouldSurfaceUpdate } from './updateLogic';
import { dismissedCode, dismissUpdate } from './updateService';
import { useSessionUpdate } from './useUpdate';

/** A dismissible strip on the Devices screen when a newer build exists; a dismissal holds until an even newer build. */
export function UpdateBanner() {
  const c = useScheme();
  const router = useRouter();
  const release = useSessionUpdate();
  const [dismissed, setDismissed] = useState<number | null>(null);
  useEffect(() => {
    let live = true;
    void dismissedCode().then((n) => live && setDismissed(n));
    return () => {
      live = false;
    };
  }, []);
  if (!release || dismissed === null || !shouldSurfaceUpdate(release, dismissed)) return null;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', marginHorizontal: Spacing.md, marginBottom: Spacing.sm, backgroundColor: c.primaryContainer, borderRadius: Radii.sm, paddingLeft: 14 }}>
      <Pressable onPress={() => router.push('/settings/about')} accessibilityLabel={t('updateAvailable', { version: release.versionName })} style={{ flex: 1, flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12 }}>
        <Download size={18} color={c.onPrimaryContainer} />
        <Text style={{ flex: 1, fontSize: 13.5, fontFamily: FontFamily.medium, color: c.onPrimaryContainer }}>{t('updateAvailable', { version: release.versionName })}</Text>
      </Pressable>
      <Pressable
        onPress={() => {
          setDismissed(release.versionCode);
          void dismissUpdate(release.versionCode);
        }}
        accessibilityLabel={t('dismissButton')}
        style={{ padding: 12 }}
      >
        <X size={16} color={c.onPrimaryContainer} />
      </Pressable>
    </View>
  );
}
