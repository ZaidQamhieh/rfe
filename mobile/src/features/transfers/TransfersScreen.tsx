import { useRouter } from 'expo-router';
import { FolderOpen, MoreVertical, RotateCw, Trash2 } from 'lucide-react-native';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { FlatList, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { humanizeError } from '../../core/humanizeError';
import { ignoreError } from '../../core/log';
import type { Host } from '../../core/models/host';
import { transfers, type TransferRecord } from '../../core/native';
import { AppBarIconButton, Button, DuoIcon, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { ExplainerText, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { restartAutoRetry } from './useAutoRetry';
import { NameClashRow } from './NameClashRow';
import { SpeedTracker } from './speedTracker';
import { BackupSummaryRow, FinishedRow, MovingRow } from './TransferRows';
import { forgetTransfers } from './forgetTransfers';
import { applyTransferUpdate, markRetrying, mergeSnapshot, retryableFailures } from './transferLogic';
import { pauseAllTarget, sectionTransfers, splitBackupDone } from './transferSections';

type Tone = 'wait' | 'stop' | 'plain';
type Item =
  | { kind: 'head'; key: string; title: string; tone: Tone; clear?: TransferRecord[] }
  | { kind: 'row'; key: string; section: 'waiting' | 'moving' | 'failed' | 'done'; r: TransferRecord }
  | { kind: 'backup'; key: string; count: number };

/** `.sec`: a 14/500 sentence-case label; amber when something waits for the owner, red when it failed. */
function SectionHead({ title, tone, trailing }: { title: string; tone: Tone; trailing?: ReactNode }) {
  const c = useScheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', paddingTop: Spacing.md3, paddingBottom: 6, paddingHorizontal: Spacing.md }}>
      <Text accessibilityRole="header" style={[LumenType.sectionLabel, { flex: 1 }]} color={tone === 'wait' ? c.wait : tone === 'stop' ? c.stop : c.onSurfaceVariant}>
        {title}
      </Text>
      {trailing}
    </View>
  );
}


/** Nothing moved yet: a short title and one plain sentence (`.empty`). */
function EmptyMoves() {
  return (
    <View style={{ paddingTop: Spacing.md, paddingHorizontal: Spacing.lg }}>
      <Text style={LumenType.rowTitle} accessibilityRole="header">
        {t('trEmptyTitle')}
      </Text>
      <Text muted style={[{ ...ExplainerText, marginTop: Spacing.sm }]}>
        {t('trEmptyBody')}
      </Text>
    </View>
  );
}

/**
 * Every transfer the native engine knows, as flat rows in four groups: waiting for the owner (a name clash, with a
 * this-one-or-that-one compare), moving now, couldn't finish, and done. The engine and its queue are untouched.
 */
export function TransfersScreen() {
  const router = useRouter();
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const dialogs = useDialogs();
  const toast = useToast();
  const [items, setItems] = useState<TransferRecord[] | null>(null);
  const [speeds, setSpeeds] = useState<Record<string, number>>({});
  const [hosts, setHosts] = useState<Host[]>([]);

  useEffect(() => {
    let live = true;
    const tracker = new SpeedTracker();
    // Events can arrive before the list answers: the snapshot must not overwrite them.
    void transfers.list().then((l) => live && setItems((cur) => mergeSnapshot(l, cur))).catch(ignoreError('transfers: list'));
    void hostStore.listHosts().then((hs) => live && setHosts(hs)).catch(ignoreError('transfers: hosts'));
    const off = transfers.subscribe((r) => {
      const speed = tracker.sample(r.id, r.received, r.state === 'RUNNING');
      setSpeeds((cur) => {
        if (speed === null) {
          if (!(r.id in cur)) return cur;
          const { [r.id]: _gone, ...rest } = cur;
          return rest;
        }
        return { ...cur, [r.id]: speed };
      });
      setItems((cur) => applyTransferUpdate(cur, r));
    });
    return () => {
      live = false;
      off();
    };
  }, []);

  /** A Retry the owner taps: the transfer's automatic attempts start again. */
  const retryNow = (id: string) => {
    restartAutoRetry(id);
    setItems((cur) => (cur ? markRetrying(cur, id) : cur));
    // If the engine refuses, show what it really holds instead of a row that only looks queued.
    transfers.resume(id).catch(() => transfers.list().then((l) => setItems(l)).catch(ignoreError('transfers: list')));
  };

  const sections = useMemo(() => sectionTransfers(items ?? []), [items]);
  const pauseAll = pauseAllTarget(sections.moving);
  const hostById = useMemo(() => new Map(hosts.map((h) => [h.id, h])), [hosts]);
  const nameOf = (id: string) => {
    const h = hostById.get(id);
    return h ? h.label || h.address : undefined;
  };

  const forget = async (rs: TransferRecord[]) => {
    const fresh = await forgetTransfers(rs, transfers, (e) => toast.error(humanizeError(e)));
    if (fresh) setItems(fresh);
  };

  const data = useMemo<Item[]>(() => {
    const out: Item[] = [];
    const add = (section: 'waiting' | 'moving' | 'failed' | 'done', title: string, tone: Tone, rows: TransferRecord[], clear?: TransferRecord[]) => {
      if (rows.length === 0) return;
      out.push({ kind: 'head', key: `h-${section}`, title, tone, clear });
      for (const r of rows) out.push({ kind: 'row', key: r.id, section, r });
    };
    add('waiting', t('trWaiting'), 'wait', sections.waiting);
    add('moving', t('trMoving'), 'plain', sections.moving);
    add('failed', t('trFailed'), 'stop', sections.failed);
    if (sections.done.length > 0) {
      const finished = splitBackupDone(sections.done);
      out.push({ kind: 'head', key: 'h-done', title: t('trDone'), tone: 'plain', clear: sections.done });
      if (finished.backups.length > 0) out.push({ kind: 'backup', key: 'backup-done', count: finished.backups.length });
      for (const r of finished.others) out.push({ kind: 'row', key: r.id, section: 'done', r });
    }
    return out;
  }, [sections]);

  const more = async () => {
    const options: { value: string; label: string; icon: ReactNode }[] = [];
    const retryable = retryableFailures(sections.failed);
    if (retryable.length > 0) options.push({ value: 'retryAll', label: t('retryAllFailed', { count: retryable.length }), icon: <RotateCw size={20} color={c.onSurface} /> });
    if (sections.done.length > 0) options.push({ value: 'clear', label: t('trClearCompleted'), icon: <Trash2 size={20} color={c.onSurface} /> });
    if (sections.failed.length > 0) options.push({ value: 'clearFailed', label: t('trClearFailed'), icon: <Trash2 size={20} color={c.onSurface} /> });
    options.push({ value: 'files', label: t('trOpenFiles'), icon: <FolderOpen size={20} color={c.onSurface} /> });
    const pick = await dialogs.choose({ title: t('trTitle'), options });
    if (pick === 'retryAll') retryable.forEach((r) => retryNow(r.id));
    if (pick === 'clear') void forget(sections.done);
    if (pick === 'clearFailed') void forget(sections.failed);
    if (pick === 'files') router.navigate('/files');
  };

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <View style={{ paddingTop: insets.top, paddingLeft: 28, paddingRight: Spacing.xs, flexDirection: 'row', alignItems: 'center', minHeight: 56 + insets.top }}>
        <Text style={[LumenType.appbar, { flex: 1 }]} accessibilityRole="header">
          {t('trTitle')}
        </Text>
        {pauseAll.ids.length > 0 ? (
          <Button
            kind="text"
            size="s"
            label={pauseAll.resume ? t('trResumeAll') : t('trPauseAll')}
            onPress={() => void Promise.all(pauseAll.ids.map((id) => (pauseAll.resume ? transfers.resume(id) : transfers.pause(id))))}
          />
        ) : null}
        <AppBarIconButton label={t('trMore')} onPress={() => void more()}>
          <DuoIcon icon={MoreVertical} size={24} />
        </AppBarIconButton>
      </View>
      {items === null ? null : items.length === 0 ? (
        <EmptyMoves />
      ) : (
        <FlatList
          data={data}
          keyExtractor={(i) => i.key}
          contentContainerStyle={{ paddingBottom: Spacing.lg }}
          renderItem={({ item }) => {
            if (item.kind === 'head')
              return <SectionHead title={item.title} tone={item.tone} trailing={item.clear ? <Button kind="text" size="s" label={t('trClear')} onPress={() => void forget(item.clear!)} /> : undefined} />;
            if (item.kind === 'backup') return <BackupSummaryRow count={item.count} />;
            const r = item.r;
            if (item.section === 'waiting') return <NameClashRow r={r} host={hostById.get(r.hostId)} />;
            if (item.section === 'moving') return <MovingRow r={r} hostName={nameOf(r.hostId)} speed={speeds[r.id]} />;
            return <FinishedRow r={r} hostName={nameOf(r.hostId)} onForget={() => void forget([r])} onRetry={() => retryNow(r.id)} />;
          }}
        />
      )}
    </View>
  );
}
