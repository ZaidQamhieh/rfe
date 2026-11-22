import * as Clipboard from 'expo-clipboard';
import { Archive, Calendar, CalendarClock, Copy, Download, ExternalLink, Eye, FilePen, Info, Link as LinkIcon, Lock, Route, Ruler, Share2, Star, Tag, Trash2, type LucideIcon } from 'lucide-react-native';
import { useEffect, useState, type ReactNode } from 'react';
import { ScrollView, View } from 'react-native';

import type { AgentClient } from '../../core/api/agentClient';
import { can, type Entry } from '../../core/api/models';
import { formatDate, formatSize } from '../../core/format';
import type { Host } from '../../core/models/host';
import { BottomSheet, Disclosure, Pressable, SheetGrabber, SheetHero, Text, useDialogs, useToast } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { useCollections } from '../../state/collections';
import { humanizeError } from '../../core/humanizeError';
import { useExternalActions } from '../preview/useExternalActions';
import { isPreviewable } from '../preview/previewKind';
import { ShareLinkSheet } from '../share/ShareLinkSheet';
import { enqueueDownloads } from '../transfers/enqueueDownloads';
import { ChmodDialog } from './ChmodDialog';
import { EntryLeading, useIconChipBg } from './EntryIcon';
import { metaAccessNotes } from './metaAccessNote';
import { hashMatches, isExtractableArchive } from './metaLogic';
import { useFileCapabilities } from './useFileCapabilities';
import { renameUndo } from './undoPlan';
import { chooseDeleteMode } from './deleteChoice';
import { deleteDoneMessage } from './deleteToast';
import {
  META_CHECKSUM_FONT_SIZE,
  META_DETAIL_ICON_SIZE,
  META_DETAIL_LABEL_WIDTH,
  META_HERO_ICON_SIZE,
  META_MORE_ICON_SIZE,
  META_MORE_ROW_MIN_HEIGHT,
  META_MORE_TILE_RADIUS,
  META_MORE_TILE_SIZE,
  META_QUICK_GAP,
  META_QUICK_ICON_SIZE,
  META_QUICK_LABEL_SIZE,
  META_QUICK_MIN_HEIGHT,
  META_ROW_DIVIDER_GAP,
} from './metaConstants';
import { toggleFavoriteToast } from './favoriteToggle';
import { extractToFreshFolder } from './extractToFolder';
import { isSameName, parentDirOf, renameDestination } from './paths';
import { invalidNameReason } from './nameValidation';
import { clientForHost } from '../../services';

type Action = { key: string; icon: LucideIcon; label: string; onPress: () => void; tint?: string };

/**
 * Per-entry details sheet (port of MetaSheet): a row of quick actions, a list of secondary actions and a
 * Details view with permissions and an on-demand checksum. Every mutation reports through `onChanged` so the
 * caller can refresh its listing, then closes the sheet.
 */
