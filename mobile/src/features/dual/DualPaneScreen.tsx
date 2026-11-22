import { ArrowLeft, ArrowUp, File as FileIcon, Folder } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { FlatList, ScrollView, View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { humanizeError } from '../../core/humanizeError';
import type { Host } from '../../core/models/host';
import { Button, Chip, EmptyState, ErrorRetry, Loading, Pressable, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { destinationStart, startRelayCopy } from '../transfers/relayCopy';
import { copyRoute, paneOrder, paneUp, relayable } from './dualLogic';

type PaneState = { host: Host; top: string; path: string; entries: Entry[]; loading: boolean; error: string | null; selected: Set<string> };

/** One folder browser: files and folders of a computer, tap a folder to open it, tap a file to select it. */
function usePane(initial: Host) {
  const [p, setP] = useState<PaneState>({ host: initial, top: '', path: '', entries: [], loading: true, error: null, selected: new Set() });
  const open = useCallback(async (host: Host, path: string, top: string) => {
    setP((s) => ({ ...s, host, top, path, loading: true, error: null, selected: new Set() }));
    try {
      const listing = await (await clientForHost(host)).list(path);
      setP((s) => (s.host.id === host.id && s.path === path ? { ...s, entries: paneOrder(listing.entries), loading: false } : s));
    } catch (e) {
      setP((s) => (s.host.id === host.id && s.path === path ? { ...s, entries: [], loading: false, error: humanizeError(e) } : s));
    }
  }, []);
  const switchHost = useCallback(
    async (host: Host) => {
      try {
        const start = await destinationStart(host);
        await open(host, start, start);
      } catch (e) {
        setP((s) => ({ ...s, host, loading: false, error: humanizeError(e) }));
      }
    },
    [open],
  );
  const toggle = (path: string) =>
    setP((s) => {
      const next = new Set(s.selected);
      if (!next.delete(path)) next.add(path);
      return { ...s, selected: next };
    });
  return { p, open, switchHost, toggle };
}

/** Two folders side by side, on one computer or two: pick files on one side and copy them into the folder open on the other. */
export function DualPaneScreen({ hosts }: { hosts: Host[] }) {
  const c = useScheme();
  const toast = useToast();
  const left = usePane(hosts[0]);
  const right = usePane(hosts[1] ?? hosts[0]);
  const [busy, setBusy] = useState(false);
  const { switchHost: leftInit } = left;
  const { switchHost: rightInit } = right;
  useEffect(() => {
    void leftInit(hosts[0]);
    void rightInit(hosts[1] ?? hosts[0]);
  }, [hosts, leftInit, rightInit]);

  async function copy(from: ReturnType<typeof usePane>, to: ReturnType<typeof usePane>) {
    const src = from.p;
    const dst = to.p;
    const paths = [...src.selected];
    if (paths.length === 0 || busy) return;
    setBusy(true);
    try {
      if (copyRoute(src.host.id, dst.host.id) === 'same') {
        const r = await (await clientForHost(src.host)).copy(paths, dst.path);
        if (r.failed.length > 0) toast.error(t('dualCopyFailed', { count: r.failed.length }));
        else toast.success(t('dualCopied', { count: paths.length }));
        await to.open(dst.host, dst.path, dst.top);
      } else if (relayable(src.entries, src.selected)) {
        const n = await startRelayCopy(src.host, paths, dst.host, dst.path);
        toast.success(t('relayCopyQueued', { count: n, name: dst.host.label }));
      } else {
        toast.error(t('dualFoldersNeedSameComputer'));
      }
      from.p.selected.clear();
    } catch (e) {
      toast.error(humanizeError(e));
    } finally {
      setBusy(false);
    }
  }

  const pane = (me: ReturnType<typeof usePane>, other: ReturnType<typeof usePane>, arrow: 'right' | 'left') => {
    const s = me.p;
    const up = paneUp(s.path, s.top);
    return (
      <View style={{ flex: 1, borderColor: c.outlineVariant, borderWidth: 1, borderRadius: 12, margin: Spacing.xs, overflow: 'hidden' }}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0 }} contentContainerStyle={{ gap: Spacing.xs, padding: Spacing.xs }}>
          {hosts.map((h) => (
            <Chip key={h.id} label={h.label} selected={h.id === s.host.id} onPress={() => void me.switchHost(h)} />
          ))}
        </ScrollView>
        <Text numberOfLines={1} style={{ paddingHorizontal: Spacing.sm, color: c.onSurfaceVariant }}>{s.path}</Text>
        <View style={{ flex: 1 }}>
          {s.loading ? (
            <Loading />
          ) : s.error ? (
            <ErrorRetry message={s.error} onRetry={() => void me.open(s.host, s.path, s.top)} />
          ) : (
            <FlatList
              data={s.entries}
              keyExtractor={(e) => e.path}
              ListHeaderComponent={
                up ? (
                  <Pressable onPress={() => void me.open(s.host, up, s.top)} accessibilityLabel={t('dualUp')}>
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, minHeight: 44, paddingHorizontal: Spacing.sm }}>
                      <ArrowUp size={18} color={c.onSurfaceVariant} />
                      <Text>..</Text>
                    </View>
                  </Pressable>
                ) : null
              }
              ListEmptyComponent={<EmptyState message={t('emptyFolderMessage')} />}
              renderItem={({ item }) => (
                <Pressable
                  onPress={() => (item.isDir ? void me.open(s.host, item.path, s.top) : me.toggle(item.path))}
                  onLongPress={() => me.toggle(item.path)}
                  accessibilityLabel={item.name}
                >
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, minHeight: 44, paddingHorizontal: Spacing.sm, backgroundColor: s.selected.has(item.path) ? c.secondaryContainer : undefined }}>
                    {item.isDir ? <Folder size={18} color={c.primary} /> : <FileIcon size={18} color={c.onSurfaceVariant} />}
                    <Text numberOfLines={1} style={[LumenType.name, { flex: 1 }]}>{item.name}</Text>
                  </View>
                </Pressable>
              )}
            />
          )}
        </View>
        <View style={{ padding: Spacing.xs }}>
          <Button
            size="s"
            label={(arrow === 'right' ? t('dualCopyRight', { count: s.selected.size }) : t('dualCopyLeft', { count: s.selected.size }))}
            disabled={s.selected.size === 0 || busy || other.p.loading || other.p.path === ''}
            renderIcon={(k) => <ArrowLeft size={16} color={k} style={arrow === 'right' ? { transform: [{ rotate: '180deg' }] } : undefined} />}
            onPress={() => void copy(me, other)}
          />
        </View>
      </View>
    );
  };

  return (
    <View style={{ flex: 1, flexDirection: 'row', backgroundColor: c.surface }}>
      {pane(left, right, 'right')}
      {pane(right, left, 'left')}
    </View>
  );
}
