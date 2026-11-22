import { ZipError, listZip, readZipEntry, type ZipEntry } from './zip';

// Pure readers for Office Open XML files (.xlsx, .pptx, .docx): they take the file's bytes and return plain
// text structures for the preview. They read only what a viewer shows (cell values, slide text, paragraphs).
// Formatting, formulas, images, charts, text boxes, tracked deletions and number/date formats are ignored,
// so dates in a sheet appear as serials. XML is read with one forward scan ([scanXml]) and no backtracking
// patterns, so hostile input (for example thousands of unclosed tags) costs time linear in its size.

/** Rows and columns of a sheet that are kept for display; [SheetData.totalRows] and [SheetData.totalCols] keep the real size. */
export const MAX_SHEET_ROWS = 500;
export const MAX_SHEET_COLS = 50;
const MAX_SHEETS = 200;
const MAX_SLIDES = 500;
/** Paragraphs read from a document, empty ones included. */
export const MAX_DOC_PARAGRAPHS = 20_000;
/** Largest single XML part that is inflated. */
const MAX_PART_BYTES = 40 * 1024 * 1024;
/** Total bytes inflated from one file over its whole life (a zip bomb cannot exhaust memory through many parts). */
const MAX_TOTAL_INFLATED_BYTES = 64 * 1024 * 1024;

export type SheetData = { rows: string[][]; totalRows: number; totalCols: number };
export type XlsxBook = { sheetNames: string[]; sheet: (index: number) => SheetData };
export type SlideText = { number: number; paragraphs: string[] };
export type DocParagraph = { text: string; heading: number };

/** Number of columns to draw for [sheet]: its real width clipped to [MAX_SHEET_COLS]; 0 when it has no cells at all. */
export function sheetColumns(sheet: SheetData): number {
  return sheet.totalRows === 0 ? 0 : Math.min(sheet.totalCols, MAX_SHEET_COLS);
}

const NAMED_ENTITIES: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" };

/** Replaces the XML character references (named and numeric) in [s] with the characters they stand for. */
function decodeXml(s: string): string {
  if (!s.includes('&')) return s;
  return s.replace(/&(#x[0-9a-fA-F]+|#[0-9]+|amp|lt|gt|quot|apos);/g, (all, ref: string) => {
    if (ref[0] !== '#') return NAMED_ENTITIES[ref];
    const code = ref[1] === 'x' ? parseInt(ref.slice(2), 16) : parseInt(ref.slice(1), 10);
    return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : all;
  });
}

type XmlEvent = 'open' | 'close' | 'self' | 'text';

/**
 * Walks [xml] once from start to end, calling [on] for each tag (name without namespace prefix, attribute text)
 * and each run of text (name '', the text in the third argument). Comments and declarations are skipped; stops
 * early when [on] returns false or the input ends inside a tag. Every step moves forward, so cost is linear.
 */
function scanXml(xml: string, on: (kind: XmlEvent, name: string, data: string) => boolean | void): void {
  const n = xml.length;
  let pos = 0;
  while (pos < n) {
    const lt = xml.indexOf('<', pos);
    if (lt < 0) {
      on('text', '', xml.slice(pos));
      return;
    }
    if (lt > pos && on('text', '', xml.slice(pos, lt)) === false) return;
    if (xml.startsWith('<!--', lt)) {
      const end = xml.indexOf('-->', lt + 4);
      if (end < 0) return;
      pos = end + 3;
      continue;
    }
    if (xml.startsWith('<![CDATA[', lt)) {
      const end = xml.indexOf(']]>', lt + 9);
      if (on('text', '', xml.slice(lt + 9, end < 0 ? n : end)) === false || end < 0) return;
      pos = end + 3;
      continue;
    }
    const gt = xml.indexOf('>', lt + 1);
    if (gt < 0) return;
    pos = gt + 1;
    let body = xml.slice(lt + 1, gt);
    if (body[0] === '?' || body[0] === '!') continue;
    const closing = body[0] === '/';
    if (closing) body = body.slice(1);
    const selfClosing = !closing && body.endsWith('/');
    if (selfClosing) body = body.slice(0, -1);
    let i = 0;
    while (i < body.length && body.charCodeAt(i) > 32) i++;
    const qname = body.slice(0, i);
    const name = qname.slice(qname.lastIndexOf(':') + 1);
    if (on(closing ? 'close' : selfClosing ? 'self' : 'open', name, body.slice(i)) === false) return;
  }
}

