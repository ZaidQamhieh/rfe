import { useFocusEffect, useRouter } from 'expo-router';
import { X } from 'lucide-react-native';
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { RefreshControl, ScrollView, View } from 'react-native';

import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { DuoIcon, Emoji, ErrorRetry, Loading, Pressable, Row, SectionLabel, Text, useToast } from '../../design/components';
import { TopBar } from '../../design/components/LumenBits';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { useCollections } from '../../state/collections';
import { useSettings } from '../../state/settings';
import { EntryLeading, isImageEntry, useIconChipBg } from '../explorer/EntryIcon';
import { basenameOf } from '../explorer/paths';
import { parentPathOf } from '../explorer/reveal';
import { Thumbnail } from '../explorer/Thumbnail';
import { usePreviewSession } from '../preview/session';
import { UpdateBanner } from '../update/UpdateBanner';
import { ComputerMenu } from './ComputerMenu';
import { ComputerStateView } from './ComputerStateView';
import { EmptyPairPrompt } from './EmptyPairPrompt';
import { useKeptFiles } from './useKeptFiles';
import { computerMenuEntries, computerMenuTarget, type ComputerMenuKey } from './computerMenuLogic';
import { ComputerSwitcherSheet } from './ComputerSwitcherSheet';
import { hostCardActions } from './hostCardLogic';
import { RECENT_ASK, RECENT_FETCH, RECENT_SHOWN, recentRows, recentWhen, withoutHidden } from './homeLogic';
import { Nameplate } from './Nameplate';
import { pathCrumb, stateLine } from './nameplateLogic';
import { homeStateView, pairAgainAddress } from './switcherLogic';
import { TransferStrip } from './TransferStrip';
import { useHostStatus } from './useHostStatus';
import { useIsHostOwner } from './useIsHostOwner';
import { usePlaces } from './usePlaces';
import { useTransfers } from './useTransfers';
import { activateHost, useWorkspaces, wakeHost } from './useWorkspaces';
import { Radii, Spacing } from '../../design/tokens';

/** Height of a "Changed lately" preview tile (`.prev .pv`); the three tiles share the row's width. */
const RECENT_TILE_HEIGHT = 152;

/** Home: the selected computer's nameplate, its places, what is pinned and what changed lately, and the transfer strip. */
export function HomeScreen() {
  const c = useScheme();
  const { hosts, selected, error, reload } = useWorkspaces();
  const activeId = useActiveHost((s) => s.active?.host.id ?? null);
  const setActive = useActiveHost((s) => s.setActive);

  // Keep the shared "active host" (what Files, Apps and Activity read) in step with what Home shows.
  useEffect(() => {
    if (!hosts) return;
    if (selected && selected.id !== activeId) activateHost(selected);
    else if (!selected && activeId) setActive(null);
  }, [hosts, selected, activeId, setActive]);

  if (selected) return <HomeHost key={selected.id} host={selected} hosts={hosts ?? [selected]} onChanged={reload} />;

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar context="RFE · Workspaces" />
      {error ? (
        <ErrorRetry message={t('errorLabel', { error })} onRetry={reload} />
      ) : hosts === null ? (
        <Loading />
      ) : (
        <EmptyPairPrompt />
      )}
    </View>
  );
}

/** Nameplate `.sec`: a sentence-case 14/500 label with 20 above and an optional action at the right edge. */
function Section({ title, action }: { title: string; action?: { label: string; onPress: () => void } }) {
  const c = useScheme();
  return (
    <View style={{ paddingHorizontal: Spacing.md2, paddingTop: Spacing.md3 }}>
      <SectionLabel
        title={title}
        trailing={
          action ? (
            <Pressable onPress={action.onPress} accessibilityLabel={action.label} hitSlop={{ top: 12, bottom: 12, left: 12, right: 12 }}>
              <Text style={LumenType.sectionLabel} color={c.onSurface}>
                {action.label}
              </Text>
            </Pressable>
          ) : undefined
        }
      />
    </View>
  );
}

