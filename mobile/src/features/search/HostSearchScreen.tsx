import { useRouter } from 'expo-router';
import { ArrowLeft, Bookmark, BookmarkPlus, History, Search, SearchX, SlidersHorizontal, X } from 'lucide-react-native';
import { useEffect, useMemo, useRef, useState } from 'react';
import { FlatList, ScrollView, TextInput, View } from 'react-native';
import { useStore } from 'zustand';

import type { Host } from '../../core/models/host';
import { Button, ErrorRetry, Pressable, Text, TopBar, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { mix } from '../../design/color';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useCollections } from '../../state/collections';
import { useResolvedVisibility } from '../../state/settings';
import { useActiveHost } from '../../state/activeHost';
import { revealInExplorer } from '../explorer/reveal';
import { humanizeError } from '../pairing/pairingService';
import { SearchFilterSheet } from './SearchFilterSheet';
import { CategoryChips, GlobIndicator, ResultCardRow, ScopePill, SearchField, SearchResultTile, TruncationBanner } from './SearchParts';
import { createSearchController } from './searchController';
import { activeFilterCount, defaultFilters, filterSearchResults, isGlobQuery, type SearchCategory, type SearchFilters } from './searchLogic';

const DEBOUNCE_MS = 450;

/** Search one host: debounced query, category chips, filter sheet, recent and saved searches. Tapping a hit shows its folder in Files. */
export function HostSearchScreen({ host, currentPath, onlyEverywhere }: { host: Host; currentPath: string; onlyEverywhere?: boolean }) {
  const c = useScheme();
  const router = useRouter();
  const dialogs = useDialogs();
  const vis = useResolvedVisibility(host.id);
  const recordSearch = useCollections((s) => s.recordSearch);
  const controller = useMemo(
    () => createSearchController({ getClient: () => clientForHost(host), humanize: humanizeError, onSearched: (q) => void recordSearch(q) }),
    [host, recordSearch],
  );
  const state = useStore(controller);
  const [text, setText] = useState('');
  const [filters, setFilters] = useState<SearchFilters>(() => ({ ...defaultFilters(), fromHere: !onlyEverywhere }));
  const [filterOpen, setFilterOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const run = (value: string, f: SearchFilters = filters) => {
    if (timer.current) clearTimeout(timer.current);
    void controller.run(value, f, currentPath);
  };
  const onChange = (value: string) => {
    setText(value);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => void controller.run(value, filters, currentPath), DEBOUNCE_MS);
  };
  const apply = (f: SearchFilters) => {
    setFilters(f);
    setFilterOpen(false);
    if (text.trim()) run(text, f);
  };
  const toggleCategory = (cat: SearchCategory) => {
    const categories = filters.categories.includes(cat) ? filters.categories.filter((x) => x !== cat) : [...filters.categories, cat];
    const next = { ...filters, categories };
    setFilters(next);
    if (text.trim()) run(text, next);
  };
  const pick = (q: string) => {
    setText(q);
    run(q);
  };

  async function saveCurrent() {
    const q = text.trim();
    if (!q) return;
    const name = await dialogs.prompt({ title: t('saveSearch'), placeholder: t('savedSearchName'), initialValue: q, confirmLabel: t('saveButton') });
    if (name) await useCollections.getState().addSavedSearch({ name, query: q });
  }

  const results = useMemo(() => filterSearchResults(state.raw, vis, filters.includeHidden), [state.raw, vis, filters.includeHidden]);
  const glob = filters.mode !== 'substring' || isGlobQuery(state.query);
  const active = useActiveHost((s) => s.active);
  const rootPath = active?.host.id === host.id ? active.rootPath : undefined;
  const banner = state.truncated ? t('showingFirstNResults', { limit: results.length }) : state.timeBudgetHit ? t('searchTimedOut') : null;

  let body: React.ReactNode;
  if (state.query === '') body = <RecentAndSaved onPick={pick} onAllHosts={() => router.replace('/search')} />;
  else if (state.loading) body = <ResultSkeleton />;
  else if (state.error) body = <ErrorRetry message={t('searchFailed', { error: state.error })} onRetry={() => run(text)} />;
  else if (results.length === 0) body = <Centered message={t('noResultsFor', { query: state.query })} />;
  else {
    body = (
      <FlatList
        data={results}
        keyExtractor={(e) => e.path}
        contentContainerStyle={{ paddingTop: 10, paddingBottom: 18 }}
        renderItem={({ item, index }) => (
          <ResultCardRow index={index} count={results.length}>
            <SearchResultTile entry={item} query={state.query} highlight={!glob} onPress={() => revealInExplorer(router, host, item.path, rootPath)} />
          </ResultCardRow>
        )}
      />
    );
  }

  const count = activeFilterCount(filters);
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar
        context={`${host.label} · Search`}
        right={
          <Pressable onPress={() => setFilterOpen(true)} accessibilityLabel={t('searchFiltersTooltip')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
            <SlidersHorizontal size={22} color={c.onSurfaceVariant} />
            {count > 0 && (
              <View style={{ position: 'absolute', top: 6, right: 6, minWidth: 18, height: 18, paddingHorizontal: 4, borderRadius: Radii.stadium, backgroundColor: c.primary, alignItems: 'center', justifyContent: 'center' }}>
                <Text style={{ fontSize: 12, lineHeight: 14, fontFamily: FontFamily.bold }} color={c.onPrimary}>{String(count)}</Text>
              </View>
            )}
          </Pressable>
        }
      />
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4, paddingLeft: 6, paddingRight: 18, paddingBottom: 8 }}>
        <Pressable onPress={() => router.back()} accessibilityLabel="Back" style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
          <ArrowLeft size={22} color={c.onSurface} />
        </Pressable>
        <SearchField style={{ flex: 1 }}>
          <Search size={18} color={c.onSurfaceVariant} />
          <TextInput
            accessibilityLabel={t('searchHint')}
            autoFocus
            value={text}
            onChangeText={onChange}
            onSubmitEditing={() => run(text)}
            placeholder={t('searchHint')}
            placeholderTextColor={c.onSurfaceVariant}
            returnKeyType="search"
            autoCapitalize="none"
            autoCorrect={false}
            style={{ flex: 1, padding: 0, fontSize: 16, fontFamily: FontFamily.regular, color: c.onSurface }}
          />
          {text !== '' && (
            <>
              <Pressable onPress={saveCurrent} hitSlop={12} accessibilityLabel={t('saveSearch')}>
                <BookmarkPlus size={18} color={c.onSurfaceVariant} />
              </Pressable>
              <Pressable
                onPress={() => {
                  onChange('');
                  run('');
                }}
                hitSlop={12}
                accessibilityLabel="Clear"
              >
                <X size={18} color={c.onSurfaceVariant} />
              </Pressable>
            </>
          )}
        </SearchField>
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center' }}>
        <ScopePill fromHere={filters.fromHere} currentPath={currentPath} onPress={() => setFilterOpen(true)} />
        <View style={{ flex: 1 }}>
          <CategoryChips selected={filters.categories} onToggle={toggleCategory} />
        </View>
      </View>
      {glob && state.query !== '' && <GlobIndicator />}
      {banner && <TruncationBanner message={banner} />}
      <View style={{ flex: 1 }}>{body}</View>
      <SearchFilterSheet visible={filterOpen} filters={filters} currentPath={currentPath} onApply={apply} onClose={() => setFilterOpen(false)} />
    </View>
  );
}

