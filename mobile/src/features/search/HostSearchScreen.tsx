import { useRouter } from 'expo-router';
import { Bookmark, ChevronDown, History, X } from 'lucide-react-native';
import { useEffect, useMemo, useState } from 'react';
import { FlatList, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useStore } from 'zustand';

import type { Host } from '../../core/models/host';
import { useDebouncedCall } from '../../core/useDebouncedCall';
import { Button, DuoIcon, Emoji, ErrorRetry, Pressable, Row, Text, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useCollections } from '../../state/collections';
import { useResolvedVisibility } from '../../state/settings';
import { useActiveHost } from '../../state/activeHost';
import { revealInExplorer } from '../explorer/reveal';
import { ComputerSwitcherSheet } from '../hosts/ComputerSwitcherSheet';
import { useWorkspaces } from '../hosts/useWorkspaces';
import { folderLabel } from '../explorer/paths';
import { humanizeError } from '../../core/humanizeError';
import { SearchFilterSheet } from './SearchFilterSheet';
import { GlobIndicator, NoResults, ResultRow, ResultSkeleton, ScopeChips, SearchBarRow, SectionHeading, TruncationBanner, type ScopeChipSpec } from './SearchParts';
import { createSearchController } from './searchController';
import { defaultFilters, filterSearchResults, isGlobQuery, suggestQuery, type SearchFilters } from './searchLogic';
import { scopeChips, scopeLabels, scopePhrase, type ScopeChipAction } from './searchScope';
import { Spacing } from '../../design/tokens';

const DEBOUNCE_MS = 450;
/** First agent API revision that reads the `content` search option. */
const CONTENT_SEARCH_API_VERSION = 3;

/** Search one host: debounced query, scope chips (this computer, all computers, folder, type, size), filter sheet, recent and saved searches. Tapping a hit shows its folder in Files. */
export function HostSearchScreen({ host, currentPath, onlyEverywhere }: { host: Host; currentPath: string; onlyEverywhere?: boolean }) {
  const c = useScheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const dialogs = useDialogs();
  const vis = useResolvedVisibility(host.id);
  const recordSearch = useCollections((s) => s.recordSearch);
  const controller = useMemo(
    () => createSearchController({ getClient: () => clientForHost(host), humanize: humanizeError, onSearched: (q) => void recordSearch(q) }),
    [host, recordSearch],
  );
  const state = useStore(controller);
  const [text, setText] = useState('');
  const [filters, setFilters] = useState<SearchFilters>(() => defaultFilters(!onlyEverywhere));
  const [filterOpen, setFilterOpen] = useState(false);
  const debounce = useDebouncedCall(DEBOUNCE_MS);
  // Searching inside files is offered only when this computer says it can (an older one would silently search names).
  const [canSearchContent, setCanSearchContent] = useState(false);
  useEffect(() => {
    let live = true;
    clientForHost(host)
      .then((cl) => cl.health())
      .then((hl) => live && setCanSearchContent(hl.apiVersion >= CONTENT_SEARCH_API_VERSION))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [host]);

  const run = (value: string, f: SearchFilters = filters) => {
    debounce.cancel();
    void controller.run(value, f, currentPath);
  };
  const onChange = (value: string) => {
    setText(value);
    debounce.schedule(() => void controller.run(value, filters, currentPath));
  };
  const apply = (f: SearchFilters) => {
    setFilters(f);
    setFilterOpen(false);
    if (text.trim()) run(text, f);
  };
  const pick = (q: string) => {
    setText(q);
    run(q);
  };
  const allComputers = () => router.replace('/search');

  async function saveCurrent() {
    const q = text.trim();
    if (!q) return;
    const name = await dialogs.prompt({ title: t('saveSearch'), placeholder: t('savedSearchName'), initialValue: q, confirmLabel: t('saveButton') });
    if (name) await useCollections.getState().addSavedSearch({ name, query: q });
  }

  const results = useMemo(() => filterSearchResults(state.raw, vis, filters.includeHidden), [state.raw, vis, filters.includeHidden]);
  const glob = !filters.content && (filters.mode !== 'substring' || isGlobQuery(state.query));
  const active = useActiveHost((s) => s.active);
  const rootPath = active?.host.id === host.id ? active.rootPath : undefined;
  const banner = state.truncated ? t('showingFirstNResults', { limit: results.length }) : state.timeBudgetHit ? t('searchTimedOut') : null;
  const labels = scopeLabels(filters, host.label, folderLabel(currentPath), currentPath);
  const scope = scopePhrase(filters.fromHere, labels);

  const runChip = (a: ScopeChipAction) => {
    if (a.kind === 'filters') apply({ ...filters, ...a.patch });
    else if (a.kind === 'allComputers') allComputers();
    else setFilterOpen(true);
  };
  const chips: ScopeChipSpec[] = scopeChips(filters, labels, { canSearchContent }).map(({ action, ...chip }) => ({ ...chip, onPress: () => runChip(action) }));

  let body: React.ReactNode;
  if (state.query === '') body = <RecentAndSaved onPick={pick} />;
  else if (state.loading) body = <ResultSkeleton />;
  else if (state.error) body = <ErrorRetry message={t('searchFailed', { error: state.error })} onRetry={() => run(text)} />;
  else if (results.length === 0) {
    const suggestion = suggestQuery(state.query);
    body = (
      <NoResults
        title={t('searchNothingCalled', { query: state.query, scope })}
        hint={t('searchNothingCheck')}
        actions={[
          { label: t('searchAllComputersButton'), primary: true, onPress: allComputers },
          ...(suggestion ? [{ label: t('searchTrySpelling', { query: suggestion }), onPress: () => pick(suggestion) }] : []),
        ]}
      />
    );
  } else {
    body = (
      <FlatList
        data={results}
        keyExtractor={(e) => e.path}
        contentContainerStyle={{ paddingBottom: 18 }}
        ListHeaderComponent={
          <View style={{ flexDirection: 'row', alignItems: 'baseline', justifyContent: 'space-between', paddingRight: Spacing.xs }}>
            <SectionHeading text={t('searchFoundOn', { count: results.length, scope })} />
            <Pressable onPress={saveCurrent} hitSlop={8} accessibilityLabel={t('saveSearch')}>
              <Text style={[LumenType.sectionLabel, { paddingHorizontal: Spacing.md2, paddingTop: Spacing.md3 }]} color={c.onSurface}>{t('saveButton')}</Text>
            </Pressable>
          </View>
        }
        renderItem={({ item }) => <ResultRow entry={item} query={state.query} highlight={!glob && !filters.content} onPress={() => revealInExplorer(router, host, item.path, rootPath)} />}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface, paddingTop: insets.top }}>
      <SearchBarRow
        value={text}
        onChange={onChange}
        onSubmit={() => run(text)}
        placeholder={t('searchHint')}
        onClear={() => {
          onChange('');
          run('');
        }}
      />
      <SearchComputer host={host} />
      <ScopeChips chips={chips} />
      {glob && state.query !== '' && <GlobIndicator />}
      {banner && <TruncationBanner message={banner} />}
      <View style={{ flex: 1 }}>{body}</View>
      <SearchFilterSheet visible={filterOpen} filters={filters} currentPath={currentPath} defaultFromHere={!onlyEverywhere} onApply={apply} onClose={() => setFilterOpen(false)} />
    </View>
  );
}

