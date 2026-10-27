import { MAX_CSV_ROWS, parseCsv, parseCsvRows } from './csv';

// Ported from app/test/features/preview/csv_preview_test.dart.
describe('parseCsvRows', () => {
  it('parses simple rows', () => expect(parseCsvRows('a,b,c\n1,2,3\n')).toEqual([['a', 'b', 'c'], ['1', '2', '3']]));
  it('trims unquoted fields', () => expect(parseCsvRows('a, b , c\n')).toEqual([['a', 'b', 'c']]));
  it('keeps commas inside quotes', () => expect(parseCsvRows('name,note\nAda,"hello, world"\n')).toEqual([['name', 'note'], ['Ada', 'hello, world']]));
  it('unescapes doubled quotes', () => expect(parseCsvRows('note\n"she said ""hi"""\n')).toEqual([['note'], ['she said "hi"']]));
  it('keeps newlines inside quotes', () => expect(parseCsvRows('note\n"line one\nline two"\nafter\n')).toEqual([['note'], ['line one\nline two'], ['after']]));
  it('drops blank lines', () => expect(parseCsvRows('a,b\n\n1,2\n\n')).toEqual([['a', 'b'], ['1', '2']]));
  it('handles a missing trailing newline', () => expect(parseCsvRows('a,b\n1,2')).toEqual([['a', 'b'], ['1', '2']]));
  it('handles CRLF', () => expect(parseCsvRows('a,b\r\n1,2\r\n')).toEqual([['a', 'b'], ['1', '2']]));
  it('returns nothing for empty text', () => expect(parseCsvRows('')).toEqual([]));
  it('preserves padding inside quotes', () => expect(parseCsvRows('note\n"  padded  "\n')).toEqual([['note'], ['  padded  ']]));
});

test('parseCsv caps data rows but reports the true total', () => {
  const text = ['h', ...Array.from({ length: MAX_CSV_ROWS + 5 }, (_, i) => String(i))].join('\n');
  const d = parseCsv(text);
  expect(d.headers).toEqual(['h']);
  expect(d.rows).toHaveLength(MAX_CSV_ROWS);
  expect(d.totalRows).toBe(MAX_CSV_ROWS + 5);
  expect(parseCsv('')).toEqual({ headers: [], rows: [], totalRows: 0 });
});
