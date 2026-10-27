import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { Globe, LogIn, Network, QrCode, Radar, Send, ShieldCheck, UserPlus, X } from 'lucide-react-native';
import { useRef, useState } from 'react';
import { ScrollView, View } from 'react-native';

import { GroupedCard, IconTile, PageHead, SectionLabel, StatePill, Text, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { LumenFooter, MetaPill, MethodRow, StackTopBar } from '../../features/hosts/LumenRows';
import { LanPanel, useLanScan } from '../../features/pairing/LanPanel';
import { ManualPanel } from '../../features/pairing/ManualPanel';
import { t } from '../../i18n';

type Method = 'qr' | 'lan' | 'address';

/**
 * Add workspace: three ways to pair (QR, local network, address) as selectable rows, the chosen one expanding under its
 * row, plus Log in / Register for account pairing. The footer action always runs the chosen method; pairing itself (the
 * first-contact pin, the approval request) is unchanged.
 */
export default function Pair() {
  const router = useRouter();
  const dialogs = useDialogs();
  const c = useScheme();
  const roles = useRoles();
  const params = useLocalSearchParams<{ address?: string; mode?: string }>();
  const [method, setMethod] = useState<Method>(params.mode === 'code' || params.address ? 'address' : 'qr');
  const [address, setAddress] = useState(params.address ?? '');
  const [touched, setTouched] = useState(false);
  const lan = useLanScan();
  const scroll = useRef<ScrollView>(null);
  /** The expanded panel can start below the fold and under the footer button: bring it into view when it appears (audit F-38). */
  const reveal = (y: number) => scroll.current?.scrollTo({ y: Math.max(0, y - 96), animated: true });

  const requestPair = (target: string) => router.push({ pathname: '/pair/request', params: { address: target } });
  const account = (path: '/pair/login' | '/pair/register') => router.push({ pathname: path, params: { address: address.trim() || params.address } });

  const footer =
    method === 'qr'
      ? { label: t('scanQrCodeButton'), icon: QrCode, run: () => router.push('/pair/scan'), disabled: false }
      : method === 'lan'
        ? { label: lan.scanning ? t('stopLocalSearch') : t('scanLocalNetwork'), icon: lan.scanning ? X : Radar, run: () => void lan.toggle(), disabled: !lan.supported }
        : {
            label: t('requestPairingButton'),
            icon: Send,
            run: () => {
              setTouched(true);
              if (address.trim()) requestPair(address.trim());
            },
            disabled: false,
          };

  const more = async () => {
    const picked = await dialogs.choose<'login' | 'register'>({
      title: t('addComputerTitle'),
      options: [
        { value: 'login', label: t('loginTab'), icon: <LogIn size={18} color={c.onSurfaceVariant} /> },
        { value: 'register', label: t('registerTab'), icon: <UserPlus size={18} color={c.onSurfaceVariant} /> },
      ],
    });
    if (picked === 'login') account('/pair/login');
    if (picked === 'register') account('/pair/register');
  };

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StackTopBar context="RFE · Add workspace" sub="New computer" right={<StatePill label="Secure setup" icon={ShieldCheck} tone="safe" />} onMore={more} />
      <ScrollView ref={scroll} style={{ flex: 1 }} contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24, gap: 10 }} keyboardShouldPersistTaps="handled">
        <View style={{ marginHorizontal: -18 }}>
          <PageHead title="Add workspace" subtitle="Pair a computer or enter its address." />
        </View>
        <GroupedCard>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
            <IconTile icon={Network} color={c.primary} size={46} radius={16} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Text style={LumenType.title} numberOfLines={1}>
                Pair your computer
              </Text>
              <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant}>
                Open RFE on the host to begin.
              </Text>
            </View>
          </View>
        </GroupedCard>

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="Choose a method" />
        </View>
        <MethodRow
          icon={QrCode}
          tone={c.primary}
          title={t('scanQrCodeButton')}
          subtitle="Quick pairing from the host screen"
          selected={method === 'qr'}
          onPress={() => setMethod('qr')}
          right={<MetaPill label="Easy" />}
        />
        {method === 'qr' ? (
          <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
            {t('pairingQrHint')}
          </Text>
        ) : null}
        <MethodRow icon={Network} tone={roles.route} title="Find on local network" subtitle="Both devices on the same Wi-Fi" selected={method === 'lan'} onPress={() => setMethod('lan')} />
        {method === 'lan' ? (
          <View onLayout={(e) => reveal(e.nativeEvent.layout.y)}>
            <LanPanel scan={lan} onSelect={requestPair} />
          </View>
        ) : null}
        <MethodRow icon={Globe} tone={roles.transfer} title="Enter address" subtitle="Hostname or IP address" selected={method === 'address'} onPress={() => setMethod('address')} />
        {method === 'address' ? (
          <View onLayout={(e) => reveal(e.nativeEvent.layout.y)}>
            <ManualPanel address={address} onChange={setAddress} showRequired={touched} />
          </View>
        ) : null}

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="With an account" />
        </View>
        <MethodRow icon={LogIn} tone={c.primary} title={t('loginTab')} subtitle="Sign in with your host account" onPress={() => account('/pair/login')} />
        <MethodRow icon={UserPlus} tone={roles.safe} title={t('registerTab')} subtitle="Create an account with a pairing code" onPress={() => account('/pair/register')} />
      </ScrollView>
      <LumenFooter buttons={footer.disabled ? [] : [{ key: 'go', label: footer.label, primary: true, onPress: footer.run, renderIcon: (k) => <footer.icon size={22} color={k} /> }]} />
    </View>
  );
}
