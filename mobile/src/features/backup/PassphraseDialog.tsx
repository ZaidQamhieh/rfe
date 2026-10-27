import { useState } from 'react';
import { KeyboardAvoidingView, Modal, Platform, Pressable as RNPressable, View } from 'react-native';

import { Button, Text, TextField } from '../../design/components';
import { useScheme } from '../../design/theme';
import { Radii } from '../../design/tokens';
import { t } from '../../i18n';
import { MIN_PASSPHRASE } from './backupPayload';

/**
 * Asks for a passphrase. The text is used exactly as typed (no trimming: spaces count) and is masked. When [confirm]
 * is set (export) a second field must match and the length is checked; an import only needs something to try,
 * the file itself decides whether it is right.
 */
export function PassphraseDialog({ visible, title, confirm, onSubmit, onCancel }: { visible: boolean; title: string; confirm: boolean; onSubmit: (passphrase: string) => void; onCancel: () => void }) {
  const c = useScheme();
  const [pass, setPass] = useState('');
  const [again, setAgain] = useState('');
  const [wasVisible, setWasVisible] = useState(visible);
  if (visible !== wasVisible) {
    setWasVisible(visible);
    if (visible) {
      setPass('');
      setAgain('');
    }
  }
  const ok = confirm ? pass.length >= MIN_PASSPHRASE && again === pass : pass.length > 0;
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel} statusBarTranslucent>
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={{ flex: 1 }}>
        <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.5)', justifyContent: 'center', padding: 24 }} onPress={onCancel} accessibilityLabel="Dismiss">
          <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.lg, borderWidth: 1, borderColor: c.outlineVariant, padding: 24, gap: 12 }}>
            <Text variant="titleLarge" accessibilityRole="header">{title}</Text>
            <TextField label={t('passphraseLabel')} error={confirm && pass.length > 0 && pass.length < MIN_PASSPHRASE ? t('passphraseMinLength') : null} value={pass} onChangeText={setPass} secureTextEntry autoFocus autoCapitalize="none" autoCorrect={false} onSubmitEditing={() => !confirm && ok && onSubmit(pass)} />
            {confirm ? <TextField label={t('confirmPassphraseLabel')} value={again} onChangeText={setAgain} secureTextEntry autoCapitalize="none" autoCorrect={false} error={again.length > 0 && again !== pass ? t('passphraseMismatch') : null} onSubmitEditing={() => ok && onSubmit(pass)} /> : null}
            <View style={{ flexDirection: 'row', justifyContent: 'flex-end', gap: 8 }}>
              <Button kind="text" label={t('cancelButton')} onPress={onCancel} />
              <Button kind="filled" label={t('continueButton')} disabled={!ok} onPress={() => onSubmit(pass)} />
            </View>
          </RNPressable>
        </RNPressable>
      </KeyboardAvoidingView>
    </Modal>
  );
}
