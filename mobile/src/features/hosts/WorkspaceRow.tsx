import { Check, EllipsisVertical } from 'lucide-react-native';
import { View } from 'react-native';

import type { Host } from '../../core/models/host';
import { DuoIcon, Emoji, Pressable, Row } from '../../design/components';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { switcherRowState } from './switcherLogic';
import { useHostStatus } from './useHostStatus';
import { Spacing } from '../../design/tokens';
import { useAppDensity } from '../../state/density';

/**
 * One computer in the Manage list, drawn like a row of the computer switcher: its picture (grey when asleep or
 * unreachable), its name, a plain state phrase (with the note the owner gave it in front) and, for the selected one, a
 * check mark; the dots at the end open its menu. Status is live: each row pings its own computer.
 */
export function WorkspaceRow({ host, selected, onSelect, onMore, onChanged }: { host: Host; selected: boolean; onSelect: () => void; onMore: () => void; onChanged: () => void }) {
  const c = useScheme();
  const st = useHostStatus(host, onChanged);
  const density = useAppDensity();
  const state = switcherRowState(st, host);
  const subtitle = host.note ? `${host.note} · ${state.phrase}` : state.phrase;
  return (
    <Row
      density={density}
      selected={selected}
      lead={<Emoji name="desktop_computer" size={28} off={state.off} />}
      title={host.label}
      subtitle={subtitle}
      onPress={onSelect}
      accessibilityLabel={`${host.label}, ${subtitle}${selected ? t('selectedSuffix') : ''}`}
      trailing={
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs }}>
          {selected ? <DuoIcon icon={Check} size={20} color={c.primary} /> : null}
          <Pressable onPress={onMore} accessibilityLabel={`${t('moreOptionsTooltip')}: ${host.label}`} pressedScale={0.92} style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
            <EllipsisVertical size={22} color={c.onSurfaceVariant} />
          </Pressable>
        </View>
      }
    />
  );
}
