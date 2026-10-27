import type { ReactNode } from 'react';
import { View } from 'react-native';

import { mix } from '../color';
import { useScheme } from '../theme';
import { Button } from './Button';

export type FooterButton = { key: string; label: string; primary: boolean; onPress: () => void; renderIcon?: (color: string) => ReactNode };

/**
 * Lumen `.action-footer`: a fixed row under the scrolling content with a hairline on top (muted at 18%), 8/9 px padding
 * (16/18 dp) and a 7 px (14 dp) gap. The primary button takes 60%, a secondary 40% (`.file-actions`), a lone button all of it.
 */
export function ActionFooter({ buttons }: { buttons: FooterButton[] }) {
  const c = useScheme();
  if (buttons.length === 0) return null;
  const lone = buttons.length === 1;
  return (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: 14,
        paddingHorizontal: 18,
        paddingTop: 16,
        paddingBottom: 18,
        marginTop: 8,
        borderTopWidth: 1,
        borderTopColor: mix(c.onSurfaceVariant, c.surface, 0.18),
        backgroundColor: c.surface,
      }}
    >
      {buttons.map((b) => (
        <Button key={b.key} size="lg" kind={b.primary ? 'filled' : 'neutral'} label={b.label} onPress={b.onPress} renderIcon={b.renderIcon} style={{ flex: lone ? 1 : b.primary ? 3 : 2 }} />
      ))}
    </View>
  );
}
