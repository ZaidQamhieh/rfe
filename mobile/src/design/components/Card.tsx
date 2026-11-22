import type { ReactNode } from 'react';
import { View, type StyleProp, type ViewStyle } from 'react-native';

import { useScheme } from '../theme';
import { LumenSize, LumenType } from '../lumen';
import { Text } from './Text';
import { Spacing } from '../tokens';

/** Flat grouped surface: the sheet colour, a 1px hairline, 8 radius, no shadow. */
export function GroupedCard({ children, padded = true, style }: { children: ReactNode; padded?: boolean; style?: StyleProp<ViewStyle> }) {
  const c = useScheme();
  return (
    <View
      style={[
        { backgroundColor: c.surfaceContainerLowest, borderWidth: 1, borderColor: c.rule, borderRadius: LumenSize.cardRadius, padding: padded ? LumenSize.cardPadding : 0, overflow: 'hidden' },
        style,
      ]}
    >
      {children}
    </View>
  );
}

/** Nameplate `.sec`: a 14/500 label in ink-2, sentence case (no uppercase, no tracking), optional trailing action. */
export function SectionLabel({ title, trailing }: { title: string; trailing?: ReactNode }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'baseline', paddingHorizontal: Spacing.xs, paddingBottom: 6 }}>
      <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { flex: 1 }]}>
        {title}
      </Text>
      {trailing}
    </View>
  );
}
