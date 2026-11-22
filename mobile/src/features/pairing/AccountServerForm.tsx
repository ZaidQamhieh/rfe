import { Check, Server } from 'lucide-react-native';
import { useState } from 'react';
import { View } from 'react-native';

import { humanizeError } from '../../core/humanizeError';
import type { Host } from '../../core/models/host';
import { Button, DuoIcon, InlineError, Row, Text, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost, pairingDeps } from '../../services';
import { useHosts } from '../hosts/useHosts';
import { AddressField } from './fields';
import { groupFingerprint } from './identity';
import { checkKnownPin, loginWithAccountServer, probeTarget, type PairingTarget } from './pairingService';

/**
 * Sign in to a computer with an account server: the saved computer the user picks (already logged in there with a
 * password) vouches for this phone at the address typed here. The member computer must already trust that server;
 * this form never changes that.
 */
export function AccountServerForm({ prefillAddress, onPaired }: { prefillAddress?: string; onPaired: (label: string) => void }) {
  const c = useScheme();
  const dialogs = useDialogs();
  const hosts = useHosts();
  const [address, setAddress] = useState(prefillAddress ?? '');
  const [serverId, setServerId] = useState<string | null>(null);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const confirmTrust = (target: PairingTarget) =>
    dialogs.confirm({
      title: t('confirmFingerprintTitle'),
      description: t('confirmTrustAccountBody'),
      details: t('confirmTrustDetails', { address: target.address, fingerprint: groupFingerprint(target.fingerprint) }),
      detailsLabel: t('metaTechnicalDetails'),
      confirmLabel: t('confirmTrustButton'),
      cancelLabel: t('cancelButton'),
    });

  async function submit() {
    setTouched(true);
    const server = (hosts ?? []).find((h: Host) => h.id === serverId);
    if (!address.trim() || !server) return;
    setBusy(true);
    setError(null);
    try {
      const target = await probeTarget(pairingDeps, address);
      // First contact: nothing is sent to this computer, beyond a challenge, until the user has trusted it.
      if ((await checkKnownPin(pairingDeps, target)) === 'new' && !(await confirmTrust(target))) return;
      const host = await loginWithAccountServer(pairingDeps, target, await clientForHost(server));
      onPaired(host.label);
    } catch (e) {
      setError(t('loginFailed', { error: humanizeError(e) }));
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={{ gap: Spacing.lg }}>
      <AddressField value={address} onChange={setAddress} error={touched && !address.trim() ? t('requiredLabel') : null} />
      <View>
        <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('accountServerPick')}</Text>
        {(hosts ?? []).map((h) => (
          <Row
            key={h.id}
            title={h.label}
            subtitle={h.address}
            selected={serverId === h.id}
            lead={<DuoIcon icon={Server} size={24} color={c.onSurfaceVariant} />}
            trailing={serverId === h.id ? <Check size={20} color={c.primary} /> : undefined}
            onPress={() => setServerId(h.id)}
          />
        ))}
        {touched && !serverId ? <Text style={LumenType.meta} color={c.error}>{t('requiredLabel')}</Text> : null}
      </View>
      <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('accountServerHint')}</Text>
      {error && <InlineError message={error} />}
      <Button size="lg" label={t('accountServerButton')} busy={busy} onPress={submit} />
    </View>
  );
}
