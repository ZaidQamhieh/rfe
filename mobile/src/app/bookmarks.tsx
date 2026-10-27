import { Stack, useRouter } from 'expo-router';
import { Bookmark as BookmarkIcon } from 'lucide-react-native';
import { useEffect, useRef, useState } from 'react';
import { ScrollView, View } from 'react-native';

import type { Host } from '../core/models/host';
import type { Bookmark } from '../core/storage/collections';
import { IconTile, Pressable, SectionLabel, Text, useDialogs, useToast } from '../design/components';
import { mix } from '../design/color';
import { LumenType } from '../design/lumen';
import { useScheme } from '../design/theme';
import { FontFamily, Spacing } from '../design/tokens';
import { ResultCardRow } from '../features/search/SearchParts';
import { t } from '../i18n';
import { sendWakeOnLan } from '../core/native';
import { wakeThenOpen } from '../features/hosts/wakeThenOpen';
import { clientForHost, hostStore } from '../services';
import { useActiveHost } from '../state/activeHost';
import { useCollections } from '../state/collections';

/** All bookmarks grouped by host. Tap opens the Files tab at that path; long-press removes (with confirmation). */
export default function Bookmarks() {
  const c = useScheme();
  const router = useRouter();
  const dialogs = useDialogs();
  const toast = useToast();
  const bookmarks = useCollections((s) => s.bookmarks);
  const remove = useCollections((s) => s.removeBookmark);
  const setActive = useActiveHost((s) => s.setActive);
  const [hosts, setHosts] = useState<Record<string, Host>>({});
  const wakingRef = useRef(false);
  // False once the screen is left: a host that answers later must not pull the user back to Files.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    hostStore.listHosts().then((l) => setHosts(Object.fromEntries(l.map((h) => [h.id, h]))));
  }, []);

  if (bookmarks.length === 0) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.xl, gap: Spacing.md }}>
        <Stack.Screen options={{ title: 'Bookmarks' }} />
        <View style={{ width: 96, height: 96, borderRadius: 28, backgroundColor: mix(c.primary, c.surfaceContainer, 0.2), alignItems: 'center', justifyContent: 'center' }}>
          <BookmarkIcon size={40} color={c.primary} />
        </View>
        <Text style={[LumenType.meta, { textAlign: 'center' }]} muted>No bookmarks yet. Long-press a file, then tap the bookmark icon.</Text>
      </View>
    );
  }

  const groups = new Map<string, Bookmark[]>();
  for (const b of bookmarks) groups.set(b.hostId, [...(groups.get(b.hostId) ?? []), b]);

  async function confirmRemove(b: Bookmark, name: string) {
    const ok = await dialogs.confirm({ title: t('removeBookmarkTitle'), description: t('removeBookmarkConfirm', { name }), confirmLabel: t('removeButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (ok) await remove(b.hostId, b.remotePath);
  }

  /** Opens a bookmark; a sleeping host with a known MAC address is woken first, and the folder opens once it answers. */
  async function openBookmark(b: Bookmark) {
    const host = hosts[b.hostId];
    if (!host || wakingRef.current) return;
    wakingRef.current = true;
    try {
      const result = await wakeThenOpen({
        mac: host.macAddress,
        isUp: () => clientForHost(host, false, 3000).then((cl) => cl.health()).then(() => true, () => false),
        wake: (mac) => sendWakeOnLan(mac).catch(() => false),
        sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
        now: Date.now,
        timeoutMs: 90_000,
        intervalMs: 3_000,
        isCancelled: () => !mounted.current,
        onWaking: () => toast.info(t('wakingHost', { hostname: host.label })),
      });
      if (result === 'cancelled' || !mounted.current) return;
      if (result === 'timeout') toast.error(t('wakeTimedOut', { hostname: host.label }));
      else if (result === 'send-failed') toast.error(t('wolPacketFailed'));
      // Anything but a wake that never came up still opens: an offline host shows its cached listing.
      setActive({ host, health: null, initialPath: b.remotePath });
      router.dismissTo('/files');
    } finally {
      wakingRef.current = false;
    }
  }

  return (
    <ScrollView contentContainerStyle={{ paddingVertical: 10 }}>
      <Stack.Screen options={{ title: 'Bookmarks' }} />
      {[...groups.entries()].map(([hostId, items]) => (
        <View key={hostId} style={{ marginBottom: 14 }}>
          <View style={{ paddingHorizontal: 22 }}>
            <SectionLabel title={hosts[hostId]?.label ?? hostId} />
          </View>
          {items.map((b, i) => {
            const name = b.remotePath.split('/').filter(Boolean).pop() ?? b.remotePath;
            return (
              <ResultCardRow key={b.remotePath} index={i} count={items.length}>
              <Pressable
                accessibilityLabel={name}
                accessibilityActions={[{ name: 'delete', label: 'Remove bookmark' }]}
                onAccessibilityAction={(ev) => ev.nativeEvent.actionName === 'delete' && confirmRemove(b, name)}
                onPress={() => void openBookmark(b)}
                onLongPress={() => confirmRemove(b, name)}
              >
                <View style={{ minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 }}>
                  <IconTile icon={BookmarkIcon} color={c.primary} size={38} radius={10} />
                  <View style={{ flex: 1 }}>
                    <Text numberOfLines={1} style={LumenType.name}>{name}</Text>
                    <Text numberOfLines={1} muted style={[LumenType.meta, { fontFamily: FontFamily.mono, fontSize: 13 }]}>{b.remotePath}</Text>
                  </View>
                  {b.tag ? (
                    <View style={{ paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12, backgroundColor: c.surfaceContainerHigh }}>
                      <Text style={LumenType.pill} muted>{b.tag}</Text>
                    </View>
                  ) : null}
                </View>
              </Pressable>
              </ResultCardRow>
            );
          })}
        </View>
      ))}
    </ScrollView>
  );
}
