import { Modal, Pressable as RNPressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Emoji, Text } from '../../design/components';
import { menuBoxShadow } from '../../design/components/Menu';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import type { ComputerMenuEntry, ComputerMenuKey } from './computerMenuLogic';

/** Mockup `.menu`: a 240 wide popover under the nameplate's more button, 8 from the right edge. */
const MENU_WIDTH = 240;
const MENU_SIDE_GAP = 8;
/** Distance from the top of the nameplate to the popover (the more button is 48 high, starting 4 below the inset). */
const MENU_TOP_OFFSET = 52;

/**
 * The computer menu (mockup `.menu`): a small card with 48 high rows, a 24 picture and 15 text, and a rule before the
 * app-wide Settings. Tapping outside closes it; a row closes it, then acts.
 */
export function ComputerMenu({ visible, entries, onClose, onPick }: { visible: boolean; entries: ComputerMenuEntry[]; onClose: () => void; onPick: (key: ComputerMenuKey) => void }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose} statusBarTranslucent>
      <RNPressable style={{ flex: 1 }} onPress={onClose} accessibilityLabel={t('cancelButton')}>
        <View
          accessibilityViewIsModal
          style={{ position: 'absolute', top: insets.top + MENU_TOP_OFFSET, right: MENU_SIDE_GAP, width: MENU_WIDTH, paddingVertical: Spacing.sm, borderRadius: Radii.control, backgroundColor: c.surfaceContainerLowest, boxShadow: menuBoxShadow(c.rule) }}
        >
          {entries.map((e) =>
            e.key === 'rule' ? (
              <View key="rule" style={{ height: 1, backgroundColor: c.rule, marginVertical: Spacing.sm }} />
            ) : (
              <RNPressable
                key={e.key}
                accessibilityRole="menuitem"
                accessibilityLabel={e.label}
                accessibilityState={{ disabled: !!e.disabled }}
                disabled={e.disabled}
                onPress={() => {
                  onClose();
                  onPick(e.key);
                }}
                style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, height: 48, paddingHorizontal: Spacing.md, opacity: e.disabled ? 0.4 : 1 }}
              >
                <Emoji name={e.emoji} size={24} />
                <Text style={{ fontSize: 15, flex: 1 }} numberOfLines={1}>
                  {e.label}
                </Text>
              </RNPressable>
            ),
          )}
        </View>
      </RNPressable>
    </Modal>
  );
}
