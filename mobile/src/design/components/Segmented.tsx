import { View } from 'react-native';

import { useScheme } from '../theme';
import { Radii } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

/** Mockup `.segmented`: pill track (surfaceContainerHigh, 3px pad), active option raised on surfaceContainerHighest. */
export function Segmented({ options, selectedIndex, onChange }: { options: string[]; selectedIndex: number; onChange: (i: number) => void }) {
  const c = useScheme();
  return (
    <View style={{ flexDirection: 'row', backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.sm, padding: 3, gap: 2 }} accessibilityRole="tablist">
      {options.map((label, i) => {
        const on = i === selectedIndex;
        return (
          <Pressable
            key={label}
            onPress={() => onChange(i)}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            style={{ flex: 1 }}
          >
            <View
              style={[
                { paddingHorizontal: 6, paddingVertical: 7, borderRadius: 11, alignItems: 'center' },
                on && { backgroundColor: c.surfaceContainerHighest, shadowColor: '#000', shadowOpacity: 0.4, shadowRadius: 2, shadowOffset: { width: 0, height: 1 }, elevation: 2 },
              ]}
            >
              <Text style={{ fontSize: 12, fontFamily: 'Lato_700Bold', textAlign: 'center' }} color={on ? c.onSurface : c.onSurfaceVariant}>
                {label}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}
