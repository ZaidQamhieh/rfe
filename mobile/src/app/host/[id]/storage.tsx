import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { ArrowRight } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ScrollView, View } from 'react-native';

import type { Drive } from '../../../core/api/models';
import { formatSize } from '../../../core/format';
import { aggregateUsage } from '../../../core/storage/usage';
import { Button, EmptyState, ErrorRetry, GroupedCard, Loading, Text } from '../../../design/components';
import { LumenType } from '../../../design/lumen';
import { useRoles, useScheme } from '../../../design/theme';
import { FontFamily } from '../../../design/tokens';
import { t } from '../../../i18n';
import { humanizeError } from '../../../features/pairing/pairingService';
import { useHostById } from '../../../features/hosts/useHostById';
import { ringSegments } from '../../../features/storage/storageLogic';
import { UsageRing } from '../../../features/storage/UsageRing';
import { clientForHost } from '../../../services';

/** Aggregate disk usage for one computer: a ring over its drives, a per-drive list, and the way into the by-type map. */
export default function StorageInsights() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  const c = useScheme();
  const roles = useRoles();
  const palette = [c.primary, roles.route, roles.warn];
  const router = useRouter();
  const [drives, setDrives] = useState<Drive[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!host) return null;
    try {
      return { ok: true as const, drives: await (await clientForHost(host)).drives() };
    } catch (e) {
      return { ok: false as const, error: humanizeError(e) };
    }
  }, [host]);
  const apply = useCallback((r: Awaited<ReturnType<typeof load>>) => {
    if (!r) return;
    if (r.ok) {
      setDrives(r.drives);
      setError(null);
    } else setError(r.error);
  }, []);
  useEffect(() => {
    let live = true;
    void load().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [load, apply]);

  const usage = drives ? aggregateUsage(drives) : null;
  const label = host ? host.label || host.address : '';
  const title = <Stack.Screen options={{ title: t('storageInsightsTitle') }} />;

  if (host === undefined) return <Loading />;
  if (host === null) return <EmptyState message={t('hostNoLongerPaired')} />;
  if (error && !drives) return <View style={{ flex: 1 }}>{title}<ErrorRetry message={t('couldNotLoadStorage', { error })} onRetry={() => void load().then(apply)} /></View>;
  if (!drives) return <View style={{ flex: 1 }}>{title}<Loading /></View>;
  if (!usage) return <View style={{ flex: 1 }}>{title}<EmptyState message={t('emptyFolderMessage')} /></View>;

  const segments = ringSegments(drives);
  const colors = segments.map((s, i) => (s.key === 'free' ? c.surfaceContainerHighest : palette[i % palette.length]));
  const used = usage.totalBytes - usage.freeBytes;
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, gap: 16, paddingBottom: 32 }}>
      {title}
      <Text style={[LumenType.meta, { textAlign: 'center' }]} muted>{t('hostStorageSubtitle', { hostLabel: label, used: formatSize(used), total: formatSize(usage.totalBytes) })}</Text>
      <View style={{ alignItems: 'center', paddingVertical: 12 }}>
        <UsageRing segments={segments.map((s, i) => ({ color: colors[i], fraction: s.fraction }))} percent={Math.round(usage.usedFraction * 100)} usedLabel={formatSize(used)} />
      </View>
      <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 4 }}>
        {segments.map((s, i) => (
          <View key={s.key} style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 14, minHeight: 52 }}>
            <View style={{ width: 14, height: 14, borderRadius: 4, backgroundColor: colors[i] }} />
            <Text numberOfLines={1} style={[LumenType.name, { flex: 1 }]}>{s.drive ? s.drive.label || s.drive.path : t('freeSpaceLabel')}</Text>
            <Text muted style={[LumenType.meta, { fontFamily: FontFamily.mono }]}>{formatSize(s.drive ? s.drive.totalBytes! - Math.min(s.drive.freeBytes!, s.drive.totalBytes!) : usage.freeBytes)}</Text>
          </View>
        ))}
      </GroupedCard>
      <Button size="lg" kind="neutral" label={t('openStorageTypeMapButton')} renderIcon={(fg) => <ArrowRight size={18} color={fg} />} onPress={() => router.push({ pathname: '/host/[id]/types', params: { id: host.id } })} />
    </ScrollView>
  );
}
