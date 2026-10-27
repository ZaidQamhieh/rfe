import * as Clipboard from 'expo-clipboard';
import { Clock, Unlink } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import type { AgentClient } from '../../core/api/agentClient';
import type { ShareLink } from '../../core/api/models';
import { BottomSheet, GhostBlockButton, Pressable, SheetHead, Text, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { humanizeError } from '../pairing/pairingService';
import { formatRemaining, remainingMs } from './shareLink';

/** Shown after minting a one-time share link: the URL with Copy, a live expiry countdown and Revoke. */
export function ShareLinkSheet({ visible, client, link, fileName, onClose }: { visible: boolean; client: AgentClient; link: ShareLink; fileName: string; onClose: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const [left, setLeft] = useState(() => remainingMs(link.expiresAt));

  useEffect(() => {
    if (!visible) return;
    const id = setInterval(() => setLeft(remainingMs(link.expiresAt)), 1000);
    return () => clearInterval(id);
  }, [visible, link.expiresAt]);

  async function copy() {
    await Clipboard.setStringAsync(link.url);
    toast.info(t('copiedPath', { path: link.url }));
  }

  async function revoke() {
    try {
      await client.revokeShareLink(link.tokenHash);
      onClose();
      toast.info(t('shareLinkRevoked'));
    } catch (e) {
      toast.error(t('shareLinkFailed', { error: humanizeError(e) }));
    }
  }

  const expired = left <= 0;
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('shareLinkSheetTitle')} subtitle={fileName} />
      <View style={{ padding: Spacing.lg, paddingTop: 0, gap: Spacing.md }}>
        <View style={{ borderWidth: 1, borderColor: c.outlineVariant, borderRadius: Radii.lg, backgroundColor: c.surface, paddingHorizontal: 4 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingVertical: 11, paddingHorizontal: 4, borderBottomWidth: 1, borderColor: c.outlineVariant }}>
            <Text numberOfLines={2} ellipsizeMode="middle" selectable muted style={{ flex: 1, fontSize: 12, fontFamily: FontFamily.mono }}>{link.url}</Text>
            <Pressable onPress={copy} pressedScale={0.97} accessibilityLabel={t('copyButton')}>
              <View style={{ paddingHorizontal: 13, paddingVertical: 7, borderRadius: Radii.sm, backgroundColor: c.surfaceContainerHigh }}>
                <Text style={{ fontSize: 12.5, fontFamily: FontFamily.semibold }}>{t('copyButton')}</Text>
              </View>
            </Pressable>
          </View>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingVertical: 11, paddingHorizontal: 4 }}>
            <Clock size={16} color={expired ? c.error : c.onSurfaceVariant} />
            <Text style={{ fontSize: 12.5 }} color={expired ? c.error : c.onSurfaceVariant}>{expired ? t('shareLinkExpired') : t('shareLinkExpiresIn', { time: formatRemaining(left) })}</Text>
          </View>
        </View>
        <GhostBlockButton label={t('shareLinkRevokeButton')} icon={<Unlink size={16} color={c.error} />} onPress={revoke} />
      </View>
    </BottomSheet>
  );
}
