import type { ReactNode } from 'react';

import { Pressable } from './Pressable';

/** `.iconbtn`: 48dp hit target, 19px glyph, press scale 0.92. */
export function AppBarIconButton({ label, onPress, children }: { label: string; onPress: () => void; children: ReactNode }) {
  return (
    <Pressable onPress={onPress} pressedScale={0.92} accessibilityLabel={label} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      {children}
    </Pressable>
  );
}
