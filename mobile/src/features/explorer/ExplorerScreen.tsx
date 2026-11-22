import * as Clipboard from 'expo-clipboard';
import { useFocusEffect, useRouter } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, BackHandler, FlatList, RefreshControl, ScrollView, View } from 'react-native';

import { listAllEntries } from '../../core/api/listAll';
import { can, type Entry } from '../../core/api/models';
import { ignoreError } from '../../core/log';
import type { Host } from '../../core/models/host';
import { Button, Chip, OfflineBanner, Pressable, Text, useDialogs, useToast } from '../../design/components';
import { ClipboardPaste, Monitor, Copy, CopyPlus, FolderInput, FolderOutput, GitCompare, Upload, Eye, EyeOff, HardDrive, Bookmark, FileUp, LayoutGrid, RefreshCw, Route, Search, SlidersHorizontal, Trash2 } from 'lucide-react-native';
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
import { humanizeError } from '../../core/humanizeError';
import { fetchPreviewFile } from '../preview/previewFile';
import { isPreviewable, previewableSiblings } from '../preview/previewKind';
import { usePreviewSession } from '../preview/session';
import { enqueueDownloads } from '../transfers/enqueueDownloads';
import { BrowseHeader, SelectionBar, SelectionHeader, SelectionTools, type OverflowAction, type SelectionChip } from './Bars';
import { BatchRenameSheet } from './BatchRenameSheet';
import { BreadcrumbBar } from './Breadcrumb';
import { CommandPalette, type PaletteAction } from './CommandPalette';
import { CreateMenu } from './CreateMenu';
import { DestinationPicker } from './DestinationPicker';
import { RelayCopySheet } from './RelayCopySheet';
import { DayHeader, EntryGridCell } from './EntryGridCell';
import { EntryTile } from './EntryTile';
import { FolderSkeleton, PreviewsOffNote, StatusBlock, type StatusAction } from './FolderStates';
import { useThumbsDenied } from './Thumbnail';
import { thumbnails } from './thumbnails';
import { buildGridRows, cellSize, folderSummary, formatDayLabel } from './gridLogic';
import { MetaSheet } from './MetaSheet';
import { soleExtractableArchive } from './metaLogic';
import { PeekSheet } from './PeekSheet';
import { FavoritesSheet, ViewOptionsSheet } from './Sheets';
import { useFileClipboard } from './clipboard';
import { normalizeTypedPath } from './paletteLogic';
import { transfers } from '../../core/native';
import { clientForHost, listingCache } from '../../services';
import { atRoot, currentPath } from './explorerStore';
import { basenameOf, folderLabel, parentDirOf } from './paths';
import { toggleFavoriteToast } from './favoriteToggle';
import { pathsText, selectedBytes } from './selectionLogic';
import { filterByTag } from './tagFilter';
import { ToolsRow } from './ToolsRow';
import { useExplorer } from './useExplorer';
import { useFileActions } from './useFileActions';
import { refreshFileCapabilities, useFileCapabilities } from './useFileCapabilities';

/** Photo grid (mockup `.grid3`): three columns with 2 dp between cells. */
const GRID_COLUMNS = 3;
const GRID_GAP = 2;
/** Side inset of the filter chips (rows themselves run edge to edge). */
const INSET = 16;

const fetchOfflineFile = (h: Host, e: Entry) => fetchPreviewFile(h, e, MAX_OFFLINE_FILE_BYTES);

/**
 * Files tab body for one host root: list/grid browsing, selection, clipboard operations, favorites, bookmarks and pinning.
 * [onLeaveRoot] goes back to the host's list of shared folders or drives; absent when there is nothing above [rootPath].
 */
