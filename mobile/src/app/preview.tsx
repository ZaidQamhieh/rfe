import { Stack, useRouter } from 'expo-router';
import { FileCode, Info, ListOrdered, Pencil, Radio } from 'lucide-react-native';
import { useCallback, useEffect, useRef, useState } from 'react';
import { FlatList, StatusBar, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { can, type Entry } from '../core/api/models';
import { BottomSheet, DuoIcon, MockupSwitch, Row, SheetHead, Text, useDialogs, useToast } from '../design/components';
import { useScheme } from '../design/theme';
import { lightScheme, Radii, Spacing } from '../design/tokens';
import { ON_DARK } from '../design/overlayPalette';
import { t } from '../i18n';
import { clientForHost } from '../services';
import { humanizeError } from '../core/humanizeError';
import { MetaSheet } from '../features/explorer/MetaSheet';
import { PreviewIconButton, PreviewTopBar } from '../features/preview/PreviewChrome';
import { FactsList, PreviewActions, PreviewFacts, TechnicalDetails } from '../features/preview/PreviewInfo';
import { factRows } from '../features/preview/previewFacts';
import { networkTransports } from '../core/native';
import { pagerChipTop, pagerChipTopClearance } from '../features/preview/pagerChip';
import { preloadNeighbours } from '../features/preview/neighbourPreload';
import { fetchPreviewFile, MAX_EDITABLE_BYTES, MAX_IN_MEMORY_PREVIEW_BYTES } from '../features/preview/previewFile';
import { previewKindOf } from '../features/preview/previewKind';
import { neighbourIndex, type SwipeDirection } from '../features/preview/swipeNav';
import { useEditorSession, usePreviewSession } from '../features/preview/session';
import { useExternalActions } from '../features/preview/useExternalActions';
import { useFileCapabilities } from '../features/explorer/useFileCapabilities';
import { deleteDoneMessage } from '../features/explorer/deleteToast';
import { useSettings } from '../state/settings';
import { utf8Length } from '../features/preview/textDecode';
import { PreviewPage } from '../features/preview/viewers/PreviewPage';
import { enqueueDownloads } from '../features/transfers/enqueueDownloads';

/**
 * Port of PreviewPager: swipe between the previewable siblings the explorer handed over, one shared
 * top bar for the current page, and a "n of m" indicator.
 */
export default function Preview() {
  const session = usePreviewSession((s) => s.session);
  const router = useRouter();
  if (!session || session.entries.length === 0) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Stack.Screen options={{ headerShown: false }} />
        <Text muted>{t('nothingToPreview')}</Text>
      </View>
    );
  }
  return <Pager key={session.entries[session.index]?.path} onClose={() => router.back()} />;
}

