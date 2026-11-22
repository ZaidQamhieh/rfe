import { ChevronRight, type LucideIcon } from 'lucide-react-native';
import { Children, isValidElement, type ReactNode } from 'react';
import { ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { DuoIcon, MockupSwitch, Pressable, Text } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';

/** A labelled group, flat like the mockup lists: a `.sec` label (14/500, quiet ink, 16 side padding), then the rows with no card around them. */
export function SettingsSection({ title, children, trailing }: { title?: string; children: ReactNode; trailing?: ReactNode }) {
  return (
    <View>
      {(title || trailing) && <View style={{ flexDirection: 'row', alignItems: 'baseline', paddingTop: Spacing.md3, paddingBottom: 6, paddingHorizontal: Spacing.md }}>
        <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { flex: 1 }]}>
          {title}
        </Text>
        {trailing}
      </View>}
      {children}
    </View>
  );
}

type RowBase = { icon: LucideIcon; title: string; subtitle?: string };

/**
 * `.row` of the mockup: at least 64 high, 16 side padding, 16 gap, a 40x40 lead holding a quiet line icon, then the
 * 16/22 title with a 14/20 quiet subtitle (wrapping, since these hints are sentences) and the trailing part.
 */
function RowBody({ icon, title, subtitle, trailing }: RowBase & { trailing?: ReactNode }) {
  const c = useScheme();
  return (
    <View style={{ minHeight: LumenSize.rowMinHeight, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md }}>
      <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={icon} size={24} color={c.onSurfaceVariant} />
      </View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={LumenType.name}>{title}</Text>
        {subtitle ? (
          <Text style={LumenType.meta} muted>
            {subtitle}
          </Text>
        ) : null}
      </View>
      {trailing}
    </View>
  );
}

/** A row that only displays a fact; `value` shows the current state as plain text on the right. */
export function InfoRow({ value, ...p }: RowBase & { value?: string }) {
  return <RowBody {...p} trailing={value ? <Text style={LumenType.meta} muted>{value}</Text> : undefined} />;
}

/** A row that opens something. */
export function NavRow({ onPress, ...p }: RowBase & { onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={p.title} pressedScale={1}>
      <RowBody {...p} trailing={<ChevronRight size={20} color={c.onSurfaceVariant} />} />
    </Pressable>
  );
}

/** A row showing its current value; tapping picks a new one. */
export function ValueRow({ onPress, value, ...p }: RowBase & { onPress: () => void; value: string }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={`${p.title}, ${value}`} pressedScale={1}>
      <RowBody
        {...p}
        trailing={
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, maxWidth: '45%' }}>
            <Text style={LumenType.meta} muted numberOfLines={1}>
              {value}
            </Text>
            <ChevronRight size={20} color={c.onSurfaceVariant} />
          </View>
        }
      />
    </Pressable>
  );
}

/** A row with a switch; the whole row toggles. */
export function ToggleRow({ value, onChange, disabled, ...p }: RowBase & { value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <Pressable onPress={disabled ? undefined : () => onChange(!value)} disabled={disabled} accessibilityRole="switch" accessibilityLabel={p.title} accessibilityState={{ checked: value, disabled: !!disabled }} style={{ opacity: disabled ? 0.5 : 1 }} pressedScale={1}>
      <RowBody {...p} trailing={<MockupSwitch value={value} />} />
    </Pressable>
  );
}

/**
 * Scroll body shared by the settings screens. A `SettingsSection` runs edge to edge (its rows carry their own 16 side
 * padding); anything else (a button, a note) is given the same 16 side padding so it lines up with the rows.
 */
export function SettingsPage({ children }: { children: ReactNode }) {
  const insets = useSafeAreaInsets();
  return (
    <ScrollView contentContainerStyle={{ paddingBottom: insets.bottom + Spacing.lg }}>
      {Children.toArray(children).map((child, i) =>
        isValidElement(child) && child.type === SettingsSection ? (
          child
        ) : (
          <View key={i} style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md }}>
            {child}
          </View>
        ),
      )}
    </ScrollView>
  );
}
