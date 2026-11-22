import { View } from 'react-native';

import { useScheme } from '../theme';
import { Spacing } from '../tokens';

/**
 * Nameplate `.pg`: a 4px bar with a 2px radius on the `--fill-2` track. The fill is the accent, or the `--wait` colour
 * while the transfer is paused. [fraction] is 0..1 and is clamped.
 */
export function ProgressBar({ fraction, waiting = false, label }: { fraction: number; waiting?: boolean; label?: string }) {
  const c = useScheme();
  const f = Number.isFinite(fraction) ? Math.min(1, Math.max(0, fraction)) : 0;
  return (
    <View
      accessibilityRole="progressbar"
      accessibilityLabel={label}
      accessibilityValue={{ min: 0, max: 100, now: Math.round(f * 100) }}
      style={{ height: 4, borderRadius: 2, backgroundColor: c.fill2, overflow: 'hidden', marginTop: Spacing.sm, marginBottom: 6 }}
    >
      <View style={{ width: `${f * 100}%`, height: 4, backgroundColor: waiting ? c.wait : c.primary }} />
    </View>
  );
}