/** Value of attribute [name] (a name or a regex source such as "(?:\\w+:)?val") in the attribute text of a tag, or undefined. */
function attr(attrs: string, name: string): string | undefined {
  const m = new RegExp(`(?:^|\\s)${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)')`).exec(attrs);
  const raw = m?.[1] ?? m?.[2];
  return raw === undefined ? undefined : decodeXml(raw);
}

/** Value of the namespaced relationship-id attribute (r:id, whatever the prefix is) of a tag. */
function relId(attrs: string): string | undefined {
  return attr(attrs, '\\w+:id');
}

/** A zip opened for reading parts by name, with a shared budget of inflated bytes. */
type Package = { has: (name: string) => boolean; text: (name: string) => string; names: string[] };

function openPackage(bytes: Uint8Array, totalBudget: number): Package {
  const entries = new Map<string, ZipEntry>(listZip(bytes).map((e) => [e.name, e]));
  let remaining = totalBudget;
  return {
    has: (name) => entries.has(name),
    names: [...entries.keys()],
    text: (name) => {
      const e = entries.get(name);
      if (!e) throw new ZipError(`Missing part ${name}`);
      const data = readZipEntry(bytes, e, Math.min(MAX_PART_BYTES, remaining));
      remaining -= data.length;
      return new TextDecoder('utf-8').decode(data);
    },
  };
}

/** Resolves a relationship target (relative to [baseDir], or absolute with a leading slash) to a package part name. */
function resolveTarget(baseDir: string, target: string): string {
  const parts = (target.startsWith('/') ? target.slice(1) : baseDir + target).split('/');
  const out: string[] = [];
  for (const p of parts) {
    if (p === '..') out.pop();
    else if (p !== '.' && p !== '') out.push(p);
  }
  return out.join('/');
}

/** Relationship id -> part name, from the .rels part that belongs to [partDir]/[file]. Empty when the part is absent. */
function readRels(pkg: Package, partDir: string, file: string): Map<string, string> {
  const rels = new Map<string, string>();
  const name = `${partDir}_rels/${file}.rels`;
  if (!pkg.has(name)) return rels;
  scanXml(pkg.text(name), (kind, tag, data) => {
    if (tag !== 'Relationship' || kind === 'close' || kind === 'text') return;
    const id = attr(data, 'Id');
    const target = attr(data, 'Target');
    if (id !== undefined && target !== undefined && attr(data, 'TargetMode') !== 'External') rels.set(id, resolveTarget(partDir, target));
  });
  return rels;
}

/** Zero-based column of a cell reference such as "AB12" ("AA" is 26); -1 when it has no letters. */
function columnOf(ref: string): number {
  let col = 0;
  let letters = 0;
  for (const ch of ref) {
    const c = ch.charCodeAt(0);
    if (c < 65 || c > 90) break;
    col = col * 26 + (c - 64);
    letters++;
  }
  return letters ? col - 1 : -1;
}

/** The strings of sharedStrings.xml in order: each <si>'s <t> text, rich runs joined, phonetic hints (<rPh>) skipped. */
function readSharedStrings(xml: string): string[] {
  const out: string[] = [];
  let cur: string | null = null;
  let inT = false;
  let phonetic = 0;
  const flush = () => {
    if (cur !== null) out.push(decodeXml(cur));
    cur = null;
    inT = false;
    phonetic = 0;
  };
  scanXml(xml, (kind, name, data) => {
    if (kind === 'text') {
      if (cur !== null && inT && phonetic === 0) cur += data;
    } else if (name === 'si') {
      flush();
      if (kind === 'open') cur = '';
      else if (kind === 'self') out.push('');
    } else if (name === 't') inT = kind === 'open';
    else if (name === 'rPh') phonetic = kind === 'open' ? phonetic + 1 : kind === 'close' ? Math.max(0, phonetic - 1) : phonetic;
  });
  flush();
  return out;
}

/** Display text of one cell from its type attribute, <v> value (undefined when absent) and inline string. */
function cellText(type: string | undefined, value: string | undefined, inline: string, shared: string[]): string {
  if (type === 'inlineStr') return decodeXml(inline);
  if (value === undefined) return '';
  if (type === 's') return shared[parseInt(value, 10)] ?? '';
  if (type === 'b') return value.trim() === '1' ? 'TRUE' : 'FALSE';
  return decodeXml(value);
}

/**
 * Reads the cells of one worksheet part into a capped grid; blanks fill skipped cells, rows and columns.
 * A row or cell tag that is never closed ends at the next one, as in a forgiving reader.
 */
