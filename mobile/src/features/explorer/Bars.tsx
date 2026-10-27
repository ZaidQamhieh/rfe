import { Archive, ArrowLeft, Bookmark, CheckSquare, Copy, Download, FilePen, FilePlus, FileUp, History, Info, Pin, PieChart, Replace, Scissors, Search, ShieldCheck, SlidersHorizontal, CloudOff, Square, Star, Terminal, Trash2, X, type LucideIcon } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { can, type FileCapability } from '../../core/api/models';
import { formatSize } from '../../core/format';
import type { Host } from '../../core/models/host';
import { ActionListCard, ActionListTile, ActionTile, BottomSheet, PageHead, Pressable, SheetHead, SheetScroll, StatePill, Text, TopBar } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { BreadcrumbBar } from './Breadcrumb';
import type { ExplorerState } from './explorerStore';
import { atRoot, currentPath } from './explorerStore';
import { folderLabel } from './paths';
import { selectedBytes } from './selectionLogic';

export type OverflowAction = 'newItem' | 'toggleFavorite' | 'bookmarks' | 'commandPalette' | 'viewOptions' | 'favorites' | 'transfers' | 'trash' | 'recent' | 'recentHere' | 'storageByType' | 'dupFinder' | 'pinOffline';

const itemsLabel = (n: number, more: boolean) => `${n}${more ? '+' : ''} ${n === 1 && !more ? 'item' : 'items'}`;

function IconButton({ label, onPress, children }: { label: string; onPress: () => void; children: React.ReactNode }) {
  return (
    <Pressable onPress={onPress} pressedScale={0.92} accessibilityLabel={label} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      {children}
    </Pressable>
  );
}

/** Browse header: the pinned `TopBar` ("Studio PC · Files", search, connection pill, overflow). The folder title and path are [FolderHead]. */
export function BrowseHeader({
  host, offline, isFav, isCurrentFolderPinned, canCreate, onSearch, onOverflow,
}: {
  host: Host; offline: boolean; isFav: boolean; isCurrentFolderPinned: boolean; canCreate: boolean; onSearch: () => void; onOverflow: (a: OverflowAction) => void;
}) {
  const c = useScheme();
  const [menuOpen, setMenuOpen] = useState(false);
  const pick = (a: OverflowAction) => () => onOverflow(a);
  const items: { key: OverflowAction; label: string; icon: LucideIcon; onPress: () => void }[] = [
    ...(canCreate ? [{ key: 'newItem' as const, label: t('newButton'), icon: FilePlus, onPress: pick('newItem') }] : []),
    { key: 'toggleFavorite', label: isFav ? t('removeFavoriteTooltip') : t('favoriteFolderTooltip'), icon: Star, onPress: pick('toggleFavorite') },
    { key: 'bookmarks', label: 'Bookmarks', icon: Bookmark, onPress: pick('bookmarks') },
    { key: 'commandPalette', label: 'Command Palette', icon: Terminal, onPress: pick('commandPalette') },
    { key: 'viewOptions', label: t('viewOptionsTitle'), icon: SlidersHorizontal, onPress: pick('viewOptions') },
    { key: 'favorites', label: t('favoritesTitle'), icon: Star, onPress: pick('favorites') },
    { key: 'transfers', label: t('transfersMenuItem'), icon: FileUp, onPress: pick('transfers') },
    { key: 'trash', label: t('trashTitle'), icon: Trash2, onPress: pick('trash') },
    { key: 'recent', label: t('recentTitle'), icon: History, onPress: pick('recent') },
    { key: 'recentHere', label: t('recentInFolder'), icon: History, onPress: pick('recentHere') },
    { key: 'storageByType', label: t('storageByTypeTitle'), icon: PieChart, onPress: pick('storageByType') },
    { key: 'dupFinder', label: 'Find Duplicates', icon: Replace, onPress: pick('dupFinder') },
    { key: 'pinOffline', label: isCurrentFolderPinned ? 'Unpin offline' : 'Pin offline', icon: Pin, onPress: pick('pinOffline') },
  ];
  return (
    <View>
      <TopBar
        context={`${host.label} · Files`}
        sub="Workspace"
        onMore={() => setMenuOpen(true)}
        actions={
          <IconButton label={t('searchTooltip')} onPress={onSearch}>
            <Search size={22} color={c.onSurfaceVariant} />
          </IconButton>
        }
        right={offline ? <StatePill label="Offline" tone="warn" icon={CloudOff} /> : <StatePill label="Connected" tone="safe" icon={ShieldCheck} />}
      />
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

/**
 * The folder title ("Docs", "9 items") and the path pills with a back arrow. It is the list's header, so it scrolls away
 * with the entries and the listing gets the screen; only the `TopBar` of [BrowseHeader] stays pinned.
 */
export function FolderHead({ state, onBack, onLeaveRoot, onNavigateTo, onJumpTo }: { state: ExplorerState; onBack: () => void; onLeaveRoot?: () => void; onNavigateTo: (i: number) => void; onJumpTo: (p: string) => void }) {
  const c = useScheme();
  const root = atRoot(state);
  // At a shared folder's top the arrow leads back to the list of shared folders (or drives), when there is one.
  const back = root ? onLeaveRoot : onBack;
  const subtitle = state.loading && state.entries.length === 0 ? undefined : itemsLabel(state.entries.length, state.nextCursor !== null);
  return (
    <View>
      <PageHead title={folderLabel(currentPath(state))} subtitle={subtitle} />
      <View style={{ flexDirection: 'row', alignItems: 'center', minHeight: 48, paddingLeft: back ? 6 : 18, marginBottom: 6 }}>
        {back && (
          <IconButton label={root ? 'Shared folders' : 'Back'} onPress={back}>
            <ArrowLeft size={22} color={c.onSurface} />
          </IconButton>
        )}
        <View style={{ flex: 1, height: 48 }}>
          <BreadcrumbBar pathStack={state.pathStack} onNavigateTo={onNavigateTo} onJumpTo={onJumpTo} />
        </View>
      </View>
    </View>
  );
}

function ChipButton({ label, icon: Icon, onPress }: { label: string; icon: LucideIcon; onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={label} hitSlop={4}>
      <View style={{ minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 16, borderRadius: 14, backgroundColor: c.surfaceContainerHigh }}>
        <Icon size={18} color={c.onSurface} />
        <Text style={LumenType.pill} numberOfLines={1}>{label}</Text>
      </View>
    </Pressable>
  );
}

/** An extra tool in the selection header's chip row (copy paths, move to, compare, ...). */
export type SelectionChip = { key: string; label: string; icon: LucideIcon; onPress: () => void };

/** Selection mode header: same `TopBar` and `PageHead` as browsing ("3 selected"), a close button, and a row of selection tools. */
export function SelectionHeader({
  host, state, shown, extras = [], onClose, onBatchRename, onSelectAll, onClearSelection, onInvertSelection, onBookmark, onDetails, canModify = true,
}: { host: Host; state: ExplorerState; shown: readonly { path: string }[]; canModify?: boolean; extras?: SelectionChip[]; onClose: () => void; onBatchRename: () => void; onSelectAll: () => void; onClearSelection: () => void; onInvertSelection: () => void; onBookmark: () => void; onDetails: () => void }) {
  const c = useScheme();
  // "All" means everything on screen (after the hidden-file and tag filters), not everything the folder holds.
  const bytes = selectedBytes(state.entries, state.selected);
  const all = shown.length > 0 && shown.every((x) => state.selected.has(x.path));
  return (
    <View>
      <TopBar
        context={`${host.label} · Files`}
        right={
          <IconButton label={t('clearSelectionTooltip')} onPress={onClose}>
            <X size={22} color={c.onSurfaceVariant} />
          </IconButton>
        }
      />
      <PageHead title={t('nSelected', { count: state.selected.size })} subtitle={bytes > 0 ? `${folderLabel(currentPath(state))} · ${formatSize(bytes)}` : folderLabel(currentPath(state))} />
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0, marginBottom: 6 }} contentContainerStyle={{ paddingHorizontal: 18, gap: 10, alignItems: 'center' }}>
        <ChipButton label={all ? t('deselectAllTooltip') : t('selectAllTooltip')} icon={all ? Square : CheckSquare} onPress={all ? onClearSelection : onSelectAll} />
        <ChipButton label={t('invertSelectionTooltip')} icon={Replace} onPress={onInvertSelection} />
        {canModify && state.selected.size > 0 && <ChipButton label={t('batchRenameTooltip')} icon={FilePen} onPress={onBatchRename} />}
        {extras.map((x) => (
          <ChipButton key={x.key} label={x.label} icon={x.icon} onPress={x.onPress} />
        ))}
        {state.selected.size === 1 && <ChipButton label={t('detailsButton')} icon={Info} onPress={onDetails} />}
        {state.selected.size === 1 && <ChipButton label="Bookmark" icon={Bookmark} onPress={onBookmark} />}
      </ScrollView>
    </View>
  );
}

