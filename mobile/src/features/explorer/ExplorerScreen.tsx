import * as Clipboard from 'expo-clipboard';
import { useFocusEffect, useRouter } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, BackHandler, FlatList, RefreshControl, ScrollView, View } from 'react-native';

import { can, type Entry } from '../../core/api/models';
import type { EntryCategory } from '../../core/entryCategory';
import type { Host } from '../../core/models/host';
import { ActionFooter, Button, EmptyState, type FooterButton, ErrorRetry, ListingSkeleton, OfflineBanner, Pressable, Text, useDialogs, useToast } from '../../design/components';
import { ClipboardPaste, Copy, CopyPlus, FolderInput, FolderOutput, GitCompare, ListChecks, Plus, Upload, Eye, EyeOff, Bookmark, FileUp, History, LayoutGrid, PieChart, RefreshCw, Replace, Route, Search, SlidersHorizontal, Trash2 } from 'lucide-react-native';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { isPinned, useCollections } from '../../state/collections';
import { useActiveHost } from '../../state/activeHost';
import { useSettings } from '../../state/settings';
import { offlineDeps } from '../offline/offlineDeps';
import { MAX_OFFLINE_FILE_BYTES, precachePinnedFolder } from '../offline/offlineBodies';
import { markPinSynced, pinSyncPending, refreshPinnedFolders } from '../offline/pinSync';
import { humanizeError } from '../pairing/pairingService';
import { fetchPreviewFile } from '../preview/previewFile';
import { isPreviewable, previewableSiblings } from '../preview/previewKind';
import { usePreviewSession } from '../preview/session';
import { enqueueDownloads } from '../transfers/enqueueDownloads';
import { BrowseHeader, FolderHead, SelectionBar, SelectionHeader, type OverflowAction, type SelectionChip } from './Bars';
import { BatchRenameSheet } from './BatchRenameSheet';
import { CommandPalette, type PaletteAction } from './CommandPalette';
import { CreateMenu } from './CreateMenu';
import { DestinationPicker } from './DestinationPicker';
import { footerActions } from './footerLogic';
import { EntryGridCell } from './EntryGridCell';
import { EntryTile } from './EntryTile';
import { MetaSheet } from './MetaSheet';
import { PeekSheet } from './PeekSheet';
import { FavoritesPinRow, FavoritesSheet, ViewOptionsSheet } from './Sheets';
import { useFileClipboard } from './clipboard';
import { normalizeTypedPath } from './paletteLogic';
import { transfers } from '../../core/native';
import { clientForHost, listingCache } from '../../services';
import { atRoot, currentPath } from './explorerStore';
import { basenameOf, folderLabel, parentDirOf } from './paths';
import { recentRouteParams } from '../recent/recentRoot';
import { rowShape } from './rowShape';
import { pathsText } from './selectionLogic';
import { filterByTag } from './tagFilter';
import { filterByType, typeChips } from './typeFilter';
import { useExplorer } from './useExplorer';
import { useFileActions } from './useFileActions';
import { useFileCapabilities } from './useFileCapabilities';

const GRID_COLUMNS_MIN_WIDTH = 144;
const GUTTER = 18;
const CARD_GAP = 10;

