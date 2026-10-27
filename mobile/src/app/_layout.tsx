import { Stack, useRouter } from 'expo-router';
import { ShareIntentProvider, useShareIntentContext } from 'expo-share-intent';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { useEffect } from 'react';

import { DialogHost, ToastProvider, useToast } from '../design/components';
import { ThemeProvider, useScheme } from '../design/theme';
import { FontFamily } from '../design/tokens';
import { t } from '../i18n';
import { ensureBackgroundTask } from '../features/background/backgroundTask';
import { watchPhotoBackup } from '../features/photoBackup/photoBackupService';
import { LockGate } from '../features/security/LockGate';
import { ensureLegacyImport } from '../services';
import { useCollections } from '../state/collections';
import { useSettings } from '../state/settings';

/** Sends a share from another app to the upload screen; text-only shares have nothing to upload. */
function ShareRouter() {
  const { hasShareIntent, shareIntent, resetShareIntent } = useShareIntentContext();
  const router = useRouter();
  const toast = useToast();
  const hasFiles = (shareIntent.files?.length ?? 0) > 0;
  useEffect(() => {
    if (!hasShareIntent) return;
    if (hasFiles) router.push('/share');
    else {
      toast.info(t('shareNothingBody'));
      resetShareIntent();
    }
  }, [hasShareIntent, hasFiles, router, toast, resetShareIntent]);
  return null;
}

function Shell() {
  const c = useScheme();
  return (
    <>
      <StatusBar style={c.dark ? 'light' : 'dark'} />
      <ShareRouter />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: c.surface },
          headerTintColor: c.onSurface,
          headerTitleStyle: { fontFamily: FontFamily.semibold, fontSize: 19 },
          headerShadowVisible: false,
          contentStyle: { backgroundColor: c.surface },
        }}
      >
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="onboarding" options={{ headerShown: false }} />
        <Stack.Screen name="workspaces" options={{ headerShown: false }} />
        <Stack.Screen name="pair/index" options={{ title: t('addComputerTitle') }} />
        <Stack.Screen name="pair/scan" options={{ headerShown: false }} />
        <Stack.Screen name="receive" options={{ headerShown: false }} />
        <Stack.Screen name="share" options={{ title: t('shareTitle') }} />
        <Stack.Screen name="pair/request" options={{ title: t('pairRequestTitle') }} />
        <Stack.Screen name="pair/login" options={{ title: t('loginTab') }} />
        <Stack.Screen name="pair/register" options={{ title: t('registerTab') }} />
        <Stack.Screen name="settings/appearance" options={{ title: t('appearanceSection') }} />
        <Stack.Screen name="settings/visibility" options={{ title: t('fileVisibilityTitle') }} />
        <Stack.Screen name="settings/storage" options={{ title: t('storageSecurityTitle') }} />
        <Stack.Screen name="settings/transfers" options={{ title: t('transfersSettingsTitle') }} />
        <Stack.Screen name="settings/about" options={{ title: t('aboutSupportTitle') }} />
        <Stack.Screen name="settings/sync" options={{ title: t('syncRulesTitle') }} />
      <Stack.Screen name="settings/backup" options={{ title: t('backupScreenTitle') }} />
      <Stack.Screen name="settings/photo-backup" options={{ title: t('photoBackupTitle') }} />
        <Stack.Screen name="settings/whatsnew" options={{ title: t('whatsNewTitle') }} />
        <Stack.Screen name="settings/licenses" options={{ title: t('openSourceLicenses') }} />
      <Stack.Screen name="settings/privacy" options={{ title: t('privacyTitle') }} />
        <Stack.Screen name="dev/gallery" options={{ title: 'Design gallery' }} />
      </Stack>
    </>
  );
}

export default function RootLayout() {
  const settings = useSettings((s) => s.state.app);
  const load = useSettings((s) => s.load);
  const loadCollections = useCollections((s) => s.load);
  useEffect(() => {
    // The one-time Flutter import must finish before settings and collections are read.
    // A damaged legacy file must not become an unhandled rejection; the app starts with the new storage (audit F-51).
    ensureLegacyImport().catch(() => undefined).finally(() => {
      void load();
      void loadCollections();
    });
  }, [load, loadCollections]);
  useEffect(() => watchPhotoBackup(), []);
  useEffect(() => {
    void ensureBackgroundTask();
  }, []);
  return (
    <ShareIntentProvider>
    <SafeAreaProvider>
      <ThemeProvider mode={settings.themeMode} amoled={settings.amoledDark} seed={settings.seedColor}>
        <ToastProvider>
          <DialogHost>
            <LockGate>
              <Shell />
            </LockGate>
          </DialogHost>
        </ToastProvider>
      </ThemeProvider>
    </SafeAreaProvider>
    </ShareIntentProvider>
  );
}
