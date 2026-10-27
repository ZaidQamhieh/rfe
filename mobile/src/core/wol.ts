/** Parses `aa:bb:cc:dd:ee:ff`; null when malformed. Same acceptance as core/platform/wol.dart. */
export function parseMac(mac: string): number[] | null {
  const parts = mac.split(':');
  if (parts.length !== 6) return null;
  const out: number[] = [];
  for (const p of parts) {
    if (!/^[0-9a-fA-F]{1,2}$/.test(p)) return null;
    out.push(parseInt(p, 16));
  }
  return out;
}

/** Magic packet: 6x 0xFF then the MAC 16 times (102 bytes). */
export function magicPacket(mac: number[]): number[] {
  return [...Array(6).fill(0xff), ...Array.from({ length: 16 }, () => mac).flat()];
}