/** Files tab body for one host root: list/grid browsing, selection, clipboard operations, favorites, bookmarks and pinning. */
/** [onLeaveRoot] goes back to the host's list of shared folders or drives; absent when there is nothing above [rootPath]. */
export function ExplorerScreen({ host, rootPath, initialPath, onLeaveRoot }: { host: Host; rootPath: string; initialPath?: string; onLeaveRoot?: () => void }) {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const dialogs = useDialogs();
  const { ex, state, view, display, hidden, hiddenCount } = useExplorer(host, rootPath);
  const actions = useFileActions(host, ex);
  const collections = useCollections();
  const clip = useFileClipboard((s) => s.clip);
  const density = useSettings((s) => s.state.overrides[host.id]?.density ?? s.state.app.density);
  const [createOpen, setCreateOpen] = useState(false);
  const caps = useFileCapabilities(host);
  const [viewOpen, setViewOpen] = useState(false);
  const [favOpen, setFavOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [metaEntry, setMetaEntry] = useState<Entry | null>(null);
  const [peekEntry, setPeekEntry] = useState<Entry | null>(null);
  // A tag filter applies only to the folder it was picked in.
  const [tagFilter, setTagFilter] = useState<{ path: string; tag: string } | null>(null);
  // A type filter (images, documents, ...) applies only to the folder it was picked in, like the tag filter.
  const [typeFilter, setTypeFilter] = useState<{ path: string; category: EntryCategory } | null>(null);
  // Move to / Copy to: the destination sheet is open for the current selection.
  const [pickFor, setPickFor] = useState<'move' | 'copy' | null>(null);
  const [width, setWidth] = useState(0);
  // Selection mode entered from the footer "Select" button; long-press also selects without it.
  const [selectMode, setSelectMode] = useState(false);
  const hadSelection = useRef(false);

  useEffect(() => {
    if (initialPath && initialPath !== rootPath) ex.jumpTo(initialPath);
  }, [ex, initialPath, rootPath]);

  // Once per launch, while the host is reachable, bring its pinned folders up to date in the background.
  const online = !state.loading && !state.error && !state.offline;
  useEffect(() => {
    if (!online || !pinSyncPending(host.id)) return;
    const paths = useCollections.getState().pins.filter((p) => p.hostId === host.id).map((p) => p.remotePath);
    if (paths.length === 0) return markPinSynced(host.id);
    void (async () => {
      const client = await clientForHost(host);
      const result = await refreshPinnedFolders(paths, {
        listAll: async (path) => {
          const all: Entry[] = [];
          let cursor: string | undefined;
          do {
            const page = await client.list(path, { cursor });
            all.push(...page.entries);
            cursor = page.nextCursor;
          } while (cursor);
          return all;
        },
        storeListing: (path, entries) => listingCache.put(host.id, path, entries),
        precache: (entries) => precachePinnedFolder(offlineDeps, host, entries, (h, e) => fetchPreviewFile(h, e, MAX_OFFLINE_FILE_BYTES)),
      });
      if (result.folders > 0) markPinSynced(host.id);
    })().catch(() => {});
  }, [online, host]);

  // A finished upload into the folder on screen shows up without a manual refresh.
  useEffect(
    () =>
      transfers.subscribe((r) => {
        if (r.direction === 'UPLOAD' && r.state === 'DONE' && r.hostId === host.id && parentDirOf(r.remotePath) === currentPath(ex.getState())) void ex.refresh();
      }),
    [ex, host.id],
  );

  // Leave selection mode once the last selected item is gone (after an action or by unticking it).
  const selectedCount = state.selected.size;
  useEffect(() => {
    if (selectedCount > 0) hadSelection.current = true;
    else if (hadSelection.current) {
      hadSelection.current = false;
      setSelectMode(false);
    }
  }, [selectedCount]);
  const endSelection = useCallback(() => {
    hadSelection.current = false;
    setSelectMode(false);
    ex.clearSelection();
  }, [ex]);

  // Hardware back: end selection, then go up a folder, then leave the tab.
  // Only while this screen is focused, so a pushed screen (preview, meta) gets Back first.
  useFocusEffect(
    useCallback(() => {
      const sub = BackHandler.addEventListener('hardwareBackPress', () => {
        const s = ex.getState();
        if (s.selected.size > 0 || selectMode) {
          endSelection();
          return true;
        }
        if (ex.popDirectory()) return true;
        // At the top of a shared folder, Back returns to the shared-folder list before it leaves the tab.
        if (onLeaveRoot) {
          onLeaveRoot();
          return true;
        }
        return false;
      });
      return () => sub.remove();
    }, [ex, selectMode, endSelection, onLeaveRoot]),
  );

  const path = currentPath(state);
  const activeTag = tagFilter?.path === path ? tagFilter.tag : null;
  const setActiveTag = (tag: string | null) => setTagFilter(tag ? { path, tag } : null);
  const isFav = collections.favorites.some((f) => f.hostId === host.id && f.path === path);
  const pinnedHere = isPinned(collections.pins, host.id, path);
  const favPaths = useMemo(() => new Set(collections.favorites.filter((f) => f.hostId === host.id).map((f) => f.path)), [collections.favorites, host.id]);
  const pinPaths = useMemo(() => new Set(collections.pins.filter((p) => p.hostId === host.id).map((p) => p.remotePath)), [collections.pins, host.id]);
  const taggedPaths = useMemo(() => new Set(collections.bookmarks.filter((b) => b.hostId === host.id && activeTag && b.tag === activeTag).map((b) => b.remotePath)), [collections.bookmarks, host.id, activeTag]);
  const tags = useMemo(() => {
    const visible = new Set(display.map((e) => e.path));
    return [...new Set(collections.bookmarks.filter((b) => b.hostId === host.id && b.tag && visible.has(b.remotePath)).map((b) => b.tag!))];
  }, [collections.bookmarks, display, host.id]);
  const activeType = typeFilter?.path === path ? typeFilter.category : null;
  const byTag = filterByTag(display, activeTag, taggedPaths);
  const chips = typeChips(byTag);
  const entries = filterByType(byTag, activeType && chips.includes(activeType) ? activeType : null);

  const openPreview = usePreviewSession((x) => x.open);
  const startPreview = useCallback(
    (e: Entry) => {
      // Swipe order follows what is on screen (sorted, visibility- and tag-filtered).
      const sib = previewableSiblings(entries, e);
      const onChanged = () => void ex.refresh();
      openPreview(sib.index >= 0 ? { host, entries: sib.entries, index: sib.index, onChanged } : { host, entries: [e], index: 0, onChanged });
      router.push('/preview');
    },
    [ex, host, router, openPreview, entries],
  );
  const openEntry = useCallback(
    async (e: Entry) => {
      const s = ex.getState();
      if (selectMode || s.selected.size > 0) return ex.toggleSelect(e.path);
      if (e.isDir) return ex.navigate(e.path);
      // Files without a previewer still have actions (download, extract, share link, details).
      if (!isPreviewable(e)) return setMetaEntry(e);
      startPreview(e);
    },
    [ex, selectMode, startPreview],
  );

  async function toggleFavorite() {
    await collections.toggleFavorite({ hostId: host.id, path, label: folderLabel(path) });
    if (isFav) toast.info(t('removedFavorite', { name: folderLabel(path) }));
    else toast.success(t('addedFavorite', { name: folderLabel(path) }));
  }

  async function togglePin() {
    if (pinnedHere) return void (await collections.unpin(host.id, path));
    await collections.pin(host.id, path);
    // Fetch the folder's files in the background so they open offline (each fetch stores its encrypted copy).
    // Page through the whole folder: the screen may hold only the first page.
    void (async () => {
      const all: Entry[] = [...ex.getState().entries];
      try {
        let cursor = ex.getState().nextCursor ?? undefined;
        if (cursor) {
          const client = await clientForHost(host);
          while (cursor) {
            const page = await client.list(path, { cursor });
            all.push(...page.entries);
            cursor = page.nextCursor;
          }
        }
      } catch {
        // Best effort: precache what was loaded.
      }
      await precachePinnedFolder(offlineDeps, host, all, (h, e) => fetchPreviewFile(h, e, MAX_OFFLINE_FILE_BYTES));
    })().catch(() => {});
  }

  async function bookmark(e: Entry) {
    const already = collections.bookmarks.some((b) => b.hostId === host.id && b.remotePath === e.path);
    if (already) {
      const ok = await dialogs.confirm({ title: 'Remove Bookmark', description: `Remove bookmark for "${e.name}"?`, confirmLabel: 'Remove', cancelLabel: t('cancelButton') });
      if (ok) await collections.removeBookmark(host.id, e.path);
      return;
    }
    const tag = await dialogs.prompt({ title: `Bookmark "${e.name}"`, placeholder: 'Tag (optional)', confirmLabel: 'Save', allowEmpty: true });
    if (tag === null) return;
    await collections.addBookmark({ hostId: host.id, remotePath: e.path, ...(tag ? { tag } : {}) });
  }

  /** Long-press starts a selection; with one already going it extends the selection up to the pressed item. */
  function longPressEntry(e: Entry) {
    const picked = ex.getState().selected;
    // On an item that is already selected a long-press still unselects it.
    if (picked.size > 0 && !picked.has(e.path)) ex.selectRange(entries, e.path);
    else ex.toggleSelect(e.path);
  }

  async function copySelectedPaths() {
    const sel = ex.getState().selected;
    if (sel.size === 0) return;
    await Clipboard.setStringAsync(pathsText(state.entries, sel));
    toast.success(t('pathsCopied', { count: sel.size }));
  }

  function bookmarkSelected() {
    const [only] = ex.getState().selected;
    const entry = state.entries.find((e) => e.path === only);
    if (!entry) return;
    ex.clearSelection();
    bookmark(entry).catch(() => {});
  }

  function detailsOfSelected() {
    const [only] = ex.getState().selected;
    const entry = state.entries.find((e) => e.path === only);
    if (!entry) return;
    ex.clearSelection();
    setMetaEntry(entry);
  }

  async function downloadSelected() {
    const paths = [...ex.getState().selected];
    try {
      await enqueueDownloads(host, paths);
      ex.clearSelection();
      toast.success(t('queuedNDownloads', { count: paths.length }));
    } catch (e) {
      toast.error(humanizeError(e));
    }
  }


  const onOverflow = (a: OverflowAction) => {
    switch (a) {
      case 'newItem':
        return setCreateOpen(true);
      case 'toggleFavorite':
        return void toggleFavorite();
      case 'bookmarks':
        return router.push('/bookmarks');
      case 'viewOptions':
        return setViewOpen(true);
      case 'favorites':
        return setFavOpen(true);
      case 'pinOffline':
        return void togglePin();
      case 'transfers':
        return router.navigate('/transfers');
      case 'trash':
        return router.push('/trash');
      case 'recent':
        return router.push('/recent');
      case 'recentHere':
        return router.push({ pathname: '/recent', params: recentRouteParams(path) });
      case 'dupFinder':
        return router.push({ pathname: '/dups', params: { path } });
      case 'storageByType':
        return router.push({ pathname: '/host/[id]/types', params: { id: host.id, path } });
      case 'commandPalette':
        return setPaletteOpen(true);
      default:
        return;
    }
  };

  const goToPath = async () => {
    const typed = await dialogs.prompt({ title: t('goToPathTitle'), placeholder: '/path/to/folder', confirmLabel: t('goButton'), mono: true });
    const target = typed === null ? null : normalizeTypedPath(typed);
    if (target) ex.jumpTo(target);
  };
  const paletteActions: PaletteAction[] = [
    { id: 'search', label: 'Search', icon: Search, run: () => router.push({ pathname: '/host/[id]/search', params: { id: host.id, path } }) },
    { id: 'refresh', label: 'Refresh', icon: RefreshCw, run: () => ex.refresh() },
    { id: 'grid', label: 'Toggle Grid/List', icon: LayoutGrid, run: () => void useSettings.getState().setApp('gridView', !view.gridView) },
    { id: 'view', label: t('viewOptionsTitle'), icon: SlidersHorizontal, run: () => setViewOpen(true) },
    { id: 'favorites', label: t('favoritesTitle'), icon: Bookmark, run: () => setFavOpen(true) },
    { id: 'transfers', label: t('transfersMenuItem'), icon: FileUp, run: () => router.navigate('/transfers') },
    { id: 'trash', label: t('trashTitle'), icon: Trash2, run: () => router.push('/trash') },
    { id: 'recent', label: t('recentTitle'), icon: History, run: () => router.push('/recent') },
    { id: 'types', label: t('storageByTypeTitle'), icon: PieChart, run: () => router.push({ pathname: '/host/[id]/types', params: { id: host.id, path } }) },
    { id: 'dups', label: 'Find Duplicates', icon: Replace, run: () => router.push({ pathname: '/dups', params: { path } }) },
    { id: 'goto', label: 'Navigate to Path', icon: Route, run: () => void goToPath() },
  ];

  const columns = Math.max(2, Math.floor((width - GUTTER * 2 + CARD_GAP) / (GRID_COLUMNS_MIN_WIDTH + CARD_GAP)));
  const showMore = state.nextCursor !== null && !activeTag;
  const showHidden = hiddenCount > 0 && !activeTag;
  const favRow = atRoot(state) ? collections.favorites.filter((f) => f.hostId === host.id) : [];
  const multi = selectMode || state.selected.size > 0;
  const selectedPaths = useMemo(() => [...state.selected], [state.selected]);
  const showPaste = clip !== null && clip.paths.length > 0 && clip.hostId === host.id && can(caps, 'modify');
  // Mockup `.file-actions`: Upload (60%) and Select (40%). The permission-gated footerActions decide the primary
  // (Paste, Upload or New); a second "New" moves to the overflow sheet so the row keeps the mockup's two buttons.
  const primaryAction = footerActions({ caps, showPaste })[0];
  const footerButtons: FooterButton[] = [
    ...(primaryAction
      ? [
          {
            key: primaryAction.key,
            primary: true,
            label: primaryAction.key === 'paste' ? t('pasteNItems', { count: clip?.paths.length ?? 0 }) : primaryAction.key === 'upload' ? t('uploadFileTooltip') : t('newButton'),
            onPress: primaryAction.key === 'paste' ? () => void actions.paste() : primaryAction.key === 'upload' ? () => void actions.upload() : () => setCreateOpen(true),
            renderIcon: (k: string) => (primaryAction.key === 'paste' ? <ClipboardPaste size={22} color={k} /> : primaryAction.key === 'upload' ? <Upload size={22} color={k} /> : <Plus size={22} color={k} />),
          },
        ]
      : []),
    { key: 'select', label: 'Select', primary: false, onPress: () => setSelectMode(true), renderIcon: (k: string) => <ListChecks size={22} color={k} /> },
  ];

  const pickedFiles = selectedCount === 2 ? state.entries.filter((e) => state.selected.has(e.path)) : [];
  const selectionChips: SelectionChip[] = [
    ...(selectedCount > 0 ? [{ key: 'paths', label: t('copyPathsTooltip', { count: selectedCount }), icon: Copy, onPress: () => void copySelectedPaths() }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') ? [{ key: 'dup', label: t('duplicateButton'), icon: CopyPlus, onPress: () => void actions.duplicateSelection() }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') && can(caps, 'delete') ? [{ key: 'moveTo', label: t('moveToTooltip'), icon: FolderInput, onPress: () => setPickFor('move') }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') ? [{ key: 'copyTo', label: t('copyToTooltip'), icon: FolderOutput, onPress: () => setPickFor('copy') }] : []),
    ...(pickedFiles.length === 2 && pickedFiles.every((e) => !e.isDir) ? [{ key: 'compare', label: t('compareTooltip'), icon: GitCompare, onPress: () => void actions.compareSelection() }] : []),
  ];

  const header = multi ? (
    <SelectionHeader host={host} state={state} shown={entries} extras={selectionChips} canModify={can(caps, 'modify')} onClose={endSelection} onBatchRename={() => setRenameOpen(true)} onSelectAll={() => ex.selectAll(entries)} onClearSelection={ex.clearSelection} onInvertSelection={() => ex.invertSelection(entries)} onBookmark={bookmarkSelected} onDetails={detailsOfSelected} />
  ) : (
    <BrowseHeader
      host={host}
      offline={state.offline || !!state.error}
      isFav={isFav}
      isCurrentFolderPinned={pinnedHere}
      canCreate={can(caps, 'modify')}
      onSearch={() => router.push({ pathname: '/host/[id]/search', params: { id: host.id, path } })}
      onOverflow={onOverflow}
    />
  );
  // Folder title, path and filter chips scroll with the entries (audit M1: pinned, they left the list about a quarter
  // of the screen). Selection mode keeps its own pinned title in SelectionHeader.
  const folderHead = (
    <View>
      {!multi && <FolderHead state={state} onBack={() => ex.popDirectory()} onLeaveRoot={onLeaveRoot} onNavigateTo={ex.navigateTo} onJumpTo={ex.jumpTo} />}
      {tags.length > 0 && <TagChips tags={tags} active={activeTag} onChange={setActiveTag} />}
      {chips.length > 0 && <TypeChips categories={chips} active={activeType} onChange={(category) => setTypeFilter(category ? { path, category } : null)} />}
    </View>
  );

  let body: React.ReactNode;
  if (state.loading && state.entries.length === 0) body = <>{folderHead}<ListingSkeleton /></>;
  else if (state.error && state.entries.length === 0) {
    // The saved folder is no longer one this device may browse (the computer's allowed folders changed): retrying cannot help, so offer the list of shared folders again.
    const rootRejected = state.errorCode === 'FORBIDDEN' && atRoot(state);
    body = (
      <View style={{ flex: 1 }}>
        {folderHead}
        <ErrorRetry message={state.error} onRetry={() => ex.refresh()} />
        {rootRejected ? (
          <View style={{ paddingHorizontal: 18, paddingBottom: 12 }}>
            <Button kind="tonal" label={t('chooseAnotherFolder')} onPress={() => { const cur = useActiveHost.getState().active; if (cur) useActiveHost.getState().setActive({ ...cur, rootPath: undefined, initialPath: undefined }); }} />
          </View>
        ) : null}
      </View>
    );
  }
  else if (entries.length === 0)
    body = (
      <View style={{ flex: 1 }}>
        {folderHead}
        <EmptyState kind={state.entries.length > 0 ? 'noMatches' : 'emptyFolder'} message={state.entries.length > 0 ? t('noMatchesMessage') : t('emptyFolderMessage')} />
      </View>
    );
  else {
    const footer = (
      <>
        {showMore && state.loadMoreError !== null && (
          <View style={{ paddingVertical: Spacing.md, paddingHorizontal: 18, alignItems: 'center', gap: Spacing.sm }}>
            <Text style={LumenType.meta} muted>{t('loadMoreFailed', { error: state.loadMoreError })}</Text>
            <Button size="md" kind="neutral" label={t('retryButton')} onPress={() => void ex.loadMore({ retry: true })} />
          </View>
        )}
        {showMore && state.loadMoreError === null && <View style={{ paddingVertical: Spacing.lg, alignItems: 'center' }}>{state.loadingMore ? <ActivityIndicator /> : <View style={{ height: 24 }} />}</View>}
        {showHidden && (
          <Pressable onPress={ex.toggleShowHidden} accessibilityLabel={`${t('nHidden', { count: hiddenCount })} ${state.showHidden ? t('hideLabel') : t('showLabel')}`}>
            <View style={{ flexDirection: 'row', justifyContent: 'center', alignItems: 'center', gap: Spacing.sm, padding: Spacing.md }}>
              {state.showHidden ? <EyeOff size={18} color={c.onSurfaceVariant} /> : <Eye size={18} color={c.onSurfaceVariant} />}
              <Text style={LumenType.meta} muted>{`${t('nHidden', { count: hiddenCount })} · `}</Text>
              <Text style={LumenType.name} color={c.primary}>{state.showHidden ? t('hideLabel') : t('showLabel')}</Text>
            </View>
          </Pressable>
        )}
      </>
    );
    const common = {
      data: entries,
      keyExtractor: (e: Entry) => e.path,
      onEndReached: () => showMore && void ex.loadMore(),
      onEndReachedThreshold: 0.6,
      refreshControl: <RefreshControl refreshing={state.loading} onRefresh={() => ex.refresh()} />,
      ListFooterComponent: footer,
      ListHeaderComponent: (
        <View>
          {/* The list content is inset by GUTTER; the head carries its own 18 dp inset. */}
          <View style={{ marginHorizontal: -GUTTER, marginTop: view.gridView ? -CARD_GAP : 0 }}>{folderHead}</View>
          {favRow.length > 0 ? <FavoritesPinRow favorites={favRow} onOpen={ex.jumpTo} onRemove={(f) => collections.removeFavorite(f.hostId, f.path)} /> : null}
        </View>
      ),
    };
    body = view.gridView ? (
      <FlatList
        key={`grid${columns}`}
        {...common}
        numColumns={columns}
        contentContainerStyle={{ padding: GUTTER, paddingTop: CARD_GAP, paddingBottom: GUTTER, gap: CARD_GAP }}
        columnWrapperStyle={{ gap: CARD_GAP }}
        renderItem={({ item }) => (
          <View style={{ flex: 1, opacity: hidden.has(item.path) ? 0.55 : 1 }}>
            <EntryGridCell entry={item} hostId={host.id} selected={state.selected.has(item.path)} multiSelect={multi} isFavorite={favPaths.has(item.path)} isPinned={pinPaths.has(item.path)} onPress={() => openEntry(item)} onLongPress={() => longPressEntry(item)} onPeek={isPreviewable(item) ? () => setPeekEntry(item) : undefined} />
          </View>
        )}
      />
    ) : (
      <FlatList
        key="list"
        {...common}
        contentContainerStyle={{ paddingHorizontal: GUTTER, paddingBottom: GUTTER }}
        renderItem={({ item, index }) => (
          <View style={{ opacity: hidden.has(item.path) ? 0.55 : 1 }}>
            <EntryTile entry={item} hostId={host.id} shape={rowShape(entries, index)} selected={state.selected.has(item.path)} multiSelect={multi} density={density} isFavorite={favPaths.has(item.path)} isPinned={pinPaths.has(item.path)} onPress={() => openEntry(item)} onLongPress={() => longPressEntry(item)} onPeek={isPreviewable(item) ? () => setPeekEntry(item) : undefined} onSelect={() => ex.toggleSelect(item.path)} onShowMeta={item.isDir ? () => setMetaEntry(item) : undefined} />
          </View>
        )}
      />
    );
  }


  return (
    <View style={{ flex: 1 }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
      {header}
      {state.offline && <OfflineBanner text={t('offlineBannerText')} />}
      <View style={{ flex: 1 }}>{body}</View>
      {multi ? (
        <SelectionBar count={state.selected.size} onCut={actions.cutSelection} onCopy={actions.copySelection} onCompress={actions.compressSelected} onDownload={downloadSelected} onDelete={actions.confirmDelete} caps={caps} />
      ) : (
        <ActionFooter buttons={footerButtons} />
      )}
      <CreateMenu
        visible={createOpen}
        onClose={() => setCreateOpen(false)}
        onNewFolder={() => void actions.createNamed(true)}
        onNewFile={() => void actions.createNamed(false)}
        onUpload={can(caps, 'upload') ? () => void actions.upload() : undefined}
        onPaste={showPaste ? () => void actions.paste() : undefined}
        pasteLabel={showPaste ? t('pasteNItems', { count: clip!.paths.length }) : undefined}
        canModify={can(caps, 'modify')}
      />
      <ViewOptionsSheet visible={viewOpen} onClose={() => setViewOpen(false)} gridView={view.gridView} density={view.density} sort={view.sort} showHidden={state.showHidden} hiddenCount={hiddenCount} onToggleShowHidden={ex.toggleShowHidden} />
      <CommandPalette visible={paletteOpen} actions={paletteActions} onClose={() => setPaletteOpen(false)} />
      <BatchRenameSheet
        visible={renameOpen}
        names={selectedPaths.map(basenameOf)}
        onClose={() => setRenameOpen(false)}
        onApply={(newNames) => {
          setRenameOpen(false);
          void actions.applyBatchRename(selectedPaths, newNames);
        }}
      />
      {pickFor && (
        <DestinationPicker
          visible
          host={host}
          originPath={path}
          title={pickFor === 'move' ? t('moveToTitle') : t('copyToTitle')}
          confirmLabel={pickFor === 'move' ? t('moveToTooltip') : t('copyToTooltip')}
          onPick={(dir) => {
            const move = pickFor === 'move';
            setPickFor(null);
            void actions.transferSelectionTo(dir, move);
          }}
          onClose={() => setPickFor(null)}
        />
      )}
      {peekEntry && <PeekSheet host={host} entry={peekEntry} onClose={() => setPeekEntry(null)} />}
      {metaEntry && <MetaSheet visible host={host} entry={metaEntry} onClose={() => setMetaEntry(null)} onChanged={() => void ex.refresh()} onPreview={startPreview} />}
      <FavoritesSheet visible={favOpen} onClose={() => setFavOpen(false)} host={host} state={state} onOpen={ex.jumpTo} />
    </View>
  );
}

function TagChips({ tags, active, onChange }: { tags: string[]; active: string | null; onChange: (t: string | null) => void }) {
  const c = useScheme();
  const chip = (label: string, on: boolean, onPress: () => void) => (
    <Pressable key={label} onPress={onPress} hitSlop={6} accessibilityLabel={label} accessibilityState={{ selected: on }}>
      <View style={{ minHeight: 36, justifyContent: 'center', paddingHorizontal: 14, borderRadius: 12, backgroundColor: on ? mix(c.primary, c.surfaceContainer, 0.2) : c.surfaceContainerHigh }}>
        <Text style={LumenType.pill} color={on ? c.primary : c.onSurfaceVariant}>{label}</Text>
      </View>
    </Pressable>
  );
  // One scrolling row rather than wrapped rows, so filters cost a single line above the list.
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: 8, paddingHorizontal: GUTTER, paddingVertical: 6, alignItems: 'center' }}>
      {active && chip('All', false, () => onChange(null))}
      {tags.map((tag) => chip(tag, active === tag, () => onChange(active === tag ? null : tag)))}
    </ScrollView>
  );
}

const CATEGORY_LABEL: Record<EntryCategory, string> = { folder: 'Folders', image: 'Images', video: 'Videos', audio: 'Audio', document: 'Documents', archive: 'Archives', other: 'Other' };

/** One chip per kind of item in the folder; tap to show only that kind, tap again (or All) to show everything. */
function TypeChips({ categories, active, onChange }: { categories: EntryCategory[]; active: EntryCategory | null; onChange: (c: EntryCategory | null) => void }) {
  const c = useScheme();
  const chip = (label: string, on: boolean, onPress: () => void) => (
    <Pressable key={label} onPress={onPress} hitSlop={6} accessibilityLabel={label} accessibilityState={{ selected: on }}>
      <View style={{ minHeight: 36, justifyContent: 'center', paddingHorizontal: 14, borderRadius: 12, backgroundColor: on ? mix(c.primary, c.surfaceContainer, 0.2) : c.surfaceContainerHigh }}>
        <Text style={LumenType.pill} color={on ? c.primary : c.onSurfaceVariant}>{label}</Text>
      </View>
    </Pressable>
  );
  // One scrolling row rather than wrapped rows, so filters cost a single line above the list.
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: 8, paddingHorizontal: GUTTER, paddingVertical: 6, alignItems: 'center' }}>
      {active && chip('All', false, () => onChange(null))}
      {categories.map((cat) => chip(CATEGORY_LABEL[cat], active === cat, () => onChange(active === cat ? null : cat)))}
    </ScrollView>
  );
}
