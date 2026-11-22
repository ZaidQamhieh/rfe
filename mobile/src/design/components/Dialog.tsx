import { Modal, Pressable as RNPressable, View } from 'react-native';

import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { insetPadding } from '../safeInsets';
import { useScheme } from '../theme';
import { Radii, SCRIM, Spacing } from '../tokens';
import { Button } from './Button';
import { Disclosure } from './Disclosure';
import { Text } from './Text';

/**
 * Confirm dialog (ShadDialog equivalent): title, description, ghost cancel + primary/destructive confirm.
 * `details` (with its `detailsLabel`) sits in a collapsed disclosure for technical text that is not part of the main message.
 */
export function ConfirmDialog({
  visible,
  title,
  description,
  details,
  detailsLabel,
  confirmLabel,
  cancelLabel,
  destructive,
  onConfirm,
  onCancel,
}: {
  visible: boolean;
  title: string;
  description: string;
  details?: string;
  detailsLabel?: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel} statusBarTranslucent>
      <RNPressable style={{ flex: 1, backgroundColor: SCRIM, justifyContent: 'center', ...insetPadding(insets, Spacing.lg) }} onPress={onCancel} accessibilityLabel={cancelLabel}>
        <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderRadius: Radii.lg, padding: Spacing.lg, gap: Spacing.md2 }}>
          <Text variant="titleLarge" accessibilityRole="header">{title}</Text>
          <Text muted style={{ lineHeight: 20 }}>{description}</Text>
          {details !== undefined && detailsLabel !== undefined ? (
            <Disclosure label={detailsLabel}>
              <Text muted style={{ lineHeight: 20 }}>{details}</Text>
            </Disclosure>
          ) : null}
          <View style={{ flexDirection: 'row', justifyContent: 'flex-end', gap: Spacing.sm, marginTop: Spacing.sm }}>
            <Button kind="text" label={cancelLabel} onPress={onCancel} />
            <Button kind={destructive ? 'neutral' : 'filled'} destructive={destructive} label={confirmLabel} onPress={onConfirm} />
          </View>
        </RNPressable>
      </RNPressable>
    </Modal>
  );
}
