import type { LucideIcon } from 'lucide-react-native';
import { EllipsisVertical } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { mix } from '../color';
import { LumenSize, LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Pressable } from './Pressable';
import { Text } from './Text';

type Tone = 'safe' | 'warn' | 'host' | 'route' | 'muted';

/** `.state-pill`: 8 px bold label on a 15% tint of its tone (Linked, Secure, Protected, Ready, Found...). */
export function StatePill({ label, tone = 'safe', icon: Icon }: { label: string; tone?: Tone; icon?: LucideIcon }) {
  const c = useScheme();
  const color = tone === 'safe' ? c.tertiary : tone === 'warn' ? (c.dark ? '#E8A16D' : '#AA571F') : tone === 'host' ? c.primary : tone === 'route' ? c.secondary : c.onSurfaceVariant;
  const bg = tone === 'muted' ? c.surfaceContainerHigh : mix(color, c.surfaceContainer, 0.15);
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, paddingVertical: 8, paddingHorizontal: 12, borderRadius: LumenSize.pillRadius, backgroundColor: bg }}>
      {Icon ? <Icon size={16} color={color} /> : null}
      <Text style={LumenType.pill} color={color} numberOfLines={1}>
        {label}
      </Text>
    </View>
  );
}

/**
 * `.sys` + `.appbar`: a muted context line on the left ("Studio PC · Files"), a state pill or the overflow dots on the
 * right. The system status bar is the real one, so only the appbar row is drawn here.
 */
export function TopBar({ context, right, onMore, sub, actions }: { context: string; right?: ReactNode; onMore?: () => void; sub?: string; actions?: ReactNode }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const more = onMore ? (
    <Pressable onPress={onMore} accessibilityLabel="More" style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <EllipsisVertical size={22} color={c.onSurfaceVariant} />
    </Pressable>
  ) : null;
  if (sub) {
    // Mockup `.appbar` with a second `.workspace-row`: context + actions on top, label + state pill below.
    return (
      <View style={{ paddingTop: insets.top + 8, paddingHorizontal: 18, paddingBottom: 4 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', minHeight: 48 }}>
          <Text style={[LumenType.appbar, { flexShrink: 1 }]} color={c.onSurfaceVariant} numberOfLines={1}>
            {context}
          </Text>
          <View style={{ flexDirection: 'row', alignItems: 'center' }}>
            {actions}
            {more}
          </View>
        </View>
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', minHeight: 48 }}>
          <Text style={LumenType.appbar} color={c.onSurfaceVariant} numberOfLines={1}>
            {sub}
          </Text>
          {right}
        </View>
      </View>
    );
  }
  return (
    <View style={{ paddingTop: insets.top + 8, paddingHorizontal: 18, paddingBottom: 4, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', minHeight: 64 + insets.top }}>
      <Text style={[LumenType.appbar, { flexShrink: 1 }]} color={c.onSurfaceVariant} numberOfLines={1}>
        {context}
      </Text>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
        {right}
        {more}
      </View>
    </View>
  );
}

/** `.pagehead`: 20 px title (40 dp) over an 8 px muted line. */
export function PageHead({ title, subtitle }: { title: string; subtitle?: string }) {
  const c = useScheme();
  return (
    <View style={{ marginTop: 16, marginBottom: 14, paddingHorizontal: 18 }}>
      <Text style={LumenType.pageTitle} accessibilityRole="header">
        {title}
      </Text>
      {subtitle ? (
        <Text style={[LumenType.caption, { marginTop: 4 }]} color={c.onSurfaceVariant}>
          {subtitle}
        </Text>
      ) : null}
    </View>
  );
}

/** A leading icon tile: `.filemark` / `.app-icon`, a tone tinted 17-19% over the raised surface. */
export function IconTile({ icon: Icon, color, size = 38, radius = 10 }: { icon: LucideIcon; color: string; size?: number; radius?: number }) {
  const c = useScheme();
  return (
    <View style={{ width: size, height: size, borderRadius: radius, alignItems: 'center', justifyContent: 'center', backgroundColor: mix(color, c.surfaceContainerHigh, 0.18) }}>
      <Icon size={Math.round(size * 0.58)} color={color} />
    </View>
  );
}
