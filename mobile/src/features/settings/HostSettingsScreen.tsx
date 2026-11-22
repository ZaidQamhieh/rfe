import { Stack, useRouter } from 'expo-router';
import { Activity, ArrowDown, ArrowUp, Check, Copy, Gauge, Folder, Link as LinkIcon, Lock, LogOut, MonitorSmartphone, Radio, Server, Settings2, Smartphone, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, RefreshControl, ScrollView, View } from 'react-native';

import { canManageHost, type AgentStatus, type BandwidthSettings, type Device, type Drive } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { isValidInternetAddress, routeForAddress, type Host } from '../../core/models/host';
import { Button, Disclosure, DuoIcon, Emoji, ErrorRetry, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost, hostStore } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { useResolvedVisibility, useSettings } from '../../state/settings';
import { ConnectionDiagnosticsSheet, ROUTE_NAME } from '../hosts/ConnectionDiagnosticsSheet';
import { humanizeError } from '../../core/humanizeError';
import { bandwidthLabel, bandwidthOptions } from './hostSettingsLogic';
import { InfoRow, NavRow, SettingsSection, ToggleRow, ValueRow } from './parts';
import { VisibilityEditor } from './VisibilityEditor';
import { withVisibilityOverride } from './visibilityEdit';

type Loaded = { status: AgentStatus; devices: Device[]; drives: Drive[]; bandwidth: BandwidthSettings; activeAddress: string };

const ZERO_BANDWIDTH = { maxUploadBytesPerSec: 0, maxDownloadBytesPerSec: 0 };

