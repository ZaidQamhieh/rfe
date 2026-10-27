import { View } from 'react-native';
import Svg, { Circle } from 'react-native-svg';

import { Text } from '../../design/components';
import { FontFamily } from '../../design/tokens';

const SIZE = 170;
const STROKE = 26;

/** The donut: one arc per segment starting at 12 o'clock, with the used percentage and size centred. */
export function UsageRing({ segments, percent, usedLabel }: { segments: { color: string; fraction: number }[]; percent: number; usedLabel: string }) {
  const r = (SIZE - STROKE) / 2;
  const circ = 2 * Math.PI * r;
  const starts = segments.map((_, i) => segments.slice(0, i).reduce((n, s) => n + s.fraction * circ, 0));
  return (
    <View style={{ width: SIZE, height: SIZE, alignItems: 'center', justifyContent: 'center' }} accessibilityLabel={`${percent}% ${usedLabel}`}>
      <Svg width={SIZE} height={SIZE} style={{ position: 'absolute' }}>
        {segments.map((s, i) => {
          if (s.fraction <= 0) return null;
          const len = s.fraction * circ;
          return (
            <Circle
              key={i}
              cx={SIZE / 2}
              cy={SIZE / 2}
              r={r}
              stroke={s.color}
              strokeWidth={STROKE}
              fill="none"
              strokeDasharray={`${len} ${circ - len}`}
              strokeDashoffset={-starts[i]}
              transform={`rotate(-90 ${SIZE / 2} ${SIZE / 2})`}
            />
          );
        })}
      </Svg>
      <Text style={{ fontFamily: FontFamily.monoMedium, fontSize: 20 }}>{percent}%</Text>
      <Text muted style={{ fontSize: 12 }}>{usedLabel}</Text>
    </View>
  );
}