function readSheet(xml: string, shared: string[]): SheetData {
  const rows: string[][] = [];
  let totalRows = 0;
  let totalCols = 0;
  let rowNo = 0;
  let rowOpen = false;
  let keep = false;
  let cells: string[] = [];
  let colNo = -1;
  let cellOpen = false;
  let cellCol = 0;
  let cellType: string | undefined;
  let value: string | undefined;
  let inline = '';
  let inV = false;
  let inT = false;
  let inIs = false;
  let phonetic = 0;
  const endCell = () => {
    if (!cellOpen) return;
    cellOpen = false;
    inV = inT = inIs = false;
    phonetic = 0;
    if (keep && cellCol < MAX_SHEET_COLS) {
      while (cells.length < cellCol) cells.push('');
      cells[cellCol] = cellText(cellType, value, inline, shared);
    }
  };
  const endRow = () => {
    endCell();
    if (!rowOpen) return;
    rowOpen = false;
    if (keep) {
      while (rows.length < rowNo - 1) rows.push([]);
      rows[rowNo - 1] = cells;
    }
  };
  scanXml(xml, (kind, name, data) => {
    if (kind === 'text') {
      if (!cellOpen) return;
      if (inV) value = (value ?? '') + data;
      else if (inIs && inT && phonetic === 0) inline += data;
      return;
    }
    switch (name) {
      case 'row': {
        endRow();
        if (kind === 'close') return;
        const given = parseInt(attr(data, 'r') ?? '', 10);
        rowNo = Number.isFinite(given) && given > 0 ? given : rowNo + 1;
        totalRows = Math.max(totalRows, rowNo);
        keep = rowNo <= MAX_SHEET_ROWS;
        cells = [];
        colNo = -1;
        rowOpen = true;
        if (kind === 'self') endRow();
        return;
      }
      case 'c': {
        endCell();
        if (kind === 'close' || !rowOpen) return;
        const ref = columnOf(attr(data, 'r') ?? '');
        colNo = ref >= 0 ? ref : colNo + 1;
        totalCols = Math.max(totalCols, colNo + 1);
        cellOpen = true;
        cellCol = colNo;
        cellType = attr(data, 't');
        value = undefined;
        inline = '';
        if (kind === 'self') endCell();
        return;
      }
      case 'v':
        inV = kind === 'open';
        return;
      case 'is':
        inIs = kind === 'open';
        return;
      case 't':
        inT = kind === 'open';
        return;
      case 'rPh':
        phonetic = kind === 'open' ? phonetic + 1 : kind === 'close' ? Math.max(0, phonetic - 1) : phonetic;
    }
  });
  endRow();
  return { rows, totalRows, totalCols };
}

/**
 * Opens an .xlsx workbook: sheet names in tab order up front, and each sheet's cells on demand (shared strings
 * are read once, each sheet is inflated and parsed once and then kept). [totalBudget] bounds the bytes inflated
 * over the book's life. Throws [ZipError] when the bytes are not a workbook or the budget is spent.
 */
export function openXlsx(bytes: Uint8Array, totalBudget = MAX_TOTAL_INFLATED_BYTES): XlsxBook {
  const pkg = openPackage(bytes, totalBudget);
  if (!pkg.has('xl/workbook.xml')) throw new ZipError('Not an Excel workbook');
  const rels = readRels(pkg, 'xl/', 'workbook.xml');
  const sheets: { name: string; part: string | undefined }[] = [];
  scanXml(pkg.text('xl/workbook.xml'), (kind, tag, data) => {
    if (tag !== 'sheet' || kind === 'close' || kind === 'text') return;
    const id = relId(data);
    sheets.push({ name: attr(data, 'name') ?? `Sheet${sheets.length + 1}`, part: id === undefined ? undefined : rels.get(id) });
    return sheets.length < MAX_SHEETS;
  });
  let shared: string[] | null = null;
  const cache = new Map<number, SheetData>();
  return {
    sheetNames: sheets.map((s) => s.name),
    sheet: (index) => {
      const hit = cache.get(index);
      if (hit) return hit;
      const part = sheets[index]?.part;
      if (!part || !pkg.has(part)) throw new ZipError('Sheet data is missing');
      shared ??= pkg.has('xl/sharedStrings.xml') ? readSharedStrings(pkg.text('xl/sharedStrings.xml')) : [];
      const data = readSheet(pkg.text(part), shared);
      cache.set(index, data);
      return data;
    },
  };
}

/**
 * Calls [onParagraph] with the text and style id of each paragraph (<a:p> or <w:p>) in order; it returns false to
 * stop. Text comes from <t> runs plus tabs and line breaks; paragraph properties, tracked deletions (delText),
 * text boxes (txbxContent, whose own paragraphs must not cut the outer one) and AlternateContent fallbacks are skipped.
 */
