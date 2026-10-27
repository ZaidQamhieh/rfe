import { CloudDownload, FolderSync, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useRef, useState } from 'react';
import { View } from 'react-native';

import { formatDate } from '../../core/format';
import type { Host } from '../../core/models/host';
import type { SyncRule } from '../../core/storage/syncRules';
import { useGuardedAction } from '../../core/useGuardedAction';
import { Button, ErrorRetry, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { humanizeError } from '../../features/pairing/pairingService';
import { folderLabel, pickLocalFolder, syncRule, syncRules } from '../../features/sync/syncService';
import { InfoRow, NavRow, SettingsPage, SettingsSection, ToggleRow } from '../../features/settings/parts';
import { hostStore } from '../../services';

/** Rules that mirror a remote folder into a local one; each rule syncs on demand. */
export default function Sync() {
  const toast = useToast();
  const dialogs = useDialogs();
  const [rules, setRules] = useState<SyncRule[] | null>(null);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [running, setRunning] = useState<{ id: string; text: string } | null>(null);
  const c = useScheme();
  const cancel = useRef(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  const reload = useCallback(async () => {
    const [r, h] = await Promise.all([syncRules.list(), hostStore.listHosts()]);
    setRules(r);
    setHosts(h);
  }, []);
  useEffect(() => {
    let live = true;
    Promise.all([syncRules.list(), hostStore.listHosts()]).then(
      ([r, h]) => {
        if (!live) return;
        setRules(r);
        setHosts(h);
      },
      // A failed read must not leave a blank screen with no way forward.
      (e) => live && setLoadError(humanizeError(e)),
    );
    return () => {
      live = false;
      cancel.current = true;
    };
  }, [attempt]);

  async function addRule() {
    if (hosts.length === 0) return void toast.info(t('noPairedPcs'));
    const hostId = await dialogs.choose<string>({ title: t('choosePc'), options: hosts.map((h) => ({ value: h.id, label: h.label })) });
    if (!hostId) return;
    const remote = await dialogs.prompt({ title: t('syncRemotePath'), placeholder: '/Documents', confirmLabel: t('continueButton'), mono: true, validate: (v) => (v.startsWith('/') || /^[A-Za-z]:[\\/]/.test(v) ? null : t('syncRemotePathHint')) });
    if (!remote) return;
    const local = await pickLocalFolder();
    if (!local) return;
    await syncRules.save({ id: `r${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`, hostId, remotePath: remote, localPath: local, enabled: true });
    await reload();
  }

  async function toggle(rule: SyncRule, enabled: boolean) {
    await syncRules.save({ ...rule, enabled });
    await reload();
  }

  async function remove(rule: SyncRule) {
    const ok = await dialogs.confirm({ title: t('syncDeleteRule'), description: t('syncDeleteConfirm'), confirmLabel: t('removeButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    await syncRules.remove(rule.id);
    await reload();
  }

  async function syncNow(rule: SyncRule) {
    cancel.current = false;
    setRunning({ id: rule.id, text: '…' });
    try {
      const r = await syncRule(rule, (p) => setRunning({ id: rule.id, text: `${p.current}/${p.total} ${p.name}` }), () => cancel.current);
      if (r.failed.length > 0) toast.error(t('syncFailedFiles', { count: r.failed.length, error: r.firstError ?? '' }));
      else toast.success(t('syncDone', { count: r.downloaded }));
    } catch (e) {
      toast.error(t('syncFailed', { error: humanizeError(e) }));
    } finally {
      setRunning(null);
      await reload().catch(() => {});
    }
  }

  // A second tap before the first action finishes is ignored (the `running` state only updates after a render), and a
  // failure shows a message instead of an unhandled rejection.
  const onActionError = (e: unknown) => toast.error(humanizeError(e));
  const guardedAdd = useGuardedAction(addRule, onActionError);
  const guardedToggle = useGuardedAction(toggle, onActionError);
  const guardedRemove = useGuardedAction(remove, onActionError);
  const guardedSyncNow = useGuardedAction(syncNow, onActionError);

  if (rules === null) {
    if (loadError === null) return null;
    return (
      <ErrorRetry
        message={loadError}
        onRetry={() => {
          setLoadError(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  }
  return (
    <SettingsPage>
      {rules.length === 0 ? (
        <View style={{ padding: Spacing.lg, alignItems: 'center', gap: Spacing.sm }}>
          <FolderSync size={40} color={c.onSurfaceVariant} />
          <Text style={[LumenType.name, { textAlign: 'center' }]}>{t('syncNoRules')}</Text>
          <Text muted style={[LumenType.meta, { textAlign: 'center' }]}>{t('syncRulesSubtitle')}</Text>
        </View>
      ) : null}
      {rules.map((rule) => {
        const host = hosts.find((h) => h.id === rule.hostId);
        const busy = running?.id === rule.id;
        return (
          <SettingsSection key={rule.id} title={`${host?.label ?? '?'} · ${rule.remotePath}`}>
            <ToggleRow icon={FolderSync} title={folderLabel(rule.localPath)} subtitle={rule.lastSync ? t('syncLastSync', { when: formatDate(new Date(rule.lastSync)) }) : t('syncNever')} value={rule.enabled} onChange={(v) => void guardedToggle(rule, v)} />
            {busy ? <InfoRow icon={CloudDownload} title={t('syncNow')} subtitle={running?.text} /> : <NavRow icon={CloudDownload} title={t('syncNow')} onPress={() => (rule.enabled ? void guardedSyncNow(rule) : toast.info(t('syncRuleOff')))} />}
            <NavRow icon={Trash2} title={t('syncDeleteRule')} onPress={() => void guardedRemove(rule)} />
          </SettingsSection>
        );
      })}
      <Button size="lg" label={t('syncAddRule')} onPress={() => void guardedAdd()} />
    </SettingsPage>
  );
}