export function ExplorerScreen({ host, rootPath, initialPath, onLeaveRoot }: { host: Host; rootPath: string; initialPath?: string; onLeaveRoot?: () => void }) {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const dialogs = useDialogs();
  const { ex, state, view, display, hidden, hiddenCount } = useExplorer(host, rootPath);
  const actions = useFileActions(host, ex);
  const collections = useCollections();
  const clip = useFileClipboard((s) => s.clip);
  const [createOpen, setCreateOpen] = useState(false);
  const caps = useFileCapabilities(host);
  const previewsOff = useThumbsDenied(host.id);
  const [viewOpen, setViewOpen] = useState(false);
  const [favOpen, setFavOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [metaEntry, setMetaEntry] = useState<Entry | null>(null);
  const [peekEntry, setPeekEntry] = useState<Entry | null>(null);
  // A tag filter applies only to the folder it was picked in.
  const [tagFilter, setTagFilter] = useState<{ path: string; tag: string } | null>(null);
  // A type filter (images, documents, ...) applies only to the folder it was picked in, like the tag filter.
  // Move to / Copy to: the destination sheet is open for the current selection.
  const [pickFor, setPickFor] = useState<'move' | 'copy' | null>(null);
  // Copy to another computer: the chooser sheet is open for the current selection.
  const [relayOpen, setRelayOpen] = useState(false);
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
          return listAllEntries((cursor) => client.list(path, { cursor }));
        },
        storeListing: (path, entries) => listingCache.put(host.id, path, entries),
        precache: (entries) => precachePinnedFolder(offlineDeps, host, entries, fetchOfflineFile),
      });
      if (result.folders > 0) markPinSynced(host.id);
    })().catch(ignoreError('pin sync'));
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
  const byTag = filterByTag(display, activeTag, taggedPaths);
  const entries = byTag;

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
    await toggleFavoriteToast((o) => collections.toggleFavorite(o), toast, { hostId: host.id, path, name: folderLabel(path), isFav });
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
      await precachePinnedFolder(offlineDeps, host, all, fetchOfflineFile);
    })().catch(ignoreError('pin precache'));
  }

  async function bookmark(e: Entry) {
    const already = collections.bookmarks.some((b) => b.hostId === host.id && b.remotePath === e.path);
    if (already) {
      const ok = await dialogs.confirm({ title: t('removeBookmarkTitle'), description: t('removeBookmarkBody', { name: e.name }), confirmLabel: t('phonesRemove'), cancelLabel: t('cancelButton') });
      if (ok) await collections.removeBookmark(host.id, e.path);
      return;
    }
    const tag = await dialogs.prompt({ title: t('bookmarkTitle', { name: e.name }), placeholder: t('tagOptionalPlaceholder'), confirmLabel: t('saveButton'), allowEmpty: true });
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
    bookmark(entry).catch(ignoreError('bookmark'));
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
      case 'commandPalette':
        return setPaletteOpen(true);
      default:
        return;
    }
  };

  const goToPath = async () => {
    const typed = await dialogs.prompt({ title: t('goToPathTitle'), placeholder: t('goToPathPlaceholder'), confirmLabel: t('goButton'), mono: true });
    const target = typed === null ? null : normalizeTypedPath(typed);
    if (target) ex.jumpTo(target);
  };
  const paletteActions: PaletteAction[] = [
    { id: 'search', label: t('searchButton'), icon: Search, run: () => router.push({ pathname: '/host/[id]/search', params: { id: host.id, path } }) },
    { id: 'refresh', label: t('refreshTooltip'), icon: RefreshCw, run: () => { refreshFileCapabilities(); return ex.refresh(); } },
    { id: 'grid', label: t('toggleGridList'), icon: LayoutGrid, run: () => void useSettings.getState().setApp('gridView', !view.gridView) },
    { id: 'view', label: t('viewOptionsTitle'), icon: SlidersHorizontal, run: () => setViewOpen(true) },
    { id: 'favorites', label: t('favoritesTitle'), icon: Bookmark, run: () => setFavOpen(true) },
    { id: 'transfers', label: t('transfersMenuItem'), icon: FileUp, run: () => router.navigate('/transfers') },
    { id: 'usage', label: t('usageMenuItem'), icon: HardDrive, run: () => router.push({ pathname: '/host/[id]/usage', params: { id: host.id, path } }) },
    { id: 'trash', label: t('trashTitle'), icon: Trash2, run: () => router.push('/trash') },
    { id: 'goto', label: t('navigateToPath'), icon: Route, run: () => void goToPath() },
  ];

  const showMore = state.nextCursor !== null && !activeTag;
  const showHidden = hiddenCount > 0 && !activeTag;
  const multi = selectMode || state.selected.size > 0;
  const soleArchive = soleExtractableArchive(state.entries, state.selected);
  const selectedPaths = useMemo(() => [...state.selected], [state.selected]);
  const showPaste = clip !== null && clip.paths.length > 0 && clip.hostId === host.id && can(caps, 'modify');

  const pickedFiles = selectedCount === 2 ? state.entries.filter((e) => state.selected.has(e.path)) : [];
  const selectionChips: SelectionChip[] = [
    ...(selectedCount > 0 ? [{ key: 'paths', label: t('copyPathsTooltip', { count: selectedCount }), icon: Copy, onPress: () => void copySelectedPaths() }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') ? [{ key: 'dup', label: t('duplicateButton'), icon: CopyPlus, onPress: () => void actions.duplicateSelection() }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') && can(caps, 'delete') ? [{ key: 'moveTo', label: t('moveToTooltip'), icon: FolderInput, onPress: () => setPickFor('move') }] : []),
    ...(selectedCount > 0 && can(caps, 'modify') ? [{ key: 'copyTo', label: t('copyToTooltip'), icon: FolderOutput, onPress: () => setPickFor('copy') }] : []),
    ...(selectedCount > 0 && can(caps, 'download') && state.entries.filter((e) => state.selected.has(e.path)).every((e) => !e.isDir) ? [{ key: 'relay', label: t('relayCopyTooltip'), icon: Monitor, onPress: () => setRelayOpen(true) }] : []),
    ...(pickedFiles.length === 2 && pickedFiles.every((e) => !e.isDir) ? [{ key: 'compare', label: t('compareTooltip'), icon: GitCompare, onPress: () => void actions.compareSelection() }] : []),
  ];

  const back = atRoot(state) ? onLeaveRoot : () => void ex.popDirectory();
  const header = multi ? (
    <SelectionHeader count={selectedCount} bytes={selectedBytes(state.entries, state.selected)} all={entries.length > 0 && entries.every((x) => state.selected.has(x.path))} onClose={endSelection} onSelectAll={() => ex.selectAll(entries)} onClearSelection={ex.clearSelection} />
  ) : (
    <BrowseHeader
      title={folderLabel(path)}
      onBack={back}
      backLabel={t('folderBackTooltip')}
      isFav={isFav}
      isCurrentFolderPinned={pinnedHere}
      canCreate={can(caps, 'modify')}
      onSearch={() => router.push({ pathname: '/host/[id]/search', params: { id: host.id, path } })}
      onOverflow={onOverflow}
    />
  );
  const reading = state.loading && state.entries.length === 0;
  // Path and tools stay pinned under the header (mockup `.path` and `.tools`); selection mode swaps the tools for its own row.
  const pathAndTools = (
    <View>
      <BreadcrumbBar pathStack={state.pathStack} onNavigateTo={ex.navigateTo} onJumpTo={ex.jumpTo} rootLabel={host.label} />
      {multi ? (
        <SelectionTools count={selectedCount} canModify={can(caps, 'modify')} extras={selectionChips} onInvertSelection={() => ex.invertSelection(entries)} onRename={() => void actions.renameOne(selectedPaths[0])} onBatchRename={() => setRenameOpen(true)} onBookmark={bookmarkSelected} onDetails={detailsOfSelected} />
      ) : entries.length > 0 || reading ? (
        <ToolsRow summary={folderSummary(entries, showMore)} reading={reading} sort={view.sort} gridView={view.gridView} onSort={() => setViewOpen(true)} onToggleGrid={() => void useSettings.getState().setApp('gridView', !view.gridView)} />
      ) : null}
    </View>
  );
  // Tag and kind filters scroll with the entries.
  const filterRows = (
    <View>
      {tags.length > 0 && <ChipRow items={tags} labelOf={(tag) => tag} active={activeTag} onChange={setActiveTag} />}
    </View>
  );

  // Photo grid: three equal cells per line; with a sort by date each day gets a header line.
  const cell = cellSize(width, GRID_COLUMNS, GRID_GAP);
  const gridRows = useMemo(
    () => (view.gridView ? buildGridRows(entries, GRID_COLUMNS, { byDay: view.sort.field === 'date', label: (d) => (d ? formatDayLabel(d) : t('undatedLabel')) }) : []),
    [entries, view.gridView, view.sort.field],
  );

  let body: React.ReactNode;
  if (reading) body = <FolderSkeleton />;
  else if (state.error && state.entries.length === 0) {
    // The saved folder is no longer one this device may browse (the computer's allowed folders changed): retrying cannot help, so offer the list of shared folders again.
    const rootRejected = state.errorCode === 'FORBIDDEN' && atRoot(state);
    const errorActions: StatusAction[] = [
      { label: t('retryButton'), onPress: () => { refreshFileCapabilities(); void ex.refresh(); }, icon: (k) => <RefreshCw size={20} color={k} /> },
      ...(rootRejected ? [{ label: t('chooseAnotherFolder'), onPress: () => { const cur = useActiveHost.getState().active; if (cur) useActiveHost.getState().setActive({ ...cur, rootPath: undefined, initialPath: undefined }); } }] : []),
    ];
    body = <StatusBlock emoji="desktop_computer" off title={t('folderErrorTitle')} hint={state.error} actions={errorActions} />;
  } else if (entries.length === 0 && state.entries.length > 0) {
    // Everything is hidden by a tag or kind filter: say so and offer the way back.
    body = (
      <View>
        {filterRows}
        <StatusBlock title={t('noMatchesMessage')} hint={t('noMatchesHint')} actions={[{ label: t('showAllButton'), onPress: () => { setTagFilter(null); } }]} />
      </View>
    );
  } else if (entries.length === 0) {
    const emptyActions: StatusAction[] = [
      ...(can(caps, 'upload') ? [{ label: t('sendFilesHere'), onPress: () => void actions.upload(), icon: (k: string) => <Upload size={20} color={k} /> }] : []),
      ...(can(caps, 'modify') ? [{ label: t('newFolderButton'), onPress: () => void actions.createNamed(true) }] : []),
    ];
    body = <StatusBlock emoji="open_file_folder" title={t('emptyFolderMessage')} hint={emptyActions.length > 0 ? t('emptyFolderHint') : undefined} actions={emptyActions} />;
  } else {
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
      onEndReached: () => showMore && void ex.loadMore(),
      onEndReachedThreshold: 0.6,
      refreshControl: <RefreshControl refreshing={state.loading} onRefresh={() => { refreshFileCapabilities(); thumbnails.forgetDenied(host.id); void ex.refresh(); }} />,
      ListFooterComponent: footer,
      ListHeaderComponent: (
        <View>
          {filterRows}
          {previewsOff && <PreviewsOffNote />}
        </View>
      ),
    };
    body = view.gridView ? (
      <FlatList
        key="grid"
        {...common}
        data={gridRows}
        keyExtractor={(r) => r.key}
        renderItem={({ item }) =>
          item.type === 'day' ? (
            <DayHeader label={item.label} count={item.count} />
          ) : (
            <View style={{ flexDirection: 'row', gap: GRID_GAP, marginBottom: GRID_GAP }}>
              {item.entries.map((e) => (
                <View key={e.path} style={{ opacity: hidden.has(e.path) ? 0.55 : 1 }}>
                  <EntryGridCell entry={e} hostId={host.id} size={cell} selected={state.selected.has(e.path)} multiSelect={multi} isFavorite={favPaths.has(e.path)} isPinned={pinPaths.has(e.path)} onPress={() => openEntry(e)} onLongPress={() => longPressEntry(e)} />
                </View>
              ))}
            </View>
          )
        }
      />
    ) : (
      <FlatList
        key="list"
        {...common}
        data={entries}
        keyExtractor={(e: Entry) => e.path}
        contentContainerStyle={{ paddingBottom: INSET }}
        renderItem={({ item }) => (
          <View style={{ opacity: hidden.has(item.path) ? 0.55 : 1 }}>
            <EntryTile entry={item} hostId={host.id} density={view.density} selected={state.selected.has(item.path)} multiSelect={multi} isFavorite={favPaths.has(item.path)} isPinned={pinPaths.has(item.path)} onPress={() => openEntry(item)} onLongPress={() => longPressEntry(item)} onPeek={isPreviewable(item) ? () => setPeekEntry(item) : undefined} onSelect={() => ex.toggleSelect(item.path)} />
          </View>
        )}
      />
    );
  }

  return (
    <View style={{ flex: 1 }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
      {header}
      {state.offline && <OfflineBanner text={t('offlineBannerText')} />}
      {pathAndTools}
      {showPaste && !multi && (
        <View style={{ flexDirection: 'row', gap: Spacing.sm, paddingHorizontal: Spacing.md2, paddingVertical: 6 }}>
          <Button label={t('pasteNItems', { count: clip!.paths.length })} renderIcon={(c) => <ClipboardPaste size={16} color={c} />} kind="tonal" size="s" onPress={() => void actions.paste()} />
          <Button label={t('dismissButton')} kind="neutral" size="s" onPress={() => useFileClipboard.getState().clear()} />
        </View>
      )}
      <View style={{ flex: 1 }}>{body}</View>
      {multi && (
        <SelectionBar count={state.selected.size} onCut={actions.cutSelection} onCopy={actions.copySelection} onCompress={actions.compressSelected} onExtract={soleArchive ? () => void actions.extractArchive(soleArchive) : undefined} onDownload={downloadSelected} onDelete={actions.confirmDelete} caps={caps} />
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
      {relayOpen && <RelayCopySheet source={host} paths={selectedPaths} onDone={() => { setRelayOpen(false); endSelection(); }} onClose={() => setRelayOpen(false)} />}
      {peekEntry && <PeekSheet host={host} entry={peekEntry} onClose={() => setPeekEntry(null)} />}
      {metaEntry && <MetaSheet visible host={host} entry={metaEntry} onClose={() => setMetaEntry(null)} onChanged={() => void ex.refresh()} onPreview={startPreview} />}
      <FavoritesSheet visible={favOpen} onClose={() => setFavOpen(false)} host={host} state={state} onOpen={ex.jumpTo} />
    </View>
  );
}

/** One chip per item (tag or kind); tap to show only that one, tap again (or All) to show everything. */
function ChipRow<T extends string>({ items, labelOf, active, onChange }: { items: T[]; labelOf: (item: T) => string; active: T | null; onChange: (item: T | null) => void }) {
  // One scrolling row rather than wrapped rows, so filters cost a single line above the list.
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: Spacing.sm, paddingHorizontal: INSET, paddingVertical: Spacing.sm, alignItems: 'center' }}>
      {active && <Chip label={t('filterAllChip')} onPress={() => onChange(null)} />}
      {items.map((item) => <Chip key={labelOf(item)} label={labelOf(item)} selected={active === item} onPress={() => onChange(active === item ? null : item)} />)}
    </ScrollView>
  );
}

