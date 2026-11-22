import { Check } from 'lucide-react-native';
import { View } from 'react-native';

import { useScheme } from '../theme';
import { Radii } from '../tokens';

const SIZE = 22;

/** Nameplate `.cb`: 22px, radius 4. Off: a 1.75px ink-2 outline. On: accent fill with an on-accent check. Display only; the row handles the press. */
export function Checkbox({ checked }: { checked: boolean }) {
  const c = useScheme();
  return (
    <View
      accessibilityRole="checkbox"
      accessibilityState={{ checked }}
      style={{ width: SIZE, height: SIZE, borderRadius: Radii.file, alignItems: 'center', justifyContent: 'center', backgroundColor: checked ? c.primary : 'transparent', borderWidth: checked ? 0 : 1.75, borderColor: c.onSurfaceVariant }}
    >
      {checked ? <Check size={16} color={c.onPrimary} strokeWidth={2.5} /> : null}
    </View>
  );
}
