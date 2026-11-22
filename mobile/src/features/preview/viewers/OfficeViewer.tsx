import { File } from 'expo-file-system';
import { useEffect, useMemo, useState } from 'react';
import { FlatList, ScrollView, View } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { humanizeError } from '../../../core/humanizeError';
import { Pressable, Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Spacing } from '../../../design/tokens';
import { t } from '../../../i18n';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { openXlsx, parseDocx, parsePptx, sheetColumns, type DocParagraph, type SlideText, type XlsxBook } from '../ooxml';
import { MAX_OFFICE_PREVIEW_BYTES, PAGER_CHIP_CLEARANCE, usePreviewFile } from '../previewFile';
import { ZipError } from '../zip';
import { EmptyFileNotice } from './TextViewer';

const COL_WIDTH = 120;
/** Spreadsheet column names: 0 -> A, 25 -> Z, 26 -> AA. */
const columnName = (i: number): string => (i >= 26 ? columnName(Math.floor(i / 26) - 1) : '') + String.fromCharCode(65 + (i % 26));

type Parsed = { kind: 'xlsx'; book: XlsxBook } | { kind: 'pptx'; slides: SlideText[] } | { kind: 'docx'; paragraphs: DocParagraph[] };
type Loaded = { status: 'idle' } | { status: 'ready'; parsed: Parsed } | { status: 'error'; error: unknown };

/** Reads and parses the OOXML bytes of the fetched file (offline: nothing here touches the network). */
async function parseOffice(uri: string, kind: Parsed['kind']): Promise<Parsed> {
  const bytes = await new File(uri).bytes();
  if (kind === 'xlsx') return { kind, book: openXlsx(bytes) };
  if (kind === 'pptx') return { kind, slides: parsePptx(bytes) };
  return { kind, paragraphs: parseDocx(bytes) };
}

/** Read-only preview of a .xlsx (sheet tabs and a table), .pptx (text per slide) or .docx (paragraphs). */
export function OfficeViewer({ host, entry, kind }: { host: Host; entry: Entry; kind: Parsed['kind'] }) {
  const { state, retry } = usePreviewFile(host, entry, MAX_OFFICE_PREVIEW_BYTES);
  const [loaded, setLoaded] = useState<Loaded>({ status: 'idle' });
  const uri = state.status === 'ready' ? state.uri : null;
  useEffect(() => {
    if (!uri) return;
    let live = true;
    parseOffice(uri, kind).then(
      (parsed) => live && setLoaded({ status: 'ready', parsed }),
      (error) => live && setLoaded({ status: 'error', error }),
    );
    return () => {
      live = false;
    };
  }, [uri, kind]);
  if (state.status === 'tooLarge') return <PreviewTooLarge size={state.size} />;
  if (state.status === 'error') return <PreviewError message={t('couldNotLoadFile', { error: humanizeError(state.error) })} onRetry={retry} />;
  if (loaded.status === 'error') {
    const message = loaded.error instanceof ZipError ? loaded.error.message : humanizeError(loaded.error);
    return <PreviewError message={t('officeReadFailed', { error: message })} onRetry={retry} />;
  }
  if (loaded.status !== 'ready') return <PreviewLoading message={t('loadingOffice')} />;
  const p = loaded.parsed;
  if (p.kind === 'xlsx') return <SheetView book={p.book} />;
  if (p.kind === 'pptx') return <SlidesView slides={p.slides} />;
  return <DocView paragraphs={p.paragraphs} />;
}

