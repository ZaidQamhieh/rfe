// Minimal CommonMark/GFM subset for the markdown preview. No HTML, and images render as their alt
// text, so a previewed file can never make the phone fetch a remote URL.

export type Inline =
  | { t: 'text'; v: string }
  | { t: 'code'; v: string }
  | { t: 'strong'; c: Inline[] }
  | { t: 'em'; c: Inline[] }
  | { t: 'del'; c: Inline[] }
  | { t: 'link'; c: Inline[]; href: string };

export type Block =
  | { t: 'heading'; level: number; c: Inline[] }
  | { t: 'para'; c: Inline[] }
  | { t: 'code'; lang: string; v: string }
  | { t: 'list'; ordered: boolean; start: number; items: Inline[][] }
  | { t: 'quote'; c: Block[] }
  | { t: 'table'; head: Inline[][]; rows: Inline[][][] }
  | { t: 'hr' };

const DELIMS: [string, 'strong' | 'em' | 'del'][] = [
  ['**', 'strong'],
  ['__', 'strong'],
  ['~~', 'del'],
  ['*', 'em'],
  ['_', 'em'],
];

export function parseInline(s: string): Inline[] {
  const out: Inline[] = [];
  let text = '';
  const push = (n: Inline) => {
    if (text) out.push({ t: 'text', v: text });
    text = '';
    out.push(n);
  };
  let i = 0;
  outer: while (i < s.length) {
    const ch = s[i];
    if (ch === '\\' && i + 1 < s.length && /[\\`*_{}[\]()#+\-.!~|>]/.test(s[i + 1])) {
      text += s[i + 1];
      i += 2;
      continue;
    }
    if (ch === '`') {
      let n = 1;
      while (s[i + n] === '`') n++;
      const fence = '`'.repeat(n);
      const end = s.indexOf(fence, i + n);
      if (end > 0) {
        push({ t: 'code', v: s.slice(i + n, end).replace(/^ (.*) $/, '$1') });
        i = end + n;
        continue;
      }
    }
    if (ch === '!' && s[i + 1] === '[') {
      const m = /^!\[([^\]]*)\]\(([^)\s]*)(?:\s+"[^"]*")?\)/.exec(s.slice(i));
      if (m) {
        text += m[1];
        i += m[0].length;
        continue;
      }
    }
    if (ch === '[') {
      const m = /^\[((?:[^\]\\]|\\.)*)\]\(([^)\s]*)(?:\s+"[^"]*")?\)/.exec(s.slice(i));
      if (m) {
        push({ t: 'link', c: parseInline(m[1]), href: m[2] });
        i += m[0].length;
        continue;
      }
    }
    for (const [d, kind] of DELIMS) {
      // a lone `*` right before another `*` is the remains of an unclosed `**`, not emphasis
      if (s.startsWith(d, i) && s[i + d.length] && s[i + d.length] !== ' ' && !(d.length === 1 && s[i + 1] === d)) {
        // intraword underscores (snake_case) are literal
        if (d[0] === '_' && i > 0 && /\w/.test(s[i - 1])) break;
        const end = findClose(s, d, i + d.length);
        if (end > i + d.length) {
          push({ t: kind, c: parseInline(s.slice(i + d.length, end)) });
          i = end + d.length;
          continue outer;
        }
      }
    }
    text += ch;
    i++;
  }
  if (text) out.push({ t: 'text', v: text });
  return out;
}

function findClose(s: string, d: string, from: number): number {
  for (let j = from; j <= s.length - d.length; j++) {
    if (s[j] === '\\') {
      j++;
      continue;
    }
    if (s[j] === '`') {
      const e = s.indexOf('`', j + 1);
      if (e > 0) j = e;
      continue;
    }
    if (s.startsWith(d, j) && s[j - 1] !== ' ') {
      if (d.length === 1 && s[j + 1] === d) {
        j++; // part of a double delimiter
        continue;
      }
      if (d[0] === '_' && /\w/.test(s[j + d.length] ?? '')) continue;
      return j;
    }
  }
  return -1;
}