function HomeHost({ host, hosts, onChanged }: { host: Host; hosts: Host[]; onChanged: () => void }) {
  const router = useRouter();
  const c = useScheme();
  const st = useHostStatus(host, onChanged);
  const { online, checking, health } = st;
  const actions = hostCardActions({ online, checking });
  const lowDiskThresholdBytes = useSettings((x) => x.state.app.lowDiskThresholdBytes);
  const favorites = useCollections((s) => s.favorites);
  const removeFavorite = useCollections((s) => s.removeFavorite);
  const [editing, setEditing] = useState(false);
  const [seeAll, setSeeAll] = useState(false);  const [menu, setMenu] = useState(false);
  const [switcher, setSwitcher] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const recents = useRecents(host, online);
  const places = usePlaces(host, health, online, st.drives);
  const records = useTransfers();
  const toast = useToast();

  // Hand the freshly read /health to the Files tab (it resolves shared roots from it) without resetting where it is.
  useEffect(() => {
    const cur = useActiveHost.getState().active;
    if (health && cur && cur.host.id === host.id && cur.health === null) useActiveHost.getState().setActive({ ...cur, health });
  }, [health, host.id]);

  // A computer that is not answering gets the asleep / lost panel above its places (cached browsing stays below).
  const stateView = homeStateView(st, host);
  const readOnly = online && health?.readOnly === true;
  const lowDisk = online && lowDiskThresholdBytes > 0 && (st.drives ?? []).some((d) => d.freeBytes != null && d.freeBytes < lowDiskThresholdBytes);
  const line = stateLine(st, host);
  // An unreachable computer that cannot be woken reads "Connection lost · trying again" on a waiting lamp (mockup `lost`).
  const { lamp, phrase } = stateView === 'lost' ? { lamp: 'wait' as const, phrase: t('stateConnectionLost') } : line;
  const kept = useKeptFiles(host.id, stateView === 'asleep');
  const openKept = (e: Entry) => {
    usePreviewSession.getState().open({ host, entries: [e], index: 0 });
    router.push('/preview');
  };
  const notes = [...(lowDisk ? [{ lamp: 'wait' as const, text: t('lowDiskWarning') }] : []), ...(readOnly ? [{ lamp: 'off' as const, text: t('stateReadOnly') }] : [])];

  const hostRoute = (pathname: string, id: string = host.id) => router.push({ pathname, params: { id } } as never);
  // Pair again: the add-computer screen, with this computer's address already filled in (no protocol change).
  const pairAgain = (address: string) => router.push({ pathname: '/pair', params: { address } });
  const openFiles = (opts: { rootPath?: string; initialPath?: string } = {}) => {
    useActiveHost.getState().setActive({ host, health, ...opts });
    router.navigate('/files');
  };

  // The computer menu entries (Trash, Apps, Phones with access, About, Settings); the technical ones live under About.
  // Phones with access names the computer's other phones, so only an owner phone is offered it.
  const isOwner = useIsHostOwner(host, online);
  const menuEntries = computerMenuEntries(host.label, actions.apps, isOwner);
  const pickMenu = (key: ComputerMenuKey) => {
    const target = computerMenuTarget(key, host.id);
    const go = target.method === 'navigate' ? router.navigate : router.push;
    go((target.params ? { pathname: target.pathname, params: target.params } : target.pathname) as never);
  };

  const pinned = favorites.filter((f) => f.hostId === host.id);
  // Removing the last pinned folder ends edit mode (the Pinned section disappears with it).
  const unpin = (hostId: string, remotePath: string) => {
    if (pinned.length <= 1) setEditing(false);
    void removeFavorite(hostId, remotePath);
  };

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <Nameplate
        name={host.label}
        picture="desktop_computer"
        off={!online && !checking}
        lamp={lamp}
        phrase={phrase}
        notes={notes}
        onSwitch={() => setSwitcher(true)}
        onSearch={actions.search ? () => hostRoute('/host/[id]/search') : undefined}
        onMore={() => setMenu(true)}
      />
      <UpdateBanner />
      <ScrollView
        contentContainerStyle={{ paddingBottom: Spacing.lg }}
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
        {stateView ? (
          <ComputerStateView
            kind={stateView}
            name={host.label}
            lastSeen={st.lastSeen}
            onWake={stateView === 'asleep' ?() => void wakeHost(host, toast) : undefined}
            onRetry={st.refresh}
            onCheck={() => hostRoute('/host/[id]/connect')}
            onPair={() => pairAgain(pairAgainAddress(st, host))}
            kept={kept}
            onOpenKept={openKept}
          />
        ) : null}
        {/* The places stay browsable from the cache but are dimmed while the computer is not answering (mockup `lost`). */}
        <View style={{ opacity: stateView === 'lost' || stateView === 'refused' ? 0.5 : 1 }}>
        <Section title={t('placesTitle')} />
        {places && places.length > 0 ? (
          places.map((p) => <Row key={p.key} lead={<Emoji name={p.kind === 'drive' ? 'drive' : 'folder'} size={32} off={!online} />} title={p.title} subtitle={p.subtitle} onPress={() => openFiles({ rootPath: p.path })} />)
        ) : (
          <Row lead={<Emoji name="folder" size={32} off={!online} />} title={t('placeFilesFallback')} onPress={() => openFiles()} />
        )}
        {pinned.length > 0 ? (
          <>
            <Section title={t('pinnedTitle')} action={{ label: editing ? t('doneAction') : t('editAction'), onPress: () => setEditing((v) => !v) }} />
            {pinned.map((b) => {
              const name = b.label || basenameOf(b.path) || b.path;
              return (
                <Row
                  key={b.path}
                  lead={<Emoji name="folder" size={32} off={!online} />}
                  title={name}
                  subtitle={pathCrumb(b.path)}
                  trailing={
                    editing ? (
                      <Pressable onPress={() => unpin(b.hostId, b.path)} accessibilityRole="button" accessibilityLabel={t('unpinAction', { name })} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
                        <DuoIcon icon={X} size={22} color={c.onSurfaceVariant} />
                      </Pressable>
                    ) : undefined
                  }
                  onPress={editing ? undefined : () => openFiles({ initialPath: b.path })}
                />
              );
            })}
          </>
        ) : null}
        {recents && recents.length > 0 ? (
          <>
            <Section title={t('changedLatelyTitle')} action={recents.length > RECENT_SHOWN ? { label: seeAll ? t('showLessAction') : t('seeAllAction'), onPress: () => setSeeAll((v) => !v) } : undefined} />
            <View style={{ gap: Spacing.md2, paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }}>
              {recentRows(recents, seeAll).map((row, i) => (
                <View key={i} style={{ flexDirection: 'row', gap: Spacing.md2 }}>
                  {row.map((e, j) =>
                    e ? <RecentTile key={e.path} hostId={host.id} entry={e} onPress={() => openFiles({ initialPath: parentPathOf(e.path) })} /> : <View key={`pad${j}`} style={{ flex: 1, minWidth: 0 }} />,
                  )}
                </View>
              ))}
            </View>
          </>
        ) : null}
        </View>
      </ScrollView>
      <TransferStrip records={records} />
      <ComputerMenu visible={menu} entries={menuEntries} onClose={() => setMenu(false)} onPick={pickMenu} />
      <ComputerSwitcherSheet
        visible={switcher}
        hosts={hosts}
        selectedId={host.id}
        onClose={() => setSwitcher(false)}
        onSelect={(h) => {
          if (h.id !== host.id) activateHost(h);
        }}
        onReview={(h) => hostRoute('/host/[id]/trust', h.id)}
        onPair={pairAgain}
        onAdd={() => router.push('/pair')}
        onManage={() => router.push('/workspaces')}
        onChanged={onChanged}
      />
    </View>
  );
}