/** The computer being searched, under the search box: its name with a chevron; tapping opens the computer switcher to search another one. */
function SearchComputer({ host }: { host: Host }) {
  const c = useScheme();
  const router = useRouter();
  const { hosts, reload } = useWorkspaces();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Pressable onPress={() => setOpen(true)} accessibilityRole="button" accessibilityLabel={`${host.label}. ${t('switchComputerLabel')}`} style={{ alignSelf: 'flex-start', maxWidth: '100%' }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm }}>
          <Emoji name="desktop_computer" size={20} />
          <Text style={[LumenType.caption, { flexShrink: 1 }]} numberOfLines={1}>{host.label}</Text>
          <DuoIcon icon={ChevronDown} size={18} color={c.onSurfaceVariant} />
        </View>
      </Pressable>
      <ComputerSwitcherSheet
        visible={open}
        hosts={hosts ?? [host]}
        selectedId={host.id}
        onClose={() => setOpen(false)}
        onSelect={(h) => {
          if (h.id !== host.id) router.replace({ pathname: '/host/[id]/search', params: { id: h.id } } as never);
        }}
        onReview={(h) => router.push({ pathname: '/host/[id]/trust', params: { id: h.id } } as never)}
        onPair={(address) => router.push({ pathname: '/pair', params: { address } })}
        onAdd={() => router.push('/pair')}
        onManage={() => router.push('/workspaces')}
        onChanged={reload}
      />
    </>
  );
}

/** Empty-query body: saved searches, then recent ones (each removable), or a short hint plus a way to search every computer when there are none. */
function RecentAndSaved({ onPick }: { onPick: (q: string) => void }) {
  const c = useScheme();
  const saved = useCollections((s) => s.savedSearches);
  const recent = useCollections((s) => s.recentSearches);
  const removeSaved = useCollections((s) => s.removeSavedSearch);
  const removeRecent = useCollections((s) => s.removeRecentSearch);
  const clearRecent = useCollections((s) => s.clearRecentSearches);
  const remove = (label: string, onRemove: () => void) => (
    <Pressable onPress={onRemove} accessibilityLabel={t('removeLabel', { label })} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <X size={20} color={c.onSurfaceVariant} />
    </Pressable>
  );
  if (saved.length === 0 && recent.length === 0) {
    return (
      <View style={{ paddingTop: Spacing.lg, paddingHorizontal: Spacing.lg }}>
        <Text style={[LumenType.name, { lineHeight: 24 }]} muted>{t('typeToSearch')}</Text>
      </View>
    );
  }
  return (
    <ScrollView keyboardShouldPersistTaps="handled">
      {saved.length > 0 && <SectionHeading text={t('savedSearches')} />}
      {saved.map((s) => (
        <Row key={s.name} lead={<DuoIcon icon={Bookmark} />} title={s.name} subtitle={s.query} onPress={() => onPick(s.query)} trailing={remove(s.name, () => void removeSaved(s.name))} />
      ))}
      {recent.length > 0 && (
        <View style={{ flexDirection: 'row', alignItems: 'baseline', justifyContent: 'space-between', paddingRight: Spacing.xs }}>
          <SectionHeading text={t('recentSearches')} />
          <Button size="s" kind="text" label={t('clearAllButton')} onPress={() => void clearRecent()} />
        </View>
      )}
      {recent.map((q) => (
        <Row key={q} lead={<DuoIcon icon={History} />} title={q} onPress={() => onPick(q)} trailing={remove(q, () => void removeRecent(q))} />
      ))}
    </ScrollView>
  );
}
