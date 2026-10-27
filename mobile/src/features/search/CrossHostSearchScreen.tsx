import { useRouter } from 'expo-router';
import { ArrowLeft, Monitor, Search } from 'lucide-react-native';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, TextInput, View } from 'react-native';
import { useStore } from 'zustand';

import { formatSize } from '../../core/format';
import type { Host } from '../../core/models/host';
import { EmptyState, Pressable, Text, TopBar } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { revealInExplorer } from '../explorer/reveal';
import { createCrossHostSearch, groupByHost, MIN_CROSS_QUERY } from './crossHostSearch';
import { ResultCardRow, ResultIconTile, SearchField } from './SearchParts';

const DEBOUNCE_MS = 400;

/** Searches every paired computer at once; hits stream in per host and unreachable hosts are listed at the bottom. */
export function CrossHostSearchScreen({ hosts }: { hosts: Host[] }) {
  const c = useScheme();
  const router = useRouter();
  const search = useMemo(() => createCrossHostSearch({ hosts, getClient: (h) => clientForHost(h) }), [hosts]);
  const state = useStore(search);
  const [text, setText] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  const onChange = (v: string) => {
    setText(v);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => void search.run(v), DEBOUNCE_MS);
  };

  const groups = groupByHost(state.results);
  const failedHosts = hosts.filter((h) => state.failed.includes(h.id));
  const short = text.trim().length < MIN_CROSS_QUERY;

  let body: React.ReactNode;
  if (state.searching && state.results.length === 0) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 }}>
        <ActivityIndicator />
        <Text>{t('crossHostSearching', { count: hosts.length })}</Text>
      </View>
    );
  } else if (short) body = <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><Text>{t('crossHostTypeToSearch')}</Text></View>;
  else if (state.results.length === 0) {
    body = (
      <View style={{ flex: 1 }}>
        <EmptyState kind="noMatches" message={t('crossHostNoResults')} />
        {failedHosts.length > 0 && <Text style={{ textAlign: 'center', fontSize: 12.5, paddingBottom: Spacing.lg }} color={c.error}>{t('crossHostUnreachableCount', { count: failedHosts.length })}</Text>}
      </View>
    );
  } else {
    body = (
      <ScrollView contentContainerStyle={{ paddingTop: 10, paddingBottom: 24 }}>
        <View style={{ paddingHorizontal: 18, flexDirection: 'row' }}>
          <View style={{ paddingHorizontal: 12, paddingVertical: 8, borderRadius: Radii.stadium, backgroundColor: mix(c.primary, c.surfaceContainer, 0.2) }}>
            <Text style={LumenType.pill} color={c.primary}>{t('crossHostSearchingCount', { count: groups.length })}</Text>
          </View>
        </View>
        {groups.map((g) => (
          <View key={g.host.id}>
            <Text style={[LumenType.sectionLabel, { paddingHorizontal: 22, paddingTop: 14, paddingBottom: 8 }]} muted>{g.host.label}</Text>
            {g.entries.map((entry, i) => (
              <ResultCardRow key={entry.path} index={i} count={g.entries.length}>
                <Pressable onPress={() => revealInExplorer(router, g.host, entry.path)} accessibilityLabel={`${entry.name} on ${g.host.label}`}>
                  <View style={{ minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 }}>
                    <ResultIconTile entry={entry} />
                    <View style={{ flex: 1, gap: 2 }}>
                      <Text numberOfLines={1} style={LumenType.name}>{entry.name}</Text>
                      <Text numberOfLines={1} muted style={[LumenType.meta, { fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 18 }]}>{entry.path}</Text>
                    </View>
                    {entry.size != null && <Text muted style={LumenType.meta}>{formatSize(entry.size)}</Text>}
                  </View>
                </Pressable>
              </ResultCardRow>
            ))}
          </View>
        ))}
        {failedHosts.map((h) => (
          <View key={h.id} style={{ marginHorizontal: 18, marginTop: 10, flexDirection: 'row', alignItems: 'center', gap: 10, padding: 14, borderRadius: LumenSize.cardRadius, backgroundColor: c.surfaceContainer, opacity: 0.7 }}>
            <Monitor size={18} color={c.onSurfaceVariant} />
            <Text style={[LumenType.meta, { flex: 1 }]} muted>{t('crossHostOffline', { host: h.label })}</Text>
          </View>
        ))}
      </ScrollView>
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar context={t('crossHostSearchTitle')} />
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4, paddingLeft: 6, paddingRight: 18, paddingBottom: 8 }}>
        <Pressable onPress={() => router.back()} accessibilityLabel="Back" style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
          <ArrowLeft size={22} color={c.onSurface} />
        </Pressable>
        <SearchField style={{ flex: 1 }}>
          <Search size={18} color={c.onSurfaceVariant} />
          <TextInput
            accessibilityLabel={t('crossHostSearchHint')}
            autoFocus
            value={text}
            onChangeText={onChange}
            placeholder={t('crossHostSearchHint')}
            placeholderTextColor={c.onSurfaceVariant}
            returnKeyType="search"
            autoCapitalize="none"
            autoCorrect={false}
            style={{ flex: 1, padding: 0, fontSize: 16, fontFamily: FontFamily.regular, color: c.onSurface }}
          />
        </SearchField>
      </View>
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}
