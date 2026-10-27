import { useRouter } from 'expo-router';
import { ArrowLeft, type LucideIcon } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ActionFooter, Pressable, Text, type FooterButton } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';

/**
 * `.sys` + `.appbar` for a stack screen that has no dock: a back button, the muted context line ("Studio PC · Connect"), an
 * optional state pill and the overflow dots. The stack's own header is hidden on these screens.
 */
export function StackTopBar({ context, right, onMore, onBack, sub }: { context: string; right?: ReactNode; onMore?: () => void; onBack?: () => void; sub?: string }) {
  const c = useScheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const back = (
    <Pressable onPress={onBack ?? (() => router.back())} accessibilityLabel="Back" style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <ArrowLeft size={24} color={c.onSurface} />
    </Pressable>
  );
  if (sub) {
    // Mockup `.appbar` + `.workspace-row`: back, context and overflow on top; label and state pill below.
    return (
      <View style={{ paddingTop: insets.top + 8, paddingLeft: 6, paddingRight: 10, paddingBottom: 4 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', minHeight: 48 }}>
          {back}
          <Text style={[LumenType.appbar, { flex: 1 }]} color={c.onSurfaceVariant} numberOfLines={1}>
            {context}
          </Text>
          {onMore ? <OverflowButton onPress={onMore} /> : null}
        </View>
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', minHeight: 48, paddingLeft: 12, paddingRight: 8 }}>
          <Text style={[LumenType.appbar, { flexShrink: 1 }]} color={c.onSurfaceVariant} numberOfLines={1}>
            {sub}
          </Text>
          {right}
        </View>
      </View>
    );
  }
  return (
    <View style={{ paddingTop: insets.top + 8, paddingLeft: 6, paddingRight: 10, paddingBottom: 4, flexDirection: 'row', alignItems: 'center', minHeight: 64 + insets.top }}>
      {back}
      <Text style={[LumenType.appbar, { flex: 1 }]} color={c.onSurfaceVariant} numberOfLines={1}>
        {context}
      </Text>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, marginLeft: 8 }}>
        {right}
        {onMore ? <OverflowButton onPress={onMore} /> : null}
      </View>
    </View>
  );
}

function OverflowButton({ onPress }: { onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel="More options" style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ flexDirection: 'row', gap: 4 }}>
        {[0, 1, 2].map((i) => (
          <View key={i} style={{ width: 5, height: 5, borderRadius: 3, backgroundColor: c.onSurfaceVariant }} />
        ))}
      </View>
    </Pressable>
  );
}

/** `.state-pill` on a raised surface (`.route-option .state-pill`, `.permission-row .state-pill`): grey pill, muted or plain text. */
export function MetaPill({ label, color, strong }: { label: string; color?: string; strong?: boolean }) {
  const c = useScheme();
  return (
    <View style={{ paddingVertical: 8, paddingHorizontal: 12, borderRadius: LumenSize.pillRadius, backgroundColor: c.surfaceContainerHigh, flexShrink: 0, maxWidth: '45%' }}>
      <Text style={LumenType.pill} color={color ?? (strong ? c.onSurface : c.onSurfaceVariant)} numberOfLines={1}>
        {label}
      </Text>
    </View>
  );
}

type RowProps = {
  icon: LucideIcon;
  /** Icon colour (`--tone`): one of `useRoles()` or a scheme colour. */
  tone: string;
  title: string;
  subtitle?: string;
  /** A second, smaller line under the subtitle (hints, latency). */
  note?: string;
  noteColor?: string;
  right?: ReactNode;
  onPress?: () => void;
  selected?: boolean;
  accessibilityLabel?: string;
};

/** `.route-option`: a flat card row with a tone coloured icon, a two-line copy block and a pill. 14 dp padding, 16 dp radius. */
export function MethodRow({ icon: Icon, tone, title, subtitle, note, noteColor, right, onPress, selected, accessibilityLabel }: RowProps) {
  const c = useScheme();
  const body = (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: 12,
        minHeight: 56,
        padding: 14,
        borderRadius: LumenSize.tileRadius,
        backgroundColor: selected ? mix(c.primary, c.surfaceContainer, 0.14) : c.surfaceContainer,
      }}
    >
      <Icon size={26} color={tone} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={LumenType.rowTitle} numberOfLines={1}>
          {title}
        </Text>
        {subtitle ? (
          <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant} numberOfLines={2}>
            {subtitle}
          </Text>
        ) : null}
        {note ? (
          <Text style={[LumenType.meta, { marginTop: 2 }]} color={noteColor ?? c.onSurfaceVariant} numberOfLines={3}>
            {note}
          </Text>
        ) : null}
      </View>
      {right}
    </View>
  );
  if (!onPress) return <View accessible accessibilityLabel={accessibilityLabel}>{body}</View>;
  return (
    <Pressable onPress={onPress} accessibilityLabel={accessibilityLabel ?? title} accessibilityState={{ selected: !!selected }}>
      {body}
    </Pressable>
  );
}

/** `.permission-row`: a row inside a grouped card, hairline-separated, with a plain pill on the right. */
export function PermissionRow({ icon: Icon, tone, title, subtitle, right, last, onPress }: Omit<RowProps, 'selected' | 'note' | 'noteColor'> & { last?: boolean }) {
  const c = useScheme();
  const body = (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 56, paddingVertical: 12, paddingHorizontal: 4, borderBottomWidth: last ? 0 : 1, borderBottomColor: mix(c.onSurfaceVariant, c.surfaceContainer, 0.16) }}>
      <Icon size={26} color={tone} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={LumenType.rowTitle} numberOfLines={2}>
          {title}
        </Text>
        {subtitle ? (
          <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant} numberOfLines={3}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      {right}
    </View>
  );
  return onPress ? (
    <Pressable onPress={onPress} accessibilityLabel={title}>
      {body}
    </Pressable>
  ) : (
    body
  );
}

/** `.action-footer`: the primary action row stays put under the scrolling list, above the bottom inset, with a hairline on top. */
export function LumenFooter({ buttons }: { buttons: FooterButton[] }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  if (buttons.length === 0) return null;
  return (
    <View style={{ backgroundColor: c.surface, paddingBottom: insets.bottom, borderTopWidth: 1, borderTopColor: mix(c.onSurfaceVariant, c.surface, 0.18) }}>
      <ActionFooter buttons={buttons} />
    </View>
  );
}
