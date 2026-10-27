import { Text as RNText, type TextProps } from 'react-native';

import { useScheme } from '../theme';
import { TypeScale, type TypeRole } from '../tokens';

type Props = TextProps & { variant?: TypeRole; muted?: boolean; color?: string };

/** Themed text: type roles from the Material scale; `muted` = onSurfaceVariant. */
export function Text({ variant = 'bodyMedium', muted, color, style, ...rest }: Props) {
  const c = useScheme();
  return <RNText {...rest} style={[TypeScale[variant], { color: color ?? (muted ? c.onSurfaceVariant : c.onSurface) }, style]} />;
}
