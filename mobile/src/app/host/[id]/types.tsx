import { Stack, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, View } from 'react-native';

import type { AgentClient } from '../../../core/api/agentClient';
import { formatSize } from '../../../core/format';
import { mix } from '../../../design/color';
import { EmptyState, ErrorRetry, GroupedCard, Loading, Pressable, Text } from '../../../design/components';
import { LumenSize, LumenType } from '../../../design/lumen';
import { useRoles, useScheme } from '../../../design/theme';
import { FontFamily, Spacing } from '../../../design/tokens';
import { t } from '../../../i18n';
import { humanizeError } from '../../../features/pairing/pairingService';
import { useHostById } from '../../../features/hosts/useHostById';
import { aggregateByExtension, bucketBySize, categoryFor, extensionOf, sortedBySize, type TypeAggregation } from '../../../features/storage/storageLogic';
import { clientForHost } from '../../../services';

/** Walks every listing page of [path] recursively, reporting the running file count. */
async function scan(client: AgentClient, path: string, out: { ext: string; size: number }[], onProgress: (n: number) => void, cancelled: () => boolean): Promise<void> {
  let cursor: string | undefined;
  do {
    if (cancelled()) return;
    const page = await client.list(path, { cursor, limit: 200 });
    for (const e of page.entries) {
      if (e.isDir) await scan(client, e.path, out, onProgress, cancelled);
      else out.push({ ext: extensionOf(e.name), size: e.size ?? 0 });
    }
    onProgress(out.length);
    cursor = page.nextCursor;
  } while (cursor);
}

/** File-type distribution for the whole computer: a four-block map by size, then a per-extension table. */
export default function TypeMap() {
  const { id, path: startPath } = useLocalSearchParams<{ id: string; path?: string }>();
  const host = useHostById(id);
  const c = useScheme();
  const roles = useRoles();
  const blockTones = [c.primary, roles.photo, roles.warn, roles.route];
  const categoryTone: Record<ReturnType<typeof categoryFor>, string> = { image: roles.photo, video: roles.photo, audio: roles.route, document: roles.doc, archive: roles.warn, code: roles.transfer, other: c.onSurfaceVariant };
  const [result, setResult] = useState<TypeAggregation | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [scanned, setScanned] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const [run, setRun] = useState(0);
  const stop = useRef(false);

  const walk = useCallback(async () => {
    if (!host) return;
    stop.current = false;
    const files: { ext: string; size: number }[] = [];
    try {
      const client = await clientForHost(host);
      await scan(client, startPath || '/', files, setScanned, () => stop.current);
      return stop.current ? undefined : { ok: true as const, result: aggregateByExtension(files) };
    } catch (e) {
      return { ok: false as const, error: humanizeError(e) };
    }
  }, [host, startPath]);
  useEffect(() => {
    let live = true;
    void walk().then((r) => {
      if (!live || !r) return;
      if (r.ok) setResult(r.result);
      else setError(r.error);
    });
    return () => {
      live = false;
      stop.current = true;
    };
  }, [walk, run]);

  const rows = useMemo(() => (result ? sortedBySize(result) : []), [result]);
  const buckets = useMemo(() => bucketBySize(rows), [rows]);
  const title = <Stack.Screen options={{ title: t('storageByTypeTitle') }} />;

  if (host === undefined) return <Loading />;
  if (host === null) return <EmptyState message={t('hostNoLongerPaired')} />;
  if (error) return <View style={{ flex: 1 }}>{title}<ErrorRetry message={error} onRetry={() => { setError(null); setResult(null); setScanned(0); setRun((n) => n + 1); }} /></View>;
  if (!result) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.sm }}>
        {title}
        <ActivityIndicator color={c.primary} />
        <Text style={LumenType.title}>{t('storageByTypeScanning')}</Text>
        <Text style={LumenType.meta} muted>{t('storageByTypeFiles', { count: scanned })}</Text>
      </View>
    );
  }
  if (result.totalFiles === 0) return <View style={{ flex: 1 }}>{title}<EmptyState message={t('storageByTypeEmpty')} /></View>;

  const block = (i: number, style: object) => (
    <View key={buckets[i].label} style={[{ backgroundColor: mix(blockTones[i % blockTones.length], c.surfaceContainer, 0.22), borderRadius: LumenSize.cardRadius, padding: 12, justifyContent: 'flex-end' }, style]}>
      <Text style={LumenType.name} color={blockTones[i % blockTones.length]}>{buckets[i].label}</Text>
      <Text style={[LumenType.meta, { fontFamily: FontFamily.mono }]} color={blockTones[i % blockTones.length]}>{formatSize(buckets[i].bytes)}</Text>
    </View>
  );
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, gap: 16, paddingBottom: 32 }}>
      {title}
      <Text style={LumenType.title}>{t('storageByTypeSummary', { count: result.totalFiles, size: formatSize(result.totalSize) })}</Text>
      {buckets.length === 1 ? (
        block(0, { height: 160 })
      ) : (
        <View style={{ height: 236, flexDirection: 'row', gap: Spacing.xs }}>
          {block(0, { flex: 16 })}
          <View style={{ flex: 10, gap: Spacing.xs }}>{buckets.slice(1).map((_, i) => block(i + 1, { flex: 1 }))}</View>
        </View>
      )}
      <Text muted style={[LumenType.meta, { textAlign: 'center' }]}>{t('storageByTypeHint')}</Text>
      <GroupedCard padded={false} style={{ padding: 8 }}>
        {rows.map((r) => (
          <Pressable key={r.ext} onPress={() => setSelected(selected === r.ext ? null : r.ext)} accessibilityLabel={r.ext}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingVertical: 10, minHeight: 48, backgroundColor: selected === r.ext ? mix(c.primary, c.surfaceContainer, 0.14) : 'transparent', borderRadius: LumenSize.tileRadius, paddingHorizontal: 8 }}>
              <View style={{ width: 10, height: 10, borderRadius: 5, backgroundColor: categoryTone[categoryFor(r.ext)] }} />
              <Text style={[LumenType.name, { flex: 1 }]} numberOfLines={1}>{r.ext}</Text>
              <Text muted style={LumenType.meta}>{formatSize(r.bytes)}</Text>
              <Text muted style={[LumenType.meta, { width: 48, textAlign: 'right' }]}>{r.count}</Text>
              <Text muted style={[LumenType.meta, { width: 60, textAlign: 'right' }]}>{result.totalSize > 0 ? ((r.bytes / result.totalSize) * 100).toFixed(1) : '0.0'}%</Text>
            </View>
          </Pressable>
        ))}
      </GroupedCard>
    </ScrollView>
  );
}