export function MetaSheet({ visible, host, entry: initial, onClose, onChanged, onPreview }: { visible: boolean; host: Host; entry: Entry; onClose: () => void; onChanged?: () => void; onPreview?: (e: Entry) => void }) {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const favorites = useCollections((s) => s.favorites);
  const toggleFavorite = useCollections((s) => s.toggleFavorite);
  const [entry, setEntry] = useState(initial);
  const [view, setView] = useState<'actions' | 'details'>('actions');
  const [client, setClient] = useState<AgentClient | null>(null);
  const caps = useFileCapabilities(host);
  const [chmodOpen, setChmodOpen] = useState(false);
  const [checksum, setChecksum] = useState<string | null>(null);
  const [checksumBusy, setChecksumBusy] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  // The sheet steps aside while one of its dialogs (expiry choice, pasted hash) is on screen.
  const [dialogOpen, setDialogOpen] = useState(false);
  const chip = useIconChipBg(entry);
  const external = useExternalActions(host);

  // Re-seed whenever a (different) entry is shown, then refresh its metadata from the agent.
  const [seenFor, setSeenFor] = useState<Entry | null>(null);
  if (visible && seenFor !== initial) {
    setSeenFor(initial);
    setEntry(initial);
    setView('actions');
    setChecksum(null);
  }
  useEffect(() => {
    if (!visible) return;
    let live = true;
    clientForHost(host).then(async (cl) => {
      if (!live) return;
      setClient(cl);
      try {
        const fresh = await cl.meta(initial.path);
        if (live) setEntry(fresh);
      } catch {
        // keep the listed entry
      }
    });
    return () => {
      live = false;
    };
  }, [visible, host, initial.path]);

  const isFav = entry.isDir && favorites.some((f) => f.hostId === host.id && f.path === entry.path);
  const previewable = !entry.isDir && isPreviewable(entry);
  const changed = () => onChanged?.();

  /** Runs a client call; failures toast. Toasts render under the sheet's window, so a still-open sheet is closed first. */
  async function run<T>(task: (cl: AgentClient) => Promise<T>, fail: (e: string) => string, closeOnError = false): Promise<T | undefined> {
    try {
      return await task(client ?? (await clientForHost(host)));
    } catch (e) {
      if (closeOnError) onClose();
      toast.error(fail(humanizeError(e)));
      return undefined;
    }
  }

  async function favorite() {
    onClose();
    await toggleFavoriteToast((o) => toggleFavorite(o), toast, { hostId: host.id, path: entry.path, name: entry.name, isFav });
  }

  async function download() {
    onClose();
    try {
      await enqueueDownloads(host, [entry.path]);
      toast.info(t('downloadingFile', { name: entry.name }));
    } catch (e) {
      toast.error(humanizeError(e));
    }
  }

  async function rename() {
    onClose();
    const name = await dialogs.prompt({ title: t('renameButton'), placeholder: t('newNameLabel'), initialValue: entry.name, confirmLabel: t('renameButton') });
    if (!name || isSameName(entry.name, name)) return;
    const bad = invalidNameReason(name);
    if (bad) return void toast.error(bad);
    const target = renameDestination(entry.path, name);
    const updated = await run((cl) => cl.rename(entry.path, target), (e) => t('renameFailed', { error: e }));
    if (updated) {
      changed();
      const back = renameUndo(entry.path, target);
      // The host refuses to replace anything, so undoing into a name that was taken meanwhile fails instead of overwriting.
      const undo = back?.kind === 'rename' ? async () => {
        const undone = await run((cl) => cl.rename(back.from, back.to), (e) => t('undoFailed', { error: e }));
        if (undone) {
          changed();
          toast.success(t('undoDone'));
        }
      } : undefined;
      toast.success(t('renamedTo', { newName: name }), undo ? () => void undo() : undefined);
    }
  }

  async function duplicate() {
    onClose();
    const res = await run((cl) => cl.copy([entry.path], parentDirOf(entry.path), { duplicate: true }), (e) => t('duplicateFailed', { error: e }));
    if (!res) return;
    if (!(res.results.length > 0 && res.results[0].ok)) return void toast.error(t('couldNotDuplicate', { name: entry.name }));
    changed();
    toast.success(t('duplicatedFile', { name: entry.name }));
  }

  async function extract() {
    onClose();
    const out = await run((cl) => extractToFreshFolder(cl, entry), (e) => t('extractFailed', { error: e }));
    if (out) {
      changed();
      toast.success(t('extractedToFolder', { folder: out }));
    }
  }

  async function remove() {
    onClose();
    const choice = await chooseDeleteMode(dialogs, { subtitle: t('moveToTrashConfirm', { name: entry.name }), tint: c.error });
    if (!choice) return;
    const permanent = choice === 'forever';
    const res = await run((cl) => cl.delete([entry.path], { permanent }), (e) => t('deleteFailed', { error: e }));
    if (res) {
      changed();
      toast.success(deleteDoneMessage(permanent, entry.name));
    }
  }

  /** Opens the share sheet for this file; it picks the length and mints the link itself. */
  async function shareLink() {
    if (!client) setClient(await clientForHost(host));
    setShareOpen(true);
  }

  const checksumOf = () => run((cl) => cl.checksum(entry.path), (e) => t('checksumFailed', { error: e }));

  /** Compares the file's SHA-256 with a hash the user pastes, and says plainly whether they match. */
  async function verifyHash() {
    setDialogOpen(true);
    try {
      const pasted = await dialogs.prompt({ title: t('verifyHashTitle'), placeholder: t('verifyHashPlaceholder'), confirmLabel: t('verifyButton'), mono: true });
      if (!pasted) return;
      const sum = checksum ?? (await checksumOf());
      if (!sum) return;
      setChecksum(sum);
      await dialogs.report({
        title: hashMatches(pasted, sum) ? t('hashMatchTitle') : t('hashMismatchTitle'),
        items: [
          { primary: t('hashExpected'), secondary: pasted },
          { primary: t('hashActual'), secondary: sum },
        ],
      });
    } finally {
      setDialogOpen(false);
    }
  }

  async function computeChecksum() {
    setChecksumBusy(true);
    const sum = await checksumOf();
    if (sum) setChecksum(sum);
    setChecksumBusy(false);
  }

  // Rename and duplicate are offered for both folders and files, so each is defined once and placed in either list.
  const renameAction: Action | null = can(caps, 'modify') ? { key: 'rename', icon: FilePen, label: t('renameButton'), onPress: rename } : null;
  const dupAction: Action | null = can(caps, 'modify') ? { key: 'dup', icon: Copy, label: t('duplicateButton'), onPress: duplicate } : null;

  const quick: Action[] = ([
    entry.isDir
      ? { key: 'fav', icon: Star, label: isFav ? t('unfavoriteButton') : t('favoriteButton'), onPress: favorite }
      : previewable && onPreview
        ? { key: 'preview', icon: Eye, label: t('previewButton'), onPress: () => { onClose(); onPreview(entry); } }
        : null,
    entry.isDir
      ? renameAction
      : can(caps, 'download') ? { key: 'download', icon: Download, label: t('downloadButton'), onPress: download } : null,
    entry.isDir ? dupAction : null,
    can(caps, 'delete') ? { key: 'delete', icon: Trash2, label: t('deleteButton'), onPress: remove, tint: c.error } : null,
  ] as (Action | null)[]).filter((a): a is Action => a !== null);

  const more: Action[] = ([
    !entry.isDir && can(caps, 'modify') && isExtractableArchive(entry.name) ? { key: 'extract', icon: Archive, label: t('extractToFolderButton'), onPress: extract } : null,
    !entry.isDir && can(caps, 'download') ? { key: 'openwith', icon: ExternalLink, label: t('openWithButton'), onPress: () => { onClose(); void external.openWith(entry); } } : null,
    !entry.isDir && can(caps, 'download') ? { key: 'share', icon: Share2, label: t('shareTooltip'), onPress: () => { onClose(); void external.share(entry); } } : null,
    !entry.isDir && can(caps, 'share') ? { key: 'link', icon: LinkIcon, label: t('shareLinkButton'), onPress: shareLink } : null,
    !entry.isDir ? renameAction : null,
    !entry.isDir ? dupAction : null,
    { key: 'details', icon: Info, label: t('detailsButton'), onPress: () => setView('details') },
  ] as (Action | null)[]).filter((a): a is Action => a !== null);

  const accessNotes = metaAccessNotes(caps, entry);

  const subtitle = [entry.size != null && !entry.isDir ? formatSize(entry.size) : null, entry.modified ? formatDate(new Date(entry.modified)) : null].filter(Boolean).join(t('metaSubtitleSeparator'));

  return (
    <>
      <BottomSheet visible={visible && !shareOpen && !dialogOpen} onClose={onClose}>
        {view === 'actions' ? (
          <ScrollView>
            <SheetHero badge={<EntryLeading entry={entry} size={META_HERO_ICON_SIZE} />} badgeColor={chip} title={entry.name} subtitle={subtitle} onClose={onClose} />
            <View style={{ paddingHorizontal: Spacing.lg, paddingBottom: Spacing.xl, gap: Spacing.md }}>
              <View style={{ flexDirection: 'row' }}>
                {quick.map((a) => (
                  <QuickAction key={a.key} action={a} />
                ))}
              </View>
              <View>
                {more.map((a, i) => (
                  <MoreRow key={a.key} action={a} divider={i > 0} />
                ))}
              </View>
              {accessNotes.map((k) => (
                <Text key={k} muted style={LumenType.caption}>{t(k)}</Text>
              ))}
            </View>
          </ScrollView>
        ) : (
          <ScrollView>
            <View style={{ paddingTop: Spacing.md, paddingHorizontal: Spacing.lg, paddingBottom: Spacing.xl, gap: Spacing.md }}>
              <SheetGrabber />
              <Text style={LumenType.title} accessibilityRole="header">{t('detailsButton')}</Text>
              <View style={{ backgroundColor: c.surfaceContainerHigh, borderRadius: LumenSize.cardRadius, paddingHorizontal: Spacing.md, paddingVertical: Spacing.xs }}>
                <DetailRows entry={entry} checksum={checksum} checksumBusy={checksumBusy} onComputeChecksum={computeChecksum} onVerifyHash={() => void verifyHash()} onEditPermissions={() => setChmodOpen(true)} />
              </View>
            </View>
          </ScrollView>
        )}
      </BottomSheet>
      {client && (
        <ChmodDialog
          visible={chmodOpen}
          entry={entry}
          client={client}
          onClose={() => setChmodOpen(false)}
          onApplied={(updated) => {
            setEntry(updated);
            setChmodOpen(false);
            changed();
          }}
        />
      )}
      {shareOpen && client && <ShareLinkSheet visible client={client} path={entry.path} fileName={entry.name} computerName={host.label} onClose={() => { setShareOpen(false); onClose(); }} />}
    </>
  );
}

