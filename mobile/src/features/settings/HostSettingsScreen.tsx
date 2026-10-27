import { Stack, useRouter } from 'expo-router';
import { Activity, ArrowDown, ArrowUp, Check, Copy, Gauge, Folder, Globe, HardDrive, Link as LinkIcon, Lock, MemoryStick, MonitorSmartphone, Network, Radio, ScrollText, Server, Settings2, Smartphone, Unplug, Wifi } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, RefreshControl, ScrollView, View } from 'react-native';

import { canManageHost, hasAppCapabilities, hasFileCapabilities, type AgentStatus, type BandwidthSettings, type Device, type Drive, type FileCapability, FILE_CAPABILITIES } from '../../core/api/models';
import { formatRelative, formatSize } from '../../core/format';
import { isValidInternetAddress, routeForAddress, type Host, type HostRoute } from '../../core/models/host';
import { mix } from '../../design/color';
import { Button, ErrorRetry, Pressable, SectionLabel, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost, hostStore } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { useResolvedVisibility, useSettings } from '../../state/settings';
import { ConnectionDiagnosticsSheet } from '../hosts/ConnectionDiagnosticsSheet';
import { humanizeError } from '../pairing/pairingService';
import { bandwidthLabel, bandwidthOptions, FILE_CAPABILITY_LABEL, nextAppCapabilities, nextFileGrants, patchDevice } from './hostSettingsLogic';
import { InfoRow, NavRow, RowBadge, SettingsSection, SmallSwitchRow, ToggleRow, ValueRow } from './parts';
import { VisibilityEditor } from './VisibilityEditor';
import { withVisibilityOverride } from './visibilityEdit';

type Loaded = { status: AgentStatus; devices: Device[]; drives: Drive[]; bandwidth: BandwidthSettings; activeAddress: string };

