/** Small colour maths for role tints and the contrast tests. Inputs are '#RRGGBB'. */

const channels = (hex: string): [number, number, number] => {
  const n = parseInt(hex.slice(1, 7), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};

const toHex = (v: number[]): string => '#' + v.map((x) => Math.round(Math.max(0, Math.min(255, x))).toString(16).padStart(2, '0')).join('').toUpperCase();

/** [fg] painted over [bg] at [alpha] (0..1), as an opaque '#RRGGBB'. */
export function mix(fg: string, bg: string, alpha: number): string {
  const f = channels(fg);
  const b = channels(bg);
  return toHex(f.map((x, i) => x * alpha + b[i] * (1 - alpha)));
}

const luminance = (hex: string): number => {
  const [r, g, b] = channels(hex).map((x) => {
    const v = x / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

/** WCAG contrast ratio between two '#RRGGBB' colours. */
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}