function QuickAction({ action }: { action: Action }) {
  const c = useScheme();
  const Icon = action.icon;
  return (
    <Pressable onPress={action.onPress} accessibilityLabel={action.label} style={{ flex: 1 }}>
      <View style={{ marginHorizontal: Spacing.xs, minHeight: META_QUICK_MIN_HEIGHT,paddingVertical: Spacing.sm, paddingHorizontal: Spacing.xs, backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.lg, alignItems: 'center', justifyContent: 'center', gap: META_QUICK_GAP }}>
        <Icon size={META_QUICK_ICON_SIZE} color={action.tint ?? c.primary} />
        <Text numberOfLines={1} color={action.tint} style={[LumenType.pill, { textAlign: 'center', fontSize: META_QUICK_LABEL_SIZE }]}>{action.label}</Text>
      </View>
    </Pressable>
  );
}

function MoreRow({ action, divider }: { action: Action; divider: boolean }) {
  const c = useScheme();
  const Icon = action.icon;
  return (
    <Pressable onPress={action.onPress} accessibilityLabel={action.label}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, minHeight: META_MORE_ROW_MIN_HEIGHT, paddingVertical: Spacing.xs, marginTop: divider ? META_ROW_DIVIDER_GAP : 0 }}>
        <View style={{ width: META_MORE_TILE_SIZE, height: META_MORE_TILE_SIZE, borderRadius: META_MORE_TILE_RADIUS, backgroundColor: c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>
          <Icon size={META_MORE_ICON_SIZE} color={c.onSurfaceVariant} />
        </View>
        <Text style={LumenType.name}>{action.label}</Text>
      </View>
    </Pressable>
  );
}

