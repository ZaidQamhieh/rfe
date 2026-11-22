import { X } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { Modal, Pressable as RNPressable, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { withAlpha } from './Callouts';
import { Pressable } from './Pressable';
import { Text } from './Text';
import { t } from '../../i18n';

/** Modal bottom sheet (16 top radius on the sheet colour, scrim, 24 bottom padding plus safe area); tap scrim or back to dismiss. */
export function BottomSheet({ visible, onClose, children, maxHeight = '85%' }: { visible: boolean; onClose: () => void; children: ReactNode; maxHeight?: number | `${number}%` }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose} statusBarTranslucent>
      <RNPressable style={{ flex: 1, backgroundColor: c.scrim, justifyContent: 'flex-end' }} onPress={onClose} accessibilityLabel={t('closeLabel')}>
        <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainerLowest, borderTopLeftRadius: Radii.sheet, borderTopRightRadius: Radii.sheet, maxHeight, paddingBottom: insets.bottom + Spacing.lg, overflow: 'hidden' }}>
          {children}
        </RNPressable>
      </RNPressable>
    </Modal>
  );
}

export function SheetGrabber() {
  const c = useScheme();
  return <View style={{ width: 32, height: 4, borderRadius: 2, backgroundColor: c.outline, opacity: 0.6, alignSelf: 'center' }} />;
}

/** Title block with grabber (SheetHead). */
export function SheetHead({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <View>
      <View style={{ paddingTop: Spacing.md2, paddingBottom: Spacing.xs }}>
        <SheetGrabber />
      </View>
      <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md2, paddingBottom: Spacing.sm }}>
        <Text style={LumenType.title} accessibilityRole="header">{title}</Text>
        {subtitle ? <Text muted style={[LumenType.meta, { marginTop: 2 }]}>{subtitle}</Text> : null}
      </View>
    </View>
  );
}

/** Sheet header with a soft radial-style tint, 56dp badge, title/subtitle and optional close (SheetHero). */
export function SheetHero({ badge, title, subtitle, tint, badgeColor, onClose, showGrabber = true }: { badge: ReactNode; title: string; subtitle?: string; tint?: string; badgeColor?: string; onClose?: () => void; showGrabber?: boolean }) {
  const c = useScheme();
  const hero = tint ?? c.primary;
  return (
    <View style={{ paddingHorizontal: Spacing.lg, paddingTop: Spacing.md, paddingBottom: Spacing.sm }}>
      {showGrabber && (
        <View style={{ marginBottom: Spacing.md }}>
          <SheetGrabber />
        </View>
      )}
      <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: Spacing.md }}>
        <View style={{ width: 56, height: 56, borderRadius: Radii.control, backgroundColor: badgeColor ?? withAlpha(hero, 0.12), alignItems: 'center', justifyContent: 'center' }}>{badge}</View>
        <View style={{ flex: 1 }}>
          <Text numberOfLines={1} accessibilityRole="header" style={LumenType.title}>{title}</Text>
          {subtitle ? <Text muted style={[LumenType.meta, { marginTop: Spacing.xs }]}>{subtitle}</Text> : null}
        </View>
        {onClose && (
          <Pressable onPress={onClose} accessibilityLabel={t('closeLabel')} style={{ width: 48, height: 48, marginTop: -4, marginRight: -8, alignItems: 'center', justifyContent: 'center' }}>
            <X size={20} color={c.onSurface} />
          </Pressable>
        )}
      </View>
    </View>
  );
}

export function ActionListTile({ icon, label, onPress, tint, trailing }: { icon: ReactNode; label: string; onPress: () => void; tint?: string; trailing?: ReactNode }) {
  return (
    <Pressable onPress={onPress} accessibilityLabel={label}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.md, minHeight: 56 }}>
        {icon}
        <Text color={tint} style={[LumenType.name, { flex: 1 }]} numberOfLines={2}>{label}</Text>
        {trailing}
      </View>
    </Pressable>
  );
}

/** Rounded card of action rows with inset dividers (ActionListCard). */
export function ActionListCard({ children }: { children: ReactNode[] }) {
  const c = useScheme();
  return (
    <View style={{ backgroundColor: c.surfaceContainerLowest, borderRadius: Radii.control, borderWidth: 1, borderColor: c.rule, overflow: 'hidden' }}>
      {children.map((ch, i) => (
        <View key={i}>
          {i > 0 && <View style={{ height: 1, marginLeft: 56, backgroundColor: c.rule }} />}
          {ch}
        </View>
      ))}
    </View>
  );
}

export function SheetScroll({ children }: { children: ReactNode }) {
  return <ScrollView keyboardShouldPersistTaps="handled">{children}</ScrollView>;
}
