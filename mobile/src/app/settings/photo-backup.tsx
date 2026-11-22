import { Battery, CalendarClock, CircleCheck, CirclePause, Clock, Image as ImageIcon, Images, LoaderCircle, Monitor, RotateCcw, Smartphone, TriangleAlert, Wifi, type LucideIcon } from 'lucide-react-native';
import { useRouter } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Linking, View } from 'react-native';

import { Button, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { transfers } from '../../core/native';
import { humanizeError } from '../../core/humanizeError';
import { AlbumPickerSheet } from '../../features/photoBackup/AlbumPickerSheet';
import { assetIdFromTransferId, defaultBackupHostId, nextSetupStep, soleHostId, type PhotoBackupPrefs } from '../../features/photoBackup/photoBackupLogic';
import { listAlbums, photoBackupStore, requestPhotoAccess, resetPhotoBackupRecord, runPhotoBackup, type AlbumInfo, type BackupResult } from '../../features/photoBackup/photoBackupService';
import { backupStatus, type BackupStatus, type StatusAction, type StatusState } from '../../features/photoBackup/photoBackupStatus';
import { useBackupHostStates } from '../../features/photoBackup/useBackupHostStates';
import { useHosts } from '../../features/hosts/useHosts';
import { InfoRow, SettingsPage, SettingsSection, ToggleRow, ValueRow } from '../../features/settings/parts';

/** Delay before the backed-up count is re-read after an upload finishes. */
const COUNT_REFRESH_DELAY_MS = 400;
const none: PhotoBackupPrefs ={ enabled: false, hostId: null, deviceName: null, wifiOnly: true, chargingOnly: false, albumIds: [], scheduled: false };

/** The status icon: words carry the state, the icon repeats it so colour is never the only signal. */
function statusIcon(state: StatusState): LucideIcon {
  if (state === 'running' || state === 'stillUploading') return LoaderCircle;
  if (state === 'pausedWifi' || state === 'pausedCharging') return CirclePause;
  if (state === 'upToDate') return CircleCheck;
  return ATTENTION_STATES.has(state) ? TriangleAlert : Clock;
}

const ATTENTION_STATES: ReadonlySet<StatusState> = new Set<StatusState>(['chooseComputer', 'unreachable', 'notAccepted', 'couldNotPrepare', 'noDestination', 'permissionDenied', 'phoneRemoved', 'albumsGone', 'policyBlocked']);
const FINISHED_UPLOAD_STATES: ReadonlySet<string> = new Set(['DONE', 'FAILED', 'CANCELLED']);

export default function PhotoBackup() {
  const toast = useToast();
  const dialogs = useDialogs();
  const hosts = useHosts();
  const router = useRouter();
  const [settingUp, setSettingUp] = useState(false);
  const [prefs, setPrefs] = useState<PhotoBackupPrefs>(none);
  const [loaded, setLoaded] = useState(false);
  const [backedUp, setBackedUp] = useState(0);
  const [busy, setBusy] = useState(false);
  const [albums, setAlbums] = useState<AlbumInfo[] | null>(null);
  const [picking, setPicking] = useState(false);
  // What the latest run or permission request ended with, and how many of its photos are still uploading: the status row reads both.
  const [last, setLast] = useState<BackupResult | null>(null);
  const [pending, setPending] = useState(0);

  // The count belongs to the chosen computer: picking another one shows what reached that one (nothing yet).
  const hostId = prefs.hostId;
  const refreshCount = useCallback(() => void photoBackupStore.doneCountFor(hostId).then(setBackedUp), [hostId]);
  useEffect(refreshCount, [refreshCount]);
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
    return () => {
      live = false;
    };
  }, []);

  // Once the computers have been checked, "Back up to" names a connected one rather than a stale saved choice.
  const hostStates = useBackupHostStates(hosts);
  const settledHost = useRef(false);
  useEffect(() => {
    if (!loaded || !hostStates || settledHost.current) return;
    settledHost.current = true;
    const id = defaultBackupHostId(prefs.hostId, hostStates);
    if (id && id !== prefs.hostId) update({ hostId: id });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once, on the first complete check
  }, [loaded, hostStates]);

  // Uploads finish after the run returns, so keep the count in step with them.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = transfers.subscribe((r) => {
      if (assetIdFromTransferId(r.id) !== null && FINISHED_UPLOAD_STATES.has(r.state)) setPending((n) => Math.max(0, n - 1));
      if (r.state !== 'DONE') return;
      clearTimeout(timer);
      timer = setTimeout(refreshCount, COUNT_REFRESH_DELAY_MS);
    });
    return () => {
      clearTimeout(timer);
      off();
    };
  }, [refreshCount]);

  function update(patch: Partial<PhotoBackupPrefs>, base: PhotoBackupPrefs = prefs) {
    const next = { ...base, ...patch };
    setPrefs(next);
    void photoBackupStore.save(next);
  }

  /** Records that photo access was refused: the status row shows it with an Open settings action, and a toast echoes it. */
  function photoAccessRefused() {
    setLast({ kind: 'permissionDenied' });
    toast.error(t('backupStatusPhotoAccessOff'));
  }

  /** Photo access is allowed now, so a refusal shown earlier no longer applies. */
  function photoAccessGranted() {
    setLast((l) => (l?.kind === 'permissionDenied' ? null : l));
  }

  async function toggleEnabled(on: boolean) {
    if (on) {
      if (!(await requestPhotoAccess())) return photoAccessRefused();
      photoAccessGranted();
    }
    update({ enabled: on });
  }

  /** Asks which paired computer to back up to; null when there is none or the question is dismissed. */
  async function pickHost(): Promise<string | null> {
    if (!hosts || hosts.length === 0) {
      toast.info(t('noPairedPcs'));      return null;
    }
    return (await dialogs.choose<string>({ title: t('choosePc'), options: hosts.map((h) => ({ value: h.id, label: h.label })) })) || null;
  }

  async function chooseHost() {
    const id = await pickHost();
    if (id) update({ hostId: id });
  }

  /**
   * Walks through whatever set-up is missing: add a computer (opens the pairing screen), choose one (skipped when only one
   * is paired), allow photo access and switch backup on, then open the album picker. Stops quietly if a step is declined.
   */
  async function startSetup() {
    const ids = (hosts ?? []).map((h) => h.id);
    setSettingUp(true);
    try {
      let cur = prefs;
      for (let step = nextSetupStep(cur, ids); step !== 'done'; step = nextSetupStep(cur, ids)) {
        if (step === 'pairComputer') return void router.push('/pair');
        if (step === 'chooseComputer') {
          const id = soleHostId(ids) ?? (await pickHost());
          if (!id) return;
          cur = { ...cur, hostId: id };
        } else {
          if (!(await requestPhotoAccess())) return photoAccessRefused();
          photoAccessGranted();
          cur = { ...cur, enabled: true };
        }
      }
      setPrefs(cur);
      void photoBackupStore.save(cur);
      await openAlbums(cur);
    } catch (e) {
      toast.error(t('backupFailed', { error: humanizeError(e) }));
    } finally {
      setSettingUp(false);
    }
  }

  async function editNickname() {
    const v = await dialogs.prompt({ title: t('deviceNicknameLabel'), placeholder: t('deviceNicknameHint'), initialValue: prefs.deviceName ?? '', helper: t('deviceNicknameHelper'), confirmLabel: t('saveButton'), allowEmpty: true });
    if (v !== null) update({ deviceName: v.trim() === '' ? null : v.trim() });
  }

  async function openAlbums(base: PhotoBackupPrefs = prefs) {
    try {
      if (!(await requestPhotoAccess())) return photoAccessRefused();
      photoAccessGranted();
      const list = await listAlbums();
      setAlbums(list);
      // An album deleted on the phone must not stay in the count.
      const live = new Set(list.map((a) => a.id));
      const kept = base.albumIds.filter((id) => live.has(id));
      if (kept.length !== base.albumIds.length) update({ albumIds: kept }, base);
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
      const result = await runPhotoBackup();
      const inFlight = result.kind === 'enqueued' || result.kind === 'waiting' ? result.count : 0;
      setLast(result);
      setPending(inFlight);
      // The toast is a short echo of the status row's headline.
      const s = statusFor(result, inFlight);
      const text = result.kind === 'upToDate' ? t('alreadyUpToDate') : t(s.headline.key, s.headline.params);
      if (s.tone === 'attention') toast.error(text);
      else toast.success(text);
    } catch (e) {
      toast.error(t('backupFailed', { error: humanizeError(e) }));
    } finally {
      setBusy(false);
      refreshCount();
    }
  }

  /** The status for a given run result and in-flight count, using the current settings and backed-up count. */
  function statusFor(result: BackupResult | null, inFlight: number): BackupStatus {
    const computer = hosts?.find((h) => h.id === prefs.hostId)?.label ?? '';
    return backupStatus({ hostChosen: prefs.hostId !== null, enabled: prefs.enabled, computer, backedUp, pending: inFlight, last: result });
  }

  /** Runs the status row's action: try the backup again, reset the record, or open the system settings for photo access. */
  function runStatusAction(a: StatusAction) {
    if (a === 'tryAgain') void backUpNow();
    else if (a === 'resetRecord') void resetRecord();
    else if (a === 'openSettings') Linking.openSettings().catch((e: unknown) => toast.error(t('backupFailed', { error: humanizeError(e) })));
  }

  async function resetRecord() {
    const ok = await dialogs.confirm({ title: t('resetBackupConfirmTitle'), description: t('resetBackupConfirmBody'), confirmLabel: t('resetButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    await resetPhotoBackupRecord();
    refreshCount();
    toast.info(t('backupRecordCleared'));
  }

  if (!loaded) return null;
  const host = hosts?.find((h) => h.id === prefs.hostId);
  const off = !prefs.enabled;
  const status = statusFor(last, pending);
  const showStatus = status.state !== 'notSetUp' && status.state !== 'off';
  const actionLabel = status.action === 'tryAgain' ? t('tryAgainButton') : status.action === 'resetRecord' ? t('resetBackupRecord') : status.action === 'openSettings' ? t('openSettingsButton') : null;
  // Until the computers are known there is nothing to decide, so no set-up card is drawn.
  const setupStep = hosts ? nextSetupStep(prefs, hosts.map((h) => h.id)) : 'done';
  return (
    <SettingsPage>
      {setupStep !== 'done' && (
        <View style={{ gap: Spacing.sm }}>
          <Text style={LumenType.sectionLabel}>{t('photoBackupSetupTitle')}</Text>
          <Text muted style={LumenType.meta}>{t('photoBackupSetupBody')}</Text>
          <Button size="lg" label={setupStep === 'pairComputer' ? t('photoBackupSetupAddComputer') : t('photoBackupSetupButton')} busy={settingUp} onPress={() => void startSetup()} />
        </View>
      )}
      {showStatus && (
        <SettingsSection>
          <InfoRow icon={statusIcon(status.state)} title={t(status.headline.key, status.headline.params)} subtitle={status.detail ? t(status.detail.key, status.detail.params) : undefined} />
          {actionLabel && (
            <View style={{ paddingHorizontal: Spacing.md }}>
              <Button label={actionLabel} onPress={() => runStatusAction(status.action)} />
            </View>
          )}
        </SettingsSection>
      )}
      <SettingsSection>
        <ToggleRow icon={Images} title={t('enablePhotoBackup')} subtitle={t('photoBackupSubtitle')} value={prefs.enabled} onChange={(v) => void toggleEnabled(v)} />
        <ValueRow icon={Monitor} title={t('backUpTo')} value={host?.label ?? t('choosePc')} onPress={() => void chooseHost()} />
        <ValueRow icon={Smartphone} title={t('deviceNicknameLabel')} value={prefs.deviceName ?? ''} onPress={() => void editNickname()} />
        <ValueRow icon={ImageIcon} title={t('albumsToBackUp')} value={prefs.albumIds.length === 0 ? t('allPhotos') : t('albumsSelected', { count: prefs.albumIds.length })} onPress={() => void openAlbums()} />
        <ToggleRow icon={Wifi} title={t('onlyOnWifi')} value={prefs.wifiOnly} onChange={(v) => update({ wifiOnly: v })} />
        <ToggleRow icon={Battery} title={t('onlyWhileCharging')} value={prefs.chargingOnly} onChange={(v) => update({ chargingOnly: v })} />
        <ToggleRow icon={CalendarClock} title={t('backUpAutomatically')} subtitle={t('backUpAutomaticallyHint')} value={prefs.scheduled} disabled={off} onChange={(v) => update({ scheduled: v })} />
      </SettingsSection>
      <View style={{ gap: Spacing.sm }}>
        {off && <Text muted style={[LumenType.meta, { textAlign: 'center' }]}>{t('enableBackupFirst')}</Text>}
        <Button size="lg" label={t('backUpNow')} disabled={off} busy={busy} onPress={() => void backUpNow()} />
      </View>
      <SettingsSection title={t('photoBackupHistory')}>
        <InfoRow icon={Clock} title={backedUp === 0 ? t('noPhotosBackedUp') : t('photosBackedUp', { count: backedUp })} />
      </SettingsSection>
      {/* Set well apart from "Back up now" and drawn in the stop colour, so a stray tap at the fold cannot land on it. */}
      <View style={{ gap: Spacing.sm, marginTop: Spacing.xl }}>
        <Text muted style={LumenType.meta}>{t('resetBackupHint')}</Text>
        <Button kind="outlined" destructive label={t('resetBackupRecord')} renderIcon={(k) => <RotateCcw size={18} color={k} />} onPress={() => void resetRecord()} />
      </View>
      <AlbumPickerSheet visible={picking} albums={albums ?? []} selected={new Set(prefs.albumIds)} onToggle={toggleAlbum} onClear={() => update({ albumIds: [] })} onClose={() => setPicking(false)} />
    </SettingsPage>
  );
}