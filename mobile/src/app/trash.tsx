import { Stack } from 'expo-router';
import { ArchiveRestore, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { FlatList, RefreshControl, View } from 'react-native';

import type { TrashEntry } from '../core/api/models';
import { formatSize, relativeOrDate } from '../core/format';
import { Button, Emoji, ErrorRetry, Loading, Text, useDialogs, useToast } from '../design/components';
import { LumenType } from '../design/lumen';
import { useScheme } from '../design/theme';
import { emojiNameFor } from '../features/explorer/entryEmoji';
import { StatusBlock } from '../features/explorer/FolderStates';
import { restoreOutcome } from '../features/explorer/trashRestore';
import { folderLabel } from '../features/explorer/paths';
import { parentPathOf } from '../features/explorer/reveal';
import { explorerFor } from '../features/explorer/useExplorer';
import { humanizeError } from '../core/humanizeError';
import { t } from '../i18n';
import { clientForHost } from '../services';
import { useActiveHost } from '../state/activeHost';
import { Spacing } from '../design/tokens';

/** The agent's trash: restore to the original location, delete one item for good, or empty everything. A restore refreshes the open folder. */
export default function Trash() {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const active = useActiveHost((s) => s.active);
  const [items, setItems] = useState<TrashEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const activeHost = active?.host;
  const fetchItems = useCallback(async (): Promise<{ items: TrashEntry[] } | { error: string }> => {
    if (!activeHost) return { items: [] };
    try {
      return { items: await (await clientForHost(activeHost)).listTrash() };
    } catch (e) {
      return { error: humanizeError(e) };
    }
  }, [activeHost]);
  const apply = useCallback((r: { items: TrashEntry[] } | { error: string }) => {
    if ('items' in r) {
      setItems(r.items);
      setError(null);
    } else setError(r.error);
    setLoading(false);
  }, []);
  const load = useCallback(async () => {
    setLoading(true);
    apply(await fetchItems());
  }, [apply, fetchItems]);

  useEffect(() => {
    let live = true;
    void fetchItems().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchItems]);

  if (!active) return null;
  const { host, rootPath } = active;
  const refreshFolder = () => {
    if (rootPath) void explorerFor(host, rootPath).refresh();
  };

  async function restore(item: TrashEntry) {
    try {
      const outcome = restoreOutcome(item.name, await (await clientForHost(host)).restoreTrash([item.id]));
      if (!outcome.ok) {
        toast.error(t('restoreFailed', { error: outcome.error }));
        return;
      }
      refreshFolder();
      toast.success(t(outcome.renamed ? 'restoredAs' : 'restoredItem', { name: outcome.name }));
      await load();
    } catch (e) {
      toast.error(t('restoreFailed', { error: humanizeError(e) }));
    }
  }

  async function deleteForever(item: TrashEntry) {
    const ok = await dialogs.confirm({ title: t('deleteForeverTitle'), description: t('deleteForeverConfirm', { name: item.name }), confirmLabel: t('deleteForeverButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    try {
      await (await clientForHost(host)).emptyTrash([item.id]);
      toast.success(t('deletedForever', { name: item.name }));
      await load();
    } catch (e) {
      toast.error(t('deleteFailed', { error: humanizeError(e) }));
    }
  }

  async function emptyAll() {
    const ok = await dialogs.confirm({ title: t('emptyTrashTitle'), description: t('emptyTrashBody', { count: items?.length ?? 0 }), confirmLabel: t('emptyTrashTooltip'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    try {
      await (await clientForHost(host)).emptyTrash();
      toast.success(t('trashEmptied'));
      await load();
    } catch (e) {
      toast.error(t('emptyFailed', { error: humanizeError(e) }));
    }
  }

  const hasItems = items !== null && items.length > 0;
  let body: React.ReactNode;
  if (loading && items === null) body = <Loading />;
  else if (error && items === null) body = <ErrorRetry message={error} onRetry={load} />;
  else if (!hasItems) body = <StatusBlock emoji="trash" title={t('trashIsEmpty')} hint={t('trashEmptySubtitle')} />;
  else {
    body = (
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        refreshControl={<RefreshControl refreshing={loading} onRefresh={load} />}
        contentContainerStyle={{ paddingBottom: Spacing.lg }}
        ListHeaderComponent={
          <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.xs, paddingBottom: Spacing.md2, borderBottomWidth: 1, borderBottomColor: c.rule }}>
            <Text muted style={LumenType.meta}>
              {t('trashKeepNote')}
            </Text>
            <View style={{ marginTop: Spacing.md2, alignSelf: 'flex-start' }}>
              <Button size="s" kind="outlined" destructive label={t('emptyTrashTooltip')} renderIcon={(k) => <Trash2 size={18} color={k} />} onPress={emptyAll} />
            </View>
          </View>
        }
        renderItem={({ item }) => <TrashRow item={item} onRestore={() => restore(item)} onDelete={() => deleteForever(item)} />}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <Stack.Screen options={{ title: t('trashTitle') }} />
      {body}
    </View>
  );
}

/**
 * One deleted item as a flat list row: its picture, name and where it was (with when it was deleted and its size),
 * and under it two short bordered buttons: Restore, and Delete forever in red.
 */
function TrashRow({ item, onRestore, onDelete }: { item: TrashEntry; onRestore: () => void; onDelete: () => void }) {
  const c = useScheme();
  const when = item.deletedAt ? t('deletedRelative', { relative: relativeOrDate(new Date(item.deletedAt)) }) : null;
  const size = !item.isDir && item.size != null ? formatSize(item.size) : null;
  // The folder the item came from, by its name only ("From Documents"); the full path is never shown.
  const parent = item.originalPath ? parentPathOf(item.originalPath) : '';
  const from = parent && parent !== item.originalPath ? t('phKeptFrom', { computer: folderLabel(parent) }) : null;
  const subtitle = [from, when, size].filter(Boolean).join(' · ');
  return (
    <View style={{ paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md, borderBottomWidth: 1, borderBottomColor: c.rule }}>
      <View style={{ minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: Spacing.md }}>
        <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
          <Emoji name={emojiNameFor({ isDir: item.isDir, name: item.name })} size={32} />
        </View>
        <View style={{ flex: 1, minWidth: 0 }}>
          <Text numberOfLines={1} ellipsizeMode="middle" style={LumenType.name}>
            {item.name}
          </Text>
          <Text numberOfLines={2} muted style={LumenType.meta}>
            {subtitle}
          </Text>
        </View>
      </View>
      <View style={{ flexDirection: 'row', gap: Spacing.sm, marginTop: Spacing.sm, marginLeft: 56 }}>
        <Button size="s" kind="outlined" label={t('restoreButton')} renderIcon={(k) => <ArchiveRestore size={18} color={k} />} onPress={onRestore} />
        <Button size="s" kind="outlined" destructive label={t('deleteForeverButton')} renderIcon={(k) => <Trash2 size={18} color={k} />} onPress={onDelete} />
      </View>
    </View>
  );
}
