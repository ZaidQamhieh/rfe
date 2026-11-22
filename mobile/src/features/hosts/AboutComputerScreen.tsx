import { Stack, useRouter } from 'expo-router';
import { Plug, ShieldCheck, SlidersHorizontal, Zap } from 'lucide-react-native';
import { ScrollView, View } from 'react-native';

import type { Host } from '../../core/models/host';
import { Disclosure, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { t } from '../../i18n';
import { NavRow, SettingsSection } from '../settings/parts';
import { wakeHost } from './useWorkspaces';
import { Spacing } from '../../design/tokens';

/** The technical facts of a computer worth showing, each with its label; absent ones are left out. */
function technicalFacts(host: Host): { label: string; value: string }[] {
  const facts: [string, string | undefined][] = [
    [t('aboutComputerTechAddress'), host.address],
    [t('aboutComputerTechTailscale'), host.tailscaleAddress],
    [t('aboutComputerTechInternet'), host.internetAddress],
    [t('aboutComputerTechMac'), host.macAddress],
    [t('aboutComputerTechFingerprint'), host.certFingerprint],
  ];
  return facts.filter((f): f is [string, string] => !!f[1]).map(([label, value]) => ({ label, value }));
}

/**
 * About this computer: where the less common things about one computer live (its settings, how the phone connects, the
 * identity check, wake-on-LAN, bookmarks). Addresses and fingerprints sit under a "Technical details" fold that starts closed.
 */
export function AboutComputerScreen({ host }: { host: Host }) {
  const router = useRouter();
  const toast = useToast();
  const hostRoute = (pathname: string) => router.push({ pathname, params: { id: host.id } } as never);
  const facts = technicalFacts(host);
  return (
    <ScrollView contentContainerStyle={{ paddingTop: Spacing.md2, paddingBottom: 48 }}>
      <Stack.Screen options={{ title: t('computerMenuAbout') }} />
      <Text style={[LumenType.title, { paddingHorizontal: Spacing.md }]} accessibilityRole="header">
        {host.label}
      </Text>
      <SettingsSection title={t('aboutComputerMore')}>
        <NavRow icon={SlidersHorizontal} title={t('aboutComputerSettings')} subtitle={t('aboutComputerSettingsHint')} onPress={() => hostRoute('/host/[id]/settings')} />
        <NavRow icon={Plug} title={t('aboutComputerConnection')} subtitle={t('aboutComputerConnectionHint')} onPress={() => hostRoute('/host/[id]/connect')} />
        <NavRow icon={ShieldCheck} title={t('aboutComputerTrust')} subtitle={t('aboutComputerTrustHint')} onPress={() => hostRoute('/host/[id]/trust')} />
        {host.macAddress ? <NavRow icon={Zap} title={t('aboutComputerWake')} subtitle={t('aboutComputerWakeHint')} onPress={() => void wakeHost(host, toast)} /> : null}
      </SettingsSection>
      <View style={{ marginTop: Spacing.md3 }}>
        <Disclosure label={t('aboutComputerTechnical')}>
          {facts.map((f) => (
            <View key={f.label}>
              <Text style={LumenType.meta} muted>
                {f.label}
              </Text>
              <Text style={LumenType.name} selectable>
                {f.value}
              </Text>
            </View>
          ))}
        </Disclosure>
      </View>
    </ScrollView>
  );
}
