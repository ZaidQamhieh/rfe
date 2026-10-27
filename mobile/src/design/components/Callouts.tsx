import { CircleAlert, Info, ShieldAlert } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

/** Inline error on the scheme's error container (InlineErrorCard). */
export function InlineError({ message }: { message: string }) {
  const c = useScheme();
  const bg = c.errorContainer;
  const fg = c.onErrorContainer;
  return (
    <View accessibilityRole="alert" style={{ flexDirection: 'row', gap: 8, alignItems: 'flex-start', backgroundColor: bg, borderRadius: Radii.card, paddingHorizontal: Spacing.md, paddingVertical: 12 }}>
      <CircleAlert size={20} color={fg} />
      <Text style={{ flex: 1 }} color={fg}>{message}</Text>
    </View>
  );
}

/** Tinted info callout: primary at 14% (PairingHintCard). */
export function HintCard({ text, kind = 'info' }: { text: string; kind?: 'info' | 'warning' }) {
  const c = useScheme();
  const Icon = kind === 'warning' ? ShieldAlert : Info;
  const bg = kind === 'warning' ? withAlpha(c.tertiaryContainer, 0.5) : withAlpha(c.primary, 0.14);
  const fg = kind === 'warning' ? c.onTertiaryContainer : c.onSurfaceVariant;
  return (
    <View style={{ flexDirection: 'row', gap: Spacing.md2, alignItems: 'flex-start', backgroundColor: bg, borderRadius: Radii.card, padding: Spacing.md }}>
      <Icon size={18} color={kind === 'warning' ? c.onTertiaryContainer : c.primary} />
      <Text style={{ flex: 1, fontSize: 12.5, lineHeight: 18 }} color={fg}>{text}</Text>
    </View>
  );
}

/** `.btn-ghost.btn-block`: full width, surfaceContainerHigh, 1px border, label then trailing icon. */
export function GhostBlockButton({ label, icon, onPress }: { label: string; icon?: ReactNode; onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} pressedScale={0.97} accessibilityLabel={label}>
      <View style={{ flexDirection: 'row', justifyContent: 'center', alignItems: 'center', gap: 7, paddingHorizontal: 18, paddingVertical: 11, backgroundColor: c.surfaceContainerHigh, borderWidth: 1, borderColor: c.outlineVariant, borderRadius: Radii.sm }}>
        <Text style={{ fontSize: 13.5, fontFamily: 'Lato_700Bold', textAlign: 'center' }}>{label}</Text>
        {icon}
      </View>
    </Pressable>
  );
}

export function withAlpha(hex: string, a: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}
