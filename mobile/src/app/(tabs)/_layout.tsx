import { Redirect, Tabs } from 'expo-router';
import { ArrowUpDown, Monitor, Smartphone } from 'lucide-react-native';
import { useEffect, useState } from 'react';

import { BottomNav, type NavDestination } from '../../design/components/BottomNav';
import { useScheme } from '../../design/theme';
import { activeTransferCount } from '../../features/hosts/transferStrip';
import { useTransfers } from '../../features/hosts/useTransfers';
import { isOnboarded } from '../../features/onboarding/onboardingState';
import { t } from '../../i18n';
import { ignoreError } from '../../core/log';
import { ensureLegacyImport, hostStore, keyValue } from '../../services';

/** Routes that stay reachable (from Home's places) but are not in the nav. */
const HIDDEN_ROUTES = ['files'] as const;

/** The nav: Computers (Home plus the file browser opened from it), This phone, and Transfers with its live count. */
function destinations(activeTransfers: number): NavDestination[] {
  return [
    { name: 'index', label: t('navComputers'), Icon: Monitor, also: [...HIDDEN_ROUTES] },
    { name: 'phone', label: t('navThisPhone'), Icon: Smartphone },
    { name: 'transfers', label: t('navTransfers'), Icon: ArrowUpDown, count: activeTransfers },
  ];
}

export default function TabsLayout() {
  const c = useScheme();
  const activeTransfers = activeTransferCount(useTransfers() ?? []);
  // null while the first-run flag is read; a fresh install is sent to the pager before it sees any tab.
  const [onboarded, setOnboarded] = useState<boolean | null>(null);
  useEffect(() => {
    ensureLegacyImport()
      // Once per start: fold copies of the same computer left by earlier versions into one. A failure must not block the app.
      .then(() => hostStore.mergeDuplicates().catch(ignoreError('merge duplicate computers')))
      .then(() => isOnboarded(keyValue, async () => (await hostStore.listHosts()).length))
      .catch(() => true)
      .then(setOnboarded);
  }, []);
  if (onboarded === null) return null;
  if (!onboarded) return <Redirect href="/onboarding" />;
  const dests = destinations(activeTransfers);
  return (
    <Tabs
      backBehavior="initialRoute"
      tabBar={(p) => <BottomNav {...p} destinations={dests} />}
      screenOptions={{ headerStyle: { backgroundColor: c.surface }, headerTintColor: c.onSurface, headerShadowVisible: false, sceneStyle: { backgroundColor: c.surface } }}
    >
      {dests.map((d) => (
        <Tabs.Screen key={d.name} name={d.name} options={{ title: d.label, headerShown: false }} />
      ))}
      {/* Files is opened from Home (Places); it is not a nav destination. */}
      {HIDDEN_ROUTES.map((name) => (
        <Tabs.Screen key={name} name={name} options={{ title: name, headerShown: false, href: null }} />
      ))}
    </Tabs>
  );
}
