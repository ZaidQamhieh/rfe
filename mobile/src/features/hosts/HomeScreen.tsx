import { useFocusEffect, useRouter } from 'expo-router';
import { Activity, Bookmark, Folder, History, LayoutGrid, Monitor, Plug, Plus, QrCode, RefreshCw, Search, Settings, ShieldCheck, ShieldX, SlidersHorizontal, Zap, type LucideIcon } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, ScrollView, View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { roleOf } from '../../core/entryCategory';
import { formatSize } from '../../core/format';
import { sendWakeOnLan } from '../../core/native';
import type { Host } from '../../core/models/host';
import { Button, ErrorRetry, Loading, Pressable, SectionLabel, Text, useToast } from '../../design/components';
import { GroupedCard } from '../../design/components/Card';
import { StatePill, TopBar } from '../../design/components/LumenBits';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { EntryLeading } from '../explorer/EntryIcon';
import { parentPathOf } from '../explorer/reveal';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { useSettings } from '../../state/settings';
import { UpdateBanner } from '../update/UpdateBanner';
import { hostCardActions, recentMeta, routeStripState } from './hostCardLogic';
import { HostHero } from './HostHero';
import { relativeLabel } from './relative';
import { OverflowSheet, type OverflowItem } from './OverflowSheet';
import { RouteStrip } from './RouteStrip';
import { useHostStatus } from './useHostStatus';
import { activateHost, useWorkspaces } from './useWorkspaces';

/** Home: the selected computer as a hero (name, state, route), the four places to go, and what changed on it recently. */
export function HomeScreen() {
  const router = useRouter();
  const c = useScheme();
  const { hosts, selected, error, reload } = useWorkspaces();
  const activeId = useActiveHost((s) => s.active?.host.id ?? null);
  const setActive = useActiveHost((s) => s.setActive);
  const [menu, setMenu] = useState(false);

  // Keep the shared "active host" (what Files, Apps and Activity read) in step with what Home shows.
  useEffect(() => {
    if (!hosts) return;
    if (selected && selected.id !== activeId) activateHost(selected);
    else if (!selected && activeId) setActive(null);
  }, [hosts, selected, activeId, setActive]);

  if (selected) return <HomeHost key={selected.id} host={selected} onChanged={reload} />;

  const baseItems: OverflowItem[] = [{ key: 'settings', label: t('settingsMenuItem'), icon: Settings, onPress: () => router.push('/(tabs)/settings') }];
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar context="RFE · Workspaces" onMore={() => setMenu(true)} />
      {error ? (
        <ErrorRetry message={t('errorLabel', { error })} onRetry={reload} />
      ) : hosts === null ? (
        <Loading />
      ) : (
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 18, gap: 14 }}>
          <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ width: 108, height: 108, borderRadius: 54, backgroundColor: mix(c.primary, c.surface, 0.17), alignItems: 'center', justifyContent: 'center' }}>
            <Monitor size={44} color={c.primary} />
          </View>
          <Text style={[LumenType.pageTitle, { textAlign: 'center' }]} accessibilityRole="header">
            {t('emptyStatePairTitle')}
          </Text>
          <Text style={[LumenType.caption, { textAlign: 'center' }]} muted>
            {t('emptyStatePairBody')}
          </Text>
          <Button size="lg" kind="filled" label={t('addComputerButton')} renderIcon={(k) => <Plus size={20} color={k} />} onPress={() => router.push('/pair')} style={{ alignSelf: 'stretch' }} />
        </View>
      )}
      <OverflowSheet visible={menu} onClose={() => setMenu(false)} items={[...baseItems, { key: 'receive', label: t('receiveFileTooltip'), icon: QrCode, onPress: () => router.push('/receive') }]} />
    </View>
  );
}

