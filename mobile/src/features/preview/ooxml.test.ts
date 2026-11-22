import { MAX_DOC_PARAGRAPHS, MAX_SHEET_COLS, MAX_SHEET_ROWS, openXlsx, parseDocx, parsePptx, sheetColumns } from './ooxml';
import { ZipError, listZip } from './zip';
import { buildZip } from './zipFixture';

// Milliseconds one call takes (the parsers are synchronous).
/* Best of three: a loaded CI box can stall one run, but quadratic parsing (1 s and more at this size) is slow on every run. */
const timeOf = (fn: () => unknown): number => {
  let best = Infinity;
  for (let i = 0; i < 3; i++) {
    const t0 = Date.now();
    fn();
    best = Math.min(best, Date.now() - t0);
  }
  return best;
};
// Upper limit for parsing 200 KB of malformed XML. The parser should take well under 100 ms.
// A parser with quadratic running time would need many seconds, so this limit still catches it.
// It is generous so that a slow, busy CI machine does not fail the test by accident.
const MAX_PARSE_MS = 1500;
const HOSTILE_BYTES = 200_000;

const NS = 'xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"';

function xlsx(sheetXml: Record<string, string>, shared?: string): Uint8Array {
  const names = Object.keys(sheetXml);
  const files: Record<string, string> = {
    'xl/workbook.xml': `<workbook ${NS}><sheets>${names.map((n, i) => `<sheet name="${n}" sheetId="${i + 1}" r:id="rId${i + 10}"/>`).join('')}</sheets></workbook>`,
    'xl/_rels/workbook.xml.rels': `<Relationships>${names.map((n, i) => `<Relationship Id="rId${i + 10}" Type="x" Target="${i === 0 ? 'worksheets/s' + i + '.xml' : '/xl/worksheets/s' + i + '.xml'}"/>`).join('')}</Relationships>`,
  };
  names.forEach((n, i) => (files[`xl/worksheets/s${i}.xml`] = `<worksheet ${NS}><sheetData>${sheetXml[n]}</sheetData></worksheet>`));
  if (shared) files['xl/sharedStrings.xml'] = `<sst ${NS}>${shared}</sst>`;
  return buildZip(files);
}

