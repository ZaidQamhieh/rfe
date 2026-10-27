import { View } from 'react-native';

import { Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { t } from '../../i18n';
import { SettingsPage, SettingsSection } from '../../features/settings/parts';

const ITEMS = ['whatsNew1', 'whatsNew2', 'whatsNew3', 'whatsNew4', 'whatsNew5', 'whatsNew6'] as const;

export default function WhatsNew() {
  return (
    <SettingsPage>
      <SettingsSection title="2.0">
        {ITEMS.map((k) => (
          <View key={k} style={{ paddingVertical: 12 }}>
            <Text style={LumenType.name}>{t(k)}</Text>
          </View>
        ))}
      </SettingsSection>
    </SettingsPage>
  );
}
