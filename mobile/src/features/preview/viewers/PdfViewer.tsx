import { Directory, File, Paths } from 'expo-file-system';
import { Image } from 'expo-image';
import { memo, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, PixelRatio, useWindowDimensions, View, type ViewToken } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { pdf } from '../../../core/native';
import { Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { Spacing } from '../../../design/tokens';
import { hashKey } from '../../explorer/thumbnails';
import { humanizeError } from '../../pairing/pairingService';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { MAX_IN_MEMORY_PREVIEW_BYTES, PAGER_CHIP_CLEARANCE, usePreviewFile } from '../previewFile';

const A4 = 1.414;
const localPath = (uri: string) => decodeURIComponent(uri.replace('file://', ''));

let pagesDir: Directory | null = null;
function pageDir(): Directory {
  if (!pagesDir) {
    pagesDir = new Directory(Paths.cache, 'pdfpages');
    pagesDir.create({ idempotent: true, intermediates: true });
  }
  return pagesDir;
}

/**
 * Port of PdfPreviewScreen on the platform PdfRenderer: pages render lazily to PNGs as they scroll
 * into view. Pages scroll vertically (Flutter paged horizontally) so they don't fight the file pager.
 */
export function PdfViewer({ host, entry }: { host: Host; entry: Entry }) {
  // Keyed, so a different file starts from its own first page instead of the previous file's count and position (audit F-175).
  return <PdfDocument key={`${host.id}:${entry.path}`} host={host} entry={entry} />;
}

function PdfDocument({ host, entry }: { host: Host; entry: Entry }) {
  const c = useScheme();
  const { state, retry } = usePreviewFile(host, entry, MAX_IN_MEMORY_PREVIEW_BYTES);
  const [pages, setPages] = useState<{ count: number } | { error: unknown } | null>(null);
  const [current, setCurrent] = useState(1);
  const uri = state.status === 'ready' ? state.uri : null;

  useEffect(() => {
    if (!uri) return;
    let live = true;
    pdf.pageCount(localPath(uri)).then(
      (count) => live && setPages({ count }),
      (error) => live && setPages({ error }),
    );
    return () => {
      live = false;
    };
  }, [uri]);

  if (state.status === 'tooLarge') return <PreviewTooLarge size={state.size} />;
  if (state.status === 'error') return <PreviewError message={`Could not load this PDF.\n${humanizeError(state.error)}`} onRetry={retry} />;
  if (!uri || !pages) return <PreviewLoading message="Loading PDF…" />;
  if ('error' in pages) return <PreviewError message={`Could not render this PDF.\n${humanizeError(pages.error)}`} />;

  return (
    <View style={{ flex: 1, backgroundColor: c.surfaceContainer }}>
      <Text variant="bodySmall" muted style={{ alignSelf: 'flex-end', paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }}>
        {`Page ${current} of ${pages.count}`}
      </Text>
      <FlatList
        data={Array.from({ length: pages.count }, (_, i) => i)}
        keyExtractor={(i) => String(i)}
        contentContainerStyle={{ padding: Spacing.sm, gap: Spacing.sm, paddingBottom: PAGER_CHIP_CLEARANCE }}
        initialNumToRender={2}
        windowSize={5}
        maxToRenderPerBatch={2}
        viewabilityConfig={{ itemVisiblePercentThreshold: 50 }}
        onViewableItemsChanged={({ viewableItems }: { viewableItems: ViewToken<number>[] }) => {
          const first = viewableItems[0]?.item;
          if (typeof first === 'number') setCurrent(first + 1);
        }}
        renderItem={({ item }) => <PdfPage src={uri} index={item} />}
      />
    </View>
  );
}

const PdfPage = memo(function PdfPage({ src, index }: { src: string; index: number }) {
  const { width } = useWindowDimensions();
  const w = width - Spacing.sm * 2;
  const px = Math.min(Math.round(w * PixelRatio.get()), 2048);
  const [page, setPage] = useState<{ uri: string; aspect: number } | { error: string } | null>(null);

  useEffect(() => {
    let live = true;
    const out = new File(pageDir(), `${hashKey(src)}-${index}-${px}.png`);
    pdf.renderPage(localPath(src), index, px, localPath(out.uri)).then(
      (s) => live && setPage({ uri: `${out.uri}?v=${px}`, aspect: s.height / s.width }),
      (e) => live && setPage({ error: humanizeError(e) }),
    );
    return () => {
      live = false;
    };
  }, [src, index, px]);

  const aspect = page && 'aspect' in page ? page.aspect : A4;
  return (
    <View style={{ width: w, height: w * aspect, backgroundColor: '#FFFFFF', alignItems: 'center', justifyContent: 'center' }} accessibilityLabel={`Page ${index + 1}`}>
      {page === null ? <ActivityIndicator /> : 'error' in page ? <Text style={{ color: '#B3261E', padding: Spacing.md }}>{page.error}</Text> : <Image source={{ uri: page.uri }} style={{ width: w, height: w * aspect }} contentFit="contain" />}
    </View>
  );
});