describe('openXlsx', () => {
  it('reads sheet tabs in order and resolves relative and absolute rel targets', () => {
    const book = openXlsx(xlsx({ First: '<row r="1"><c r="A1" t="inlineStr"><is><t>one</t></is></c></row>', 'Q&amp;A': '<row r="1"><c r="A1" t="inlineStr"><is><t>two</t></is></c></row>' }));
    expect(book.sheetNames).toEqual(['First', 'Q&A']);
    expect(book.sheet(0).rows).toEqual([['one']]);
    expect(book.sheet(1).rows).toEqual([['two']]);
  });
  it('resolves shared strings (rich runs, phonetic text skipped), numbers, booleans, errors and entities', () => {
    const shared = '<si><t>Name</t></si><si><r><t>Rich </t></r><r><t>text</t></r><rPh><t>ignored</t></rPh></si><si><t>a &lt; b &amp; c &#65;</t></si>';
    const sheet = '<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row>'
      + '<row r="2"><c r="A2"><v>3.5</v></c><c r="B2" t="b"><v>1</v></c><c r="C2" t="e"><v>#DIV/0!</v></c><c r="D2" t="str"><f>A2*2</f><v>7</v></c></row>';
    const s = openXlsx(xlsx({ S: sheet }, shared)).sheet(0);
    expect(s.rows).toEqual([['Name', 'Rich text', 'a < b & c A'], ['3.5', 'TRUE', '#DIV/0!', '7']]);
  });
  it('places cells by reference: skipped columns and rows become blanks, self-closing cells are empty', () => {
    const sheet = '<row r="2"><c r="B2" t="inlineStr"><is><t>x</t></is></c><c r="D2"/><c r="E2" t="inlineStr"><is><t>y</t></is></c></row><row r="4"><c r="A4" t="inlineStr"><is><t>z</t></is></c></row>';
    expect(openXlsx(xlsx({ S: sheet })).sheet(0).rows).toEqual([[], ['', 'x', '', '', 'y'], [], ['z']]);
  });
  it('caps rows and columns and reports the real totals', () => {
    let rows = '';
    for (let r = 1; r <= MAX_SHEET_ROWS + 5; r++) rows += `<row r="${r}"><c r="A${r}"><v>${r}</v></c><c r="ZZ${r}"><v>9</v></c></row>`;
    const s = openXlsx(xlsx({ S: rows })).sheet(0);
    expect(s.rows).toHaveLength(MAX_SHEET_ROWS);
    expect(s.totalRows).toBe(MAX_SHEET_ROWS + 5);
    expect(s.totalCols).toBe(702);
    expect(Math.max(...s.rows.map((r) => r.length))).toBeLessThanOrEqual(MAX_SHEET_COLS);
  });
  it('parses 200 KB of unclosed row, cell and shared-string tags in linear time', () => {
    const rows = '<row>'.repeat(HOSTILE_BYTES / 5);
    const cells = '<row r="1">' + '<c>'.repeat(HOSTILE_BYTES / 3) + '</row>';
    const sis = '<si>'.repeat(HOSTILE_BYTES / 4);
    const one = '<row r="1"><c r="A1" t="s"><v>0</v></c></row>';
    expect(timeOf(() => openXlsx(xlsx({ S: rows })).sheet(0))).toBeLessThan(MAX_PARSE_MS);
    expect(timeOf(() => openXlsx(xlsx({ S: cells })).sheet(0))).toBeLessThan(MAX_PARSE_MS);
    expect(timeOf(() => openXlsx(xlsx({ S: one }, sis)).sheet(0))).toBeLessThan(MAX_PARSE_MS);
  });
  it('caches each inflated sheet and spends the total inflate budget once per part', () => {
    const zip = xlsx({ A: '<row r="1"><c r="A1"><v>1</v></c></row>'.repeat(40), B: '<row r="1"><c r="A1"><v>2</v></c></row>'.repeat(40) });
    const sizes = listZip(zip).map((e) => e.size);
    const all = sizes.reduce((a, b) => a + b, 0);
    const book = openXlsx(zip, all);
    expect(book.sheet(0)).toBe(book.sheet(0));
    expect(book.sheet(1).rows[0]).toEqual(['2']);
    expect(book.sheet(1)).toBe(book.sheet(1));
    const tight = openXlsx(zip, all - 1);
    tight.sheet(0);
    expect(() => tight.sheet(1)).toThrow(ZipError);
  });
  it('shows a sheet whose only cell lies beyond the column cap as non-empty, with the columns clipped', () => {
    const s = openXlsx(xlsx({ S: '<row r="1"><c r="AZ1" t="inlineStr"><is><t>x</t></is></c></row>' })).sheet(0);
    expect(s.totalCols).toBe(52);
    expect(s.rows).toEqual([[]]);
    expect(sheetColumns(s)).toBe(MAX_SHEET_COLS);
    expect(sheetColumns(openXlsx(xlsx({ S: '' })).sheet(0))).toBe(0);
  });
  it('throws on a zip that is not a workbook', () => {
    expect(() => openXlsx(buildZip({ 'a.txt': 'x' }))).toThrow();
  });
});

describe('parsePptx', () => {
  const A = 'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="p" xmlns:r="r"';
  const slide = (...paras: string[]) => `<p:sld ${A}><p:cSld><p:spTree><p:sp><p:txBody>${paras.map((p) => `<a:p>${p}</a:p>`).join('')}</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`;
  it('orders slides by the presentation, not by file name, and extracts paragraphs and line breaks', () => {
    const zip = buildZip({
      'ppt/presentation.xml': `<p:presentation ${A}><p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId1"/></p:sldIdLst></p:presentation>`,
      'ppt/_rels/presentation.xml.rels': '<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/><Relationship Id="rId2" Target="slides/slide2.xml"/></Relationships>',
      'ppt/slides/slide1.xml': slide('<a:r><a:t>Second </a:t></a:r><a:r><a:t>&amp; last</a:t></a:r>'),
      'ppt/slides/slide2.xml': slide('<a:r><a:t>Title</a:t></a:r>', '', '<a:r><a:t>line1</a:t></a:r><a:br/><a:r><a:t>line2</a:t></a:r>', '<a:pPr><a:tabLst><a:tab pos="1"/></a:tabLst></a:pPr><a:r><a:t>tabbed</a:t></a:r>'),
    });
    expect(parsePptx(zip)).toEqual([
      { number: 1, paragraphs: ['Title', 'line1\nline2', 'tabbed'] },
      { number: 2, paragraphs: ['Second & last'] },
    ]);
  });
  it('falls back to numeric file order without a presentation part, and keeps empty slides', () => {
    const zip = buildZip({ 'ppt/slides/slide10.xml': slide('<a:r><a:t>ten</a:t></a:r>'), 'ppt/slides/slide2.xml': slide(), 'ppt/slides/slideLayout1.xml': slide('<a:r><a:t>no</a:t></a:r>') });
    expect(parsePptx(zip)).toEqual([{ number: 1, paragraphs: [] }, { number: 2, paragraphs: ['ten'] }]);
  });
  it('enforces the total inflate budget across slides', () => {
    const filler = '<a:r><a:t>' + 'y'.repeat(2000) + '</a:t></a:r>';
    const zip = buildZip({ 'ppt/slides/slide1.xml': slide(filler), 'ppt/slides/slide2.xml': slide(filler), 'ppt/slides/slide3.xml': slide(filler) });
    expect(parsePptx(zip)).toHaveLength(3);
    expect(() => parsePptx(zip, 4000)).toThrow(/too large/);
  });
  it('parses 200 KB of unclosed paragraph tags in linear time', () => {
    const zip = buildZip({ 'ppt/slides/slide1.xml': `<p:sld ${A}>` + '<a:p>'.repeat(HOSTILE_BYTES / 5) + '</p:sld>' });
    expect(timeOf(() => parsePptx(zip))).toBeLessThan(MAX_PARSE_MS);
  });
  it('throws on a zip with no slides', () => {
    expect(() => parsePptx(buildZip({ 'a.txt': 'x' }))).toThrow();
  });
});