/** Sheet tabs on top and the selected sheet as a two-way scrolling grid with column letters. */
function SheetView({ book }: { book: XlsxBook }) {
  const c = useScheme();
  const [index, setIndex] = useState(0);
  const result = useMemo(() => {
    try {
      return { sheet: book.sheet(index), error: null };
    } catch (error) {
      return { sheet: null, error };
    }
  }, [book, index]);
  const sheet = result.sheet;
  const cols = sheet ? sheetColumns(sheet) : 0;
  const width = cols * COL_WIDTH;
  const cell = (v: string, header: boolean, key: number) => (
    <Text key={key} numberOfLines={header ? 1 : 3} style={{ width: COL_WIDTH, paddingHorizontal: 10, paddingVertical: Spacing.sm, fontSize: 13, fontFamily: header ? FontFamily.semibold : FontFamily.regular }}>
      {v}
    </Text>
  );
  return (
    <View style={{ flex: 1 }}>
      <ScrollView horizontal style={{ flexGrow: 0 }} contentContainerStyle={{ padding: Spacing.sm, gap: Spacing.sm }}>
        {book.sheetNames.map((name, i) => (
          <Pressable key={i} onPress={() => setIndex(i)} accessibilityLabel={name} style={{ paddingHorizontal: Spacing.md, paddingVertical: Spacing.xs, borderRadius: 16, backgroundColor: i === index ? c.primaryContainer : c.surfaceContainerHighest }}>
            <Text numberOfLines={1} variant="labelLarge" color={i === index ? c.onPrimaryContainer : undefined}>{name}</Text>
          </Pressable>
        ))}
      </ScrollView>
      {result.error || !sheet ? (
        <PreviewError message={t('officeReadFailed', { error: result.error instanceof ZipError ? result.error.message : humanizeError(result.error) })} />
      ) : cols === 0 ? (
        <EmptyFileNotice />
      ) : (
        <>
          {sheet.totalRows > sheet.rows.length || sheet.totalCols > cols ? (
            <Text variant="bodySmall" muted style={{ paddingHorizontal: Spacing.md }}>
              {t('sheetTruncated', { rows: sheet.rows.length, totalRows: sheet.totalRows, cols, totalCols: sheet.totalCols })}
            </Text>
          ) : null}
          <ScrollView horizontal contentContainerStyle={{ padding: Spacing.md }}>
            <View style={{ width, flex: 1 }}>
              <View style={{ flexDirection: 'row', backgroundColor: c.surfaceContainerHighest }}>{Array.from({ length: cols }, (_, i) => cell(columnName(i), true, i))}</View>
              <FlatList
                data={sheet.rows}
                keyExtractor={(_, i) => String(i)}
                style={{ width }}
                contentContainerStyle={{ paddingBottom: PAGER_CHIP_CLEARANCE }}
                initialNumToRender={40}
                renderItem={({ item }) => (
                  <View style={{ flexDirection: 'row', borderBottomWidth: 1, borderColor: c.outlineVariant, minHeight: 28 }}>{Array.from({ length: cols }, (_, i) => cell(item[i] ?? '', false, i))}</View>
                )}
              />
            </View>
          </ScrollView>
        </>
      )}
    </View>
  );
}

/** One card per slide: its number and the text found on it. */
function SlidesView({ slides }: { slides: SlideText[] }) {
  const c = useScheme();
  return (
    <FlatList
      data={slides}
      keyExtractor={(s) => String(s.number)}
      contentContainerStyle={{ padding: Spacing.md, gap: Spacing.md, paddingBottom: PAGER_CHIP_CLEARANCE }}
      renderItem={({ item }) => (
        <View style={{ padding: Spacing.md, borderRadius: 12, borderWidth: 1, borderColor: c.outlineVariant, gap: Spacing.sm }}>
          <Text variant="labelMedium" muted>{t('slideLabel', { number: item.number })}</Text>
          {item.paragraphs.length === 0 ? (
            <Text muted>{t('slideNoText')}</Text>
          ) : (
            item.paragraphs.map((p, i) => (
              <Text key={i} selectable style={i === 0 ? { fontFamily: FontFamily.semibold, fontSize: 16 } : undefined}>{p}</Text>
            ))
          )}
        </View>
      )}
    />
  );
}

const HEADING_SIZE = [0, 24, 21, 19, 17, 16, 16, 16, 16, 16];

/** The document's paragraphs in order, headings larger. */
function DocView({ paragraphs }: { paragraphs: DocParagraph[] }) {
  if (paragraphs.length === 0) return <EmptyFileNotice />;
  return (
    <FlatList
      data={paragraphs}
      keyExtractor={(_, i) => String(i)}
      contentContainerStyle={{ padding: Spacing.md, gap: Spacing.sm, paddingBottom: PAGER_CHIP_CLEARANCE }}
      renderItem={({ item }) => (
        <Text selectable style={item.heading > 0 ? { fontFamily: FontFamily.semibold, fontSize: HEADING_SIZE[item.heading], lineHeight: HEADING_SIZE[item.heading] * 1.3 } : { lineHeight: 22 }}>
          {item.text}
        </Text>
      )}
    />
  );
}