const HEADING = /^ {0,3}(#{1,6})(?:\s+(.*?))?\s*#*\s*$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)/;
const HR = /^ {0,3}([-*_])(?:\s*\1){2,}\s*$/;
const BULLET = /^ {0,3}[-*+]\s+(.*)$/;
const ORDERED = /^ {0,3}(\d{1,9})[.)]\s+(.*)$/;
const QUOTE = /^ {0,3}>\s?(.*)$/;
const TABLE_SEP = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;

const cells = (line: string) =>
  line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split(/(?<!\\)\|/)
    .map((c) => parseInline(c.trim()));

export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n');
  const blocks: Block[] = [];
  let i = 0;
  const blank = (l: string | undefined) => l === undefined || l.trim() === '';
  while (i < lines.length) {
    const line = lines[i];
    if (blank(line)) {
      i++;
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trimStart().startsWith(fence[1])) body.push(lines[i++]);
      i++;
      blocks.push({ t: 'code', lang: fence[2], v: body.join('\n') });
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      blocks.push({ t: 'heading', level: h[1].length, c: parseInline(h[2] ?? '') });
      i++;
      continue;
    }
    if (HR.test(line)) {
      blocks.push({ t: 'hr' });
      i++;
      continue;
    }
    if (/^( {4}|\t)/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && (/^( {4}|\t)/.test(lines[i]) || (blank(lines[i]) && /^( {4}|\t)/.test(lines[i + 1] ?? '')))) body.push(lines[i++].replace(/^( {4}|\t)/, ''));
      blocks.push({ t: 'code', lang: '', v: body.join('\n') });
      continue;
    }
    if (QUOTE.test(line)) {
      const body: string[] = [];
      while (i < lines.length && !blank(lines[i]) && QUOTE.test(lines[i])) body.push(QUOTE.exec(lines[i++])![1]);
      blocks.push({ t: 'quote', c: parseMarkdown(body.join('\n')) });
      continue;
    }
    const b = BULLET.exec(line);
    const o = ORDERED.exec(line);
    if (b || o) {
      const ordered = !b;
      const re = ordered ? ORDERED : BULLET;
      const items: string[] = [];
      while (i < lines.length) {
        const m = re.exec(lines[i]);
        if (m) items.push(ordered ? m[2] : m[1]);
        else if (!blank(lines[i]) && /^\s+\S/.test(lines[i]) && items.length) items[items.length - 1] += ` ${lines[i].trim()}`;
        else break;
        i++;
      }
      blocks.push({ t: 'list', ordered, start: o ? Number(o[1]) : 1, items: items.map((s) => parseInline(s.replace(/^\[( |x|X)\]\s+/, (_m, x: string) => (x === ' ' ? '☐ ' : '☑ ')))) });
      continue;
    }
    if (line.includes('|') && TABLE_SEP.test(lines[i + 1] ?? '') && (lines[i + 1] ?? '').includes('-')) {
      const head = cells(line);
      i += 2;
      const rows: Inline[][][] = [];
      while (i < lines.length && !blank(lines[i]) && lines[i].includes('|')) rows.push(cells(lines[i++]));
      blocks.push({ t: 'table', head, rows });
      continue;
    }
    const para: string[] = [lines[i++].trim()]; // always consume at least one line
    while (i < lines.length && !blank(lines[i]) && !HEADING.test(lines[i]) && !FENCE.test(lines[i]) && !HR.test(lines[i]) && !QUOTE.test(lines[i]) && !BULLET.test(lines[i])) {
      // setext heading underline
      if (para.length && /^ {0,3}(=+|-+)\s*$/.test(lines[i])) {
        blocks.push({ t: 'heading', level: lines[i].trim()[0] === '=' ? 1 : 2, c: parseInline(para.join(' ')) });
        para.length = 0;
        i++;
        break;
      }
      para.push(lines[i++].trim());
    }
    if (para.length) blocks.push({ t: 'para', c: parseInline(para.join(' ')) });
  }
  return blocks;
}
