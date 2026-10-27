import * as DocumentPicker from 'expo-document-picker';
import { File } from 'expo-file-system';
import { Download, Upload } from 'lucide-react-native';
import { useState } from 'react';
import { View } from 'react-native';

import { Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { exportEnvelope, importEnvelope, shareBackupFile } from '../../features/backup/backupService';
import { humanizeError } from '../../features/pairing/pairingService';
import { PassphraseDialog } from '../../features/backup/PassphraseDialog';
import { NavRow, SettingsPage, SettingsSection } from '../../features/settings/parts';
import { backupStorage } from '../../services';

type Ask = { mode: 'export' } | { mode: 'import'; envelope: string } | null;

/** Encrypted export and import of hosts, tokens, pins and settings; the file format is the Flutter app's. */
const MAX_BACKUP_BYTES = 8 * 1024 * 1024;

export default function Backup() {
  const toast = useToast();
  const dialogs = useDialogs();
  const [ask, setAsk] = useState<Ask>(null);
  const [busy, setBusy] = useState(false);

  async function chooseImportFile() {
    try {
      const picked = await DocumentPicker.getDocumentAsync({ multiple: false, copyToCacheDirectory: true });
      if (picked.canceled || !picked.assets[0]) return;
      // A backup is a few KB; refuse a huge pick before reading it into a string (audit F-117).
      if ((picked.assets[0].size ?? 0) > MAX_BACKUP_BYTES) throw new Error('That file is too large to be a backup');
      const envelope = await new File(picked.assets[0].uri).text();
      setAsk({ mode: 'import', envelope });
    } catch (e) {
      toast.error(t('couldNotReadFile', { error: humanizeError(e) }));
    }
  }

  async function run(mode: Ask, passphrase: string) {
    setAsk(null);
    if (!mode) return;
    if (mode.mode === 'import') {
      const go = await dialogs.confirm({ title: t('replaceCurrentConfig'), description: t('importWarningMessage'), confirmLabel: t('replaceButton'), cancelLabel: t('cancelButton') });
      if (!go) return;
    }
    setBusy(true);
    try {
      if (mode.mode === 'export') {
        toast.info(t('preparingBackup'));
        const shared = await shareBackupFile(await exportEnvelope(backupStorage, passphrase));
        if (shared) toast.success(t('backupReadyToShare'));
        else toast.error(t('exportFailed'));
      } else {
        toast.info(t('restoringConfig'));
        await importEnvelope(backupStorage, mode.envelope, passphrase);
        toast.success(t('configRestored'));
      }
    } catch (e) {
      toast.error(`${mode.mode === 'export' ? t('exportFailed') : t('importFailed')}: ${humanizeError(e)}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <SettingsPage>
      <SettingsSection title={t('backupRestoreSection')}>
        <NavRow icon={Upload} title={t('exportConfig')} subtitle={t('exportConfigSubtitle')} onPress={() => !busy && setAsk({ mode: 'export' })} />
        <NavRow icon={Download} title={t('importConfig')} subtitle={t('importConfigSubtitle')} onPress={() => !busy && void chooseImportFile()} />
      </SettingsSection>
      <View style={{ paddingHorizontal: Spacing.xs }}>
        <Text style={LumenType.meta} muted>{t('backupEncryptionWarning')}</Text>
      </View>
      <PassphraseDialog visible={ask !== null} title={ask?.mode === 'import' ? t('importConfigTitle') : t('exportConfig')} confirm={ask?.mode !== 'import'} onSubmit={(p) => void run(ask, p)} onCancel={() => setAsk(null)} />
    </SettingsPage>
  );
}