/** One recently changed file as a preview tile (`.prev figure`): a thumbnail for images, else its type glyph, with name and date below. */
function RecentTile({ hostId, entry, onPress }: { hostId: string; entry: Entry; onPress: () => void }): ReactNode {
  const c = useScheme();
  const chip = useIconChipBg(entry);
  const when = recentWhen(entry.modified, new Date(), { today: t('todayLabel'), yesterday: t('yesterdayLabel') });
  const glyph = (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: chip }}>
      <EntryLeading entry={entry} size={40} />
    </View>
  );
  return (
    <Pressable onPress={onPress} accessibilityLabel={entry.name} style={{ flex: 1, minWidth: 0 }}>
      <View style={{ height: RECENT_TILE_HEIGHT, borderRadius: Radii.file, overflow: 'hidden', borderWidth: 1, borderColor: c.outlineVariant }}>
        {isImageEntry(entry) ? <Thumbnail hostId={hostId} entry={entry} size={RECENT_TILE_HEIGHT * 2} style={{ flex: 1 }} fallback={glyph} /> : glyph}
      </View>
      <View style={{ marginTop: Spacing.sm }}>
        <Text numberOfLines={1} ellipsizeMode="middle" style={LumenType.caption}>
          {entry.name}
        </Text>
        {when ? (
          <Text numberOfLines={1} muted style={LumenType.meta}>
            {when}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

/** The newest files on [host] (real `/fs/recent`), refreshed on focus; null while offline, unloaded or unavailable. */
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
      .then((cl) => cl.recent({ limit: RECENT_ASK }))
      .then(
        (r) => live && setEntries(withoutHidden(r.entries).slice(0, RECENT_FETCH)),
        () => live && setEntries(null),
      );
    return () => {
      live = false;
    };
  }, [host, online, tick]);
  return online ? entries : null;
}
