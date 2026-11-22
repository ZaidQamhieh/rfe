import { useRouter } from 'expo-router';
import { useMemo, useState } from 'react';
import { ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useStore } from 'zustand';

import type { Host } from '../../core/models/host';
import { useDebouncedCall } from '../../core/useDebouncedCall';
import { Emoji, Text } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { revealInExplorer } from '../explorer/reveal';
import { createCrossHostSearch, groupByHost, MIN_CROSS_QUERY } from './crossHostSearch';
import { NoResults, ResultRow, ResultSkeleton, ScopeChips, SearchBarRow, SectionHeading, type ScopeChipSpec } from './SearchParts';
import { Spacing } from '../../design/tokens';

const DEBOUNCE_MS = 400;

/** Searches every paired computer at once; hits stream in per computer under its name and unreachable computers are listed at the bottom. */
export function CrossHostSearchScreen({ hosts }: { hosts: Host[] }) {
  const c = useScheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const active = useActiveHost((s) => s.active);
  const search = useMemo(() => createCrossHostSearch({ hosts, getClient: (h) => clientForHost(h) }), [hosts]);
  const state = useStore(search);
  const [text, setText] = useState('');
  const debounce = useDebouncedCall(DEBOUNCE_MS);
  const onChange = (v: string) => {
    setText(v);
    debounce.schedule(() => void search.run(v));
  };

  const query = text.trim();
  const groups = groupByHost(state.results);
  const failedHosts = hosts.filter((h) => state.failed.includes(h.id));
  const short = query.length < MIN_CROSS_QUERY;
  const current = active ? hosts.find((h) => h.id === active.host.id) : undefined;

  const chips: ScopeChipSpec[] = [
    ...(current ? [{ key: 'this', label: t('searchScopeThisComputer'), onPress: () => router.replace({ pathname: '/host/[id]/search', params: { id: current.id, path: '' } }) }] : []),
    { key: 'all', label: t('searchScopeAllComputers'), selected: true, onPress: () => undefined },
  ];

  let body: React.ReactNode;
  if (short) {
    body = (
      <View style={{ paddingTop: Spacing.lg, paddingHorizontal: Spacing.lg }}>
        <Text style={[LumenType.name, { lineHeight: 24 }]} muted>{t('searchShortQuery', { count: MIN_CROSS_QUERY })}</Text>
      </View>
    );
  } else if (state.searching && state.results.length === 0) body = <ResultSkeleton />;
  else if (state.results.length === 0) {
    body = (
      <View>
        <NoResults title={t('searchNothingEverywhere', { query })} hint={t('searchNothingEverywhereHint')} />
        {failedHosts.length > 0 && <Text style={[LumenType.meta, { paddingHorizontal: Spacing.lg, paddingTop: Spacing.md }]} color={c.stop}>{t('crossHostUnreachableCount', { count: failedHosts.length })}</Text>}
      </View>
    );
  } else {
    body = (
      <ScrollView contentContainerStyle={{ paddingBottom: Spacing.lg }} keyboardShouldPersistTaps="handled">
        {groups.map((g) => (
          <View key={g.host.id}>
            <SectionHeading text={g.host.label} />
            {g.entries.map((entry) => (
              <ResultRow key={entry.path} entry={entry} query={query} highlight onPress={() => revealInExplorer(router, g.host, entry.path)} label={t('searchResultOnHost', { name: entry.name, host: g.host.label })} />
            ))}
          </View>
        ))}
        {failedHosts.map((h) => (
          <View key={h.id} style={{ minHeight: LumenSize.rowMinHeight, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.md }}>
            <View style={{ width: 40, alignItems: 'center' }}>
              <Emoji name="desktop_computer" size={32} off />
            </View>
            <Text style={[LumenType.meta, { flex: 1 }]} muted>{t('crossHostOffline', { host: h.label })}</Text>
          </View>
        ))}
      </ScrollView>
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface, paddingTop: insets.top }}>
      <SearchBarRow value={text} onChange={onChange} placeholder={t('crossHostSearchHint')} onClear={() => onChange('')} />
      <ScopeChips chips={chips} />
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}
