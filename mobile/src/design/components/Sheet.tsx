import { LinearGradient } from 'expo-linear-gradient';
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

/** Modal bottom sheet (28 top radius, scrim, safe-area padding); tap scrim or back to dismiss. */
export function BottomSheet({ visible, onClose, children, maxHeight = '85%' }: { visible: boolean; onClose: () => void; children: ReactNode; maxHeight?: number | `${number}%` }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose} statusBarTranslucent>
      <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.4)', justifyContent: 'flex-end' }} onPress={onClose} accessibilityLabel="Close">
        <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderTopLeftRadius: Radii.sheet, borderTopRightRadius: Radii.sheet, maxHeight, paddingBottom: insets.bottom, overflow: 'hidden' }}>
          {children}
        </RNPressable>
      </RNPressable>
    </Modal>
  );
}

export function SheetGrabber() {
  const c = useScheme();
  return <View style={{ width: 40, height: 4, borderRadius: 2, backgroundColor: c.outlineVariant, alignSelf: 'center' }} />;
}

/** Title block with grabber (SheetHead). */
export function SheetHead({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <View>
      <View style={{ paddingTop: 10, paddingBottom: 4, alignItems: 'center' }}>
        <View style={{ width: 36, height: 4, borderRadius: Radii.stadium, backgroundColor: '#8884' }} />
      </View>
      <View style={{ paddingHorizontal: 20, paddingTop: 6, paddingBottom: 12 }}>
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
        <View style={{ width: 56, height: 56, borderRadius: Radii.card, backgroundColor: badgeColor ?? withAlpha(hero, 0.16), alignItems: 'center', justifyContent: 'center' }}>{badge}</View>
        <View style={{ flex: 1 }}>
          <Text numberOfLines={1} accessibilityRole="header" style={LumenType.title}>{title}</Text>
          {subtitle ? <Text muted style={[LumenType.meta, { marginTop: Spacing.xs }]}>{subtitle}</Text> : null}
        </View>
        {onClose && (
          <Pressable onPress={onClose} accessibilityLabel="Close" style={{ width: 48, height: 48, marginTop: -4, marginRight: -8, alignItems: 'center', justifyContent: 'center' }}>
            <X size={20} color={c.onSurface} />
          </Pressable>
        )}
      </View>
    </View>
  );
}

/** Round gradient action (68 wide, 52 circle) used in quick-action rows. */
export function GradientActionCircle({ icon, label, gradient, onPress }: { icon: ReactNode; label: string; gradient: [string, string]; onPress: () => void }) {
  return (
    <Pressable onPress={onPress} accessibilityLabel={label} style={{ width: 68 }}>
      <View style={{ alignItems: 'center', gap: Spacing.xs }}>
        <LinearGradient colors={gradient} start={{ x: 0, y: 0 }} end={{ x: 1, y: 1 }} style={{ width: 52, height: 52, borderRadius: 26, alignItems: 'center', justifyContent: 'center', shadowColor: gradient[1], shadowOpacity: 0.4, shadowRadius: 10, shadowOffset: { width: 0, height: 4 }, elevation: 4 }}>
          {icon}
        </LinearGradient>
        <Text numberOfLines={1} style={{ fontSize: 11, textAlign: 'center' }}>{label}</Text>
      </View>
    </Pressable>
  );
}

export function QuickActionRow({ children }: { children: ReactNode }) {
  const c = useScheme();
  return <View style={{ flexDirection: 'row', justifyContent: 'space-around', backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.card, paddingVertical: Spacing.md, paddingHorizontal: Spacing.xs }}>{children}</View>;
}

export function ActionListTile({ icon, label, onPress, tint, trailing }: { icon: ReactNode; label: string; onPress: () => void; tint?: string; trailing?: ReactNode }) {
  return (
    <Pressable onPress={onPress} accessibilityLabel={label}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: Spacing.md, minHeight: 56 }}>
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
    <View style={{ backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.card, overflow: 'hidden' }}>
      {children.map((ch, i) => (
        <View key={i}>
          {i > 0 && <View style={{ height: 1, marginLeft: 56, backgroundColor: withAlpha(c.outlineVariant, 0.5) }} />}
          {ch}
        </View>
      ))}
    </View>
  );
}

export function SheetScroll({ children }: { children: ReactNode }) {
  return <ScrollView keyboardShouldPersistTaps="handled">{children}</ScrollView>;
}
