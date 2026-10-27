import { ScrollView } from 'react-native';

import { Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { t } from '../../i18n';

export default function Privacy() {
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingVertical: 12, gap: 14, paddingBottom: 32 }}>
      <Text style={LumenType.name}>{t('privacyBody1')}</Text>
      <Text style={LumenType.name}>{t('privacyBody2')}</Text>
      <Text style={LumenType.name}>{t('privacyBody3')}</Text>
    </ScrollView>
  );
}
