import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { LogIn, QrCode, Server, UserPlus } from 'lucide-react-native';
import { useEffect, useRef, useState } from 'react';
import { Keyboard, KeyboardAvoidingView, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Button, Disclosure, DuoIcon, Row, SectionLabel, Text } from '../../design/components';
import { useScheme } from '../../design/theme';
import { GettingInBar } from '../../features/pairing/GettingInViews';
import { hasAddress } from '../../features/pairing/gettingInLogic';
import { LanPanel, useLanScan } from '../../features/pairing/LanPanel';
import { ManualPanel } from '../../features/pairing/ManualPanel';
import { useHosts } from '../../features/hosts/useHosts';
import { t } from '../../i18n';
import { Spacing } from '../../design/tokens';

/**
 * Add a computer: scan the code on the computer (the main way), pick a computer found on this Wi-Fi, or sign in with an
 * account. Typing an address is the last, closed "Technical details" row. Pairing itself (the first-contact pin and the
 * Allow request) is unchanged and runs on the next screen.
 */
export default function Pair() {
  const router = useRouter();
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const params = useLocalSearchParams<{ address?: string; mode?: string }>();
  const [address, setAddress] = useState(params.address ?? '');
  const [touched, setTouched] = useState(false);
  const lan = useLanScan();
  const saved = useHosts();
  const { supported, toggle } = lan;
  const scroll = useRef<ScrollView>(null);

  // The address field and Ask button sit at the bottom: when the keyboard opens, bring them into view.
  useEffect(() => {
    const sub = Keyboard.addListener('keyboardDidShow', () => setTimeout(() => scroll.current?.scrollToEnd({ animated: true }), 50));
    return () => sub.remove();
  }, []);

  // Look on this Wi-Fi once when the screen opens, so the list is ready without another tap.
  useEffect(() => {
    if (supported) void toggle();
  }, [supported, toggle]);

  const requestPair = (target: string) => router.push({ pathname: '/pair/request', params: { address: target } });
  const account = (path: '/pair/login' | '/pair/register' | '/pair/account-server') => router.push({ pathname: path, params: { address: address.trim() || params.address } });
  const sendAddress = () => {
    setTouched(true);
    if (hasAddress(address)) requestPair(address.trim());
  };

  return (
    <KeyboardAvoidingView style={{ flex: 1, backgroundColor: c.surface }} behavior="padding">
      <Stack.Screen options={{ headerShown: false }} />
      <GettingInBar onClose={() => router.back()} back title={t('giAddComputer')} />
      <ScrollView ref={scroll} style={{ flex: 1 }} contentContainerStyle={{ paddingBottom: insets.bottom + 16 }} keyboardShouldPersistTaps="handled">
        <Text style={{ paddingHorizontal: Spacing.md, fontSize: 15, lineHeight: 22 }}>{t('giAddIntro')}</Text>
        <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md, paddingBottom: Spacing.md2 }}>
          <Button label={t('giScanCode')} kind="filled" onPress={() => router.push('/pair/scan')} icon={<DuoIcon icon={QrCode} size={22} color={c.onPrimary} />} style={{ alignSelf: 'stretch' }} />
        </View>
        <LanPanel scan={lan} onSelect={requestPair} saved={saved ?? []} />
        <View style={{ paddingHorizontal: Spacing.md3 }}>
          <SectionLabel title={t('giWithAccount')} />
        </View>
        <Row lead={<DuoIcon icon={LogIn} size={24} color={c.onSurfaceVariant} />} title={t('giSignIn')} onPress={() => account('/pair/login')} />
        <Row lead={<DuoIcon icon={Server} size={24} color={c.onSurfaceVariant} />} title={t('accountServerTitle')} onPress={() => account('/pair/account-server')} />
        <Row lead={<DuoIcon icon={UserPlus} size={24} color={c.onSurfaceVariant} />} title={t('giCreateAccount')} onPress={() => account('/pair/register')} />
        <View style={{ marginTop: Spacing.sm }}>
          <Disclosure label={t('giTechnical')} defaultOpen={!!params.address || params.mode === 'code'}>
            <ManualPanel address={address} onChange={setAddress} showRequired={touched} onSubmit={sendAddress} />
          </Disclosure>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
