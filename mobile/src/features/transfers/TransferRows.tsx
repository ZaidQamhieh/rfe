import { Images, MoreVertical, Pause, Play, ShieldCheck, Trash2, X } from 'lucide-react-native';
import { useEffect, useState, type ReactNode } from 'react';
import { View } from 'react-native';

import { formatSize } from '../../core/format';
import { ignoreError } from '../../core/log';
import { openPublicUri, sha256File, transfers, type TransferRecord } from '../../core/native';
import { AppBarIconButton, Button, DuoIcon, Emoji, Text, useDialogs, useToast } from '../../design/components';
import { ProgressBar } from '../../design/components/ProgressBar';
import { rowDensity } from '../../design/density';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost, hostStore } from '../../services';
import { useAppDensity } from '../../state/density';
import { emojiNameFor } from '../explorer/entryEmoji';
import { externalMime } from '../preview/externalFiles';
import { cancelTransfer } from './cancelTransfer';
import { etaSeconds, formatEta } from './speedTracker';
import { hasAutoRetryLeft } from './useAutoRetry';
import { finishedWhen, isActive, isAutoRetryable, isUpload, savedWhere, transferErrorMessage, transferName, verifyOutcome, type VerifyOutcome } from './transferLogic';
import { movingLine, percentDone } from './transferSections';

const SUMMARY_LEAD = 40;
const SUMMARY_ICON = 28;
/** How often a finished row is drawn again so its "5 minutes ago" text moves on, in ms. */
const AGE_TICK_MS = 20_000;
const SUMMARY_GAP = 16;
const SUMMARY_PAD_X = 16;

/** The finished photo-backup uploads as one line, "Photo backup: 14 photos", instead of one row per photo. */
export function BackupSummaryRow({ count }: { count: number }) {
  const c = useScheme();
  const size = rowDensity(useAppDensity());
  return (
    <View style={{ minHeight: size.frameMinHeight, flexDirection: 'row', alignItems: 'center', gap: SUMMARY_GAP, paddingVertical: size.framePaddingY, paddingHorizontal: SUMMARY_PAD_X }}>
      <View style={{ width: SUMMARY_LEAD, height: SUMMARY_LEAD, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={Images} size={SUMMARY_ICON} color={c.onSurfaceVariant} />
      </View>
      <Text numberOfLines={1} style={[LumenType.name, { flex: 1 }]}>
        {t('trBackupSummary', { count })}
      </Text>
    </View>
  );
}

/** A flat row in the shape of the app's `.row`: a 40x40 lead, the text column, then whatever sits at the end. */
export function TransferFrame({ r, children, end }: { r: TransferRecord; children: ReactNode; end?: ReactNode }) {
  const name = transferName(r);
  const size = rowDensity(useAppDensity());
  return (
    <View style={{ minHeight: size.frameMinHeight, flexDirection: 'row', alignItems: 'flex-start', gap: Spacing.md, paddingVertical: size.framePaddingY, paddingHorizontal: Spacing.md }}>
      <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
        <Emoji name={emojiNameFor({ isDir: false, name })} size={28} />
      </View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text numberOfLines={1} ellipsizeMode="middle" style={LumenType.name}>
          {name}
        </Text>
        {children}
      </View>
      {end}
    </View>
  );
}

/** One muted 14/20 line under a name. */
export function MetaLine({ children, color, lines = 1 }: { children: ReactNode; color?: string; lines?: number }) {
  return (
    <Text muted={!color} color={color} numberOfLines={lines} style={LumenType.meta}>
      {children}
    </Text>
  );
}

/**
 * A transfer in progress: the name, "to Studio PC · 63%", a 4px bar (amber while paused), the bytes done as a mono figure
 * with the time left beside it, and pause or resume plus cancel as icon buttons.
 */
export function MovingRow({ r, hostName, speed }: { r: TransferRecord; hostName?: string; speed?: number }) {
  const c = useScheme();
  const paused = r.state === 'PAUSED';
  const pct = percentDone(r);
  const eta = isActive(r) && speed !== undefined ? etaSeconds(r.total, r.received, speed) : null;
  const status = paused ? t('trPaused') : eta !== null ? t('trLeft', { eta: formatEta(eta) }) : r.state === 'QUEUED' ? t('trWaitingToStart') : '';
  const toggle = paused ? { label: t('trResume'), icon: Play, run: () => transfers.resume(r.id) } : { label: t('trPause'), icon: Pause, run: () => transfers.pause(r.id) };
  return (
    <TransferFrame
      r={r}
      end={
        <View style={{ flexDirection: 'row', marginTop: -4, marginRight: -12 }}>
          <AppBarIconButton label={toggle.label} onPress={() => void toggle.run()}>
            <DuoIcon icon={toggle.icon} size={24} />
          </AppBarIconButton>
          <AppBarIconButton label={t('trCancel')} onPress={() => void cancelTransfer(r.id, transfers.cancel, ignoreError('transfers: cancel'))}>
            <DuoIcon icon={X} size={24} color={c.onSurfaceVariant} />
          </AppBarIconButton>
        </View>
      }
    >
      <MetaLine>{movingLine(r, hostName)}</MetaLine>
      {pct !== null ? <ProgressBar fraction={pct / 100} waiting={paused} label={transferName(r)} /> : null}
      <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: Spacing.md2 }}>
        {r.total > 0 ? (
          <Text muted style={{ fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 20 }}>{`${formatSize(r.received)} / ${formatSize(r.total)}`}</Text>
        ) : (
          <View />
        )}
        {status ? <MetaLine color={paused ? c.wait : undefined}>{status}</MetaLine> : null}
      </View>
    </TransferFrame>
  );
}

