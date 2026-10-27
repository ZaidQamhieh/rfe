import { Check, Gauge, RefreshCw, Route, Shield, X, type LucideIcon } from 'lucide-react-native';
import { ActivityIndicator, ScrollView, View } from 'react-native';

import type { Host, HostRoute } from '../../core/models/host';
import { BottomSheet, Button, SheetHead, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { FontFamily } from '../../design/tokens';
import { t } from '../../i18n';
import type { ProbeResult } from './diagnostics';
import { useRouteProbe, type RouteProbe } from './useRouteProbe';

const ROUTE_NAME: Record<HostRoute, () => string> = {
  lan: () => t('routeLanName'),
  tailscale: () => t('routeTailscaleName'),
  directHttps: () => t('routeInternetName'),
  custom: () => t('routeCustomName'),
};

/**
 * Checks each route to a host separately: reachability, pinned TLS, whether this device is let in, latency and the path
 * used, with a hint for the first thing that failed. Nothing is sent to a host whose certificate does not match the pin.
 * Pass [probe] to show a result the caller already has (the Connect screen does); without it the sheet probes on open.
 */
export function ConnectionDiagnosticsSheet({ host, onClose, probe }: { host: Host; onClose: () => void; probe?: RouteProbe }) {
  const own = useRouteProbe(host, probe === undefined);
  const p = probe ?? own;
  const results = p.results;

  return (
    <BottomSheet visible onClose={onClose}>
      <SheetHead title={t('connectionDiagnosticsTitle')} subtitle={`${host.label} · ${host.address}`} />
      <ScrollView contentContainerStyle={{ paddingHorizontal: 16, paddingBottom: 20, gap: 14 }}>
        {results === null ? (
          <View style={{ paddingVertical: 24, alignItems: 'center' }}>
            <ActivityIndicator />
          </View>
        ) : (
          results.map((r) => (
            <View key={r.address}>
              {results.length > 1 && (
                <Text muted style={[LumenType.sectionLabel, { paddingHorizontal: 4, paddingBottom: 8 }]}>
                  {ROUTE_NAME[r.route]()}
                </Text>
              )}
              <Checks result={r} />
            </View>
          ))
        )}
        <Button kind="neutral" label={t('runAgainButton')} busy={results === null} onPress={p.rerun} renderIcon={(k) => <RefreshCw size={20} color={k} />} />
      </ScrollView>
    </BottomSheet>
  );
}

function Checks({ result: r }: { result: ProbeResult }) {
  const c = useScheme();
  const roles = useRoles();
  const grey = c.onSurfaceVariant;
  const good = roles.safe;
  const bad = c.error;
  const reachable = r.health !== undefined;
  const mismatch = r.failure === 'pinMismatch';
  const failureBadge = { none: t('diagOkBadge'), missingPin: t('diagPinRequiredBadge'), pinMismatch: t('diagMismatchBadge'), dns: t('probeDnsFailedBadge'), unreachable: t('probeNoResponseBadge'), other: t('probeError') }[r.failure];
  const authBadge = { accepted: t('diagAuthAcceptedBadge'), denied: t('diagAuthDeniedBadge'), notChecked: t('diagAuthUnknownBadge') }[r.auth];
  const authTint = r.auth === 'accepted' ? good : r.auth === 'denied' ? bad : grey;
  const hint = probeHint(r);
  const pathBadge = { lan: t('diagLanDirect'), tailscale: t('networkTailscale'), directHttps: t('routeInternetName'), custom: t('routeCustomName') }[r.route];
  return (
    <View style={{ backgroundColor: c.surfaceContainerHigh, borderRadius: LumenSize.cardRadius, paddingHorizontal: 12, paddingVertical: 4 }}>
      <Row icon={reachable ? Check : X} tint={reachable ? good : mismatch ? bad : grey} title={t('diagHostReachable')} subtitle={r.address} badge={failureBadge} />
      <Row
        icon={mismatch ? X : reachable ? Check : Shield}
        tint={mismatch ? bad : reachable ? good : grey}
        title={t('diagTlsPinned')}
        badge={mismatch ? t('diagMismatchBadge') : reachable ? t('diagPinnedBadge') : r.failure === 'missingPin' ? t('diagPinRequiredBadge') : t('diagUnknownBadge')}
      />
      <Row icon={r.auth === 'accepted' ? Check : r.auth === 'denied' ? X : Shield} tint={authTint} title={t('diagAuthentication')} badge={authBadge} />
      <Row icon={Gauge} tint={reachable ? good : grey} title={t('diagLatency')} badge={reachable ? t('probeLatencyMs', { ms: r.latencyMs ?? 0 }) : '-'} mono />
      <Row icon={Route} tint={c.primary} title={t('diagPath')} badge={pathBadge} badgeTint={reachable ? c.primary : grey} />
      {hint ? (
        <Text style={[LumenType.meta, { paddingHorizontal: 4, paddingTop: 4, paddingBottom: 10 }]} color={c.onSurfaceVariant}>
          {hint}
        </Text>
      ) : null}
    </View>
  );
}

/** The hint for the first thing that failed on a route, or null when it is healthy. */
function probeHint(r: ProbeResult): string | null {
  if (r.failure === 'pinMismatch') return t('probePinMismatchHint');
  if (r.failure === 'missingPin') return t('probeMissingPinHint');
  if (r.failure === 'dns') return t('probeDnsHint');
  if (r.failure === 'unreachable') return t('probeReachabilityHint');
  if (r.failure === 'other') return t('probeGenericHint');
  if (r.auth === 'denied') return t('probeAuthRejectedHint');
  return null;
}

function Row({ icon: Icon, tint, title, subtitle, badge, badgeTint, mono }: { icon: LucideIcon; tint: string; title: string; subtitle?: string; badge: string; badgeTint?: string; mono?: boolean }) {
  const c = useScheme();
  const bt = badgeTint ?? tint;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 56, paddingVertical: 8 }}>
      <View style={{ width: 40, height: 40, borderRadius: 14, backgroundColor: mix(tint, c.surfaceContainerHigh, 0.18), alignItems: 'center', justifyContent: 'center' }}>
        <Icon size={20} color={tint} />
      </View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={LumenType.rowTitle} numberOfLines={1}>
          {title}
        </Text>
        {subtitle ? (
          <Text numberOfLines={1} style={{ fontSize: 14, lineHeight: 19, fontFamily: FontFamily.mono }} color={c.onSurfaceVariant}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      <View style={{ flexShrink: 1, maxWidth: '42%', paddingHorizontal: 12, paddingVertical: 6, borderRadius: LumenSize.pillRadius, backgroundColor: mix(bt, c.surfaceContainerHigh, 0.16) }}>
        <Text numberOfLines={1} style={[LumenType.pill, mono && { fontFamily: FontFamily.monoMedium }]} color={bt}>
          {badge}
        </Text>
      </View>
    </View>
  );
}
