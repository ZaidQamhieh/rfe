import { View } from 'react-native';

import { Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { AddressField } from './fields';

/** Enter a computer's address; it then asks the owner to approve on that computer (nothing else to type). The footer submits. */
export function ManualPanel({ address, onChange, showRequired }: { address: string; onChange: (v: string) => void; showRequired: boolean }) {
  const c = useScheme();
  return (
    <View style={{ gap: 10 }}>
      <AddressField value={address} onChange={onChange} error={showRequired && !address.trim() ? t('requiredLabel') : null} />
      <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
        {t('pairingHint')}
      </Text>
    </View>
  );
}
