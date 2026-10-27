import * as Clipboard from 'expo-clipboard';
import { Copy, QrCode } from 'lucide-react-native';
import { View } from 'react-native';
import QRCode from 'react-native-qrcode-svg';

import { BottomSheet, GhostBlockButton, SheetHero, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { encodeHandoff, type HandoffPayload } from './handoffLogic';

/** A QR another phone paired to the same computer can scan to fetch this file with its own credentials. */
export function HandoffQrSheet({ visible, payload, onClose }: { visible: boolean; payload: HandoffPayload; onClose: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const data = encodeHandoff(payload);
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHero badge={<QrCode size={26} color={c.primary} />} title={payload.name} subtitle={t('qrHandoffSheetTitle')} onClose={onClose} />
      <View style={{ paddingHorizontal: Spacing.lg, paddingBottom: Spacing.lg, gap: Spacing.lg, alignItems: 'center' }}>
        {/* White tile: the code is black on transparent by default, which no phone can scan against a dark surface. */}
        <View style={{ padding: Spacing.md2, backgroundColor: '#fff', borderRadius: Radii.card }}>
          <QRCode value={data} size={200} backgroundColor="#fff" color="#000" />
        </View>
        <View style={{ alignSelf: 'stretch' }}>
          <GhostBlockButton
            label={t('qrHandoffCopyButton')}
            icon={<Copy size={16} color={c.onSurface} />}
            onPress={async () => {
              await Clipboard.setStringAsync(data);
              toast.success(t('qrHandoffCopied'));
            }}
          />
        </View>
      </View>
    </BottomSheet>
  );
}
