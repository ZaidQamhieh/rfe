import { Stack } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, ScrollView, View } from 'react-native';

import type { UsageResult } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { humanizeError } from '../../core/humanizeError';
import type { Host } from '../../core/models/host';
import { ErrorRetry, Loading, Row, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { barFraction, sharePercent } from './usageLogic';

/** What is eating space under one folder: each child with its total size and a bar, largest first. */
export function StorageUsageScreen({ host, path }: { host: Host; path: string }) {
  const c = useScheme();
  const [data, setData] = useState<UsageResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const fetchIt = useCallback(async (): Promise<{ ok: UsageResult } | { error: string }> => {
    try {
      return { ok: await (await clientForHost(host)).usage(path) };
    } catch (e) {
      return { error: humanizeError(e) };
    }
  }, [host, path]);
  const apply = useCallback((r: { ok: UsageResult } | { error: string }) => {
    if ('ok' in r) {
      setData(r.ok);
      setError(null);
    } else setError(r.error);
  }, []);
  useEffect(() => {
    let live = true;
    void fetchIt().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchIt]);

  const retry = () => {
    setError(null);
    void fetchIt().then(apply);
  };
  if (error && !data) {
    return (
      <View style={{ flex: 1, justifyContent: 'center' }}>
        <Stack.Screen options={{ title: t('usageTitle') }} />
        <ErrorRetry message={t('errorLabel', { error })} onRetry={retry} />
      </View>
    );
  }
  if (!data) return <Loading />;
  return (
    <ScrollView
      contentContainerStyle={{ paddingBottom: Spacing.xl }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={async () => {
            setRefreshing(true);
            apply(await fetchIt());
            setRefreshing(false);
          }}
        />
      }
    >
      <Stack.Screen options={{ title: t('usageTitle') }} />
      <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }]} muted>
        {t('usageSummary', { path: data.path || path, size: formatSize(data.totalBytes) })}
      </Text>
      {data.partial ? (
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }]} muted>
          {t('usagePartial')}
        </Text>
      ) : null}
      {data.entries.length === 0 ? (
        <Text style={{ padding: Spacing.md }} muted>
          {t('usageEmpty')}
        </Text>
      ) : (
        data.entries.map((e) => (
          <Row
            key={e.name}
            title={e.name}
            subtitle={`${formatSize(e.bytes)}  ·  ${sharePercent(e.bytes, data.totalBytes)}%`}
            trailing={
              <View style={{ width: 90, height: 8, borderRadius: Radii.control, backgroundColor: c.surfaceContainer, overflow: 'hidden' }}>
                <View style={{ width: `${Math.round(barFraction(e, data.entries) * 100)}%`, height: 8, backgroundColor: c.primary }} />
              </View>
            }
            accessibilityLabel={`${e.name}. ${formatSize(e.bytes)}`}
          />
        ))
      )}
    </ScrollView>
  );
}
