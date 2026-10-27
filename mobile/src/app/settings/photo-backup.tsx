import { Battery, CalendarClock, Clock, Image as ImageIcon, Images, Monitor, RotateCcw, Smartphone, Wifi } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { View } from 'react-native';

import { Button, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { transfers } from '../../core/native';
import { humanizeError } from '../../features/pairing/pairingService';
import { AlbumPickerSheet } from '../../features/photoBackup/AlbumPickerSheet';
import type { PhotoBackupPrefs } from '../../features/photoBackup/photoBackupLogic';
import { listAlbums, photoBackupStore, requestPhotoAccess, runPhotoBackup, type AlbumInfo, type BackupResult } from '../../features/photoBackup/photoBackupService';
import { useHosts } from '../../features/hosts/useHosts';
import { InfoRow, NavRow, SettingsPage, SettingsSection, ToggleRow, ValueRow } from '../../features/settings/parts';

const none: PhotoBackupPrefs = { enabled: false, hostId: null, deviceName: null, wifiOnly: true, chargingOnly: false, albumIds: [], scheduled: false };

/** A backup run's result as the one line a person can act on. */
function describe(r: BackupResult): { ok: boolean; text: string } {
  switch (r.kind) {
    case 'enqueued': return { ok: true, text: t('backingUpPhotos', { count: r.count }) };
    case 'upToDate': return { ok: true, text: t('alreadyUpToDate') };
    case 'incomplete': return { ok: false, text: t('backupIncomplete', { count: r.count }) };
    case 'disabled': return { ok: false, text: t('enableBackupFirst') };
    case 'notConfigured': return { ok: false, text: t('pickPcFirst') };
    case 'permissionDenied': return { ok: false, text: t('photoAccessDenied') };
    case 'serverNotConfigured': return { ok: false, text: t('serverDestNotConfigured') };
    case 'skipped':
      if (r.reason === 'wifi') return { ok: false, text: t('backupWaitingWifi') };
      if (r.reason === 'charging') return { ok: false, text: t('backupWaitingCharging') };
      if (r.reason === 'unreachable') return { ok: false, text: t('backupHostUnreachable', { host: r.host ?? '' }) };
      return { ok: false, text: t('backupPolicyBlocked', { host: r.host ?? '' }) };
  }
}

export default function PhotoBackup() {
  const toast = useToast();
  const dialogs = useDialogs();
  const hosts = useHosts();
  const [prefs, setPrefs] = useState<PhotoBackupPrefs>(none);
  const [loaded, setLoaded] = useState(false);
  const [backedUp, setBackedUp] = useState(0);
  const [busy, setBusy] = useState(false);
  const [albums, setAlbums] = useState<AlbumInfo[] | null>(null);
  const [picking, setPicking] = useState(false);

  const refreshCount = useCallback(() => void photoBackupStore.doneIds().then((s) => setBackedUp(s.size)), []);
  useEffect(() => {
    let live = true;
    void photoBackupStore
      .load()
      .then((p) => {
        if (live) setPrefs(p);
      })
      .catch(() => undefined) // show the defaults rather than a screen that loads forever
      .then(() => {
        if (live) setLoaded(true);
      });
    refreshCount();
    return () => {
      live = false;
    };
  }, [refreshCount]);

  // Uploads finish after the run returns, so keep the count in step with them.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = transfers.subscribe((r) => {
      if (r.state !== 'DONE') return;
      clearTimeout(timer);
      timer = setTimeout(refreshCount, 400);
    });
    return () => {
      clearTimeout(timer);
      off();
    };
  }, [refreshCount]);

  function update(patch: Partial<PhotoBackupPrefs>) {
    const next = { ...prefs, ...patch };
    setPrefs(next);
    void photoBackupStore.save(next);
  }

  async function toggleEnabled(on: boolean) {
    if (on && !(await requestPhotoAccess())) {
      toast.error(t('photoAccessDenied'));
      return;
    }
    update({ enabled: on });
  }

  async function chooseHost() {
    if (!hosts || hosts.length === 0) return void toast.info(t('noPairedPcs'));
    const id = await dialogs.choose<string>({ title: t('choosePc'), options: hosts.map((h) => ({ value: h.id, label: h.label })) });
    if (id) update({ hostId: id });
  }

  async function editNickname() {
    const v = await dialogs.prompt({ title: t('deviceNicknameLabel'), placeholder: t('deviceNicknameHint'), initialValue: prefs.deviceName ?? '', helper: t('deviceNicknameHelper'), confirmLabel: t('saveButton'), allowEmpty: true });
    if (v !== null) update({ deviceName: v.trim() === '' ? null : v.trim() });
  }

  async function openAlbums() {
    try {
      if (!(await requestPhotoAccess())) return void toast.error(t('photoAccessDenied'));
      setAlbums(await listAlbums());
      setPicking(true);
    } catch (e) {
      toast.error(t('backupFailed', { error: humanizeError(e) }));
    }
  }

  function toggleAlbum(id: string) {
    const set = new Set(prefs.albumIds);
    if (!set.delete(id)) set.add(id);
    update({ albumIds: [...set] });
  }

  async function backUpNow() {
    setBusy(true);
    try {
      const r = describe(await runPhotoBackup());
      if (r.ok) toast.success(r.text);
      else toast.error(r.text);
    } catch (e) {
      toast.error(t('backupFailed', { error: humanizeError(e) }));
    } finally {
      setBusy(false);
      refreshCount();
    }
  }

  if (!loaded) return null;
  const host = hosts?.find((h) => h.id === prefs.hostId);
  const off = !prefs.enabled;
  return (
    <SettingsPage>
      <SettingsSection title={t('photoBackupSection')}>
        <ToggleRow icon={Images} title={t('enablePhotoBackup')} subtitle={t('photoBackupSubtitle')} value={prefs.enabled} onChange={(v) => void toggleEnabled(v)} />
        <ValueRow icon={Monitor} title={t('backUpTo')} value={host?.label ?? t('choosePc')} onPress={() => void chooseHost()} />
        <ValueRow icon={Smartphone} title={t('deviceNicknameLabel')} value={prefs.deviceName ?? ''} onPress={() => void editNickname()} />
        <ValueRow icon={ImageIcon} title={t('albumsToBackUp')} value={prefs.albumIds.length === 0 ? t('allPhotos') : t('albumsSelected', { count: prefs.albumIds.length })} onPress={() => void openAlbums()} />
        <ToggleRow icon={Wifi} title={t('onlyOnWifi')} value={prefs.wifiOnly} onChange={(v) => update({ wifiOnly: v })} />
        <ToggleRow icon={Battery} title={t('onlyWhileCharging')} value={prefs.chargingOnly} onChange={(v) => update({ chargingOnly: v })} />
        <ToggleRow icon={CalendarClock} title={t('backUpAutomatically')} subtitle={t('backUpAutomaticallyHint')} value={prefs.scheduled} disabled={off} onChange={(v) => update({ scheduled: v })} />
      </SettingsSection>
      <SettingsSection title={t('backUpNow')}>
        <InfoRow icon={Clock} title={t('photosBackedUp', { count: backedUp })} />
        <NavRow
          icon={RotateCcw}
          title={t('resetBackupRecord')}
          subtitle={t('resetBackupHint')}
          onPress={() => void photoBackupStore.clearDone().then(() => { refreshCount(); toast.info(t('backupRecordCleared')); })}
        />
      </SettingsSection>
      <View style={{ gap: Spacing.sm }}>
        {off && <Text muted style={[LumenType.meta, { textAlign: 'center' }]}>{t('enableBackupFirst')}</Text>}
        <Button size="lg" label={t('backUpNow')} disabled={off} busy={busy} onPress={() => void backUpNow()} />
      </View>
      <AlbumPickerSheet visible={picking} albums={albums ?? []} selected={new Set(prefs.albumIds)} onToggle={toggleAlbum} onClear={() => update({ albumIds: [] })} onClose={() => setPicking(false)} />
    </SettingsPage>
  );
}
