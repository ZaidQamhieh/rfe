import { File as FileIcon, FileArchive, FileText, Image as ImageIcon, Music, Pause, Play, RotateCw, ShieldCheck, Trash2, Video, X, type LucideIcon } from 'lucide-react-native';
import { useRouter } from 'expo-router';
import { useEffect, useMemo, useState } from 'react';
import { FlatList, View } from 'react-native';

import { formatSize } from '../../core/format';
import { openPublicUri, sha256File, transfers, type TransferRecord } from '../../core/native';
import { mix } from '../../design/color';
import { Button, EmptyState, GroupedCard, PageHead, Pressable, SectionLabel, Text, TopBar, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost, hostStore } from '../../services';
import { externalMime } from '../preview/externalFiles';
import { etaSeconds, formatEta, formatSpeed, SpeedTracker } from './speedTracker';
import { finishedWhen, groupTransfers, isActive, isUpload, retryableFailures, savedWhere, transferErrorMessage, transferKind, transferName, transferPillLabel, transferProgress, transferSummary, transferTone, verifyOutcome, type TransferKind, type VerifyOutcome } from './transferLogic';

const GLYPHS: Record<TransferKind['glyph'], LucideIcon> = { image: ImageIcon, video: Video, audio: Music, archive: FileArchive, doc: FileText, file: FileIcon };

type Item = { kind: 'head'; key: string; title: string; clear?: TransferRecord[] } | { kind: 'row'; key: string; r: TransferRecord };

/** Every transfer the native engine knows, sectioned Active / Completed / Failed, each with its own controls. */
export function TransfersScreen() {
  const router = useRouter();
  const c = useScheme();
  const dialogs = useDialogs();
  const [items, setItems] = useState<TransferRecord[] | null>(null);
  const [speeds, setSpeeds] = useState<Record<string, number>>({});
  const [hostNames, setHostNames] = useState<Record<string, string>>({});

  useEffect(() => {
    let live = true;
    const tracker = new SpeedTracker();
    void transfers.list().then((l) => live && setItems(l));
    void hostStore.listHosts().then((hs) => live && setHostNames(Object.fromEntries(hs.map((h) => [h.id, h.label || h.address]))));
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
      setItems((cur) => [...(cur ?? []).filter((x) => x.id !== r.id), r]);
    });
    return () => {
      live = false;
      off();
    };
  }, []);

  const groups = useMemo(() => groupTransfers(items ?? []), [items]);
  const running = groups.active.filter(isActive).length;
  const data = useMemo<Item[]>(() => {
    const out: Item[] = [];
    const add = (key: string, title: string, rows: TransferRecord[], clear?: TransferRecord[]) => {
      if (rows.length === 0) return;
      out.push({ kind: 'head', key: `h-${key}`, title, clear });
      for (const r of rows) out.push({ kind: 'row', key: r.id, r });
    };
    add('active', 'In progress', groups.active);
    // Failures need a decision (Retry or Remove), so they come before the finished list rather than under it.
    add('failed', 'Failed', groups.failed);
    add('done', 'Recent', groups.finished, groups.finished);
    return out;
  }, [groups]);

  const forget = async (rs: TransferRecord[]) => {
    await Promise.all(rs.map((r) => transfers.remove(r.id)));
    const gone = new Set(rs.map((r) => r.id));
    setItems((cur) => (cur ?? []).filter((x) => !gone.has(x.id)));
  };

  const more = async () => {
    const options: { value: string; label: string; icon: React.ReactNode }[] = [];
    if (groups.finished.length > 0) options.push({ value: 'clear', label: 'Clear completed', icon: <Trash2 size={20} color={c.onSurface} /> });
    if (groups.failed.length > 0) options.push({ value: 'clearFailed', label: 'Clear failed', icon: <Trash2 size={20} color={c.onSurface} /> });
    const retryable = retryableFailures(groups.failed);
    if (retryable.length > 0) options.unshift({ value: 'retryAll', label: t('retryAllFailed', { count: retryable.length }), icon: <RotateCw size={20} color={c.onSurface} /> });
    options.push({ value: 'files', label: 'Open Files', icon: <FileIcon size={20} color={c.onSurface} /> });
    const pick = await dialogs.choose({ title: t('transfersSettingsTitle'), options });
    if (pick === 'retryAll') void Promise.all(retryable.map((r) => transfers.resume(r.id)));
    if (pick === 'clear') void forget(groups.finished);
    if (pick === 'clearFailed') void forget(groups.failed);
    if (pick === 'files') router.navigate('/files');
  };

  return (
    <View style={{ flex: 1 }}>
      <TopBar context="Activity" sub="Transfer activity" right={running > 0 ? <Pill label={`${running} active`} color={c.primary} /> : undefined} onMore={() => void more()} />
      <PageHead title="Transfers" subtitle={items === null ? undefined : transferSummary(groups)} />
      {items === null ? null : items.length === 0 ? (
        <EmptyState message="No transfers yet" action={{ label: 'Open Files', onPress: () => router.navigate('/files') }} />
      ) : (
        <FlatList
          data={data}
          keyExtractor={(i) => i.key}
          contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24 }}
          renderItem={({ item }) =>
            item.kind === 'head' ? (
              <SectionLabel title={item.title} trailing={item.clear ? <TextAction label="Clear" onPress={() => void forget(item.clear!)} /> : undefined} />
            ) : (
              <View style={{ marginBottom: 12 }}>
                <Row r={item.r} speed={speeds[item.r.id]} hostName={hostNames[item.r.hostId]} onForget={() => void forget([item.r])} />
              </View>
            )
          }
        />
      )}
    </View>
  );
}