function DetailRows({ entry, checksum, checksumBusy, onComputeChecksum, onVerifyHash, onEditPermissions }: { entry: Entry; checksum: string | null; checksumBusy: boolean; onComputeChecksum: () => void; onVerifyHash: () => void; onEditPermissions: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const rows: ReactNode[] = [];
  const add = (key: string, icon: LucideIcon, label: string, value: ReactNode, onPress?: () => void) => rows.push(<DetailRow key={key} icon={icon} label={label} value={value} onPress={onPress} />);
  // Full path, permissions and link details are for people who need them, so they sit under "Technical details".
  const technical: ReactNode[] = [];
  const addTechnical = (key: string, icon: LucideIcon, label: string, value: ReactNode, onPress?: () => void) => technical.push(<DetailRow key={key} icon={icon} label={label} value={value} onPress={onPress} />);
  addTechnical('path', Route, t('metaPath'), entry.path);
  if (entry.size != null && !entry.isDir) add('size', Ruler, t('metaSize'), formatSize(entry.size));
  if (entry.mimeType) add('type', Tag, t('metaType'), entry.mimeType);
  if (entry.modified) add('mod', CalendarClock, t('metaModified'), new Date(entry.modified).toLocaleString());
  if (entry.created) add('created', Calendar, t('metaCreated'), new Date(entry.created).toLocaleString());
  if (entry.mode) addTechnical('mode', Lock, t('metaPermissions'), entry.mode, onEditPermissions);
  addTechnical('link', LinkIcon, t('metaSymlink'), entry.isSymlink ? (entry.symlinkTarget ?? t('yesLabel')) : t('noLabel'));
  if (!entry.isDir) {
    add(
      'sum',
      Tag,
      t('sha256Label'),
      checksum ? (
        <Text selectable style={{ fontFamily: FontFamily.mono, fontSize: META_CHECKSUM_FONT_SIZE }} onLongPress={() => void Clipboard.setStringAsync(checksum).then(() => toast.info(t('copiedPath', { path: checksum })))}>{checksum}</Text>
      ) : (
        <Pressable onPress={checksumBusy ? undefined : onComputeChecksum} accessibilityLabel={t('computeChecksumLabel')}>
          <Text color={c.primary} style={[LumenType.name, { textDecorationLine: 'underline' }]}>{checksumBusy ? t('checksumBusyLabel') : t('computeChecksumButton')}</Text>
        </Pressable>
      ),
    );
    add('verify', Tag, t('verifyHashRow'), (
      <Pressable onPress={onVerifyHash} accessibilityLabel={t('verifyHashTitle')}>
        <Text color={c.primary} style={[LumenType.name, { textDecorationLine: 'underline' }]}>{t('verifyHashAction')}</Text>
      </Pressable>
    ));
  }
  return (
    <>
      {rows.map((r, i) => (
        <View key={i} style={{ marginTop: i ? META_ROW_DIVIDER_GAP : 0 }}>{r}</View>
      ))}
      {technical.length > 0 ? <Disclosure label={t('metaTechnicalDetails')}>{technical}</Disclosure> : null}
    </>
  );
}

function DetailRow({ icon: Icon, label, value, onPress }: { icon: LucideIcon; label: string; value: ReactNode; onPress?: () => void }) {
  const c = useScheme();
  const body = (
    <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: Spacing.sm, paddingVertical: Spacing.sm }}>
      <Icon size={META_DETAIL_ICON_SIZE} color={c.onSurfaceVariant} />
      <Text style={[LumenType.meta, { width: META_DETAIL_LABEL_WIDTH }]} muted>{label}</Text>
      <View style={{ flex: 1 }}>{typeof value === 'string' ? <Text style={LumenType.name}>{value}</Text> : value}</View>
    </View>
  );
  return onPress ? <Pressable onPress={onPress} accessibilityLabel={label}>{body}</Pressable> : body;
}
