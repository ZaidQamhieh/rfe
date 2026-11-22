import { CameraView, useCameraPermissions } from 'expo-camera';
import { Stack, useRouter } from 'expo-router';
import { Flashlight } from 'lucide-react-native';
import { useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { CertPinMismatch } from '../../core/api/pin';
import { humanizeError } from '../../core/humanizeError';
import { Button, Disclosure, DuoIcon, Pressable, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { GettingInBar } from '../../features/pairing/GettingInViews';
import { pairWithCode, parsePairingQr } from '../../features/pairing/pairingService';
import { Brackets } from '../../features/pairing/QrPanel';
import { t } from '../../i18n';
import { pairingDeps } from '../../services';
import { Radii, Spacing } from '../../design/tokens';

/** What went wrong in plain words, with the technical text kept apart for the closed "Technical details" row. */
type ScanError = { message: string; detail: string };

/** The viewfinder colour from `.vf`: a dark panel with pale corner brackets, the same in light and dark themes. */
const VIEWFINDER = '#2a2c28';
const VIEWFINDER_INK = '#f3f2ee';
/** The veil over the viewfinder while a scanned code is being checked: the panel colour at 70%. */
const VIEWFINDER_BUSY_VEIL = 'rgba(42,44,40,0.7)';

/**
 * Scan the code on the computer. The camera sits in a flat dark viewfinder with four corner brackets; a bad or foreign
 * code gives one plain sentence and its technical cause under "Technical details". Pairing from the code (pinning the
 * fingerprint it carries) is unchanged.
 */
export default function Scan() {
  const router = useRouter();
  const toast = useToast();
  const insets = useSafeAreaInsets();
  const c = useScheme();
  const [perm, requestPerm] = useCameraPermissions();
  const [torch, setTorch] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ScanError | null>(null);
  const handling = useRef(false);

  async function onScan(raw: string) {
    if (handling.current) return;
    const parsed = parsePairingQr(raw);
    if (!parsed.ok) {
      setError({ message: t('giScanBad'), detail: t(parsed.error) });
      return;
    }
    handling.current = true;
    setBusy(true);
    setError(null);
    try {
      const host = await pairWithCode(pairingDeps, { address: parsed.qr.address, fingerprint: parsed.qr.certFingerprint }, parsed.qr.pairingCode);
      toast.success(t('pairedWith', { name: host.label }));
      router.dismissTo('/');
    } catch (e) {
      setError({ message: e instanceof CertPinMismatch ? t('giScanIdentity') : t('giScanFailed'), detail: humanizeError(e) });
      handling.current = false;
    } finally {
      setBusy(false);
    }
  }

  const blocked = perm && !perm.canAskAgain;
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <Stack.Screen options={{ headerShown: false }} />
      <View style={{ flexDirection: 'row', alignItems: 'center' }}>
        <View style={{ flex: 1 }}>
          <GettingInBar onClose={() => router.back()} back title={t('giAddComputer')} />
        </View>
        <View style={{ paddingTop: insets.top, paddingRight: Spacing.xs }}>
          <Pressable onPress={() => setTorch((v) => !v)} accessibilityLabel={t('torchLabel')} accessibilityState={{ selected: torch }} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
            <DuoIcon icon={Flashlight} size={24} color={torch ? c.primary : c.onSurface} />
          </Pressable>
        </View>
      </View>
      <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingBottom: insets.bottom + 16 }}>
        <Text style={{ paddingHorizontal: Spacing.md, fontSize: 15, lineHeight: 22 }}>{t('giAddIntro')}</Text>
        <View style={{ marginHorizontal: Spacing.md, marginTop: Spacing.md2, height: 300, borderRadius: Radii.card, backgroundColor: VIEWFINDER, overflow: 'hidden' }}>
          {perm?.granted ? (
            <CameraView
              style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0 }}
              facing="back"
              enableTorch={torch}
              barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
              onBarcodeScanned={(r) => onScan(r.data)}
            />
          ) : (
            <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
              <Text style={[LumenType.meta, { textAlign: 'center' }]} color={VIEWFINDER_INK}>
                {blocked ? t('giCameraBlocked') : t('giCameraNeeded')}
              </Text>
              {!blocked ? <Button label={t('giAllowCamera')} kind="neutral" onPress={requestPerm} /> : null}
            </View>
          )}
          <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0 }} pointerEvents="none">
            <Brackets size={28} width={3} inset={24} color={VIEWFINDER_INK} />
            <Text style={{ position: 'absolute', left: 0, right: 0, bottom: 12, textAlign: 'center', fontSize: 14 }} color={VIEWFINDER_INK}>
              {t('giHoldStill')}
            </Text>
          </View>
          {busy ? (
            <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: VIEWFINDER_BUSY_VEIL, alignItems: 'center', justifyContent: 'center' }}>
              <ActivityIndicator size="large" color={VIEWFINDER_INK} />
            </View>
          ) : null}
        </View>
        {error ? (
          <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md2, flexDirection: 'row', gap: Spacing.sm, alignItems: 'flex-start' }} accessibilityRole="alert">
            <View style={{ width: 8, height: 8, borderRadius: Radii.file, marginTop: 7, backgroundColor: c.stop }} />
            <Text style={[LumenType.name, { flex: 1 }]}>{error.message}</Text>
          </View>
        ) : null}
        <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }}>
          <Button label={t('giTypeInstead')} kind="neutral" onPress={() => router.replace({ pathname: '/pair', params: { mode: 'code' } })} style={{ alignSelf: 'stretch' }} />
        </View>
        {error ? (
          <View style={{ marginTop: Spacing.md2 }}>
            <Disclosure label={t('giTechnical')}>
              <Text style={LumenType.meta} color={c.onSurfaceVariant} selectable>
                {error.detail}
              </Text>
            </Disclosure>
          </View>
        ) : null}
      </ScrollView>
    </View>
  );
}