function TextAction({ label, onPress }: { label: string; onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={`${label} completed transfers`} style={{ minHeight: 48, paddingHorizontal: 8, justifyContent: 'center' }}>
      <Text style={LumenType.pill} color={c.primary}>{label}</Text>
    </Pressable>
  );
}

/** `.state-pill` in any colour: 15% of the tone over the card. */
function Pill({ label, color }: { label: string; color: string }) {
  const c = useScheme();
  return (
    <View style={{ paddingVertical: 8, paddingHorizontal: 12, borderRadius: 24, backgroundColor: mix(color, c.surfaceContainer, 0.15) }}>
      <Text style={LumenType.pill} color={color} numberOfLines={1}>{label}</Text>
    </View>
  );
}

/** `.transfer-card`: file glyph + name + state pill, a detail line, then the bar and pace for live transfers. */
function Row({ r, speed, hostName, onForget }: { r: TransferRecord; speed?: number; hostName?: string; onForget: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  const toast = useToast();
  // The result of "Verify" for this row; kept only while the screen is open.
  const [verified, setVerified] = useState<VerifyOutcome | 'checking' | null>(null);
  // "5s ago" must not stay frozen while the screen is open: draw the row again every 20 s (audit F-37).
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), 20_000);
    return () => clearInterval(id);
  }, []);
  const name = transferName(r);
  const kind = transferKind(name);
  const Glyph = GLYPHS[kind.glyph];
  const glyphColor = kind.role ? roles[kind.role] : c.onSurfaceVariant;
  const up = isUpload(r);
  const progress = transferProgress(r);
  const failed = r.state === 'FAILED';
  const live = r.state === 'RUNNING' || r.state === 'QUEUED';
  const paused = r.state === 'PAUSED';
  const done = r.state === 'DONE';
  const tone = transferTone(r.state);
  const tint = tone === 'error' ? c.error : tone === 'muted' ? c.onSurfaceVariant : roles[tone];
  const openable = done && !!r.publicUri;
  const tappable = openable || paused;

  const open = async () => {
    if (!r.publicUri || !(await openPublicUri(r.publicUri, externalMime({ name })))) toast.info('No app can open this file');
  };
  const tap = () => {
    if (paused) void transfers.resume(r.id);
    else if (openable) void open();
  };

  const verify = async () => {
    setVerified('checking');
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === r.hostId);
      if (!host) return setVerified('unavailable');
      const [local, remote] = await Promise.all([sha256File(r.publicUri ?? r.destPath), clientForHost(host).then((cl) => cl.checksum(r.remotePath))]);
      setVerified(verifyOutcome(local, remote));
    } catch {
      setVerified('unavailable');
    }
  };

  const place = hostName ? ` ${up ? 'to' : 'from'} ${hostName}` : '';
  const size = r.total > 0 ? formatSize(r.total) : r.received > 0 ? formatSize(r.received) : '';
  const lead = size ? `${size} · ` : '';
  const detail = failed
    ? transferErrorMessage(r.error)
    : done
      ? `${lead}${savedWhere(r)}${place}${finishedWhen(r) ? ` · ${finishedWhen(r)}` : ''}`
      : r.state === 'CANCELLED'
        ? `${lead}Cancelled`
        : paused
          ? `${lead}Tap to resume`
          : r.state === 'QUEUED'
            ? `${lead}Waiting${place}`
            : `${lead}${up ? 'Sending' : 'Receiving'}${place}`;
  const verdictText = verified === 'checking' ? t('verifyChecking') : verified === 'verified' ? t('verifyVerified') : verified === 'mismatch' ? t('verifyMismatch') : verified === 'unavailable' ? t('verifyUnavailable') : '';
  const eta = live && speed !== undefined ? etaSeconds(r.total, r.received, speed) : null;
  const pace = live && speed !== undefined ? `${formatSpeed(speed)}${eta === null ? '' : ` · about ${formatEta(eta)} left`}` : '';
  const showBar = progress !== null && !done && r.state !== 'CANCELLED';

  const actions: { key: string; label: string; Icon: LucideIcon; primary?: boolean; onPress: () => void }[] = [];
  if (live) actions.push({ key: 'pause', label: 'Pause', Icon: Pause, onPress: () => void transfers.pause(r.id) });
  if (paused) actions.push({ key: 'resume', label: 'Resume', Icon: Play, primary: true, onPress: () => void transfers.resume(r.id) });
  if (failed) actions.push({ key: 'retry', label: 'Retry', Icon: RotateCw, primary: true, onPress: () => void transfers.resume(r.id) });
  if (openable) actions.push({ key: 'open', label: 'Open', Icon: FileIcon, primary: true, onPress: () => void open() });
  if (done && !up && verified !== 'checking') actions.push({ key: 'verify', label: t('verifyDownloadButton'), Icon: ShieldCheck, onPress: () => void verify() });
  if (live || paused) actions.push({ key: 'cancel', label: 'Cancel', Icon: X, onPress: () => void transfers.cancel(r.id) });
  if (failed || done || r.state === 'CANCELLED') actions.push({ key: 'remove', label: 'Remove', Icon: Trash2, onPress: onForget });

  return (
    <GroupedCard padded={false} style={{ padding: 16 }}>
      <Pressable disabled={!tappable} onPress={tap} accessibilityRole={tappable ? 'button' : 'summary'} accessibilityLabel={tappable ? `${paused ? 'Resume' : 'Open'} ${name}` : undefined}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
          <Glyph size={26} color={glyphColor} />
          <Text style={[LumenType.rowTitle, { flex: 1 }]} numberOfLines={1}>{name}</Text>
          <Pill label={transferPillLabel(r)} color={tint} />
        </View>
        <Text style={[LumenType.meta, { marginTop: 6, marginLeft: 38 }]} color={c.onSurfaceVariant} numberOfLines={failed ? 3 : 2}>{verdictText ? `${detail} · ${verdictText}` : detail}</Text>
        {showBar ? (
          <View accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 100, now: Math.round(progress * 100) }} style={{ height: 6, borderRadius: 8, marginTop: 12, backgroundColor: c.surfaceContainerHigh, overflow: 'hidden' }}>
            <View style={{ width: `${progress * 100}%`, height: 6, borderRadius: 8, backgroundColor: tint }} />
          </View>
        ) : null}
        {showBar && r.total > 0 ? (
          <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: 12, marginTop: 10 }}>
            <Text style={[LumenType.meta, { flex: 1 }]} color={c.onSurfaceVariant} numberOfLines={2}>{pace}</Text>
            <Text style={LumenType.meta} color={tint}>{`${formatSize(r.received)} of ${formatSize(r.total)}`}</Text>
          </View>
        ) : null}
      </Pressable>
      {actions.length > 0 ? (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'flex-end', gap: 10, marginTop: 12 }}>
          {/* Wraps: Open + Verify + Remove do not fit one row on a 360 dp phone (the first button was pushed out of the card). */}
          {actions.map((a) => (
            <Button
              key={a.key}
              kind={a.primary ? 'filled' : 'neutral'}
              label={a.label}
              onPress={a.onPress}
              renderIcon={(fg) => <a.Icon size={16} color={fg} />}
            />
          ))}
        </View>
      ) : null}
    </GroupedCard>
  );
}
