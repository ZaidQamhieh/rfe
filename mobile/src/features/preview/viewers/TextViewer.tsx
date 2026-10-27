import { File } from 'expo-file-system';
import { useEffect, useRef, useState } from 'react';
import { FlatList, View } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Spacing } from '../../../design/tokens';
import { humanizeError } from '../../pairing/pairingService';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { MAX_IN_MEMORY_PREVIEW_BYTES, PAGER_CHIP_CLEARANCE, usePreviewFile } from '../previewFile';
import { NotTextError, decodeAsText } from '../textDecode';
import { useFollowText } from '../useFollowText';

export const MONO = { fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 18.2 } as const;

type Loaded = { status: 'idle' } | { status: 'ready'; text: string } | { status: 'error'; error: unknown };

/** Fetches [entry] and decodes it as strict UTF-8 (shared by the text, markdown and CSV viewers). */
export function usePreviewText(host: Host, entry: Entry) {
  const { state, retry } = usePreviewFile(host, entry, MAX_IN_MEMORY_PREVIEW_BYTES);
  const [loaded, setLoaded] = useState<Loaded>({ status: 'idle' });
  const uri = state.status === 'ready' ? state.uri : null;
  useEffect(() => {
    if (!uri) return;
    let live = true;
    new File(uri).bytes().then(
      (b) => {
        if (!live) return;
        try {
          setLoaded({ status: 'ready', text: decodeAsText(b) });
        } catch (error) {
          setLoaded({ status: 'error', error });
        }
      },
      (error) => live && setLoaded({ status: 'error', error }),
    );
    return () => {
      live = false;
    };
  }, [uri]);
  return { file: state, text: loaded, retry };
}

/** Shared loading / too-large / error body for the text-based viewers; null once the text is ready. */
export function textStateView(file: ReturnType<typeof usePreviewText>['file'], text: Loaded, retry: () => void, loadingLabel: string) {
  if (file.status === 'tooLarge') return <PreviewTooLarge size={file.size} />;
  if (file.status === 'error') return <PreviewError message={`Could not load this file.\n${humanizeError(file.error)}`} onRetry={retry} />;
  if (text.status === 'error') {
    return text.error instanceof NotTextError ? <PreviewError message={text.error.message} /> : <PreviewError message={`Could not load this file.\n${humanizeError(text.error)}`} onRetry={retry} />;
  }
  if (text.status !== 'ready') return <PreviewLoading message={loadingLabel} />;
  return null;
}

/** Port of TextPreviewScreen's body: monospace, selectable, optional 1-based line-number gutter. */
export function TextViewer({ host, entry, showLineNumbers, follow = false, onText }: { host: Host; entry: Entry; showLineNumbers: boolean; follow?: boolean; onText?: (text: string | null) => void }) {
  const c = useScheme();
  const { file, text, retry } = usePreviewText(host, entry);
  const ready = text.status === 'ready' ? text.text : null;
  useEffect(() => onText?.(ready), [ready, onText]);
  // Follow mode: lines appended on the host after this copy was fetched, added at the end as they arrive.
  const baseBytes = file.status === 'ready' && ready !== null ? (new File(file.uri).size ?? null) : null;
  const { extra, stopped } = useFollowText(host, entry, follow, baseBytes);
  const lines = ready === null ? [] : (ready + extra).split('\n');
  const gutter = String(lines.length).length;
  const list = useRef<FlatList<string>>(null);
  const grown = extra.length;
  // The list is virtualized, so one scrollToEnd only reaches the end of the rows measured so far (a 2000-line log
  // stopped near line 200). While following, keep going to the end as more rows are measured; turning Follow on
  // jumps to the tail at once.
  useEffect(() => {
    if (follow) list.current?.scrollToEnd({ animated: false });
  }, [follow, grown]);

  const pending = textStateView(file, text, retry, 'Loading text…');
  if (pending) return pending;
  if (ready === '') {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Text color={c.outline}>(empty file)</Text>
      </View>
    );
  }
  return (
    <FlatList
      ref={list}
      data={lines}
      keyExtractor={(_, i) => String(i)}
      contentContainerStyle={{ padding: Spacing.md, paddingBottom: PAGER_CHIP_CLEARANCE }}
      onContentSizeChange={() => {
        if (follow) list.current?.scrollToEnd({ animated: false });
      }}
      ListFooterComponent={stopped ? <Text style={{ color: c.error, marginTop: Spacing.md }}>{stopped}</Text> : null}
      initialNumToRender={60}
      windowSize={11}
      renderItem={({ item, index }) => (
        <View style={{ flexDirection: 'row' }}>
          {showLineNumbers && (
            <Text style={[MONO, { color: c.outline, textAlign: 'right', width: gutter * 8 + 4, marginRight: Spacing.md }]} selectable={false}>
              {index + 1}
            </Text>
          )}
          <Text selectable style={[MONO, { flex: 1, color: c.onSurface }]}>{item.length ? item : ' '}</Text>
        </View>
      )}
    />
  );
}