/**
 * A finished, cancelled or failed transfer. A finished download opens with a tap on "Open"; Retry restarts a failed one.
 * The "more" button holds the rarer actions: check a download against the computer's copy, and remove the row.
 */
export function FinishedRow({ r, hostName, onForget, onRetry }: { r: TransferRecord; hostName?: string; onForget: () => void; onRetry: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const [verified, setVerified] = useState<VerifyOutcome | 'checking' | null>(null);
  // "5 minutes ago" must not stay frozen while the screen is open: draw the row again every 20 s.
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), AGE_TICK_MS);
    return () => clearInterval(id);
  }, []);
  const name = transferName(r);
  const up = isUpload(r);
  const failed = r.state === 'FAILED';
  const done = r.state === 'DONE';
  const openable = done && !!r.publicUri;

  const open = async () => {
    if (!r.publicUri || !(await openPublicUri(r.publicUri, externalMime({ name })))) toast.info(t('trNoApp'));
  };
  const verify = async () => {
    setVerified('checking');
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === r.hostId);
      if (!host) return setVerified('unavailable');
      const [local, remote] = await Promise.all([sha256File(r.publicUri ?? r.destPath), clientForHost(host).then((cl) => cl.checksum(r.remotePath))]);
      setVerified(verifyOutcome(local, remote));
    } catch {
      // The check is advisory: a network or read failure is shown as "could not check", never as a mismatch.
      setVerified('unavailable');
    }
  };
  const more = async () => {
    const options: { value: 'verify' | 'remove'; label: string; icon: ReactNode }[] = [];
    if (done && !up && verified !== 'checking') options.push({ value: 'verify', label: t('verifyDownloadButton'), icon: <ShieldCheck size={20} color={c.onSurface} /> });
    options.push({ value: 'remove', label: t('trRemove'), icon: <Trash2 size={20} color={c.error} /> });
    const pick = await dialogs.choose({ title: name, options });
    if (pick === 'verify') void verify();
    if (pick === 'remove') onForget();
  };

  const place = hostName ? ` ${up ? t('trTo', { computer: hostName }) : t('trFrom', { computer: hostName })}` : '';
  const when = finishedWhen(r);
  const verdict = verified === 'checking' ? t('verifyChecking') : verified === 'verified' ? t('verifyVerified') : verified === 'mismatch' ? t('verifyMismatch') : verified === 'unavailable' ? t('verifyUnavailable') : '';
  const line = failed ? `${transferErrorMessage(r.error)}${isAutoRetryable(r) && hasAutoRetryLeft(r.id) ? ` ${t('trRetryingSoon')}` : ''}` : done ? `${savedWhere(r)}${place}${when ? ` · ${when}` : ''}` : t('trCancelled');
  return (
    <TransferFrame
      r={r}
      end={
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs, marginRight: -12, marginTop: -4 }}>
          {failed ? <Button kind="neutral" size="s" label={t('trRetry')} onPress={() => void onRetry()} /> : null}
          {openable ? <Button kind="text" size="s" label={t('trOpen')} onPress={() => void open()} /> : null}
          <AppBarIconButton label={t('trMore')} onPress={() => void more()}>
            <DuoIcon icon={MoreVertical} size={24} color={c.onSurfaceVariant} />
          </AppBarIconButton>
        </View>
      }
    >
      <MetaLine lines={failed ? 3 : 2}>{verdict ? `${line} · ${verdict}` : line}</MetaLine>
    </TransferFrame>
  );
}
