import { Network, Smartphone } from 'lucide-react-native';
import { View } from 'react-native';

import type { HostRoute } from '../../core/models/host';
import { Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import type { RouteStripState } from './hostCardLogic';

export const routeLabel = (r: HostRoute): string =>
  r === 'lan' ? t('networkLan') : r === 'tailscale' ? t('networkTailscale') : r === 'directHttps' ? t('networkInternet') : 'Custom route';

/** `.route`: Phone ---- LAN . active. The label is the route that really answered; offline or checking it says so and the line goes neutral. */
export function RouteStrip({ state }: { state: RouteStripState }) {
  const c = useScheme();
  const roles = useRoles();
  const active = state.kind === 'active';
  const tone = active ? roles.route : c.onSurfaceVariant;
  const label = state.kind === 'active' ? `${routeLabel(state.route)} · active` : state.kind === 'checking' ? t('checkingStatus') : t('offlineStatus');
  return (
    <View
      accessible
      accessibilityLabel={`Phone to computer: ${label}`}
      style={{ height: 54, borderRadius: 16, backgroundColor: c.surfaceContainer, flexDirection: 'row', alignItems: 'center', paddingHorizontal: 14 }}
    >
      <View style={{ flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6 }}>
        <Smartphone size={16} color={c.onSurfaceVariant} />
        <Text style={LumenType.caption} color={c.onSurfaceVariant} numberOfLines={1}>
          Phone
        </Text>
      </View>
      <View style={{ width: 38, height: 4, borderRadius: 2, backgroundColor: active ? roles.route : c.outlineVariant, marginHorizontal: 6 }} />
      <View style={{ flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6 }}>
        <Network size={16} color={tone} />
        <Text style={LumenType.caption} color={tone} numberOfLines={1} adjustsFontSizeToFit minimumFontScale={0.85}>
          {label}
        </Text>
      </View>
    </View>
  );
}
