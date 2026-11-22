import { Download, X } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import { humanizeError } from '../../core/humanizeError';
import { isUserCancel } from '../../core/userCancel';
import { Pressable, Text, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { isUpdateAvailable, type AppRelease } from './updateLogic';
import { dismissedCode, dismissUpdate, downloadUpdate, installUpdate } from './updateService';
import { useSessionUpdate } from './useUpdate';

/** A dismissible strip on the Devices screen when a newer build exists; a dismissal holds until an even newer build. */
export function UpdateBanner() {
  const c = useScheme();
  const toast = useToast();
  const release = useSessionUpdate();
  const [dismissed, setDismissed] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);

  /** Downloads the build (checked against its hash) and hands it to the system installer; a cancelled download is not an error. */
  async function update(r: AppRelease) {
    if (busy) return;
    setBusy(true);
    try {
      toast.info(t('downloadingUpdate', { percent: 0 }));
      await downloadUpdate(r, (percent) => toast.info(t('downloadingUpdate', { percent })));
      toast.info(t('openingInstallerConfirm'));
      const outcome = await installUpdate(r);
      if (outcome === 'needsPermission') toast.info(t('installPermissionNeeded'));
      else if (outcome === 'failed') toast.error(t('couldNotOpenInstaller'));
    } catch (e) {
      if (!isUserCancel(e)) toast.error(t('updateFailed', { error: humanizeError(e) }));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    let live = true;
    void dismissedCode().then((n) => live && setDismissed(n));
    return () => {
      live = false;
    };
  }, []);
  if (!release || dismissed === null || !isUpdateAvailable(dismissed, release)) return null;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', marginHorizontal: Spacing.md, marginBottom: Spacing.sm, backgroundColor: c.primaryContainer, borderRadius: Radii.sm, paddingLeft: 14 }}>
      <Pressable onPress={() => void update(release)} accessibilityLabel={t('updateAvailable', { version: release.versionName })} style={{ flex: 1, flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: Spacing.md2 }}>
        <Download size={18} color={c.onPrimaryContainer} />
        <Text style={{ flex: 1, fontSize: 13.5, fontFamily: FontFamily.medium, color: c.onPrimaryContainer }}>{t('updateAvailable', { version: release.versionName })}</Text>
      </Pressable>
      <Pressable
        onPress={() => {
          setDismissed(release.versionCode);
          void dismissUpdate(release.versionCode);
        }}
        accessibilityLabel={t('dismissButton')}
        style={{ padding: Spacing.md2 }}
      >
        <X size={16} color={c.onPrimaryContainer} />
      </Pressable>
    </View>
  );
}
