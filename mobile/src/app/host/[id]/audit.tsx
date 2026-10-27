import { Stack, useLocalSearchParams } from 'expo-router';
import { Ban, CircleDot, Link as LinkIcon, Link2Off, LogIn, RefreshCw, ScrollText, Settings2, ShieldAlert, Smartphone, Trash2, UserPlus, type LucideIcon } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, View } from 'react-native';

import { AgentApiError } from '../../../core/api/agentClient';
import type { AuditEntry } from '../../../core/api/models';
import { formatRelative } from '../../../core/format';
import { mix } from '../../../design/color';
import { Button, ErrorRetry, GroupedCard, Text, useToast } from '../../../design/components';
import { LumenSize, LumenType } from '../../../design/lumen';
import { useRoles, useScheme } from '../../../design/theme';
import type { Roles, Scheme } from '../../../design/tokens';
import { appendOlder, olderCursor } from '../../../features/settings/auditPaging';
import { auditLabel } from '../../../features/settings/hostSettingsLogic';
import { humanizeError } from '../../../features/pairing/pairingService';
import { t } from '../../../i18n';
import { clientForHost, hostStore } from '../../../services';

type Tone = (c: Scheme, r: Roles) => string;
const VISUALS: Record<string, { icon: LucideIcon; tint: Tone }> = {
  pair: { icon: Smartphone, tint: (_c, r) => r.safe },
  register: { icon: UserPlus, tint: (_c, r) => r.safe },
  login: { icon: LogIn, tint: (c) => c.primary },
  login_failed: { icon: ShieldAlert, tint: (c) => c.error },
  device_revoked: { icon: Ban, tint: (c) => c.error },
  device_removed: { icon: Trash2, tint: (c) => c.error },
  device_updated: { icon: Settings2, tint: (_c, r) => r.warn },
  share_created: { icon: LinkIcon, tint: (_c, r) => r.route },
  share_revoked: { icon: Link2Off, tint: (_c, r) => r.warn },
  agent_restart: { icon: RefreshCw, tint: (c) => c.primary },
};

/** The host's activity log: pairings, logins, device changes and share links, newest first. Owner-only on the agent. */
export default function Audit() {
  const c = useScheme();
  const toast = useToast();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [entries, setEntries] = useState<AuditEntry[] | null>(null);
  const [error, setError] = useState<{ message: string; adminOnly: boolean } | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  // The id to page from, or null when the newest-first list reached its end.
  const [older, setOlder] = useState<number | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);

  const fetchEntries = useCallback(async (): Promise<{ entries: AuditEntry[] } | { error: { message: string; adminOnly: boolean } }> => {
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === id);
      if (!host) return { error: { message: 'This computer is no longer paired.', adminOnly: false } };
      return { entries: await (await clientForHost(host)).audit() };
    } catch (e) {
      return { error: { message: humanizeError(e), adminOnly: e instanceof AgentApiError && e.statusCode === 403 } };
    }
  }, [id]);
  const apply = useCallback((r: Awaited<ReturnType<typeof fetchEntries>>) => {
    if ('entries' in r) {
      setEntries(r.entries);
      setOlder(olderCursor(r.entries));
      setError(null);
    } else setError(r.error);
  }, []);
  useEffect(() => {
    let live = true;
    void fetchEntries().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchEntries]);

  async function loadOlder() {
    if (older === null || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const host = (await hostStore.listHosts()).find((h) => h.id === id);
      if (!host) return;
      const page = await (await clientForHost(host)).audit({ before: older });
      setEntries((cur) => appendOlder(cur ?? [], page));
      setOlder(olderCursor(page));
    } catch (e) {
      toast.error(humanizeError(e));
    } finally {
      setLoadingOlder(false);
    }
  }

  const notice = (message: string) => (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 12 }}>
      <ScrollText size={56} color={c.onSurfaceVariant} />
      <Text style={[LumenType.name, { textAlign: 'center' }]} muted>{message}</Text>
    </View>
  );

  let body: React.ReactNode;
  if (error) body = error.adminOnly ? notice(t('activityLogAdminOnly')) : <ErrorRetry message={error.message} onRetry={() => void fetchEntries().then(apply)} />;
  else if (entries === null) body = <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><ActivityIndicator color={c.primary} /></View>;
  else if (entries.length === 0) body = notice(t('activityLogEmpty'));
  else {
    body = (
      <FlatList
        data={entries}
        keyExtractor={(e) => String(e.id)}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={async () => {
              setRefreshing(true);
              apply(await fetchEntries());
              setRefreshing(false);
            }}
          />
        }
        contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, paddingBottom: 32, gap: 10 }}
        renderItem={({ item }) => <Row entry={item} />}
        ListFooterComponent={older !== null ? <View style={{ paddingTop: 4 }}><Button size="md" kind="neutral" label={t('loadOlderButton')} onPress={() => void loadOlder()} disabled={loadingOlder} /></View> : null}
      />
    );
  }
  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ title: t('activityLogTitle') }} />
      {body}
    </View>
  );
}

function Row({ entry }: { entry: AuditEntry }) {
  const c = useScheme();
  const roles = useRoles();
  const v = VISUALS[entry.action];
  const Icon = v?.icon ?? CircleDot;
  const tint = v ? v.tint(c, roles) : c.onSurfaceVariant;
  const sub = [entry.actor, entry.target, entry.detail].filter(Boolean).join(' · ');
  return (
    <GroupedCard padded={false} style={{ padding: 12 }}>
      <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 12 }}>
        <View style={{ width: 38, height: 38, borderRadius: LumenSize.tileRadius, backgroundColor: mix(tint, c.surfaceContainerHigh, 0.18), alignItems: 'center', justifyContent: 'center' }}>
          <Icon size={20} color={tint} />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={LumenType.rowTitle}>{auditLabel(entry.action)}</Text>
          {sub ? <Text muted style={[LumenType.meta, { marginTop: 2 }]}>{sub}</Text> : null}
        </View>
        <Text muted style={LumenType.meta}>{formatRelative(entry.at)}</Text>
      </View>
    </GroupedCard>
  );
}
