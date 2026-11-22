import { Archive, ArrowLeft, Columns2, Bookmark, CloudOff, Copy, Download, EllipsisVertical, FilePen, FilePlus, FileUp, Info, PackageOpen, Pin, Replace, Scissors, Search, ShieldCheck, SlidersHorizontal, Star, Terminal, Trash2, X, type LucideIcon } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { can, type FileCapability } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { ActionListCard, ActionListTile, BottomSheet, Button, Chip, DuoIcon, Pressable, SheetHead, SheetScroll, StatePill, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';

export type OverflowAction = 'dual' | 'newItem' | 'toggleFavorite' | 'commandPalette' | 'viewOptions' | 'favorites' | 'transfers' | 'trash' | 'pinOffline';

/** The connection pill in a folder's top bar: Offline (warn) or Connected (safe). */
export function ConnectionPill({ offline }: { offline?: boolean }) {
  return offline ? <StatePill label={t('offlineStatus')} tone="warn" icon={CloudOff} /> : <StatePill label={t('stateConnected')} tone="safe" icon={ShieldCheck} />;
}

/** Mockup `.ib`: a 48x48 icon button. */
function BarButton({ label, onPress, children }: { label: string; onPress: () => void; children: React.ReactNode }) {
  return (
    <Pressable onPress={onPress} pressedScale={0.92} accessibilityLabel={label} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      {children}
    </Pressable>
  );
}

/**
 * Mockup `.bar` for a folder: a back arrow (when there is somewhere to go back to), the folder name at 20/28 weight 500
 * cut in the middle, then search and the "more" button that opens the actions sheet. Sits under the status bar.
 */
export function BrowseHeader({
  title, onBack, backLabel, isFav, isCurrentFolderPinned, canCreate, onSearch, onOverflow,
}: {
  title: string; onBack?: () => void; backLabel?: string; isFav: boolean; isCurrentFolderPinned: boolean; canCreate: boolean; onSearch: () => void; onOverflow: (a: OverflowAction) => void;
}) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const [menuOpen, setMenuOpen] = useState(false);
  const pick = (a: OverflowAction) => () => onOverflow(a);
  const items: { key: OverflowAction; label: string; icon: LucideIcon; onPress: () => void }[] = [
    ...(canCreate ? [{ key: 'newItem' as const, label: t('newButton'), icon: FilePlus, onPress: pick('newItem') }] : []),
    { key: 'toggleFavorite', label: isFav ? t('removeFavoriteTooltip') : t('favoriteFolderTooltip'), icon: Star, onPress: pick('toggleFavorite') },
    { key: 'commandPalette', label: t('commandPaletteTitle'), icon: Terminal, onPress: pick('commandPalette') },
    { key: 'viewOptions', label: t('viewOptionsTitle'), icon: SlidersHorizontal, onPress: pick('viewOptions') },
    { key: 'favorites', label: t('favoritesTitle'), icon: Star, onPress: pick('favorites') },
    { key: 'transfers', label: t('transfersMenuItem'), icon: FileUp, onPress: pick('transfers') },
    { key: 'dual', label: t('dualMenuItem'), icon: Columns2, onPress: pick('dual') },
    { key: 'trash', label: t('trashTitle'), icon: Trash2, onPress: pick('trash') },
    { key: 'pinOffline', label: isCurrentFolderPinned ? t('unpinOfflineAction') : t('pinOfflineAction'), icon: Pin, onPress: pick('pinOffline') },
  ];
  return (
    <View style={{ paddingTop: insets.top, backgroundColor: c.surface }}>
      <View style={{ height: 56, flexDirection: 'row', alignItems: 'center', paddingHorizontal: Spacing.xs }}>
        {onBack ? (
          <BarButton label={backLabel ?? t('folderBackTooltip')} onPress={onBack}>
            <DuoIcon icon={ArrowLeft} />
          </BarButton>
        ) : null}
        <Text numberOfLines={1} ellipsizeMode="middle" style={[LumenType.appbar, { flex: 1, minWidth: 0, paddingLeft: onBack ? 4 : 12 }]}>
          {title}
        </Text>
        <BarButton label={t('searchTooltip')} onPress={onSearch}>
          <DuoIcon icon={Search} />
        </BarButton>
        <BarButton label={t('moreTooltip')} onPress={() => setMenuOpen(true)}>
          <DuoIcon icon={EllipsisVertical} />
        </BarButton>
      </View>
      <BottomSheet visible={menuOpen} onClose={() => setMenuOpen(false)}>
        <SheetHead title={t('moreTooltip')} />
        <SheetScroll>
          <View style={{ paddingHorizontal: 18, paddingBottom: 18 }}>
            <ActionListCard>
              {items.map((it) => (
                <ActionListTile
                  key={it.key}
                  label={it.label}
                  icon={<it.icon size={20} color={c.onSurfaceVariant} />}
                  onPress={() => {
                    setMenuOpen(false);
                    it.onPress();
                  }}
                />
              ))}
            </ActionListCard>
          </View>
        </SheetScroll>
      </BottomSheet>
    </View>
  );
}

/** An extra tool in the selection tools row (copy paths, move to, compare, ...). */
export type SelectionChip = { key: string; label: string; icon: LucideIcon; onPress: () => void };

/**
 * Mockup `.bar.ctx`: close, "3 selected" with the size of the picked files below it (mono), and a "Select all" /
 * "Deselect all" text button, on the raised sheet colour with a hairline underneath.
 */
