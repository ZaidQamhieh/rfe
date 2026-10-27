import { CameraView, useCameraPermissions } from 'expo-camera';
import { Stack, useRouter } from 'expo-router';
import { ArrowLeft, Flashlight } from 'lucide-react-native';
import { useRef, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { CertPinMismatch } from '../../core/api/pin';
import { Button, InlineError, Text, withAlpha, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { LumenFooter } from '../../features/hosts/LumenRows';
import { Brackets } from '../../features/pairing/QrPanel';
import { ScanIconButton, Scanline } from '../../features/pairing/ScannerChrome';
import { humanizeError, pairWithCode, parsePairingQr } from '../../features/pairing/pairingService';
import { t } from '../../i18n';
import { pairingDeps } from '../../services';

export default function Scan() {
  const router = useRouter();
  const toast = useToast();
  const insets = useSafeAreaInsets();
  const c = useScheme();
  const [perm, requestPerm] = useCameraPermissions();
  const [torch, setTorch] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const handling = useRef(false);

  async function onScan(raw: string) {
    if (handling.current) return;
    const parsed = parsePairingQr(raw);
    if (!parsed.ok) {
      setError(t(parsed.error));
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
      setError(e instanceof CertPinMismatch ? t('fingerprintMismatch', { error: humanizeError(e) }) : t('pairingFailed', { error: humanizeError(e) }));
      handling.current = false;
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <Stack.Screen options={{ headerShown: false }} />
      <View style={{ paddingTop: insets.top + 8, paddingHorizontal: 6, paddingBottom: 8, flexDirection: 'row', alignItems: 'center' }}>
        <ScanIconButton onPress={() => router.back()} label="Back"><ArrowLeft size={22} color={c.onSurface} /></ScanIconButton>
        <Text style={[LumenType.appbar, { flex: 1, marginLeft: 6 }]} color={c.onSurfaceVariant} numberOfLines={1}>{t('scanQrTab')}</Text>
        <ScanIconButton onPress={() => setTorch((v) => !v)} label="Torch" active={torch}><Flashlight size={22} color={torch ? c.onPrimaryContainer : c.onSurface} /></ScanIconButton>
      </View>
      <View style={{ flex: 1, overflow: 'hidden', backgroundColor: c.surfaceContainerLowest }}>
        {perm?.granted ? (
          <CameraView
            style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0 }}
            facing="back"
            enableTorch={torch}
            barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
            onBarcodeScanned={(r) => onScan(r.data)}
          />
        ) : (
          <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 16 }}>
            <Text style={[LumenType.meta, { textAlign: 'center' }]} color={c.onSurfaceVariant}>
              {perm && !perm.canAskAgain ? 'Camera access is blocked. Enable it in system settings, or enter the address instead.' : 'Camera access is needed to scan the pairing QR.'}
            </Text>
            {(!perm || perm.canAskAgain) && <Button label="Allow camera" onPress={requestPerm} />}
          </View>
        )}
        <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, alignItems: 'center', justifyContent: 'center' }} pointerEvents="none">
          <View style={{ width: 240, height: 240 }}>
            <Brackets size={40} width={4} inset={0} color={c.primary} />
            <Scanline height={240} color={c.primary} />
          </View>
        </View>
        {busy && (
          <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: withAlpha(c.surface, 0.6), alignItems: 'center', justifyContent: 'center' }}>
            <ActivityIndicator size="large" color={c.primary} />
          </View>
        )}
      </View>
      <View style={{ paddingHorizontal: 18, paddingTop: 12, paddingBottom: 4, gap: 10 }}>
        {error && <InlineError message={error} />}
        <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('pairingScanHint')}</Text>
      </View>
      <LumenFooter buttons={[{ key: 'manual', label: t('enterCodeManuallyButton'), primary: false, onPress: () => router.replace({ pathname: '/pair', params: { mode: 'code' } }) }]} />
    </View>
  );
}
