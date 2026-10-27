export const CODE_LENGTH = 8;

/** Uppercases, keeps A-Z/0-9 only and caps at the agent's 8-character pairing-code length. */
export function sanitizeCode(text: string, len = CODE_LENGTH): string {
  return text.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, len);
}
