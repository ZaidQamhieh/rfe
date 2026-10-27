import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { Activity, Globe, Monitor, Network, Power, RefreshCw, Settings, Shield, ShieldCheck } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { ScrollView, View } from 'react-native';

import { sendWakeOnLan } from '../../../core/native';
import type { Host } from '../../../core/models/host';
import { GroupedCard, IconTile, Loading, PageHead, SectionLabel, StatePill, Text, useDialogs, useToast } from '../../../design/components';
import { LumenType } from '../../../design/lumen';
import { useRoles, useScheme } from '../../../design/theme';
import { buildRouteRows, routeSubtitle, STATE_LABEL, stateTone, summarize, type RouteRow } from '../../../features/hosts/connectLogic';
import { ConnectionDiagnosticsSheet } from '../../../features/hosts/ConnectionDiagnosticsSheet';
import { LumenFooter, MethodRow, MetaPill, StackTopBar } from '../../../features/hosts/LumenRows';
import { relativeLabel } from '../../../features/hosts/relative';
import { useHostById } from '../../../features/hosts/useHostById';
import { useRouteProbe } from '../../../features/hosts/useRouteProbe';
import { t } from '../../../i18n';
import { hostStore } from '../../../services';

/** Connect: every route this phone can use to reach a computer (local network, Tailscale, direct HTTPS) with what the last check found. */
export default function ConnectRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) {
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ headerShown: false }} />
        <StackTopBar context="Connect" />
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <Text muted>This computer is no longer paired.</Text>
        </View>
      </View>
    );
  }
  return <Connect host={host} />;
}

function Connect({ host }: { host: Host }) {
  const c = useScheme();
  const roles = useRoles();
  const router = useRouter();
  const dialogs = useDialogs();
  const toast = useToast();
  const probe = useRouteProbe(host);
  const [sheet, setSheet] = useState(false);
  const [lastSeen, setLastSeen] = useState<Date | null>(null);
  useEffect(() => {
    let live = true;
    void hostStore.getLastSeen(host.id).then((d) => live && setLastSeen(d));
    return () => {
      live = false;
    };
  }, [host.id, probe.checkedAt]);

  const rows = buildRouteRows(host, probe.results, probe.current);
  const summary = summarize(rows);
  const openSettings = () => router.push({ pathname: '/host/[id]/settings', params: { id: host.id } });
  const wake = async () => {
    if (!host.macAddress) return;
    const sent = await sendWakeOnLan(host.macAddress).catch(() => false);
    if (sent) toast.info(t('wolPacketSent', { hostname: host.label }));
    else toast.error(t('wolPacketFailed'));
  };

  const more = async () => {
    const picked = await dialogs.choose<'check' | 'details' | 'wake' | 'trust' | 'settings'>({
      title: host.label,
      options: [
        { value: 'check', label: 'Check connection', icon: <RefreshCw size={18} color={c.onSurfaceVariant} /> },
        { value: 'details', label: t('connectionDiagnosticsTitle'), icon: <Activity size={18} color={c.onSurfaceVariant} /> },
        ...(host.macAddress ? [{ value: 'wake' as const, label: t('wakeButton'), icon: <Power size={18} color={c.onSurfaceVariant} /> }] : []),
        { value: 'trust', label: 'Trust and access', icon: <ShieldCheck size={18} color={c.onSurfaceVariant} /> },
        { value: 'settings', label: t('settingsMenuItem'), icon: <Settings size={18} color={c.onSurfaceVariant} /> },
      ],
    });
    if (picked === 'check') probe.rerun();
    else if (picked === 'details') setSheet(true);
    else if (picked === 'wake') await wake();
    else if (picked === 'trust') router.push({ pathname: '/host/[id]/trust', params: { id: host.id } });
    else if (picked === 'settings') openSettings();
  };

  const pill =
    summary === 'online' ? <StatePill label="Ready" icon={ShieldCheck} tone="safe" /> : summary === 'checking' ? <StatePill label="Checking" tone="muted" /> : summary === 'attention' ? <StatePill label="Needs attention" tone="warn" /> : <StatePill label="Offline" tone="muted" />;
  const hostSub =
    summary === 'online' ? 'Ready to connect' : summary === 'checking' ? 'Checking routes…' : lastSeen ? `Not reachable now · last seen ${relativeLabel(lastSeen)}` : 'Not reachable right now';

  const iconOf = (r: RouteRow) => (r.route === 'lan' ? Network : r.route === 'tailscale' ? Shield : Globe);
  const toneOf = (r: RouteRow) => (r.route === 'lan' ? roles.route : r.route === 'tailscale' ? roles.transfer : c.primary);
  const onRow = (r: RouteRow) => {
    if (r.address !== null) return () => setSheet(true);
    return r.route === 'directHttps' ? openSettings : undefined;
  };

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StackTopBar context={`${host.label} · Connect`} sub="Connection" right={pill} onMore={more} />
      <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24, gap: 10 }}>
        <View style={{ marginHorizontal: -18 }}>
          <PageHead title="Connect" subtitle="How this phone reaches your computer." />
        </View>
        <GroupedCard>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
            <IconTile icon={Monitor} color={c.primary} size={46} radius={16} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Text style={LumenType.title} numberOfLines={1}>
                {host.label}
              </Text>
              <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant} numberOfLines={2}>
                {hostSub}
              </Text>
            </View>
            <StatePill label={summary === 'online' ? t('onlineStatus') : summary === 'checking' ? t('checkingStatus') : t('offlineStatus')} tone={summary === 'online' ? 'safe' : 'muted'} />
          </View>
        </GroupedCard>

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="Connection methods" />
        </View>
        {rows.map((r) => (
          <MethodRow
            key={r.route}
            icon={iconOf(r)}
            tone={toneOf(r)}
            title={r.title}
            subtitle={routeSubtitle(r)}
            selected={r.state === 'current'}
            onPress={onRow(r)}
            accessibilityLabel={`${r.title}, ${STATE_LABEL[r.state]}`}
            right={<MetaPill label={r.state === 'notConfigured' && r.route === 'tailscale' ? 'Not found' : STATE_LABEL[r.state]} color={stateTone(r.state) === 'safe' ? roles.safe : stateTone(r.state) === 'warn' ? roles.warn : undefined} />}
          />
        ))}
        <Text style={[LumenType.meta, { paddingHorizontal: 4, marginTop: 4 }]} color={c.onSurfaceVariant}>
          {t('routePriorityAndSecurityHint')}
        </Text>
      </ScrollView>
      <LumenFooter
        buttons={[
          { key: 'check', label: 'Re-check', primary: true, onPress: probe.running ? () => {} : probe.rerun, renderIcon: (k) => <RefreshCw size={22} color={k} /> },
          { key: 'details', label: 'Details', primary: false, onPress: () => setSheet(true) },
        ]}
      />
      {sheet && <ConnectionDiagnosticsSheet host={host} probe={probe} onClose={() => setSheet(false)} />}
    </View>
  );
}
