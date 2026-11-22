import type { LucideIcon } from 'lucide-react-native';

import { useScheme } from '../theme';

/** Share of the icon colour used to tint the inside of the shape (`.i.duo` in style.css). */
export const DUO_FILL_OPACITY = 0.14;

/**
 * Nameplate action icon: a lucide line icon, 1.5 stroke, with its shape filled in the same colour at 14% opacity
 * (the `.duo` look). Defaults to the theme's ink colour.
 */
export function DuoIcon({ icon: Icon, size = 24, color }: { icon: LucideIcon; size?: number; color?: string }) {
  const c = useScheme();
  const tone = color ?? c.onSurface;
  return <Icon size={size} color={tone} strokeWidth={1.5} fill={tone} fillOpacity={DUO_FILL_OPACITY} />;
}
