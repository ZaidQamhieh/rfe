import { CircleAlert, Info, ShieldAlert } from 'lucide-react-native';
import { View } from 'react-native';

import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Text } from './Text';

/** Inline error on the scheme's error container (InlineErrorCard). */
export function InlineError({ message }: { message: string }) {
  const c = useScheme();
  const bg = c.errorContainer;
  const fg = c.onErrorContainer;
  return (
    <View accessibilityRole="alert" style={{ flexDirection: 'row', gap: Spacing.sm, alignItems: 'flex-start', backgroundColor: bg, borderRadius: Radii.card, paddingHorizontal: Spacing.md, paddingVertical: Spacing.md2 }}>
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

export function withAlpha(hex: string, a: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}