function HomeHost({ host, onChanged }: { host: Host; onChanged: () => void }) {
  const router = useRouter();
  const c = useScheme();
  const roles = useRoles();
  const toast = useToast();
  const st = useHostStatus(host, onChanged);
  const { online, checking, health } = st;
  const actions = hostCardActions({ online, checking });
  const lowDiskThresholdBytes = useSettings((x) => x.state.app.lowDiskThresholdBytes);
  const [menu, setMenu] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const recents = useRecents(host, online);

  // Hand the freshly read /health to the Files tab (it resolves shared roots from it) without resetting where it is.
  useEffect(() => {
    const cur = useActiveHost.getState().active;
    if (health && cur && cur.host.id === host.id && cur.health === null) useActiveHost.getState().setActive({ ...cur, health });
  }, [health, host.id]);

  const readOnly = online && health?.readOnly === true;
  const lowDisk = online && lowDiskThresholdBytes > 0 && (st.drives ?? []).some((d) => d.freeBytes != null && d.freeBytes < lowDiskThresholdBytes);
  const version = health?.version?.trim() ? `v${health.version.trim()}` : '';
  const status = checking ? t('checkingStatus') : st.untrusted ? t('statusUntrusted') : online ? [host.note, version, 'Connected securely'].filter(Boolean).join(' · ') : st.lastSeen ? t('statusOfflineLastSeen', { relative: relativeLabel(st.lastSeen) }) : t('offlineStatus');

  const wake = async () => {
    if (!host.macAddress) return;
    const sent = await sendWakeOnLan(host.macAddress).catch(() => false);
    if (sent) toast.info(t('wolPacketSent', { hostname: host.label }));
    else toast.error(t('wolPacketFailed'));
  };
  const hostRoute = (pathname: string) => router.push({ pathname, params: { id: host.id } } as never);

  const items: OverflowItem[] = [
    { key: 'settings', label: t('settingsMenuItem'), icon: Settings, onPress: () => router.push('/(tabs)/settings') },
    { key: 'search', label: t('searchButton'), icon: Search, disabled: !actions.search, onPress: () => hostRoute('/host/[id]/search') },
    { key: 'bookmarks', label: 'Bookmarks', icon: Bookmark, onPress: () => router.push('/bookmarks') },
    { key: 'recent', label: t('recentTitle'), icon: History, disabled: !online, onPress: () => router.push('/recent') },
    ...(host.macAddress ? [{ key: 'wol', label: 'Wake on LAN', icon: Zap, onPress: () => void wake() }] : []),
    { key: 'connection', label: 'Connection', icon: Plug, onPress: () => hostRoute('/host/[id]/connect') },
    { key: 'computer', label: `${host.label} settings`, icon: SlidersHorizontal, onPress: () => hostRoute('/host/[id]/settings') },
    { key: 'receive', label: t('receiveFileTooltip'), icon: QrCode, onPress: () => router.push('/receive') },
    { key: 'refresh', label: t('refreshTooltip'), icon: RefreshCw, disabled: checking, onPress: st.refresh },
  ];

  const tiles: { key: string; label: string; icon: LucideIcon; color: string; disabled?: boolean; onPress: () => void }[] = [
    { key: 'files', label: 'Files', icon: Folder, color: roles.folder, onPress: () => router.navigate('/files') },
    { key: 'apps', label: t('hostAppsButton'), icon: LayoutGrid, color: c.primary, disabled: !actions.apps, onPress: () => router.navigate('/apps') },
    { key: 'activity', label: 'Activity', icon: Activity, color: roles.transfer, onPress: () => router.navigate('/transfers') },
    { key: 'trust', label: 'Trust', icon: ShieldCheck, color: roles.safe, onPress: () => hostRoute('/host/[id]/trust') },
  ];

  const pill = checking ? <StatePill label={t('checkingStatus')} tone="muted" /> : st.untrusted ? <StatePill label="Not trusted" tone="warn" icon={ShieldX} /> : online ? <StatePill label="Linked" tone="safe" icon={ShieldCheck} /> : <StatePill label={t('offlineStatus')} tone="warn" />;

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar context="RFE · Workspaces" right={pill} onMore={() => setMenu(true)} />
      <UpdateBanner />
      <ScrollView
        contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24 }}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={async () => {
              setRefreshing(true);
              st.refresh();
              await onChanged();
              setRefreshing(false);
            }}
          />
        }
      >
        <HostHero name={host.label} status={status} readOnly={readOnly} lowDisk={lowDisk} onSwitch={() => router.push('/workspaces')} />
        <RouteStrip state={routeStripState({ online, checking, activeAddress: st.activeAddress }, host)} />
        <View style={{ height: 14 }} />
        <SectionLabel title="Move into this computer" />
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 10 }}>
          {tiles.map((tile) => (
            <Pressable key={tile.key} disabled={tile.disabled} onPress={tile.onPress} accessibilityLabel={tile.label} accessibilityState={{ disabled: !!tile.disabled }} style={{ width: '48.5%', flexGrow: 1, opacity: tile.disabled ? 0.4 : 1 }}>
              <View style={{ height: 78, borderRadius: LumenSize.tileRadius, backgroundColor: c.surfaceContainer, alignItems: 'center', justifyContent: 'center', gap: 6, paddingHorizontal: 12 }}>
                <tile.icon size={32} color={tile.color} />
                <Text style={LumenType.sectionLabel} numberOfLines={1}>
                  {tile.label}
                </Text>
              </View>
            </Pressable>
          ))}
        </View>
        {recents && recents.length > 0 ? (
          <GroupedCard padded={false} style={{ marginTop: 12, paddingHorizontal: 14, paddingVertical: 10 }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
              <Text style={[LumenType.sectionLabel, { flexShrink: 1 }]} muted numberOfLines={1} accessibilityRole="header">
                {`Recent on ${host.label}`}
              </Text>
              <Pressable onPress={() => router.push('/recent')} accessibilityLabel="See all recent files" hitSlop={{ top: 12, bottom: 12, left: 12, right: 0 }}>
                <Text style={LumenType.meta} color={c.primary}>
                  See all
                </Text>
              </Pressable>
            </View>
            {recents.map((e) => (
              <RecentRow
                key={e.path}
                entry={e}
                onPress={() => {
                  useActiveHost.getState().setActive({ host, health, initialPath: parentPathOf(e.path) });
                  router.navigate('/files');
                }}
              />
            ))}
          </GroupedCard>
        ) : null}
      </ScrollView>
      <OverflowSheet visible={menu} title={host.label} items={items} onClose={() => setMenu(false)} />
    </View>
  );
}

