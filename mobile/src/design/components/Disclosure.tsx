import { ChevronDown, ChevronUp } from 'lucide-react-native';
import { useState, type ReactNode } from 'react';
import { View } from 'react-native';

import { LumenType } from '../lumen';
import { useScheme } from '../theme';
import { DuoIcon } from './DuoIcon';
import { Pressable } from './Pressable';
import { Text } from './Text';
import { Spacing } from '../tokens';

/**
 * Nameplate `.disc`: a 52 high row with a hairline above, the label in the quiet ink and a chevron. It starts closed;
 * tapping it shows [children] below. Used for "Technical details" so addresses and fingerprints stay out of the way.
 */
export function Disclosure({ label, children, defaultOpen = false }: { label: string; children: ReactNode; defaultOpen?: boolean }) {
  const c = useScheme();
  const [open, setOpen] = useState(defaultOpen);
  return (
    <View style={{ borderTopWidth: 1, borderTopColor: c.rule }}>
      <Pressable onPress={() => setOpen((o) => !o)} accessibilityLabel={label} accessibilityState={{ expanded: open }} pressedScale={1}>
        <View style={{ height: 52, paddingHorizontal: Spacing.md, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
          <Text style={[LumenType.name, { fontSize: 15 }]} color={c.onSurfaceVariant}>
            {label}
          </Text>
          <DuoIcon icon={open ? ChevronUp : ChevronDown} size={18} color={c.onSurfaceVariant} />
        </View>
      </Pressable>
      {open ? <View style={{ paddingHorizontal: Spacing.md, paddingBottom: Spacing.md, gap: 10 }}>{children}</View> : null}
    </View>
  );
}
