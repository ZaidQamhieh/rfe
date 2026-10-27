import { CornerDownRight, File as FileIcon, Folder, X } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { FlatList, View } from 'react-native';

import type { ArchiveEntry, Entry } from '../../../core/api/models';
import { formatSize } from '../../../core/format';
import type { Host } from '../../../core/models/host';
import { Pressable, Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { Brand, FontFamily, Radii, Spacing } from '../../../design/tokens';
import { clientForHost } from '../../../services';
import { humanizeError } from '../../pairing/pairingService';
import { PAGER_CHIP_CLEARANCE } from '../previewFile';
import { PreviewError, PreviewLoading } from '../PreviewChrome';

/** Port of ArchivePreviewScreen: the agent lists the archive; tapping a folder filters to its prefix. */
export function ArchiveViewer({ host, entry }: { host: Host; entry: Entry }) {
  const c = useScheme();
  const [entries, setEntries] = useState<ArchiveEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    clientForHost(host)
      .then((client) => client.archiveList(entry.path))
      .then(
        (l) => live && setEntries(l),
        (e) => live && setError(humanizeError(e)),
      );
    return () => {
      live = false;
    };
  }, [host, entry.path, attempt]);

  if (error) {
    return (
      <PreviewError
        message={`Could not read archive: ${error}`}
        onRetry={() => {
          setError(null);
          setEntries(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  }
  if (!entries) return <PreviewLoading message="Loading archive contents…" />;
  const shown = filter ? entries.filter((e) => e.path.startsWith(filter)) : entries;
  return (
    <View style={{ flex: 1 }}>
      <Text variant="bodySmall" muted style={{ alignSelf: 'flex-end', paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }}>
        {`${entries.length} ${entries.length === 1 ? 'entry' : 'entries'}`}
      </Text>
      {filter ? (
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 18, paddingVertical: 8 }}>
          <CornerDownRight size={16} color={c.onSurfaceVariant} />
          <Text numberOfLines={1} muted style={{ flex: 1, fontSize: 12.5 }}>{`/${filter}`}</Text>
          <Pressable onPress={() => setFilter('')} accessibilityLabel="Clear filter">
            <X size={18} color={c.onSurfaceVariant} />
          </Pressable>
        </View>
      ) : null}
      {shown.length === 0 ? (
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
          <Text>Empty archive</Text>
        </View>
      ) : (
        <FlatList
          data={shown}
          keyExtractor={(e) => e.path}
          contentContainerStyle={{ paddingHorizontal: 4, paddingBottom: PAGER_CHIP_CLEARANCE }}
          renderItem={({ item: e }) => (
            <Pressable onPress={e.isDir ? () => setFilter(e.path) : undefined} disabled={!e.isDir} accessibilityLabel={e.path}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: 11, paddingHorizontal: 14 }}>
                <View style={{ width: 38, height: 38, borderRadius: Radii.sm, alignItems: 'center', justifyContent: 'center', backgroundColor: e.isDir ? `${Brand.amber}24` : c.surfaceContainerHigh }}>
                  {e.isDir ? <Folder size={19} color={Brand.amber} /> : <FileIcon size={19} color={c.onSurfaceVariant} />}
                </View>
                <View style={{ flex: 1 }}>
                  <Text numberOfLines={1} style={{ fontSize: 14, fontFamily: FontFamily.medium }}>{e.path}</Text>
                  {!e.isDir ? <Text muted style={{ fontSize: 11.5, marginTop: 2 }}>{formatSize(e.size)}</Text> : null}
                </View>
              </View>
            </Pressable>
          )}
        />
      )}
    </View>
  );
}