describe('parseDocx', () => {
  const W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"';
  it('extracts paragraphs with heading levels, tabs, breaks and entities; ignores paragraph properties', () => {
    const body = '<w:p><w:pPr><w:pStyle w:val="Heading2"/><w:tabs><w:tab w:val="left" w:pos="1"/></w:tabs></w:pPr><w:r><w:t>Intro</w:t></w:r></w:p>'
      + '<w:p/><w:p></w:p>'
      + '<w:p w:rsidR="1"><w:r><w:t xml:space="preserve">a </w:t></w:r><w:r><w:tab/><w:t>b</w:t></w:r><w:r><w:br/><w:t>c &lt;d&gt;</w:t></w:r></w:p>'
      + '<w:tbl><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>';
    const zip = buildZip({ 'word/document.xml': `<w:document ${W}><w:body>${body}</w:body></w:document>` });
    expect(parseDocx(zip)).toEqual([
      { text: 'Intro', heading: 2 },
      { text: 'a \tb\nc <d>', heading: 0 },
      { text: 'cell', heading: 0 },
    ]);
  });
  const doc = (body: string) => buildZip({ 'word/document.xml': `<w:document ${W} xmlns:mc="mc"><w:body>${body}</w:body></w:document>` });
  it('counts empty paragraphs toward the paragraph cap and stops there', () => {
    const empties = '<w:p></w:p>'.repeat(MAX_DOC_PARAGRAPHS);
    expect(parseDocx(doc(`<w:p><w:r><w:t>early</w:t></w:r></w:p>${empties}<w:p><w:r><w:t>late</w:t></w:r></w:p>`)).map((p) => p.text)).toEqual(['early']);
    expect(parseDocx(doc(`${empties.slice(0, -'<w:p></w:p>'.length)}<w:p><w:r><w:t>last</w:t></w:r></w:p>`)).map((p) => p.text)).toEqual(['last']);
  });
  it('drops tracked deletions', () => {
    const body = '<w:p><w:del><w:r><w:delText>gone</w:delText></w:r></w:del><w:r><w:t>kept</w:t></w:r></w:p>';
    expect(parseDocx(doc(body))).toEqual([{ text: 'kept', heading: 0 }]);
  });
  it('keeps an outer paragraph whole around a text box and skips the AlternateContent fallback', () => {
    const body = '<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>before </w:t></w:r><mc:AlternateContent><mc:Choice><w:r><w:pict><w:txbxContent><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:txbxContent></w:pict></w:r></mc:Choice>'
      + '<mc:Fallback><w:r><w:t>fallback</w:t></w:r></mc:Fallback></mc:AlternateContent><w:r><w:t>after</w:t></w:r></w:p>';
    expect(parseDocx(doc(body))).toEqual([{ text: 'before after', heading: 1 }]);
  });
  it('parses 200 KB of unclosed paragraph tags in linear time', () => {
    const zip = doc('<w:p>'.repeat(HOSTILE_BYTES / 5));
    expect(timeOf(() => parseDocx(zip))).toBeLessThan(MAX_PARSE_MS);
  });
  it('throws without word/document.xml', () => {
    expect(() => parseDocx(buildZip({ 'a.txt': 'x' }))).toThrow();
  });
});
