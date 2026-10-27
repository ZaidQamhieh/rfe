import { Lock, Monitor, TriangleAlert } from 'lucide-react-native';
import { View } from 'react-native';

import { mix } from '../../design/color';
import { Pressable, Text } from '../../design/components';
import { StatePill } from '../../design/components/LumenBits';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';

/**
 * `.orbit` + `.host` + `.subtitle`: a ring around the host glyph, the host name at 36 sp, one status line and the inline
 * "Switch workspace" link. [status] is real (Connected securely / Checking / last seen), never a made-up place name.
 */
export function HostHero({ name, status, readOnly, lowDisk, onSwitch }: { name: string; status: string; readOnly: boolean; lowDisk: boolean; onSwitch: () => void }) {
  const c = useScheme();
  return (
    <View style={{ alignItems: 'center' }}>
      <View
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={{ width: 108, height: 108, marginTop: 18, marginBottom: 8, borderRadius: 54, borderWidth: 2, borderColor: mix(c.primary, c.surface, 0.65), alignItems: 'center', justifyContent: 'center' }}
      >
        <View style={{ width: 64, height: 64, borderRadius: 32, backgroundColor: mix(c.primary, c.surface, 0.17), alignItems: 'center', justifyContent: 'center' }}>
          <Monitor size={32} color={c.primary} />
        </View>
      </View>
      <Text style={[LumenType.hostName, { textAlign: 'center', alignSelf: 'stretch' }]} numberOfLines={1} adjustsFontSizeToFit minimumFontScale={0.6} accessibilityRole="header">
        {name}
      </Text>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'center', columnGap: 8, rowGap: 4, marginTop: 4, marginBottom: 14 }}>
        <Text style={LumenType.caption} color={c.onSurfaceVariant} numberOfLines={2}>
          {status}
        </Text>
        <Pressable onPress={onSwitch} accessibilityLabel="Switch workspace" hitSlop={{ top: 14, bottom: 14, left: 8, right: 8 }}>
          <View style={{ paddingVertical: 4, paddingHorizontal: 8, borderRadius: 10, backgroundColor: mix(c.primary, c.surface, 0.14) }}>
            <Text style={{ fontSize: 12, lineHeight: 16, fontFamily: LumenType.pill.fontFamily }} color={c.primary} numberOfLines={1}>
              Switch workspace
            </Text>
          </View>
        </Pressable>
      </View>
      {readOnly || lowDisk ? (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'center', gap: 8, marginBottom: 14 }}>
          {readOnly ? <StatePill label="Read-only" tone="muted" icon={Lock} /> : null}
          {lowDisk ? <StatePill label={t('lowDiskWarning')} tone="warn" icon={TriangleAlert} /> : null}
        </View>
      ) : null}
    </View>
  );
}
