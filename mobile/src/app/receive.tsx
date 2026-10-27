import { CameraView, useCameraPermissions } from 'expo-camera';
import { Stack, useRouter } from 'expo-router';
import { ArrowLeft, Flashlight } from 'lucide-react-native';
import { useRef, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Button, InlineError, Text, useToast } from '../design/components';
import { Spacing } from '../design/tokens';
import { matchHandoffHost, parseHandoff } from '../features/handoff/handoffLogic';
import { Brackets } from '../features/pairing/QrPanel';
import { DarkIconButton, Scanline } from '../features/pairing/ScannerChrome';
import { enqueueDownloads } from '../features/transfers/enqueueDownloads';
import { humanizeError } from '../features/pairing/pairingService';
import { t } from '../i18n';
import { hostStore } from '../services';

/** Receive a file another paired phone hands off: scan its QR, find the matching paired computer, download the file. */
export default function Receive() {
  const router = useRouter();
  const toast = useToast();
  const insets = useSafeAreaInsets();
  const [perm, requestPerm] = useCameraPermissions();
  const [torch, setTorch] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const handling = useRef(false);

  async function onScan(raw: string) {
    if (handling.current) return;
    const payload = parseHandoff(raw);
    if (!payload) return void setError(t('invalidQrFormat'));
    handling.current = true;
    setBusy(true);
    setError(null);
    try {
      let hosts;
      try {
        const list = await hostStore.listHosts();
        hosts = await Promise.all(list.map(async (host) => ({ host, pin: await hostStore.getPin(host.id) })));
      } catch {
        setError(t('qrHandoffPinReadFailed'));
        handling.current = false;
        return;
      }
      const host = matchHandoffHost(hosts, payload.certFingerprint);
      if (!host) {
        setError(t('qrHandoffNoHostMatch'));
        handling.current = false;
        return;
      }
      await enqueueDownloads(host, [payload.path]);
      toast.success(t('downloadingFile', { name: payload.name }));
      router.back();
    } catch (e) {
      setError(humanizeError(e));
      handling.current = false;
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={{ flex: 1, backgroundColor: '#000' }}>
      <Stack.Screen options={{ headerShown: false }} />
      {perm?.granted ? (
        <CameraView style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0 }} facing="back" enableTorch={torch} barcodeScannerSettings={{ barcodeTypes: ['qr'] }} onBarcodeScanned={(r) => void onScan(r.data)} />
      ) : (
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
          <Text color="#fff" style={{ textAlign: 'center' }}>
            {perm && !perm.canAskAgain ? 'Camera access is blocked. Enable it in system settings.' : 'Camera access is needed to scan the hand-off QR.'}
          </Text>
          {(!perm || perm.canAskAgain) && <Button label="Allow camera" onPress={requestPerm} />}
        </View>
      )}
      <View style={{ flex: 1, paddingTop: insets.top }} pointerEvents="box-none">
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm }}>
          <DarkIconButton onPress={() => router.back()} label="Back"><ArrowLeft size={19} color="#fff" /></DarkIconButton>
          <Text variant="screenTitle" color="#fff" style={{ fontSize: 19 }}>{t('receiveFileTitle')}</Text>
          <DarkIconButton onPress={() => setTorch((v) => !v)} label="Torch"><Flashlight size={19} color={torch ? '#4C8DFF' : '#fff'} /></DarkIconButton>
        </View>
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }} pointerEvents="none">
          <View style={{ width: 230, height: 230 }}>
            <Brackets size={36} width={4} inset={0} color="#4C8DFF" />
            <Scanline height={230} />
          </View>
        </View>
        <View style={{ paddingHorizontal: Spacing.lg, paddingBottom: insets.bottom + Spacing.xl, gap: Spacing.md }}>
          {error && <InlineError message={error} />}
          <Text variant="bodySmall" color="rgba(255,255,255,0.7)" style={{ textAlign: 'center', fontSize: 12.5 }}>{t('qrHandoffScanHint')}</Text>
        </View>
      </View>
      {busy && (
        <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.4)', alignItems: 'center', justifyContent: 'center' }}>
          <ActivityIndicator size="large" />
        </View>
      )}
    </View>
  );
}
