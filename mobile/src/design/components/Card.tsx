import type { ReactNode } from 'react';
import { View, type StyleProp, type ViewStyle } from 'react-native';

import { useScheme } from '../theme';
import { LumenSize, LumenType } from '../lumen';
import { Text } from './Text';

/** Lumen `.app-card` / `.collection`: flat raised-by-one surface, 18 dp radius, 14 dp padding, no border. */
export function GroupedCard({ children, padded = true, style }: { children: ReactNode; padded?: boolean; style?: StyleProp<ViewStyle> }) {
  const c = useScheme();
  return (
    <View
      style={[
        { backgroundColor: c.surfaceContainer, borderRadius: LumenSize.cardRadius, padding: padded ? LumenSize.cardPadding : 0, overflow: 'hidden' },
        style,
      ]}
    >
      {children}
    </View>
  );
}

/** Lumen `.section-label`: 9 px / 600, sentence case, subtle colour. */
export function SectionLabel({ title, trailing }: { title: string; trailing?: ReactNode }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', paddingHorizontal: 4, paddingBottom: 8 }}>
      <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { flex: 1 }]}>
        {title}
      </Text>
      {trailing}
    </View>
  );
}
