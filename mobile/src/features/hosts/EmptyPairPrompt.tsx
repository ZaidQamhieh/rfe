import { useRouter } from 'expo-router';
import { Monitor, Plus } from 'lucide-react-native';
import { View } from 'react-native';

import { mix } from '../../design/color';
import { Button, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';

/** Diameter of the round chip behind the computer icon. */
const ICON_CHIP_SIZE = 108;
/** Share of the primary colour mixed into the surface for the chip. */
const ICON_CHIP_TINT = 0.17;
const ICON_SIZE = 44;
const ADD_ICON_SIZE = 20;
const CONTENT_GAP = 14;
const CONTENT_PADDING_X = 18;
const PAIR_ROUTE = '/pair';

/** First-run prompt for a phone with no computer paired yet: an icon, a short title and body, and the Add button. */
export function EmptyPairPrompt() {
  const router = useRouter();
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', paddingHorizontal: CONTENT_PADDING_X, gap: CONTENT_GAP }}>
      <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ width: ICON_CHIP_SIZE, height: ICON_CHIP_SIZE, borderRadius: ICON_CHIP_SIZE / 2, backgroundColor: mix(c.primary, c.surface, ICON_CHIP_TINT), alignItems: 'center', justifyContent: 'center' }}>
        <Monitor size={ICON_SIZE} color={c.primary} />
      </View>
      <Text style={[LumenType.pageTitle, { textAlign: 'center' }]} accessibilityRole="header">
        {t('emptyStatePairTitle')}
      </Text>
      <Text style={[LumenType.caption, { textAlign: 'center' }]} muted>
        {t('emptyStatePairBody')}
      </Text>
      <Button size="lg" kind="filled" label={t('addComputerButton')} renderIcon={(k) => <Plus size={ADD_ICON_SIZE} color={k} />} onPress={() => router.push(PAIR_ROUTE)} style={{ alignSelf: 'stretch' }} />
    </View>
  );
}
