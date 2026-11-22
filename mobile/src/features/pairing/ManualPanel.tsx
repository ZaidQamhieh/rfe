import { View } from 'react-native';

import { Button, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { AddressField } from './fields';

/** Enter a computer's address (kept under Technical details); the computer then asks its owner to allow this phone. */
export function ManualPanel({ address, onChange, showRequired, onSubmit }: { address: string; onChange: (v: string) => void; showRequired: boolean; onSubmit: () => void }) {
  const c = useScheme();
  return (
    <View style={{ gap: 10 }}>
      <Text style={LumenType.meta} color={c.onSurfaceVariant}>
        {t('giAddressHelp')}
      </Text>
      <AddressField value={address} onChange={onChange} error={showRequired && !address.trim() ? t('requiredLabel') : null} />
      <Button label={t('giSend')} kind="filled" onPress={onSubmit} style={{ alignSelf: 'flex-start' }} />
    </View>
  );
}
