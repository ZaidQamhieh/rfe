import type { ReactNode } from 'react';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useScheme } from '../theme';
import { Pressable } from './Pressable';
import { ScreenHeader } from './ScreenHeader';

/** Tab-screen app bar: ScreenHeader on the left, 48dp icon actions on the right (toolbarHeight 64/72). */
export function AppBar({ title, subtitle, actions, tall }: { title: string; subtitle?: string; actions?: ReactNode; tall?: boolean }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <View style={{ paddingTop: insets.top, backgroundColor: c.surface }}>
      <View style={{ minHeight: tall ? 72 : 64, flexDirection: 'row', alignItems: 'center', paddingLeft: 16, paddingRight: 8 }}>
        <View style={{ flex: 1 }}>
          <ScreenHeader title={title} subtitle={subtitle} />
        </View>
        {actions}
      </View>
    </View>
  );
}

/** `.iconbtn`: 48dp hit target, 19px glyph, press scale 0.92. */
export function AppBarIconButton({ label, onPress, children }: { label: string; onPress: () => void; children: ReactNode }) {
  return (
    <Pressable onPress={onPress} pressedScale={0.92} accessibilityLabel={label} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      {children}
    </Pressable>
  );
}
