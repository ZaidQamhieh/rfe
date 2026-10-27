// Minimal ICU MessageFormat subset used by the Flutter ARB strings:
// `{name}` substitution and `{n, plural, =0{..} =1{..} one{..} other{..}}` (nested placeholders allowed).

export type Params = Record<string, string | number>;

function matchBrace(s: string, open: number): number {
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    if (s[i] === '{') depth++;
    else if (s[i] === '}' && --depth === 0) return i;
  }
  throw new Error(`unbalanced braces in message: ${s}`);
}

function pluralCategory(n: number): 'one' | 'other' {
  return n === 1 ? 'one' : 'other';
}

export function format(message: string, params: Params = {}): string {
  let out = '';
  for (let i = 0; i < message.length; i++) {
    const ch = message[i];
    if (ch === "'" && message[i + 1] === "'") {
      out += "'";
      i++;
      continue;
    }
    if (ch !== '{') {
      out += ch;
      continue;
    }
    const end = matchBrace(message, i);
    const body = message.slice(i + 1, end);
    const comma = body.indexOf(',');
    if (comma < 0) {
      const v = params[body.trim()];
      out += v === undefined ? `{${body}}` : String(v);
    } else {
      const name = body.slice(0, comma).trim();
      const rest = body.slice(comma + 1).trimStart();
      if (!rest.startsWith('plural,')) throw new Error(`unsupported ICU type in: ${message}`);
      const n = Number(params[name] ?? 0);
      const cases = new Map<string, string>();
      const src = rest.slice('plural,'.length);
      for (let j = 0; j < src.length; j++) {
        if (/\s/.test(src[j])) continue;
        let k = j;
        while (k < src.length && src[k] !== '{') k++;
        const key = src.slice(j, k).trim();
        const close = matchBrace(src, k);
        cases.set(key, src.slice(k + 1, close));
        j = close;
      }
      const chosen = cases.get(`=${n}`) ?? cases.get(pluralCategory(n)) ?? cases.get('other') ?? '';
      out += format(chosen.replaceAll('#', String(n)), params);
    }
    i = end;
  }
  return out;
}