/** Per-host settings: agent name and access, connection routes, bandwidth limits, allowed folders, paired devices and their grants. */
export function HostSettingsScreen({ host: initialHost }: { host: Host }) {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const reportUpdateFailed = (e: unknown) => toast.error(t('updateFailed', { error: humanizeError(e) }));
  const dialogs = useDialogs();
  const [host, setHost] = useState(initialHost);
  const [data, setData] = useState<Loaded | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [diagOpen, setDiagOpen] = useState(false);

  const fetchAll = useCallback(async (): Promise<{ ok: Loaded } | { error: string }> => {
    try {
      const client = await clientForHost(host);
      const status = await client.status();
      const [devices, drives, bandwidth] = await Promise.all([
        client.listDevices(),
        client.drives(),
        // Bandwidth is host-wide policy; a device that cannot manage the host does not fetch it.
        canManageHost(status) ? client.getBandwidth().catch(() => ZERO_BANDWIDTH) : Promise.resolve(ZERO_BANDWIDTH),
      ]);
      return { ok: { status, devices, drives, bandwidth, activeAddress: client.activeAddress } };
    } catch (e) {
      return { error: humanizeError(e) };
    }
  }, [host]);
  const apply = useCallback((r: { ok: Loaded } | { error: string }) => {
    if ('ok' in r) {
      setData(r.ok);
      setError(null);
    } else setError(r.error);
  }, []);
  useEffect(() => {
    let live = true;
    void fetchAll().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchAll]);

  if (error && !data) {
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ title: host.label || host.address }} />
        <ErrorRetry message={t('errorLabel', { error })} onRetry={() => void fetchAll().then(apply)} />
      </View>
    );
  }
  if (!data) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Stack.Screen options={{ title: host.label || host.address }} />
        <ActivityIndicator color={c.primary} />
      </View>
    );
  }

  const { status: s, devices, drives, bandwidth } = data;
  const owner = canManageHost(s);
  const me = devices.find((d) => d.current);
  const readOnlyHint = s.readOnly ? t('writesRejected') : t('phoneCanModify');
  const shareLinksHint = s.allowSharing ? t('shareLinksEnabledHint') : t('shareLinksDisabledHint');
  const setLoaded = (patch: Partial<Loaded>) => setData((d) => (d ? { ...d, ...patch } : d));

  async function patch(change: { readOnly?: boolean; agentName?: string; allowSharing?: boolean }, success?: string) {
    const prev = s;
    setLoaded({ status: { ...s, ...change } });
    try {
      const updated = await (await clientForHost(host)).updateSettings(change);
      setLoaded({ status: { ...s, ...updated } });
      if (success) toast.success(success);
    } catch (e) {
      setLoaded({ status: prev });
      reportUpdateFailed(e);
    }
  }

  async function editName() {
    const name = await dialogs.prompt({ title: t('renameAgentTitle'), initialValue: s.agentName, confirmLabel: t('saveButton') });
    if (name) await patch({ agentName: name }, t('renamedTo', { newName: name }));
  }

  async function editInternetAddress() {
    const value = await dialogs.prompt({
      title: t('internetRouteDialogTitle'),
      description: t('internetRouteOwnerSetupNote'),
      placeholder: t('internetRouteAddressHint'),
      helper: t('internetRouteAddressHelper'),
      initialValue: host.internetAddress ?? '',
      allowEmpty: true,
      mono: true,
      keyboardType: 'url',
      validate: (v) => (isValidInternetAddress(v) ? null : t('internetRouteAddressInvalid')),
      confirmLabel: t('saveButton'),
    });
    if (value === null) return;
    const { internetAddress: _drop, ...rest } = host;
    const updated: Host = value === '' ? rest : { ...rest, internetAddress: value };
    // Patch only this field: a background learn (address, MAC) that landed meanwhile must not be reverted.
    await hostStore.patchHost(host.id, { internetAddress: value === '' ? undefined : value });
    setHost(updated);
    toast.success(value === '' ? t('internetRouteRemoved') : t('internetRouteSaved'));
  }

  async function pickBandwidth(field: keyof BandwidthSettings, title: string) {
    const current = bandwidth[field];
    const picked = await dialogs.choose<string>({
      title,
      options: bandwidthOptions(current).map((v) => ({ value: String(v), label: bandwidthLabel(v), icon: v === current ? <Check size={18} color={c.primary} /> : <Gauge size={18} color={c.onSurfaceVariant} /> })),
    });
    if (picked === null) return;
    const next = { ...bandwidth, [field]: Number(picked) };
    setLoaded({ bandwidth: next });
    try {
      setLoaded({ bandwidth: await (await clientForHost(host)).setBandwidth(next) });
    } catch (e) {
      setLoaded({ bandwidth });
      reportUpdateFailed(e);
    }
  }

  /** After the host is forgotten locally, leave every screen that belonged to it. */
  async function leaveHost() {
    if (useActiveHost.getState().active?.host.id === host.id) useActiveHost.getState().setActive(null);
    router.dismissAll();
  }

  async function disconnectThisDevice() {
    if (!me) return;
    const name = s.agentName || host.label || host.address;
    const ok = await dialogs.confirm({ title: t('disconnectDeviceTitle'), description: t('disconnectDeviceMessage', { pcName: name }), confirmLabel: t('disconnectButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    try {
      await (await clientForHost(host)).deleteDevice(me.id);
    } catch (e) {
      toast.error(t('disconnectFailed', { error: humanizeError(e) }));
      return;
    }
    // The token is dead the moment that call succeeds: clear the credentials locally and leave.
    try {
      await hostStore.removeHost(host.id);
    } catch (e) {
      reportUpdateFailed(e);
      return;
    }
    await leaveHost();
  }

  async function forgetThisDevice() {
    const ok = await dialogs.confirm({ title: t('forgetComputerTitle'), description: t('forgetComputerConfirm', { hostLabel: host.label }), confirmLabel: t('forgetButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    try {
      await hostStore.removeHost(host.id);
    } catch (e) {
      // A keystore failure leaves the computer listed (secrets are deleted first); say so instead of failing silently.
      reportUpdateFailed(e);
      return;
    }
    await leaveHost();
  }

  const routeName = (address: string) => ROUTE_NAME[routeForAddress(host, address)]();
  const capable = drives.filter((d) => (d.totalBytes ?? 0) > 0);

  return (
    <ScrollView
      contentContainerStyle={{ paddingBottom: 48 }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={async () => {
            setRefreshing(true);
            apply(await fetchAll());
            setRefreshing(false);
          }}
        />
      }
    >
      <Stack.Screen options={{ title: host.label || host.address }} />
      {owner ? (
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }]} muted>
          {t('securityWarning')}
        </Text>
      ) : null}

      <SettingsSection title={t('agentSection')}>
        {owner ? <ValueRow icon={Server} title={t('agentNameLabel')} value={s.agentName} onPress={editName} /> : <InfoRow icon={Server} title={t('agentNameLabel')} subtitle={s.agentName} />}
      </SettingsSection>

      <SettingsSection title={t('accessSection')}>
        {owner ? (
          <>
            <ToggleRow icon={Lock} title={t('readOnlyMode')} subtitle={readOnlyHint} value={s.readOnly} onChange={(v) => void patch({ readOnly: v })} />
            <ToggleRow icon={LinkIcon} title={t('enableShareLinks')} subtitle={shareLinksHint} value={s.allowSharing} onChange={(v) => void patch({ allowSharing: v })} />
          </>
        ) : (
          <>
            <InfoRow icon={Lock} title={t('readOnlyMode')} subtitle={readOnlyHint} value={s.readOnly ? t('switchOn') : t('switchOff')} />
            <InfoRow icon={LinkIcon} title={t('enableShareLinks')} subtitle={shareLinksHint} value={s.allowSharing ? t('switchOn') : t('switchOff')} />
          </>
        )}
      </SettingsSection>

      <SettingsSection title={t('connectionRoutesTitle')}>
        <InfoRow icon={Radio} title={t('currentRouteLabel')} subtitle={data.activeAddress ? t('currentRouteVia', { route: routeName(data.activeAddress) }) : t('currentRouteUnavailable')} />
        <NavRow icon={Activity} title={t('connectionDiagnosticsTitle')} onPress={() => setDiagOpen(true)} />
      </SettingsSection>

      {owner ? (
        <SettingsSection title={t('limitsSection')}>
          <ValueRow icon={ArrowUp} title={t('bandwidthUploadLimit')} value={bandwidthLabel(bandwidth.maxUploadBytesPerSec)} onPress={() => void pickBandwidth('maxUploadBytesPerSec', t('bandwidthUploadLimit'))} />
          <ValueRow icon={ArrowDown} title={t('bandwidthDownloadLimit')} value={bandwidthLabel(bandwidth.maxDownloadBytesPerSec)} onPress={() => void pickBandwidth('maxDownloadBytesPerSec', t('bandwidthDownloadLimit'))} />
        </SettingsSection>
      ) : null}

      <SettingsSection title={t('allowedFoldersSection')}>
        {s.accessDenied ? (
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: Spacing.md2, paddingHorizontal: Spacing.md }}>
            <View style={{ width: 40, alignItems: 'center' }}>
              <DuoIcon icon={Lock} size={24} color={c.error} />
            </View>
            <Text style={[LumenType.name, { flex: 1 }]} color={c.error}>
              {t('deviceNoFolderAccess')}
            </Text>
          </View>
        ) : s.roots.length === 0 ? (
          <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm }]} muted>
            {t('allFoldersAllowed')}
          </Text>
        ) : (
          s.roots.map((r) => <InfoRow key={r} icon={Folder} title={r} />)
        )}
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }]} muted>
          {t('managedOnPc')}
        </Text>
      </SettingsSection>

      <DeviceVisibilitySection hostId={host.id} />

      {owner ? (
        <SettingsSection title={t('pairedDevicesSection')}>
          <NavRow icon={Smartphone} title={t('phonesSettingsRow')} subtitle={t('phonesSettingsRowHint')} onPress={() => router.push({ pathname: '/host/[id]/phones', params: { id: host.id } })} />
        </SettingsSection>
      ) : null}

      <SettingsSection title={t('aboutSection')}>
        <InfoRow icon={MonitorSmartphone} title={t('pcNameLabel')} subtitle={s.agentName || host.label || host.address} />
        {capable.map((d) => (
          <DriveRow key={d.path} drive={d} />
        ))}
      </SettingsSection>

      <View style={{ marginTop: Spacing.md3 }}>
        <Disclosure label={t('aboutComputerTechnical')}>
          <TechnicalFact label={t('currentRouteLabel')} value={data.activeAddress || undefined} />
          <TechnicalFact label={t('routeLanName')} value={host.address} />
          <TechnicalFact label={t('routeTailscaleName')} value={host.tailscaleAddress && host.tailscaleAddress !== host.address ? host.tailscaleAddress : undefined} />
          <TechnicalFact label={t('routeInternetName')} value={host.internetAddress ?? t('routeNotConfigured')} />
          <Text style={LumenType.meta} muted>
            {t('routePriorityAndSecurityHint')}
          </Text>
          <View style={{ flexDirection: 'row' }}>
            <Button size="s" kind="outlined" label={t('editInternetRouteTitle')} renderIcon={(k) => <Settings2 size={16} color={k} />} onPress={() => void editInternetAddress()} />
          </View>
        </Disclosure>
      </View>

      <SettingsSection title={t('dangerZoneSection')}>
        <View style={{ gap: Spacing.sm, paddingHorizontal: Spacing.md }}>
          <Button kind="outlined" destructive label={t('revokeAccessButton')} disabled={!me} renderIcon={(k) => <LogOut size={18} color={k} />} onPress={() => void disconnectThisDevice()} />
          <Button kind="outlined" destructive label={t('forgetThisDeviceButton')} renderIcon={(k) => <Trash2 size={18} color={k} />} onPress={() => void forgetThisDevice()} />
        </View>
      </SettingsSection>

      {diagOpen && <ConnectionDiagnosticsSheet host={host} onClose={() => setDiagOpen(false)} />}
    </ScrollView>
  );
}

