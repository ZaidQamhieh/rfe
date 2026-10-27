// Port of parseCsvRows / _parseCsv (csv_preview.dart).

export const MAX_CSV_ROWS = 1000;

/**
 * RFC 4180-ish: a field opening with `"` may hold commas, CR/LF and `""` escapes. Unquoted fields
 * are trimmed; quoted content is kept exactly. Blank lines are dropped.
 */
export function parseCsvRows(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let inQuotes = false;
  let fieldQuoted = false;
  const endField = () => {
    row.push(fieldQuoted ? field : field.trim());
    field = '';
    fieldQuoted = false;
  };
  const endRow = () => {
    endField();
    if (!(row.length === 1 && row[0] === '')) rows.push(row);
    row = [];
  };
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        field += ch;
      }
      continue;
    }
    if (ch === '"' && field.length === 0) {
      inQuotes = true;
      fieldQuoted = true;
    } else if (ch === ',') endField();
    else if (ch === '\n') endRow();
    else if (ch !== '\r') field += ch;
  }
  if (field.length > 0 || row.length > 0) endRow();
  return rows;
}

export type CsvData = { headers: string[]; rows: string[][]; totalRows: number };

/** First row is the header; data rows are capped at [MAX_CSV_ROWS] (totalRows is the uncapped count). */
export function parseCsv(text: string): CsvData {
  const rows = parseCsvRows(text);
  if (rows.length === 0) return { headers: [], rows: [], totalRows: 0 };
  const data = rows.slice(1);
  return { headers: rows[0], rows: data.slice(0, MAX_CSV_ROWS), totalRows: data.length };
}