/** Per-host settings: agent name and access, connection routes, bandwidth limits, allowed folders, paired devices and their grants. */
export function HostSettingsScreen({ host: initialHost }: { host: Host }) {
  const c = useScheme();
  const roles = useRoles();
  const router = useRouter();
  const toast = useToast();
  const dialogs = useDialogs();
  const [host, setHost] = useState(initialHost);
  const [data, setData] = useState<Loaded | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [diagOpen, setDiagOpen] = useState(false);

  const fetchAll = useCallback(async (): Promise<{ ok: Loaded } | { error: string }> => {
    try {
      const client = await clientForHost(host);
      const status = await client.status();
      const [devices, drives, bandwidth] = await Promise.all([
        client.listDevices(),
        client.drives(),
        // Bandwidth is host-wide policy; a device that cannot manage the host does not fetch it.
        canManageHost(status) ? client.getBandwidth().catch(() => ({ maxUploadBytesPerSec: 0, maxDownloadBytesPerSec: 0 })) : Promise.resolve({ maxUploadBytesPerSec: 0, maxDownloadBytesPerSec: 0 }),
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
  const setLoaded = (patch: Partial<Loaded>) => setData((d) => (d ? { ...d, ...patch } : d));
  const markBusy = (id: string, on: boolean) =>
    setBusy((b) => {
      const next = new Set(b);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  async function patch(change: { readOnly?: boolean; agentName?: string; allowSharing?: boolean }, success?: string) {
    const prev = s;
    setLoaded({ status: { ...s, ...change } });
    try {
      const updated = await (await clientForHost(host)).updateSettings(change);
      setLoaded({ status: { ...s, ...updated } });
      if (success) toast.success(success);
    } catch (e) {
      setLoaded({ status: prev });
      toast.error(t('updateFailed', { error: humanizeError(e) }));
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
      toast.error(t('updateFailed', { error: humanizeError(e) }));
    }
  }

  async function updateApps(d: Device, change: { viewApps?: boolean; launchApps?: boolean }) {
    if (!hasAppCapabilities(d)) return;
    const next = nextAppCapabilities(d, change);
    markBusy(d.id, true);
    try {
      await (await clientForHost(host)).updateDeviceAppCapabilities(d.id, next);
      setData((cur) => (cur ? { ...cur, devices: patchDevice(cur.devices, d.id, next) } : cur));
    } catch (e) {
      toast.error(t('updateFailed', { error: humanizeError(e) }));
    } finally {
      markBusy(d.id, false);
    }
  }

  async function updateGrants(d: Device, change: Partial<Record<FileCapability, boolean>>) {
    if (!hasFileCapabilities(d) || d.viaLogin) return;
    const next = nextFileGrants(d as Record<FileCapability, boolean>, change);
    markBusy(d.id, true);
    try {
      await (await clientForHost(host)).updateDeviceFileCapabilities(d.id, next);
      setData((cur) => (cur ? { ...cur, devices: patchDevice(cur.devices, d.id, next) } : cur));
    } catch (e) {
      toast.error(t('updateFailed', { error: humanizeError(e) }));
    } finally {
      markBusy(d.id, false);
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
      toast.error(t('updateFailed', { error: humanizeError(e) }));
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
      toast.error(t('updateFailed', { error: humanizeError(e) }));
      return;
    }
    await leaveHost();
  }

  const routeName = (address: string) => ROUTE_NAME[routeForAddress(host, address)]();
  const capable = drives.filter((d) => (d.totalBytes ?? 0) > 0);

  return (
    <ScrollView
      contentContainerStyle={{ paddingHorizontal: 18, paddingTop: 12, paddingBottom: 48, gap: 16 }}
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
      <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} muted>
        {host.address}
      </Text>
      {owner && <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} muted>{t('securityWarning')}</Text>}

      <SettingsSection title={t('agentSection')}>
        {owner ? <ValueRow icon={Server} tint={c.primary} title={t('agentNameLabel')} value={s.agentName} onPress={editName} /> : <InfoRow icon={Server} tint={c.primary} title={t('agentNameLabel')} subtitle={s.agentName} />}
      </SettingsSection>

      <SettingsSection title={t('accessSection')}>
        {owner ? (
          <>
            <ToggleRow icon={Lock} tint={c.primary} title={t('readOnlyMode')} subtitle={s.readOnly ? t('writesRejected') : t('phoneCanModify')} value={s.readOnly} onChange={(v) => void patch({ readOnly: v })} />
            <ToggleRow icon={LinkIcon} tint={c.primary} title={t('enableShareLinks')} subtitle={s.allowSharing ? t('shareLinksEnabledHint') : t('shareLinksDisabledHint')} value={s.allowSharing} onChange={(v) => void patch({ allowSharing: v })} />
          </>
        ) : (
          <>
            <InfoRow icon={Lock} tint={c.primary} title={t('readOnlyMode')} subtitle={s.readOnly ? t('writesRejected') : t('phoneCanModify')} />
            <InfoRow icon={LinkIcon} tint={c.primary} title={t('enableShareLinks')} subtitle={s.allowSharing ? t('shareLinksEnabledHint') : t('shareLinksDisabledHint')} />
          </>
        )}
      </SettingsSection>

      <SettingsSection title={t('connectionRoutesTitle')}>
        <InfoRow icon={Radio} tint={c.primary} title={t('currentRouteLabel')} subtitle={data.activeAddress ? t('currentRouteDescription', { route: routeName(data.activeAddress), address: data.activeAddress }) : t('currentRouteUnavailable')} />
        <InfoRow icon={Wifi} tint={c.primary} title={t('routeLanName')} subtitle={host.address} />
        {host.tailscaleAddress && host.tailscaleAddress !== host.address ? <InfoRow icon={Network} tint={c.primary} title={t('routeTailscaleName')} subtitle={host.tailscaleAddress} /> : null}
        <InfoRow icon={Globe} tint={c.primary} title={t('routeInternetName')} subtitle={host.internetAddress ?? t('routeNotConfigured')} />
        <NavRow icon={Settings2} tint={c.primary} title={t('editInternetRouteTitle')} subtitle={t('editInternetRouteSubtitle')} onPress={editInternetAddress} />
        <Text style={[LumenType.meta, { paddingVertical: 10 }]} muted>{t('routePriorityAndSecurityHint')}</Text>
      </SettingsSection>

      <SettingsSection title={t('limitsSection')}>
        {owner ? (
          <>
            <ValueRow icon={ArrowUp} tint={roles.warn} title={t('bandwidthUploadLimit')} value={bandwidthLabel(bandwidth.maxUploadBytesPerSec)} onPress={() => void pickBandwidth('maxUploadBytesPerSec', t('bandwidthUploadLimit'))} />
            <ValueRow icon={ArrowDown} tint={roles.warn} title={t('bandwidthDownloadLimit')} value={bandwidthLabel(bandwidth.maxDownloadBytesPerSec)} onPress={() => void pickBandwidth('maxDownloadBytesPerSec', t('bandwidthDownloadLimit'))} />
          </>
        ) : null}
        <NavRow icon={HardDrive} tint={c.primary} title={t('storageInsightsTitle')} subtitle={t('storageInsightsRowSubtitle')} onPress={() => router.push({ pathname: '/host/[id]/storage', params: { id: host.id } })} />
        <NavRow icon={Activity} tint={roles.safe} title={t('connectionDiagnosticsTitle')} onPress={() => setDiagOpen(true)} />
        <NavRow icon={LinkIcon} tint={roles.route} title={t('activeShareLinksTitle')} onPress={() => router.push({ pathname: '/host/[id]/shares', params: { id: host.id } })} />
        {owner ? <NavRow icon={ScrollText} tint={roles.warn} title={t('activityLogTitle')} onPress={() => router.push({ pathname: '/host/[id]/audit', params: { id: host.id } })} /> : null}
      </SettingsSection>

      <SettingsSection title={t('allowedFoldersSection')}>
        {s.accessDenied ? (
          <View style={{ flexDirection: 'row', gap: 10, paddingVertical: 12 }}>
            <Lock size={20} color={c.error} />
            <Text style={[LumenType.name, { flex: 1 }]} color={c.error}>This device has no folder access. Ask the PC owner to review its folder restriction.</Text>
          </View>
        ) : s.roots.length === 0 ? (
          <Text style={[LumenType.meta, { paddingVertical: 14 }]} muted>{t('allFoldersAllowed')}</Text>
        ) : (
          s.roots.map((r) => <InfoRow key={r} icon={Folder} tint={c.primary} title={r} />)
        )}
        <Text style={[LumenType.meta, { paddingVertical: 10 }]} muted>{t('managedOnPc')}</Text>
      </SettingsSection>

      <DeviceVisibilitySection hostId={host.id} />

      <SettingsSection title={t('pairedDevicesSection')}>
        {devices.map((d) => (
          <DeviceRow key={d.id} device={d} isAdmin={s.isAdmin} busy={busy.has(d.id)} onDisconnect={disconnectThisDevice} onApps={(change) => void updateApps(d, change)} onGrants={(change) => void updateGrants(d, change)} />
        ))}
      </SettingsSection>

      <SettingsSection title={t('aboutSection')}>
        <InfoRow icon={MonitorSmartphone} tint={c.primary} title={t('pcNameLabel')} subtitle={s.agentName || host.label || host.address} />
        {capable.map((d) => (
          <DriveRow key={d.path} drive={d} />
        ))}
      </SettingsSection>

      <View style={{ gap: 10 }}>
        <SectionLabel title={t('dangerZoneSection')} />
        <Button size="lg" kind="neutral" destructive label={t('revokeAccessButton')} disabled={!me} onPress={() => void disconnectThisDevice()} />
        <Button size="lg" kind="neutral" label={t('forgetThisDeviceButton')} onPress={() => void forgetThisDevice()} />
      </View>

      {diagOpen && <ConnectionDiagnosticsSheet host={host} onClose={() => setDiagOpen(false)} />}
    </ScrollView>
  );
}

const ROUTE_NAME: Record<HostRoute, () => string> = {
  lan: () => t('routeLanName'),
  tailscale: () => t('routeTailscaleName'),
  directHttps: () => t('routeInternetName'),
  custom: () => t('routeCustomName'),
};

/** This device's own file-visibility rules for the host, or "follow the app default". */
function DeviceVisibilitySection({ hostId }: { hostId: string }) {
  const c = useScheme();
  const effective = useResolvedVisibility(hostId);
  const overrides = useSettings((x) => x.state.overrides);
  const setOverrides = useSettings((x) => x.setOverrides);
  const own = overrides[hostId];
  const overridden = own?.visibility !== undefined;
  return (
    <SettingsSection title={t('fileVisibilityDeviceSection')}>
      <Text style={[LumenType.meta, { paddingVertical: 12 }]} muted>{t('followsAppDefaultVisibility')}</Text>
      <ToggleRow
        icon={Copy}
        tint={c.primary}
        title={t('overrideForDevice')}
        subtitle={overridden ? t('usingDeviceVisibility') : t('usingAppDefault')}
        value={overridden}
        onChange={(on) => void setOverrides({ ...overrides, [hostId]: withVisibilityOverride(own, effective, on) })}
      />
      {overridden ? <View style={{ paddingTop: 8, paddingBottom: 12 }}><VisibilityEditor prefs={effective} onChange={(next) => void setOverrides({ ...overrides, [hostId]: { ...own, visibility: next } })} /></View> : null}
    </SettingsSection>
  );
}

function DeviceRow({ device: d, isAdmin, busy, onDisconnect, onApps, onGrants }: { device: Device; isAdmin: boolean; busy: boolean; onDisconnect: () => void; onApps: (c: { viewApps?: boolean; launchApps?: boolean }) => void; onGrants: (c: Partial<Record<FileCapability, boolean>>) => void }) {
  const c = useScheme();
  const status = d.current
    ? t('thisDevice')
    : [d.revoked ? t('revokedStatus') : t('activeStatus'), d.lastAddress, d.lastVersion ? `v${d.lastVersion}` : '', formatRelative(new Date(d.lastSeen))].filter(Boolean).join(' · ');
  const roles = useRoles();
  const statusColor = d.revoked ? c.error : roles.safe;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 12, paddingVertical: 12 }}>
      <RowBadge icon={d.revoked ? Unplug : Smartphone} tint={d.revoked ? c.error : c.primary} />
      <View style={{ flex: 1, gap: 2 }}>
        <Text style={LumenType.rowTitle}>{d.label}</Text>
        <Text style={LumenType.meta} color={statusColor}>{status}</Text>
        {d.jailRoot ? <Text style={LumenType.meta} color={c.tertiary}>{t('limitedTo', { path: d.jailRoot })}</Text> : null}
        {!d.current ? <Text muted style={LumenType.meta}>{t('managedOnPc')}</Text> : null}
        {isAdmin && hasAppCapabilities(d) ? (
          <View style={{ paddingTop: 10 }}>
            <Text style={LumenType.name}>{t('appAccessTitle')}</Text>
            <Text muted style={LumenType.meta}>{t('appAccessDefaultOffHint')}</Text>
            <SmallSwitchRow label={t('viewAppsLabel')} subtitle={t('viewAppsDescription')} value={d.viewApps ?? false} disabled={busy} onChange={(v) => onApps({ viewApps: v })} />
            <SmallSwitchRow label={t('launchAppsLabel')} subtitle={t('launchAppsDescription')} value={(d.viewApps ?? false) && (d.launchApps ?? false)} disabled={busy || !(d.viewApps ?? false)} onChange={(v) => onApps({ launchApps: v })} />
          </View>
        ) : null}
        {isAdmin && hasFileCapabilities(d) && !d.viaLogin ? (
          <View style={{ paddingTop: 10 }}>
            <Text style={LumenType.name}>File access</Text>
            <Text style={LumenType.meta} muted>New paired devices can browse only. Upload also allows overwriting existing files. Read-only mode, allowed folders, and global sharing still apply.</Text>
            {FILE_CAPABILITIES.map((cap) => (
              <SmallSwitchRow key={cap} label={FILE_CAPABILITY_LABEL[cap]} value={d[cap] ?? false} disabled={busy} onChange={(v) => onGrants({ [cap]: v })} />
            ))}
          </View>
        ) : null}
      </View>
      {d.current ? (
        <Pressable onPress={onDisconnect} accessibilityLabel={t('disconnectButton')} style={{ paddingHorizontal: 10, minHeight: 48, justifyContent: 'center' }}>
          <Text style={LumenType.pill} color={c.error}>{t('disconnectButton')}</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function DriveRow({ drive }: { drive: Drive }) {
  const c = useScheme();
  const total = drive.totalBytes ?? 0;
  const free = drive.freeBytes ?? 0;
  const name = drive.label ? drive.label : drive.path;
  const p = { used: formatSize(total - free), total: formatSize(total), free: formatSize(free) };
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 12 }}>
      <RowBadge icon={drive.isOS ? MemoryStick : HardDrive} tint={c.primary} />
      <View style={{ flex: 1 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs }}>
          <Text numberOfLines={1} style={[LumenType.rowTitle, { flexShrink: 1 }]}>{name}</Text>
          {drive.isOS ? (
            <View style={{ paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8, backgroundColor: mix(c.primary, c.surfaceContainer, 0.15) }}>
              <Text style={LumenType.pill} color={c.primary}>{t('osLabel')}</Text>
            </View>
          ) : null}
        </View>
        <Text muted style={LumenType.meta}>{drive.isOS ? t('driveCapacityLineOs', p) : t('driveCapacityLine', p)}</Text>
      </View>
    </View>
  );
}