function scanParagraphs(xml: string, onParagraph: (text: string, style: string) => boolean | void): void {
  let open = false;
  let text = '';
  let style = '';
  let inT = false;
  let inPPr = false;
  let skipName = '';
  let skipDepth = 0;
  let stopped = false;
  const finish = () => {
    if (!open) return;
    open = false;
    const t = decodeXml(text);
    const s = style;
    text = style = '';
    inT = inPPr = false;
    if (onParagraph(t, s) === false) stopped = true;
  };
  scanXml(xml, (kind, name, data) => {
    if (skipDepth > 0) {
      if (name === skipName) skipDepth += kind === 'open' ? 1 : kind === 'close' ? -1 : 0;
      return;
    }
    if (kind === 'text') {
      if (open && inT) text += data;
      return !stopped;
    }
    switch (name) {
      case 'txbxContent':
      case 'Fallback':
        if (kind === 'open') {
          skipName = name;
          skipDepth = 1;
          inT = false;
        }
        break;
      case 'p':
        if (kind === 'open') {
          if (!open) open = true;
        } else if (kind === 'self') {
          if (!open && onParagraph('', '') === false) stopped = true;
        } else finish();
        break;
      case 'pPr':
        if (open && kind !== 'self') inPPr = kind === 'open';
        break;
      case 'pStyle':
        if (open && inPPr && kind !== 'close') style = attr(data, '(?:\\w+:)?val') ?? style;
        break;
      case 't':
        if (open && !inPPr) inT = kind === 'open';
        break;
      case 'tab':
        if (open && !inPPr && kind !== 'close') text += '\t';
        break;
      case 'br':
      case 'cr':
        if (open && !inPPr && kind !== 'close') text += '\n';
        break;
    }
    return !stopped;
  });
  if (!stopped) finish();
}

/** Slide part names in presentation order (falling back to file-name order when the presentation part cannot be followed). */
function slideParts(pkg: Package): string[] {
  if (pkg.has('ppt/presentation.xml')) {
    const rels = readRels(pkg, 'ppt/', 'presentation.xml');
    const ordered: string[] = [];
    scanXml(pkg.text('ppt/presentation.xml'), (kind, tag, data) => {
      if (tag !== 'sldId' || kind === 'close' || kind === 'text') return;
      const part = rels.get(relId(data) ?? '');
      if (part && pkg.has(part)) ordered.push(part);
      return ordered.length < MAX_SLIDES;
    });
    if (ordered.length) return ordered;
  }
  const num = (n: string) => parseInt(/slide(\d+)\.xml$/.exec(n)![1], 10);
  return pkg.names.filter((n) => /^ppt\/slides\/slide\d+\.xml$/.test(n)).sort((a, b) => num(a) - num(b));
}

/**
 * Text of each slide of a .pptx in presentation order, one entry per paragraph (empty ones dropped). Throws
 * [ZipError] when there are no slides or more than [totalBudget] bytes would be inflated.
 */
export function parsePptx(bytes: Uint8Array, totalBudget = MAX_TOTAL_INFLATED_BYTES): SlideText[] {
  const pkg = openPackage(bytes, totalBudget);
  const parts = slideParts(pkg).slice(0, MAX_SLIDES);
  if (parts.length === 0) throw new ZipError('Not a PowerPoint presentation');
  return parts.map((part, i) => {
    const paragraphs: string[] = [];
    scanParagraphs(pkg.text(part), (text) => {
      if (text.trim() !== '') paragraphs.push(text);
    });
    return { number: i + 1, paragraphs };
  });
}

/**
 * Paragraphs of a .docx body in order (table cells included, empty ones dropped); heading is 1-9 for Title/Heading
 * styles, else 0. Reading stops after [MAX_DOC_PARAGRAPHS] paragraphs, empty ones counted.
 */
export function parseDocx(bytes: Uint8Array, totalBudget = MAX_TOTAL_INFLATED_BYTES): DocParagraph[] {
  const pkg = openPackage(bytes, totalBudget);
  if (!pkg.has('word/document.xml')) throw new ZipError('Not a Word document');
  const out: DocParagraph[] = [];
  let seen = 0;
  scanParagraphs(pkg.text('word/document.xml'), (text, style) => {
    seen++;
    if (text.trim() !== '') {
      const level = /^heading\s*([1-9])$/i.exec(style)?.[1];
      out.push({ text, heading: level ? parseInt(level, 10) : style.toLowerCase() === 'title' ? 1 : 0 });
    }
    return seen < MAX_DOC_PARAGRAPHS;
  });
  return out;
}
