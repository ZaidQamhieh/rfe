/* eslint-disable no-console -- the one place allowed to write to the console */

const REDACTED = '[redacted]';

// Bearer tokens and authorization headers, `token=`/`password=`/`code=` style pairs, 64-hex certificate
// fingerprints (also colon-separated), and long base64 blobs that could be keys.
const PATTERNS: [RegExp, string][] = [
  [/\b(Bearer)\s+[A-Za-z0-9._~+/=-]+/gi, `$1 ${REDACTED}`],
  // Also compound names such as deviceToken or rfe_token_<id> (audit F-81).
  [/\b([A-Za-z0-9_]*(?:authorization|token|password|secret)[A-Za-z0-9_]*|pairingCode|code|pin)(["']?\s*[:=]\s*["']?)[^\s"',&}]+/gi, `$1$2${REDACTED}`],
  [/\b(?:[0-9a-f]{2}:){31}[0-9a-f]{2}\b/gi, REDACTED],
  [/\b[0-9a-f]{64}\b/gi, REDACTED],
  [/\b[A-Za-z0-9+/_-]{40,}={0,2}(?![A-Za-z0-9+/_-])/g, REDACTED],
];

/** Scrubs credentials, certificate pins and key-like blobs from any text before it can reach the console. */
export function redact(text: string): string {
  return PATTERNS.reduce((s, [re, sub]) => s.replace(re, sub), text);
}

function render(args: unknown[]): string {
  return args
    .map((a) => {
      if (typeof a === 'string') return a;
      if (a instanceof Error) return `${a.name}: ${a.message}`;
      try {
        return JSON.stringify(a);
      } catch {
        return String(a);
      }
    })
    .join(' ');
}

/** The app's only logger: everything goes through [redact]. Use this instead of `console`. */
export const log = {
  debug: (...args: unknown[]) => {
    if (__DEV__) console.debug(redact(render(args)));
  },
  info: (...args: unknown[]) => console.info(redact(render(args))),
  warn: (...args: unknown[]) => console.warn(redact(render(args))),
  error: (...args: unknown[]) => console.error(redact(render(args))),
};