export function SelectionHeader({ count, bytes, all, onClose, onSelectAll, onClearSelection }: { count: number; bytes: number; all: boolean; onClose: () => void; onSelectAll: () => void; onClearSelection: () => void }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <View style={{ paddingTop: insets.top, backgroundColor: c.surfaceContainerLowest, borderBottomWidth: 1, borderBottomColor: c.rule }}>
      <View style={{ height: 56, flexDirection: 'row', alignItems: 'center', paddingHorizontal: Spacing.xs }}>
        <BarButton label={t('clearSelectionTooltip')} onPress={onClose}>
          <DuoIcon icon={X} />
        </BarButton>
        <View style={{ flex: 1, minWidth: 0, paddingLeft: Spacing.xs }}>
          <Text numberOfLines={1} style={LumenType.appbar}>{t('nSelected', { count })}</Text>
          {bytes > 0 ? (
            <Text numberOfLines={1} muted style={{ fontSize: 14, lineHeight: 18, fontFamily: FontFamily.mono }}>
              {formatSize(bytes)}
            </Text>
          ) : null}
        </View>
        <Button kind="text" size="s" label={all ? t('deselectAllTooltip') : t('selectAllTooltip')} onPress={all ? onClearSelection : onSelectAll} />
      </View>
    </View>
  );
}

/** The row of extra selection tools under the path (invert, rename, details, bookmark and the caller's extras), as outlined chips. */
export function SelectionTools({ count, canModify, extras = [], onInvertSelection, onRename, onBatchRename, onBookmark, onDetails }: { count: number; canModify: boolean; extras?: SelectionChip[]; onInvertSelection: () => void; onRename: () => void; onBatchRename: () => void; onBookmark: () => void; onDetails: () => void }) {
  const c = useScheme();
  const icon = (I: LucideIcon) => <DuoIcon icon={I} size={18} color={c.onSurface} />;
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0 }} contentContainerStyle={{ paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm, gap: Spacing.sm, alignItems: 'center' }}>
      <Chip label={t('invertSelectionTooltip')} icon={icon(Replace)} onPress={onInvertSelection} />
      {canModify && count === 1 ? <Chip label={t('renameButton')} icon={icon(FilePen)} onPress={onRename} /> : null}
      {canModify && count > 1 ? <Chip label={t('batchRenameTooltip')} icon={icon(FilePen)} onPress={onBatchRename} /> : null}
      {extras.map((x) => (
        <Chip key={x.key} label={x.label} icon={icon(x.icon)} onPress={x.onPress} />
      ))}
      {count === 1 ? <Chip label={t('detailsButton')} icon={icon(Info)} onPress={onDetails} /> : null}
      {count === 1 ? <Chip label={t('bookmarkButton')} icon={icon(Bookmark)} onPress={onBookmark} /> : null}
    </ScrollView>
  );
}

/** One cell of the selection bar: an icon over a 13/16 label, sharing the bar's width equally; dimmed when off, red when destructive. */
function ActCell({ label, icon, onPress, disabled, danger = false }: { label: string; icon: LucideIcon; onPress: () => void; disabled: boolean; danger?: boolean }) {
  const c = useScheme();
  const tone = danger ? c.stop : c.onSurface;
  return (
    <Pressable onPress={onPress} disabled={disabled} accessibilityRole="button" accessibilityLabel={label} accessibilityState={{ disabled }} pressedScale={0.96} style={{ flex: 1 }}>
      <View style={{ height: 75, alignItems: 'center', justifyContent: 'center', gap: 6, opacity: disabled ? 0.4 : 1 }}>
        <DuoIcon icon={icon} size={24} color={tone} />
        <Text numberOfLines={1} style={LumenType.dock} color={tone}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}

/**
 * Mockup `.acts`: a 76 dp bar on the sheet colour with a hairline on top; cut, copy, compress, extract (only when the
 * caller passes [onExtract], for one selected archive), download and delete as an icon over a label, each shown only
 * when the device's permissions allow it. Delete is the one red action.
 */
export function SelectionBar({ count, onCut, onCopy, onCompress, onExtract, onDownload, onDelete, caps }: { count: number; onCut: () => void; onCopy: () => void; onCompress: () => void; onExtract?: () => void; onDownload: () => void; onDelete: () => void; caps?: Record<FileCapability, boolean> }) {
  const c = useScheme();
  // Nothing the device may do with a selection: no empty strip.
  if (!can(caps, 'modify') && !can(caps, 'download') && !can(caps, 'delete')) return null;
  const off = count === 0;
  return (
    <View style={{ height: 76, flexDirection: 'row', backgroundColor: c.surfaceContainerLowest, borderTopWidth: 1, borderTopColor: c.rule }}>
      {can(caps, 'modify') && can(caps, 'delete') && <ActCell label={t('cutButton')} icon={Scissors} disabled={off} onPress={onCut} />}
      {can(caps, 'modify') && <ActCell label={t('copyButton')} icon={Copy} disabled={off} onPress={onCopy} />}
      {can(caps, 'modify') && <ActCell label={t('compressButton')} icon={Archive} disabled={off} onPress={onCompress} />}
      {can(caps, 'modify') && onExtract && <ActCell label={t('extractButton')} icon={PackageOpen} disabled={off} onPress={onExtract} />}
      {can(caps, 'download') && <ActCell label={t('downloadButton')} icon={Download} disabled={off} onPress={onDownload} />}
      {can(caps, 'delete') && <ActCell label={t('deleteButton')} icon={Trash2} danger disabled={off} onPress={onDelete} />}
    </View>
  );
}
