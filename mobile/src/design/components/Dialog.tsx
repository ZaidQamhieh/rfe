import { Modal, Pressable as RNPressable, View } from 'react-native';

import { useScheme } from '../theme';
import { Radii } from '../tokens';
import { Button } from './Button';
import { Text } from './Text';

/** Confirm dialog (ShadDialog equivalent): title, description, ghost cancel + primary/destructive confirm. */
export function ConfirmDialog({
  visible,
  title,
  description,
  confirmLabel,
  cancelLabel,
  destructive,
  onConfirm,
  onCancel,
}: {
  visible: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const c = useScheme();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel} statusBarTranslucent>
      <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.5)', justifyContent: 'center', padding: 24 }} onPress={onCancel} accessibilityLabel={cancelLabel}>
        <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderRadius: Radii.lg, padding: 24, gap: 12 }}>
          <Text variant="titleLarge" accessibilityRole="header">{title}</Text>
          <Text muted style={{ lineHeight: 20 }}>{description}</Text>
          <View style={{ flexDirection: 'row', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
            <Button kind="text" label={cancelLabel} onPress={onCancel} />
            <Button kind={destructive ? 'neutral' : 'filled'} destructive={destructive} label={confirmLabel} onPress={onConfirm} />
          </View>
        </RNPressable>
      </RNPressable>
    </Modal>
  );
}