function Centered({ message }: { message: string }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.xl, gap: Spacing.md }}>
      <SearchX size={56} color={c.outline} />
      <Text style={[LumenType.name, { textAlign: 'center' }]}>{message}</Text>
    </View>
  );
}

function ResultSkeleton() {
  const c = useScheme();
  return (
    <View accessibilityRole="progressbar" accessibilityLabel="Searching">
      {Array.from({ length: 8 }, (_, i) => (
        <View key={i} style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm }}>
          <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: c.surfaceContainerHigh }} />
          <View style={{ gap: Spacing.xs }}>
            <View style={{ height: 12, width: i % 2 ? 130 : 180, borderRadius: 6, backgroundColor: c.surfaceContainerHighest }} />
            <View style={{ height: 10, width: 90, borderRadius: 6, backgroundColor: c.surfaceContainerHighest }} />
          </View>
        </View>
      ))}
    </View>
  );
}

/** Empty-query body: saved searches, then recent ones (each removable), or a hint when there are none. */
function RecentAndSaved({ onPick, onAllHosts }: { onPick: (q: string) => void; onAllHosts: () => void }) {
  const c = useScheme();
  const saved = useCollections((s) => s.savedSearches);
  const recent = useCollections((s) => s.recentSearches);
  const removeSaved = useCollections((s) => s.removeSavedSearch);
  const removeRecent = useCollections((s) => s.removeRecentSearch);
  const clearRecent = useCollections((s) => s.clearRecentSearches);
  const allHosts = (
    <View style={{ padding: 18 }}>
      <Button size="lg" kind="neutral" label={t('searchEveryHostButton')} onPress={onAllHosts} />
    </View>
  );
  if (saved.length === 0 && recent.length === 0) {
    return (
      <View style={{ flex: 1 }}>
        <Centered message={t('typeToSearch')} />
        {allHosts}
      </View>
    );
  }
  const label = (text: string, trailing?: React.ReactNode) => (
    <View style={{ flexDirection: 'row', alignItems: 'center', paddingLeft: 22, paddingRight: 10, paddingTop: 14, paddingBottom: 4 }}>
      <Text style={[LumenType.sectionLabel, { flex: 1 }]} muted>{text}</Text>
      {trailing}
    </View>
  );
  return (
    <ScrollView keyboardShouldPersistTaps="handled">
      {saved.length > 0 && label(t('savedSearches'))}
      {saved.map((s) => (
        <Row key={s.name} icon={<Bookmark size={22} color={c.primary} />} tint={c.primary} title={s.name} subtitle={s.query} onPress={() => onPick(s.query)} onRemove={() => void removeSaved(s.name)} />
      ))}
      {recent.length > 0 &&
        label(
          t('recentSearches'),
          <Pressable onPress={() => void clearRecent()} accessibilityLabel={t('clearAllButton')}>
            <Text style={[LumenType.pill, { paddingHorizontal: 8, paddingVertical: 12 }]} color={c.primary}>{t('clearAllButton')}</Text>
          </Pressable>,
        )}
      {recent.map((q) => (
        <Row key={q} icon={<History size={22} color={c.onSurfaceVariant} />} title={q} onPress={() => onPick(q)} onRemove={() => void removeRecent(q)} />
      ))}
      {allHosts}
    </ScrollView>
  );
}

function Row({ icon, tint, title, subtitle, onPress, onRemove }: { icon: React.ReactNode; tint?: string; title: string; subtitle?: string; onPress: () => void; onRemove: () => void }) {
  const c = useScheme();
  return (
    <View style={{ marginHorizontal: 18, marginTop: 10 }}>
      <View style={{ minHeight: 64, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 14, paddingRight: 4, borderRadius: Radii.card, backgroundColor: c.surfaceContainer }}>
        <Pressable onPress={onPress} accessibilityLabel={title} style={{ flex: 1 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 64 }}>
            <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: tint ? mix(tint, c.surfaceContainerHigh, 0.17) : c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>{icon}</View>
            <View style={{ flex: 1 }}>
              <Text numberOfLines={1} style={LumenType.name}>{title}</Text>
              {subtitle ? <Text numberOfLines={1} muted style={LumenType.meta}>{subtitle}</Text> : null}
            </View>
          </View>
        </Pressable>
        <Pressable onPress={onRemove} accessibilityLabel={`Remove ${title}`} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
          <X size={20} color={c.onSurfaceVariant} />
        </Pressable>
      </View>
    </View>
  );
}