/** This device's own file-visibility rules for the host, or "follow the app default". */
function DeviceVisibilitySection({ hostId }: { hostId: string }) {
  const effective = useResolvedVisibility(hostId);
  const overrides = useSettings((x) => x.state.overrides);
  const setOverrides = useSettings((x) => x.setOverrides);
  const own = overrides[hostId];
  const overridden = own?.visibility !== undefined;
  return (
    <SettingsSection title={t('fileVisibilityDeviceSection')}>
      <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingBottom: Spacing.xs }]} muted>
        {t('followsAppDefaultVisibility')}
      </Text>
      <ToggleRow
        icon={Copy}
        title={t('overrideForDevice')}
        subtitle={overridden ? t('usingDeviceVisibility') : t('usingAppDefault')}
        value={overridden}
        onChange={(on) => void setOverrides({ ...overrides, [hostId]: withVisibilityOverride(own, effective, on) })}
      />
      {overridden ? <VisibilityEditor prefs={effective} onChange={(next) => void setOverrides({ ...overrides, [hostId]: { ...own, visibility: next } })} /> : null}
    </SettingsSection>
  );
}

/** One labelled technical value (an address) inside the closed Technical details fold; nothing when there is no value. */
function TechnicalFact({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <View>
      <Text style={LumenType.meta} muted>
        {label}
      </Text>
      <Text style={LumenType.name} selectable>
        {value}
      </Text>
    </View>
  );
}

/** One disk of the computer: its picture, name, and used/free space in words ("OS" marks the system drive). */
function DriveRow({ drive }: { drive: Drive }) {
  const total = drive.totalBytes ?? 0;
  const free = drive.freeBytes ?? 0;
  const name = drive.label ? drive.label : drive.path;
  const p = { used: formatSize(total - free), total: formatSize(total), free: formatSize(free) };
  return (
    <View style={{ minHeight: 64, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md }}>
      <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
        <Emoji name="drive" size={32} />
      </View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text numberOfLines={1} style={LumenType.name}>
          {name}
        </Text>
        <Text muted style={LumenType.meta}>
          {drive.isOS ? t('driveCapacityLineOs', p) : t('driveCapacityLine', p)}
        </Text>
      </View>
      {drive.isOS ? (
        <Text muted style={LumenType.meta}>
          {t('osLabel')}
        </Text>
      ) : null}
    </View>
  );
}

