import { Check, Lock } from 'lucide-react-native';
import { useState } from 'react';
import { Modal, Pressable as RNPressable, View } from 'react-native';

import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { Button, SheetHero, Text, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { humanizeError } from '../pairing/pairingService';
import { bitsToOctal, bitsToSymbolic, parseModeBits } from './metaLogic';

const ROWS = ['Owner', 'Group', 'Other'];
const COLS = ['R', 'W', 'X'];

/** 3x3 rwx grid for POSIX permissions; Apply sends the octal mode and hands back the updated entry. */
export function ChmodDialog({ visible, entry, client, onClose, onApplied }: { visible: boolean; entry: Entry; client: AgentClient; onClose: () => void; onApplied: (e: Entry) => void }) {
  const c = useScheme();
  const toast = useToast();
  const [bits, setBits] = useState(() => parseModeBits(entry.mode));
  const [applying, setApplying] = useState(false);
  // Re-seed from the entry each time the dialog opens (adjusted during render, not in an effect).
  const [wasVisible, setWasVisible] = useState(visible);
  if (visible !== wasVisible) {
    setWasVisible(visible);
    if (visible) setBits(parseModeBits(entry.mode));
  }

  async function apply() {
    setApplying(true);
    try {
      onApplied(await client.chmod(entry.path, bitsToOctal(bits)));
    } catch (e) {
      toast.error(`chmod failed: ${humanizeError(e)}`);
    } finally {
      setApplying(false);
    }
  }

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose} statusBarTranslucent>
      <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.5)', justifyContent: 'center', padding: 24 }} onPress={onClose} accessibilityLabel={t('cancelButton')}>
        <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderRadius: Radii.lg, overflow: 'hidden' }}>
          <SheetHero showGrabber={false} badge={<Lock size={24} color={c.primary} />} title={t('metaPermissions')} />
          <View style={{ paddingHorizontal: Spacing.lg, gap: 4 }}>
            <Text variant="headlineSmall" style={{ fontFamily: FontFamily.mono }} accessibilityLabel={`Permissions ${bitsToSymbolic(bits)}`}>{bitsToSymbolic(bits)}</Text>
            <Text variant="bodySmall" muted style={{ fontFamily: FontFamily.mono }}>{bitsToOctal(bits)}</Text>
            <View style={{ marginTop: Spacing.md, gap: Spacing.xs }}>
              {ROWS.map((row, r) => (
                <View key={row} style={{ flexDirection: 'row', alignItems: 'center' }}>
                  <Text style={{ width: 64 }}>{row}</Text>
                  {COLS.map((col, k) => {
                    const i = r * 3 + k;
                    return (
                      <RNPressable
                        key={col}
                        disabled={applying}
                        onPress={() => setBits((b) => b.map((v, j) => (j === i ? !v : v)))}
                        accessibilityRole="checkbox"
                        accessibilityLabel={`${row} ${col}`}
                        accessibilityState={{ checked: bits[i], disabled: applying }}
                        style={{ flexDirection: 'row', alignItems: 'center', gap: 6, width: 72, minHeight: 44 }}
                      >
                        <View style={{ width: 22, height: 22, borderRadius: 7, backgroundColor: bits[i] ? c.primary : c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>
                          {bits[i] && <Check size={14} color={c.onPrimary} />}
                        </View>
                        <Text>{col}</Text>
                      </RNPressable>
                    );
                  })}
                </View>
              ))}
            </View>
          </View>
          <View style={{ flexDirection: 'row', justifyContent: 'flex-end', gap: Spacing.sm, padding: Spacing.md }}>
            <Button kind="text" label={t('cancelButton')} disabled={applying} onPress={onClose} />
            <Button kind="filled" label="Apply" busy={applying} onPress={apply} />
          </View>
        </RNPressable>
      </RNPressable>
    </Modal>
  );
}
