import { CircleCheck, Download, RefreshCw } from 'lucide-react-native';
import Constants from 'expo-constants';
import { useState } from 'react';

import { useToast } from '../../design/components';
import { t } from '../../i18n';
import { humanizeError } from '../pairing/pairingService';
import { InfoRow, NavRow, SettingsSection } from '../settings/parts';
import type { AppRelease } from './updateLogic';
import { downloadUpdate, installUpdate } from './updateService';
import { recheckUpdate, useSessionUpdate } from './useUpdate';

type Phase = { kind: 'idle' } | { kind: 'checking' } | { kind: 'upToDate' } | { kind: 'downloading'; pct: number } | { kind: 'installing' };

/** About > Updates: check for a newer build, download it (verified) and hand it to the system installer. */
export function UpdateSection() {
  const toast = useToast();
  const found = useSessionUpdate();
  const [checked, setChecked] = useState<{ release: AppRelease | null } | null>(null);
  const [phase, setPhase] = useState<Phase>({ kind: 'idle' });
  const release = checked ? checked.release : found;
  const version = Constants.expoConfig?.version ?? '';

  async function check() {
    setPhase({ kind: 'checking' });
    try {
      const r = await recheckUpdate();
      setChecked({ release: r });
      setPhase(r ? { kind: 'idle' } : { kind: 'upToDate' });
    } catch (e) {
      setPhase({ kind: 'idle' });
      toast.error(t('updateFailed', { error: humanizeError(e) }));
    }
  }

  async function update(r: AppRelease) {
    try {
      setPhase({ kind: 'downloading', pct: 0 });
      await downloadUpdate(r, (pct) => setPhase({ kind: 'downloading', pct }));
      setPhase({ kind: 'installing' });
      toast.info(t('openingInstallerConfirm'));
      const outcome = await installUpdate(r);
      if (outcome === 'needsPermission') toast.info(t('installPermissionNeeded'));
      else if (outcome === 'failed') toast.error(t('couldNotOpenInstaller'));
    } catch (e) {
      // The user's own cancel of the download is not a failure.
      if ((e as { code?: string }).code !== 'ERR_CANCELLED') toast.error(t('updateFailed', { error: humanizeError(e) }));
    } finally {
      setPhase({ kind: 'idle' });
    }
  }

  return (
    <SettingsSection title={t('updatesSection')}>
      {phase.kind === 'checking' ? (
        <InfoRow icon={RefreshCw} title={t('checkingForUpdates')} />
      ) : phase.kind === 'downloading' ? (
        <InfoRow icon={Download} title={t('downloadingUpdate', { percent: phase.pct })} />
      ) : phase.kind === 'installing' ? (
        <InfoRow icon={Download} title={t('openingInstaller')} />
      ) : release ? (
        <NavRow icon={Download} title={t('updateAvailable', { version: release.versionName })} subtitle={t('updateButton')} onPress={() => void update(release)} />
      ) : phase.kind === 'upToDate' ? (
        <InfoRow icon={CircleCheck} title={t('upToDate', { version })} />
      ) : null}
      {phase.kind === 'checking' || phase.kind === 'downloading' || phase.kind === 'installing' ? null : <NavRow icon={RefreshCw} title={t('checkForUpdates')} onPress={() => void check()} />}
    </SettingsSection>
  );
}
