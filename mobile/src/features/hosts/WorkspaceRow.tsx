import { EllipsisVertical, Monitor } from 'lucide-react-native';
import { View } from 'react-native';

import type { Host } from '../../core/models/host';
import { Pressable, Text } from '../../design/components';
import { IconTile, StatePill } from '../../design/components/LumenBits';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { routeStripState } from './hostCardLogic';
import { relativeLabel } from './relative';
import { routeLabel } from './RouteStrip';
import { useHostStatus } from './useHostStatus';

/**
 * One computer in Workspaces. The selected one is its own tinted card (`.selected-workspace`); the others are rows of a
 * shared card (`.permission-row`), [position] telling a row where it sits so the group reads as one rounded card.
 * Status is live: each row pings its host, the subtitle says which route answered, or when it was last seen.
 */
export function WorkspaceRow({
  host,
  selected,
  position,
  onSelect,
  onMore,
  onChanged,
}: {
  host: Host;
  selected: boolean;
  position: 'only' | 'first' | 'middle' | 'last';
  onSelect: () => void;
  onMore: () => void;
  onChanged: () => void;
}) {
  const c = useScheme();
  const roles = useRoles();
  const st = useHostStatus(host, onChanged);
  const route = routeStripState({ online: st.online, checking: st.checking, activeAddress: st.activeAddress }, host);
  const status = st.checking
    ? t('checkingStatus')
    : st.untrusted
      ? t('statusUntrusted')
      : st.online
      ? `${host.note ?? (route.kind === 'active' ? routeLabel(route.route) : t('onlineStatus'))} · connected now`
      : st.lastSeen
        ? t('statusOfflineLastSeen', { relative: relativeLabel(st.lastSeen) })
        : t('offlineStatus');

  const first = position === 'only' || position === 'first';
  const last = position === 'only' || position === 'last';
  const r = LumenSize.cardRadius;
  const shape = selected ? { borderRadius: r } : { borderTopLeftRadius: first ? r : 0, borderTopRightRadius: first ? r : 0, borderBottomLeftRadius: last ? r : 0, borderBottomRightRadius: last ? r : 0 };

  return (
    <View style={[{ backgroundColor: selected ? mix(c.primary, c.surfaceContainer, 0.1) : c.surfaceContainer, paddingLeft: 14, flexDirection: 'row', alignItems: 'center', overflow: 'hidden' }, shape]}>
      {!first && !selected ? <View style={{ position: 'absolute', top: 0, left: 14, right: 14, height: 1, backgroundColor: mix(c.onSurfaceVariant, c.surfaceContainer, 0.16) }} /> : null}
      <Pressable onPress={onSelect} accessibilityLabel={`${host.label}, ${status}${selected ? ', selected' : ''}`} style={{ flex: 1 }}>
        <View style={{ minHeight: 76, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 12 }}>
          {selected ? <IconTile icon={Monitor} color={c.primary} size={LumenSize.appIconTile} radius={12} /> : <Monitor size={26} color={st.online ? roles.route : c.onSurfaceVariant} />}
          <View style={{ flex: 1 }}>
            <Text style={LumenType.rowTitle} numberOfLines={1}>
              {host.label}
            </Text>
            <View style={{ flexDirection: 'row', alignItems: 'center', marginTop: 2 }}>
              <Text style={[LumenType.meta, { flex: 1 }]} muted numberOfLines={2}>
                {status}
              </Text>
            </View>
          </View>
          {selected ? <StatePill label="Selected" tone="safe" /> : <StatePill label="Select" tone="muted" />}
        </View>
      </Pressable>
      <Pressable onPress={onMore} accessibilityLabel={`${t('moreOptionsTooltip')}: ${host.label}`} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        <EllipsisVertical size={22} color={c.onSurfaceVariant} />
      </Pressable>
    </View>
  );
}
