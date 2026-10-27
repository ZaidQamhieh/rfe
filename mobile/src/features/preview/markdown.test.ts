import { parseInline, parseMarkdown } from './markdown';

describe('parseInline', () => {
  it('handles emphasis, code, links and escapes', () => {
    expect(parseInline('a **b** _c_ `d*e` ~~f~~ \\*g')).toEqual([
      { t: 'text', v: 'a ' },
      { t: 'strong', c: [{ t: 'text', v: 'b' }] },
      { t: 'text', v: ' ' },
      { t: 'em', c: [{ t: 'text', v: 'c' }] },
      { t: 'text', v: ' ' },
      { t: 'code', v: 'd*e' },
      { t: 'text', v: ' ' },
      { t: 'del', c: [{ t: 'text', v: 'f' }] },
      { t: 'text', v: ' *g' },
    ]);
    expect(parseInline('[site](https://x.y) snake_case_name')).toEqual([
      { t: 'link', c: [{ t: 'text', v: 'site' }], href: 'https://x.y' },
      { t: 'text', v: ' snake_case_name' },
    ]);
  });
  it('renders images as alt text only (never a fetchable URL)', () => {
    expect(parseInline('see ![a cat](https://evil/p.png) here')).toEqual([{ t: 'text', v: 'see a cat here' }]);
  });
  it('leaves unmatched delimiters literal', () => {
    expect(parseInline('2 * 3 and **open')).toEqual([{ t: 'text', v: '2 * 3 and **open' }]);
  });
});

describe('parseMarkdown', () => {
  it('parses the common block types', () => {
    const md = ['# Title', '', 'para one', 'continues', '', '```js', 'const x = 1;', '```', '- a', '- [x] b', '1. one', '2. two', '> quoted', '---', '| h1 | h2 |', '|---|:-:|', '| 1 | 2 |', 'Setext', '==='].join('\n');
    const b = parseMarkdown(md);
    expect(b.map((x) => x.t)).toEqual(['heading', 'para', 'code', 'list', 'list', 'quote', 'hr', 'table', 'heading']);
    expect(b[1]).toEqual({ t: 'para', c: [{ t: 'text', v: 'para one continues' }] });
    expect(b[2]).toEqual({ t: 'code', lang: 'js', v: 'const x = 1;' });
    expect(b[3]).toMatchObject({ ordered: false, items: [[{ t: 'text', v: 'a' }], [{ t: 'text', v: '☑ b' }]] });
    expect(b[4]).toMatchObject({ ordered: true, start: 1 });
    expect(b[7]).toMatchObject({ head: [[{ t: 'text', v: 'h1' }], [{ t: 'text', v: 'h2' }]], rows: [[[{ t: 'text', v: '1' }], [{ t: 'text', v: '2' }]]] });
    expect(b[8]).toEqual({ t: 'heading', level: 1, c: [{ t: 'text', v: 'Setext' }] });
  });
  it('always terminates, even on odd input', () => {
    for (const s of ['#hashtag', '|', '```', '    code', '> ', '***', '1)', '-', '[x](', '__', '\r\n\r\n']) {
      expect(() => parseMarkdown(s)).not.toThrow();
    }
  });
});
