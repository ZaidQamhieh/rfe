import { Stack, useLocalSearchParams } from 'expo-router';
import { Link as LinkIcon, Unlink } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, View } from 'react-native';

import type { ShareLink } from '../../../core/api/models';
import { ErrorRetry, GroupedCard, GhostBlockButton, Text, useDialogs, useToast } from '../../../design/components';
import { LumenType } from '../../../design/lumen';
import { useScheme } from '../../../design/theme';
import { humanizeError } from '../../../features/pairing/pairingService';
import { basenameOf } from '../../../features/explorer/paths';
import { liveLinks, withoutLink } from '../../../features/share/activeShares';
import { formatRemaining, remainingMs } from '../../../features/share/shareLink';
import { t } from '../../../i18n';
import { clientForHost, hostStore } from '../../../services';

/** The share links on this computer that still work, each with its time left and a Revoke. */
export default function ActiveShares() {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [links, setLinks] = useState<ShareLink[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [, tick] = useState(0);

  const fetchLinks = useCallback(async (): Promise<{ links: ShareLink[] } | { error: string }> => {
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === id);
      if (!host) return { error: 'This computer is no longer paired.' };
      return { links: liveLinks(await (await clientForHost(host)).listShareLinks()) };
    } catch (e) {
      return { error: humanizeError(e) };
    }
  }, [id]);
  const apply = useCallback((r: Awaited<ReturnType<typeof fetchLinks>>) => {
    if ('links' in r) {
      setLinks(r.links);
      setError(null);
    } else setError(r.error);
  }, []);
  const load = useCallback(() => fetchLinks().then(apply), [fetchLinks, apply]);
  useEffect(() => {
    let live = true;
    void fetchLinks().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [fetchLinks, apply]);
  // Keeps the countdowns moving and lets a link that just expired drop off.
  useEffect(() => {
    const timer = setInterval(() => {
      tick((n) => n + 1);
      setLinks((cur) => (cur ? liveLinks(cur) : cur));
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  async function revoke(link: ShareLink) {
    const ok = await dialogs.confirm({ title: t('shareLinkRevokeButton'), description: t('revokeShareLinkBody', { name: basenameOf(link.path) }), confirmLabel: t('shareLinkRevokeButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === id);
      if (!host) return;
      await (await clientForHost(host)).revokeShareLink(link.tokenHash);
      setLinks((cur) => (cur ? withoutLink(cur, link.tokenHash) : cur));
      toast.info(t('shareLinkRevoked'));
    } catch (e) {
      toast.error(t('shareLinkFailed', { error: humanizeError(e) }));
    }
  }

  let body: React.ReactNode;
  if (error && links === null) body = <ErrorRetry message={error} onRetry={() => void load()} />;
  else if (links === null) body = <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><ActivityIndicator color={c.primary} /></View>;
  else if (links.length === 0) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 12 }}>
        <LinkIcon size={56} color={c.onSurfaceVariant} />
        <Text style={[LumenType.name, { textAlign: 'center' }]} muted>{t('noActiveShareLinks')}</Text>
      </View>
    );
  } else {
    body = (
      <FlatList
        data={links}
        keyExtractor={(l) => l.tokenHash}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
        contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, paddingBottom: 32, gap: 10 }}
        renderItem={({ item }) => (
          <GroupedCard padded={false} style={{ padding: 12, gap: 8 }}>
            <Text style={LumenType.rowTitle} numberOfLines={1}>{basenameOf(item.path)}</Text>
            <Text muted style={LumenType.meta} numberOfLines={1} ellipsizeMode="middle">{item.path}</Text>
            <Text muted style={LumenType.meta}>{t('shareLinkExpiresIn', { time: formatRemaining(remainingMs(item.expiresAt)) })}</Text>
            <GhostBlockButton label={t('shareLinkRevokeButton')} icon={<Unlink size={16} color={c.error} />} onPress={() => void revoke(item)} />
          </GroupedCard>
        )}
      />
    );
  }
  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ title: t('activeShareLinksTitle') }} />
      {body}
    </View>
  );
}
