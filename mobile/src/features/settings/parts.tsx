import { ChevronRight, type LucideIcon } from 'lucide-react-native';
import { Fragment, type ReactNode } from 'react';
import { ScrollView, View } from 'react-native';

import { mix } from '../../design/color';
import { GroupedCard, IconTile, MockupSwitch, Pressable, SectionLabel, Text } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';

/** A labelled group: the section label, then one flat card whose rows are split by faint dividers (`.permission-row`). */
export function SettingsSection({ title, children, trailing }: { title: string; children: ReactNode; trailing?: ReactNode }) {
  const c = useScheme();
  const rows = (Array.isArray(children) ? children.flat(Infinity) : [children]).filter(Boolean) as ReactNode[];
  const divider = mix(c.onSurfaceVariant, c.surfaceContainer, 0.16);
  return (
    <View>
      <SectionLabel title={title} trailing={trailing} />
      <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 4 }}>
        {rows.map((row, i) => (
          <Fragment key={i}>
            {i > 0 && <View style={{ height: 1, backgroundColor: divider }} />}
            {row}
          </Fragment>
        ))}
      </GroupedCard>
    </View>
  );
}

/** The leading icon tile of a settings row (`.app-icon` scale: 38 dp, tone at 18% over the raised surface). */
export function RowBadge({ icon, tint }: { icon: LucideIcon; tint: string }) {
  return <IconTile icon={icon} color={tint} size={38} radius={LumenSize.tileRadius} />;
}

type RowBase = { icon: LucideIcon; title: string; subtitle?: string; tint?: string };

function RowBody({ icon, title, subtitle, tint, trailing }: RowBase & { trailing?: ReactNode }) {
  const c = useScheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 12, minHeight: 64 }}>
      <RowBadge icon={icon} tint={tint ?? c.primary} />
      <View style={{ flex: 1 }}>
        <Text style={LumenType.rowTitle}>{title}</Text>
        {subtitle ? <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant}>{subtitle}</Text> : null}
      </View>
      {trailing}
    </View>
  );
}

/** A row that only displays a fact. */
export function InfoRow(p: RowBase) {
  return <RowBody {...p} />;
}

/** A row that opens something. */
export function NavRow({ onPress, ...p }: RowBase & { onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={p.title} pressedScale={0.99}>
      <RowBody {...p} trailing={<ChevronRight size={20} color={c.onSurfaceVariant} />} />
    </Pressable>
  );
}

/** A row showing its current value; tapping picks a new one. */
export function ValueRow({ onPress, value, ...p }: RowBase & { onPress: () => void; value: string }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={`${p.title}, ${value}`} pressedScale={0.99}>
      <RowBody
        {...p}
        trailing={
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, maxWidth: '45%' }}>
            <Text style={LumenType.meta} color={c.onSurfaceVariant} numberOfLines={1}>{value}</Text>
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
    <Pressable onPress={disabled ? undefined : () => onChange(!value)} disabled={disabled} accessibilityRole="switch" accessibilityLabel={p.title} accessibilityState={{ checked: value, disabled: !!disabled }} style={{ opacity: disabled ? 0.5 : 1 }} pressedScale={0.99}>
      <RowBody {...p} trailing={<MockupSwitch value={value} />} />
    </Pressable>
  );
}

/** A plain labelled switch used inside a row (device grants). */
export function SmallSwitchRow({ label, subtitle, value, onChange, disabled }: { label: string; subtitle?: string; value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  const c = useScheme();
  return (
    <Pressable onPress={disabled ? undefined : () => onChange(!value)} disabled={disabled} accessibilityRole="switch" accessibilityLabel={label} accessibilityState={{ checked: value, disabled: !!disabled }} style={{ opacity: disabled ? 0.5 : 1 }} pressedScale={0.99}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 8, minHeight: 48 }}>
        <View style={{ flex: 1 }}>
          <Text style={LumenType.name}>{label}</Text>
          {subtitle ? <Text style={LumenType.meta} color={c.onSurfaceVariant}>{subtitle}</Text> : null}
        </View>
        <MockupSwitch value={value} />
      </View>
    </Pressable>
  );
}

/** Scroll body shared by the app-settings screens. */
export function SettingsPage({ children }: { children: ReactNode }) {
  return <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, gap: 16, paddingBottom: 32 }}>{children}</ScrollView>;
}