function RecentRow({ entry, onPress }: { entry: Entry; onPress: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  const role = roleOf(entry);
  const meta = recentMeta(entry, formatSize);
  return (
    <Pressable onPress={onPress} accessibilityLabel={entry.name}>
      <View style={{ minHeight: 56, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 }}>
        <View style={{ width: 38, height: 38, borderRadius: 10, alignItems: 'center', justifyContent: 'center', backgroundColor: role ? mix(roles[role], c.surfaceContainerHigh, 0.17) : c.surfaceContainerHigh }}>
          <EntryLeading entry={entry} size={22} />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={LumenType.name} numberOfLines={1}>
            {entry.name}
          </Text>
          {meta ? (
            <Text style={LumenType.meta} muted numberOfLines={1}>
              {meta}
            </Text>
          ) : null}
        </View>
      </View>
    </Pressable>
  );
}

/** The few newest files on [host] (real `/fs/recent`), refreshed on focus; null while offline, unloaded or unavailable. */
function useRecents(host: Host, online: boolean): Entry[] | null {
  const [entries, setEntries] = useState<Entry[] | null>(null);
  const [tick, setTick] = useState(0);
  useFocusEffect(
    useCallback(() => {
      setTick((n) => n + 1);
    }, []),
  );
  useEffect(() => {
    if (!online) return;
    let live = true;
    clientForHost(host)
      .then((cl) => cl.recent({ limit: 3 }))
      .then(
        (r) => live && setEntries(r.entries.slice(0, 3)),
        () => live && setEntries(null),
      );
    return () => {
      live = false;
    };
  }, [host, online, tick]);
  return online ? entries : null;
}
