import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { Activity, Globe, Network, Power, RefreshCw, Settings, Shield, ShieldCheck } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { ScrollView, View } from 'react-native';

import { sendWakeOnLan } from '../../../core/native';
import type { Host } from '../../../core/models/host';
import { Button, DuoIcon, Loading, Row, SectionLabel, Text, useDialogs, useToast } from '../../../design/components';
import { LumenType } from '../../../design/lumen';
import { useScheme } from '../../../design/theme';
import { buildRouteRows, connectHeadline, routeSubtitle, STATE_LABEL, summarize, type RouteRow } from '../../../features/hosts/connectLogic';
import { ConnectionDiagnosticsSheet } from '../../../features/hosts/ConnectionDiagnosticsSheet';
import { Nameplate } from '../../../features/hosts/Nameplate';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';
import { useRouteProbe } from '../../../features/hosts/useRouteProbe';
import { t } from '../../../i18n';
import { hostStore } from '../../../services';
import { Spacing } from '../../../design/tokens';

/** Connect: every route this phone can use to reach a computer (local network, Tailscale, direct HTTPS) with what the last check found. */
export default function ConnectRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone context={t('connectMethodsTitle')} />;
  return <Connect host={host} />;
}

/**
 * The connection screen for one computer: its nameplate with a lamp, a flat block saying whether it can be reached (with
 * "Try now" / "Check connection" when it cannot), then one flat row per way in. Checks run through [useRouteProbe].
 */
function Connect({ host }: { host: Host }) {
  const c = useScheme();
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
  const head = connectHeadline(summary, host.label, lastSeen);
  const openSettings = () => router.push({ pathname: '/host/[id]/settings', params: { id: host.id } });
  const recheck = probe.running ? () => {} : probe.rerun;
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
        { value: 'check', label: t('giCheckConnection'), icon: <RefreshCw size={18} color={c.onSurfaceVariant} /> },
        { value: 'details', label: t('connectionDiagnosticsTitle'), icon: <Activity size={18} color={c.onSurfaceVariant} /> },
        ...(host.macAddress ? [{ value: 'wake' as const, label: t('wakeButton'), icon: <Power size={18} color={c.onSurfaceVariant} /> }] : []),
        { value: 'trust', label: t('connectTrustAccess'), icon: <ShieldCheck size={18} color={c.onSurfaceVariant} /> },
        { value: 'settings', label: t('settingsMenuItem'), icon: <Settings size={18} color={c.onSurfaceVariant} /> },
      ],
    });
    if (picked === 'check') probe.rerun();
    else if (picked === 'details') setSheet(true);
    else if (picked === 'wake') await wake();
    else if (picked === 'trust') router.push({ pathname: '/host/[id]/trust', params: { id: host.id } });
    else if (picked === 'settings') openSettings();
  };

  const iconOf = (r: RouteRow) => (r.route === 'lan' ? Network : r.route === 'tailscale' ? Shield : Globe);
  const onRow = (r: RouteRow) => {
    if (r.address !== null) return () => setSheet(true);
    return r.route === 'directHttps' ? openSettings : undefined;
  };
  const stateLabel = (r: RouteRow) => (r.state === 'notConfigured' && r.route === 'tailscale' ? t('connectNotFound') : STATE_LABEL[r.state]);

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <Stack.Screen options={{ headerShown: false }} />
      <Nameplate name={host.label} picture="desktop_computer" off={summary === 'offline'} lamp={head.lamp} phrase={head.phrase} onBack={() => router.back()} onMore={() => void more()} />
      <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingBottom: Spacing.lg }}>
        <View style={{ paddingTop: Spacing.md2, paddingHorizontal: Spacing.md, paddingBottom: 14, borderBottomWidth: 1, borderBottomColor: c.rule }}>
          <Text style={LumenType.name}>{head.title}</Text>
          {head.body ? (
            <Text style={[LumenType.meta, { marginTop: 2 }]} muted>
              {head.body}
            </Text>
          ) : null}
          <View style={{ flexDirection: 'row', gap: Spacing.sm, marginTop: Spacing.md2 }}>
            <Button label={head.retryLabel} kind="neutral" size="s" onPress={recheck} icon={<DuoIcon icon={RefreshCw} size={18} color={c.onSurface} />} />
            <Button label={head.detailsLabel} kind="text" size="s" onPress={() => setSheet(true)} />
          </View>
        </View>

        <View style={{ paddingHorizontal: Spacing.md2, paddingTop: Spacing.md3 }}>
          <SectionLabel title={t('connectMethodsTitle')} />
        </View>
        {rows.map((r) => (
          <Row
            key={r.route}
            lead={<DuoIcon icon={iconOf(r)} size={24} />}
            title={r.title}
            subtitle={routeSubtitle(r)}
            selected={r.state === 'current'}
            onPress={onRow(r)}
            accessibilityLabel={`${r.title}, ${stateLabel(r)}`}
            trailing={
              <Text style={LumenType.meta} muted>
                {stateLabel(r)}
              </Text>
            }
          />
        ))}
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, marginTop: Spacing.md2 }]} muted>
          {t('routePriorityAndSecurityHint')}
        </Text>
      </ScrollView>
      {sheet && <ConnectionDiagnosticsSheet host={host} probe={probe} onClose={() => setSheet(false)} />}
    </View>
  );
}