/** Bottom action row for the current selection: cut, copy, compress, download, delete as flat tiles, each gated by the device's permissions. */
export function SelectionBar({ count, onCut, onCopy, onCompress, onDownload, onDelete, caps }: { count: number; onCut: () => void; onCopy: () => void; onCompress: () => void; onDownload: () => void; onDelete: () => void; caps?: Record<FileCapability, boolean> }) {
  const c = useScheme();
  // Nothing the device may do with a selection: no empty strip.
  if (!can(caps, 'modify') && !can(caps, 'download') && !can(caps, 'delete')) return null;
  const off = count === 0;
  // Sits above the tab bar, which already owns the bottom inset.
  return (
    <View style={{ flexDirection: 'row', gap: 8, paddingHorizontal: 18, paddingTop: 12, paddingBottom: 14, backgroundColor: c.surface }}>
      {can(caps, 'modify') && can(caps, 'delete') && <ActionTile compact label={t('cutButton')} disabled={off} onPress={onCut} renderIcon={(k) => <Scissors size={22} color={k} />} />}
      {can(caps, 'modify') && <ActionTile compact label={t('copyButton')} disabled={off} onPress={onCopy} renderIcon={(k) => <Copy size={22} color={k} />} />}
      {can(caps, 'modify') && <ActionTile compact label={t('compressButton')} disabled={off} onPress={onCompress} renderIcon={(k) => <Archive size={22} color={k} />} />}
      {can(caps, 'download') && <ActionTile compact label={t('downloadButton')} disabled={off} onPress={onDownload} renderIcon={(k) => <Download size={22} color={k} />} />}
      {can(caps, 'delete') && <ActionTile compact label={t('deleteButton')} disabled={off} onPress={onDelete} renderIcon={() => <Trash2 size={22} color={c.error} />} />}
    </View>
  );
}
