import { View } from 'react-native';

import { LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

/**
 * Nameplate `.seg`: a fill-coloured track (hairline border, radius 8, 4px padding and gap) of 40 high options with a 4px
 * radius, 15/500 labels in ink-2; the selected option sits on the sheet colour with a `--sel-bd` border.
 */
export function Segmented({ options, selectedIndex, onChange }: { options: string[]; selectedIndex: number; onChange: (i: number) => void }) {
  const c = useScheme();
  return (
    <View style={{ flexDirection: 'row', backgroundColor: c.surfaceContainer, borderWidth: 1, borderColor: c.rule, borderRadius: Radii.control, padding: Spacing.xs, gap: Spacing.xs }} accessibilityRole="tablist">
      {options.map((label, i) => {
        const on = i === selectedIndex;
        return (
          <Pressable key={label} onPress={() => onChange(i)} accessibilityRole="tab" accessibilityState={{ selected: on }} style={{ flex: 1 }}>
            <View
              style={[
                { minHeight: 40, paddingHorizontal: 6, borderRadius: Radii.file, alignItems: 'center', justifyContent: 'center', borderWidth: 1, borderColor: 'transparent' },
                on && { backgroundColor: c.surfaceContainerLowest, borderColor: c.selBd, boxShadow: '0 1px 2px rgba(0,0,0,0.06)' },
              ]}
            >
              <Text style={[LumenType.action, { textAlign: 'center' }]} color={on ? c.onSurface : c.onSurfaceVariant}>
                {label}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}
