import { Eye, EyeOff, Lock, LogIn, UserPlus, UserRound } from 'lucide-react-native';
import { useState } from 'react';
import { View } from 'react-native';

import { Button, InlineError, Pressable, Text, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { pairingDeps } from '../../services';
import { AddressField, CodeBoxRow, LumenField } from './fields';
import { humanizeError } from '../../core/humanizeError';
import { checkKnownPin, loginWithAccount, probeTarget, registerAccount, type PairingTarget } from './pairingService';
import { groupFingerprint } from './identity';

const PASSWORD_MIN_LENGTH = 8;

/** Login (existing account) and Register (new account + one-time pairing code) share one form. */
export function AccountForm({ mode, prefillAddress, onPaired }: { mode: 'login' | 'register'; prefillAddress?: string; onPaired: (label: string) => void }) {
  const c = useScheme();
  const dialogs = useDialogs();

  /** Resolves true only when the user taps Trust; cancel or dismiss resolves false. */
  const confirmTrust = (target: PairingTarget) =>
    dialogs.confirm({
      title: t('confirmFingerprintTitle'),
      description: t('confirmTrustBody'),
      details: t('confirmTrustDetails', { address: target.address, fingerprint: groupFingerprint(target.fingerprint) }),
      detailsLabel: t('metaTechnicalDetails'),
      confirmLabel: t('confirmTrustButton'),
      cancelLabel: t('cancelButton'),
    });
  const [address, setAddress] = useState(prefillAddress ?? '');
  const [code, setCode] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [obscure, setObscure] = useState(true);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const reg = mode === 'register';

  const req = (v: string) => (touched && !v.trim() ? t('requiredLabel') : null);
  const pwError = touched ? (!password ? t('requiredLabel') : reg && password.length < PASSWORD_MIN_LENGTH ? t('passwordTooShort') : null) : null;
  const confirmError = touched && reg && confirm !== password ? t('passwordMismatch') : null;

  async function submit() {
    setTouched(true);
    const invalid =
      !address.trim() || !username.trim() || !password || (reg && (!code.trim() || password.length < PASSWORD_MIN_LENGTH || confirm !== password));
    if (invalid) return;
    setBusy(true);
    setError(null);
    try {
      const target = await probeTarget(pairingDeps, address);
      // First contact: the password is only sent once the user has trusted this computer.
      if ((await checkKnownPin(pairingDeps, target)) === 'new' && !(await confirmTrust(target))) return;
      const host = reg
        ? await registerAccount(pairingDeps, target, { pairingCode: code, username, password })
        : await loginWithAccount(pairingDeps, target, { username, password });
      onPaired(host.label);
    } catch (e) {
      const msg = humanizeError(e);
      setError(reg ? t('registerFailed', { error: msg }) : t('loginFailed', { error: msg }));
    } finally {
      setBusy(false);
    }
  }

  const eye = (
    <Pressable onPress={() => setObscure((o) => !o)} accessibilityLabel={obscure ? t('showPasswordLabel') : t('hidePasswordLabel')} style={{ width: 48, height: 48, marginRight: -12, alignItems: 'center', justifyContent: 'center' }}>
      {obscure ? <Eye size={22} color={c.onSurfaceVariant} /> : <EyeOff size={22} color={c.onSurfaceVariant} />}
    </Pressable>
  );
  return (
    <View style={{ gap: Spacing.lg }}>
      <AddressField value={address} onChange={setAddress} error={req(address)} />
      {reg && (
        <View style={{ gap: Spacing.sm }}>
          <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('pairingCodeLabel')}</Text>
          <CodeBoxRow value={code} onChange={setCode} />
          {touched && !code.trim() ? <Text style={LumenType.meta} color={c.error}>{t('requiredLabel')}</Text> : null}
        </View>
      )}
      <LumenField label={t('usernameLabel')} value={username} onChangeText={setUsername} autoCapitalize="none" autoCorrect={false} error={req(username)} leading={<UserRound size={22} color={c.onSurfaceVariant} />} />
      <LumenField label={t('passwordLabel')} value={password} onChangeText={setPassword} secureTextEntry={obscure} autoCapitalize="none" autoCorrect={false} error={pwError} leading={<Lock size={22} color={c.onSurfaceVariant} />} trailing={eye} />
      {reg && <LumenField label={t('confirmPasswordLabel')} value={confirm} onChangeText={setConfirm} secureTextEntry={obscure} autoCapitalize="none" autoCorrect={false} error={confirmError} leading={<Lock size={22} color={c.onSurfaceVariant} />} />}
      <Text style={LumenType.meta} color={c.onSurfaceVariant}>{reg ? t('registerHint') : t('loginHint')}</Text>
      {error && <InlineError message={error} />}
      <Button size="lg" label={reg ? t('registerButton') : t('loginButton')} busy={busy} onPress={submit} renderIcon={(k) => (reg ? <UserPlus size={22} color={k} /> : <LogIn size={22} color={k} />)} />
    </View>
  );
}
