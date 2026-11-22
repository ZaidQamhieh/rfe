/** The system bars' sizes on each edge (status bar and cutout on top, navigation or gesture bar at the bottom). */
export type SystemInsets = { top: number; bottom: number; left: number; right: number };

/** Padding for a full-screen overlay (dialog, menu): [gap] of air on every edge, plus whatever the system bars take, so nothing sits under them. */
export function insetPadding(insets: SystemInsets, gap: number): { paddingTop: number; paddingBottom: number; paddingLeft: number; paddingRight: number } {
  return { paddingTop: insets.top + gap, paddingBottom: insets.bottom + gap, paddingLeft: insets.left + gap, paddingRight: insets.right + gap };
}

/** Top and bottom padding for a floating panel that wants [base] margin but never closer than [extra] to a system bar. */
export function paletteMargins(insets: SystemInsets, base: number, extra: number): { paddingTop: number; paddingBottom: number } {
  return { paddingTop: Math.max(base, insets.top + extra), paddingBottom: Math.max(base, insets.bottom + extra) };
}
