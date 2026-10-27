import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { History } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, RefreshControl, ScrollView, View } from 'react-native';

import type { Entry, SearchResult } from '../core/api/models';
import { formatRelative } from '../core/format';
import { ErrorRetry, Pressable, SectionLabel, Text, useToast } from '../design/components';
import { LumenType } from '../design/lumen';
import { useScheme } from '../design/theme';
import { Spacing } from '../design/tokens';
import { EntryLeading, useIconChipBg } from '../features/explorer/EntryIcon';
import { revealInExplorer } from '../features/explorer/reveal';
import { humanizeError } from '../features/pairing/pairingService';
import { groupRecent } from '../features/recent/recentBuckets';
import { recentOptions } from '../features/recent/recentRoot';
import { ResultCardRow, TruncationBanner } from '../features/search/SearchParts';
import { t } from '../i18n';
import { clientForHost } from '../services';
import { useActiveHost } from '../state/activeHost';

/** The most recently modified files on the host, grouped Today / Yesterday / Earlier. Tapping one shows its folder in Files. */
export default function Recent() {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const active = useActiveHost((s) => s.active);
  // Opened from a folder's menu: only files under it.
  const { root } = useLocalSearchParams<{ root?: string }>();
  const [result, setResult] = useState<SearchResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const host = active?.host;

  const fetchRecent = useCallback(async (): Promise<{ result: SearchResult } | { error: string }> => {
    if (!host) return { result: { entries: [], truncated: false, timeBudgetHit: false } };
    try {
      return { result: await (await clientForHost(host)).recent(recentOptions(root)) };
    } catch (e) {
      return { error: humanizeError(e) };
    }
  }, [host, root]);
  const apply = useCallback((r: { result: SearchResult } | { error: string }) => {
    if ('result' in r) {
      setResult(r.result);
      setError(null);
    } else setError(r.error);
    setLoading(false);
  }, []);
  const hasResult = result !== null;
  const reload = useCallback(async () => {
    setLoading(true);
    const r = await fetchRecent();
    apply(r);
    // With a list on screen the full-page error is not shown, so a failed pull-to-refresh would look like "nothing new".
    if ('error' in r && hasResult) toast.error(r.error);
  }, [apply, fetchRecent, hasResult, toast]);
  useEffect(() => {
    let live = true;
    void fetchRecent().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchRecent]);

  if (!active) return null;
  let body: React.ReactNode;
  if (loading && result === null) body = <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><ActivityIndicator /></View>;
  else if (error && result === null) body = <ErrorRetry message={error} onRetry={reload} />;
  else if (!result || result.entries.length === 0) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, padding: Spacing.lg }}>
        <History size={64} color={c.outline} />
        <Text style={LumenType.title}>{t('recentIsEmpty')}</Text>
        <Text style={LumenType.meta} muted>{t('recentEmptySubtitle')}</Text>
      </View>
    );
  } else {
    const groups = groupRecent(result.entries, new Date());
    body = (
      <View style={{ flex: 1 }}>
        {result.timeBudgetHit && <TruncationBanner message={t('recentTimedOut')} />}
        <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={reload} />} contentContainerStyle={{ paddingVertical: 10 }}>
          {groups.map((g) => (
            <View key={g.label} style={{ marginBottom: 14 }}>
              <View style={{ paddingHorizontal: 22 }}>
                <SectionLabel title={g.label} />
              </View>
              {g.entries.map((entry, i) => (
                <ResultCardRow key={entry.path} index={i} count={g.entries.length}>
                  <RecentRow entry={entry} hostLabel={active.host.label} onPress={() => revealInExplorer(router, active.host, entry.path, active.rootPath)} />
                </ResultCardRow>
              ))}
            </View>
          ))}
        </ScrollView>
      </View>
    );
  }

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ title: t('recentTitle') }} />
      {body}
    </View>
  );
}

function RecentRow({ entry, hostLabel, onPress }: { entry: Entry; hostLabel: string; onPress: () => void }) {
  const chip = useIconChipBg(entry);
  const sub = entry.modified ? `${hostLabel}  ·  modified ${formatRelative(new Date(entry.modified))}` : hostLabel;
  return (
    <Pressable onPress={onPress} accessibilityLabel={entry.name}>
      <View style={{ minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 }}>
        <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: chip, alignItems: 'center', justifyContent: 'center' }}>
          <EntryLeading entry={entry} size={22} />
        </View>
        <View style={{ flex: 1 }}>
          <Text numberOfLines={1} style={LumenType.name}>{entry.name}</Text>
          <Text numberOfLines={1} muted style={LumenType.meta}>{sub}</Text>
        </View>
      </View>
    </Pressable>
  );
}