function Pager({ onClose }: { onClose: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const router = useRouter();
  const { width } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const session = usePreviewSession((s) => s.session)!;
  const openEditor = useEditorSession((s) => s.open);
  const [entries, setEntries] = useState<Entry[]>(session.entries);
  const [index, setIndex] = useState(Math.min(Math.max(session.index, 0), session.entries.length - 1));
  const [zoomed, setZoomed] = useState(false);
  const [lineNumbers, setLineNumbers] = useState(false);
  const [rawMarkdown, setRawMarkdown] = useState(false);
  const [follow, setFollow] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [metaOpen, setMetaOpen] = useState(false);
  const [texts, setTexts] = useState<Record<string, string>>({});
  const list = useRef<FlatList<Entry>>(null);
  const external = useExternalActions(session.host);
  const allowCellular = useSettings((x) => x.state.app.preloadPreviewOnCellular);

  // Warm the images either side of the current page so the next swipe is instant.
  useEffect(() => {
    void preloadNeighbours({ host: session.host, entries, index, allowCellular, transports: networkTransports, fetchFile: (h, e) => fetchPreviewFile(h, e, MAX_IN_MEMORY_PREVIEW_BYTES) });
  }, [session.host, entries, index, allowCellular]);

  const current = entries[index];
  const kind = previewKindOf(current);
  const onDark = kind === 'image' || kind === 'video';
  const loadedText = texts[current.path] as string | undefined;
  const caps = useFileCapabilities(session.host);
  const canEdit = can(caps, 'modify') && (kind === 'text' || kind === 'markdown') && loadedText !== undefined && (current.size ?? utf8Length(loadedText)) <= MAX_EDITABLE_BYTES;

  const onText = useCallback((path: string, text: string | null) => {
    setTexts((m) => {
      if (text === null) {
        if (!(path in m)) return m;
        const { [path]: _drop, ...rest } = m;
        return rest;
      }
      return m[path] === text ? m : { ...m, [path]: text };
    });
  }, []);

  function replaceCurrent(e: Entry) {
    setEntries((l) => l.map((x) => (x.path === e.path ? e : x)));
    session.onChanged?.();
  }

  // A swipe an image page caught itself: move the pager one page that way (nothing at either end).
  const goBy = useCallback(
    (dir: SwipeDirection) => {
      const ni = neighbourIndex(index, entries.length, dir);
      if (ni === null) return;
      setIndex(ni);
      setZoomed(false);
      list.current?.scrollToIndex({ index: ni, animated: true });
    },
    [index, entries.length],
  );

  function dropCurrent() {
    session.onChanged?.();
    if (entries.length <= 1) return onClose();
    const next = entries.filter((_, i) => i !== index);
    const ni = Math.min(index, next.length - 1);
    setEntries(next);
    setIndex(ni);
    requestAnimationFrame(() => list.current?.scrollToIndex({ index: ni, animated: false }));
  }

  async function save() {
    setMoreOpen(false);
    try {
      await enqueueDownloads(session.host, [current.path]);
      toast.info(t('savingFile', { name: current.name }));
    } catch (e) {
      toast.error(humanizeError(e));
    }
  }

  async function remove() {
    setMoreOpen(false);
    const choice = await dialogs.choose<'trash' | 'forever'>({
      title: t('deleteTitle'),
      subtitle: t('moveToTrashConfirm', { name: current.name }),
      options: [
        { value: 'trash', label: t('moveToTrashButton') },
        { value: 'forever', label: t('deleteForeverButton'), tint: c.error },
      ],
    });
    if (!choice) return;
    try {
      const client = await clientForHost(session.host);
      await client.delete([current.path], { permanent: choice === 'forever' });
      toast.success(deleteDoneMessage(choice === 'forever', current.name));
      dropCurrent();
    } catch (e) {
      toast.error(t('deleteFailed', { error: humanizeError(e) }));
    }
  }

  function edit() {
    if (loadedText === undefined) return;
    openEditor({
      host: session.host,
      entry: current,
      text: loadedText,
      onSaved: (e) => {
        onText(e.path, null);
        replaceCurrent(e);
      },
    });
    router.push('/edit');
  }

  const leading = (
    <>
      {/* Line numbers and Follow live in the "…" sheet so the file name keeps its room; while Follow is on it stays here too, to show it is live and stop it in one tap. */}
      {kind === 'text' && follow && (
        <PreviewIconButton label={t('followButton')} selected={follow} onPress={() => setFollow((v) => !v)}>
          <Radio size={19} color={follow ? c.primary : c.onSurfaceVariant} />
        </PreviewIconButton>
      )}
      {kind === 'markdown' && (
        <PreviewIconButton label={t('showSourceLabel')} selected={rawMarkdown} onPress={() => setRawMarkdown((v) => !v)}>
          <FileCode size={19} color={rawMarkdown ? c.primary : c.onSurfaceVariant} />
        </PreviewIconButton>
      )}
      {canEdit && (
        <PreviewIconButton label={t('editLabel')} onPress={edit}>
          <Pencil size={19} color={c.onSurfaceVariant} />
        </PreviewIconButton>
      )}
    </>
  );

  return (
    <View style={{ flex: 1, backgroundColor: onDark ? ON_DARK.canvas : c.surface }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StatusBar barStyle={onDark || c.surface !== lightScheme.surface ? 'light-content' : 'dark-content'} />
      <PreviewTopBar entry={current} onDark={onDark} onBack={onClose} onMore={() => setMoreOpen(true)} leading={leading} />
      <FlatList
        ref={list}
        data={entries}
        horizontal
        pagingEnabled
        scrollEnabled={!zoomed}
        keyExtractor={(e) => e.path}
        initialScrollIndex={index}
        getItemLayout={(_, i) => ({ length: width, offset: width * i, index: i })}
        windowSize={3}
        initialNumToRender={1}
        maxToRenderPerBatch={1}
        showsHorizontalScrollIndicator={false}
        onMomentumScrollEnd={(e) => {
          const i = Math.round(e.nativeEvent.contentOffset.x / width);
          if (i !== index && i >= 0 && i < entries.length) {
            setIndex(i);
            setZoomed(false);
          }
        }}
        renderItem={({ item, index: i }) => (
          // The action row below the pager already clears the gesture bar; the top clearance keeps the "n of m" chip off the page's first line.
          <View style={{ width, flex: 1, paddingTop: pagerChipTopClearance(entries.length, ['image', 'video'].includes(previewKindOf(item))) }}>
            <PreviewPage host={session.host} entry={item} isCurrent={i === index} lineNumbers={lineNumbers} rawMarkdown={rawMarkdown} follow={follow} onZoomChange={setZoomed} onSwipe={goBy} onText={onText} />
          </View>
        )}
      />
      {entries.length > 1 && (
        <View pointerEvents="none" style={{ position: 'absolute', top: pagerChipTop(insets.top), left: 0, right: 0, alignItems: 'center' }}>
          <View style={{ backgroundColor: ON_DARK.pill, borderRadius: Radii.stadium, paddingHorizontal: Spacing.md, paddingVertical: Spacing.xs }}>
            <Text variant="bodySmall" color={ON_DARK.ink}>{t('previewPageIndicator', { current: index + 1, total: entries.length })}</Text>
          </View>
        </View>
      )}
      {/* Photos and videos show their info under the picture; every other file keeps it in the "…" sheet. */}
      {onDark ? <PreviewFacts host={session.host} entry={current} media onDark /> : null}
      <PreviewActions
        onDark={onDark}
        bottomInset={insets.bottom}
        onShare={() => void external.share(current)}
        onSave={can(caps, 'download') ? () => void save() : undefined}
        onOpen={can(caps, 'download') ? () => void external.openWith(current) : undefined}
        onDelete={can(caps, 'delete') ? () => void remove() : undefined}
      />
      <BottomSheet visible={moreOpen} onClose={() => setMoreOpen(false)}>
        <SheetHead title={current.name} />
        <View style={{ paddingBottom: Spacing.md }}>
          {kind === 'text' ? <Row title={t('lineNumbersLabel')} lead={<DuoIcon icon={ListOrdered} />} trailing={<MockupSwitch value={lineNumbers} />} onPress={() => { setMoreOpen(false); setLineNumbers((v) => !v); }} /> : null}
          {kind === 'text' ? <Row title={t('followButton')} lead={<DuoIcon icon={Radio} />} trailing={<MockupSwitch value={follow} />} onPress={() => { setMoreOpen(false); setFollow((v) => !v); }} /> : null}
          <Row title={t('detailsButton')} lead={<DuoIcon icon={Info} />} onPress={() => { setMoreOpen(false); setMetaOpen(true); }} />
          {onDark ? null : <FactsList rows={factRows(current)} />}
          <TechnicalDetails path={current.path} />
        </View>
      </BottomSheet>
      {/* Mutations here (rename, delete, extract...) leave the pager stale, so they return to the refreshed folder. */}
      {metaOpen && <MetaSheet visible host={session.host} entry={current} onClose={() => setMetaOpen(false)} onChanged={() => { session.onChanged?.(); onClose(); }} />}
    </View>
  );
}
