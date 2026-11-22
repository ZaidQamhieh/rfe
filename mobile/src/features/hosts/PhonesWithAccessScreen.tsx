import { Stack } from 'expo-router';
import { ChevronDown, ChevronUp, Trash2 } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, ScrollView, View } from 'react-native';

import { hasAppCapabilities, hasFileCapabilities, type AgentStatus, type Device, type DeviceGrant, type PairRequest } from '../../core/api/models';
import { humanizeError } from '../../core/humanizeError';
import type { Host } from '../../core/models/host';
import { Button, DuoIcon, Emoji, ErrorRetry, Loading, MockupSwitch, Pressable, Row, SectionLabel, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { Radii, Spacing } from '../../design/tokens';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { nextAppCapabilities, nextFileGrants, patchDevice } from '../settings/hostSettingsLogic';
import { canBlockPhone, canEditPhone, folderProblem, onlyInsideLabel, PENDING_POLL_MS, pendingPhoneSubtitle, phoneSubtitle, phoneToggles, withoutRequest } from './phonesLogic';

type Loaded = { status: AgentStatus; devices: Device[]; pending: PairRequest[] };

/** One plain switch line: a 15 text label at the left, the switch at the right, 48 high; the whole line toggles. */
function SwitchLine({ label, value, disabled, onChange }: { label: string; value: boolean; disabled?: boolean; onChange: (v: boolean) => void }) {
  return (
    <Pressable onPress={disabled ? undefined : () => onChange(!value)} disabled={disabled} accessibilityRole="switch" accessibilityLabel={label} accessibilityState={{ checked: value, disabled: !!disabled }} style={{ opacity: disabled ? 0.5 : 1 }} pressedScale={0.99}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md2, minHeight: 48, paddingHorizontal: Spacing.md }}>
        <Text style={{ flex: 1, fontSize: 15 }}>{label}</Text>
        <MockupSwitch value={value} />
      </View>
    </Pressable>
  );
}

/** A phone's "Technical details" fold, closed until opened: the last address, app version and phone id. */
function TechnicalFold({ device: d }: { device: Device }) {
  const c = useScheme();
  const [open, setOpen] = useState(false);
  const facts = [
    [t('phonesTechAddress'), d.lastAddress],
    [t('phonesTechVersion'), d.lastVersion],
    [t('phonesTechId'), d.id],
  ].filter((f) => !!f[1]);
  return (
    <View style={{ paddingHorizontal: Spacing.md }}>
      <Pressable onPress={() => setOpen((o) => !o)} accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={t('phonesTechnical')}>
        <View style={{ flexDirection: 'row', alignItems: 'center', minHeight: 40, gap: Spacing.sm }}>
          <Text style={[LumenType.meta, { flex: 1 }]} muted>
            {t('phonesTechnical')}
          </Text>
          <DuoIcon icon={open ? ChevronUp : ChevronDown} size={18} color={c.onSurfaceVariant} />
        </View>
      </Pressable>
      {open
        ? facts.map(([label, value]) => (
            <View key={label} style={{ paddingBottom: 6 }}>
              <Text style={LumenType.meta} muted>
                {label}
              </Text>
              <Text style={LumenType.meta} selectable>
                {value}
              </Text>
            </View>
          ))
        : null}
    </View>
  );
}

/**
 * Phones with access: every phone paired with this computer. An owner opens a phone to switch six plain things on or
 * off (see, save, send, rename or move, delete, share links), see the folder it is kept inside, block it or remove it.
 * Changes go to the computer through the pinned agent client (PATCH or DELETE on the phone's record).
 */
export function PhonesWithAccessScreen({ host }: { host: Host }) {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const [data, setData] = useState<Loaded | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [openId, setOpenId] = useState<string | null>(null);

  // Asks the computer for its status and phone list; the answer is applied by the caller so an unmounted screen ignores it.
  const fetchAll = useCallback(async (): Promise<{ ok: Loaded } | { error: string }> => {
    try {
      const client = await clientForHost(host);
      const [status, devices] = await Promise.all([client.status(), client.listDevices()]);
      // Only an owner may see who is waiting; a failure here leaves the list empty rather than hiding the phones.
      const pending = status.isAdmin ? await client.listPairRequests().catch(() => []) : [];
      return { ok: { status, devices, pending } };
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
  const load = useCallback(async () => apply(await fetchAll()), [apply, fetchAll]);
  useEffect(() => {
    let live = true;
    void fetchAll().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchAll]);

  // While an owner has the screen open, asks again for the waiting phones so a new request shows up without a pull to refresh.
  const isOwner = data?.status.isAdmin === true;
  useEffect(() => {
    if (!isOwner) return;
    let live = true;
    const timer = setInterval(() => {
      void clientForHost(host)
        .then((cl) => cl.listPairRequests())
        .then((pending) => live && setData((cur) => (cur ? { ...cur, pending } : cur)))
        .catch(() => undefined); // The next tick or a pull to refresh tries again; the screen keeps what it has.
    }, PENDING_POLL_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [host, isOwner]);

  const markBusy = (id: string, on: boolean) =>
    setBusy((b) => {
      const next = new Set(b);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  const failed = (e: unknown) => toast.error(t('updateFailed', { error: humanizeError(e) }));

  // Sends one change for a phone and, once the computer accepted it, mirrors it in the list.
  async function change(d: Device, send: (cl: Awaited<ReturnType<typeof clientForHost>>) => Promise<void>, patch: Partial<Device>) {
    markBusy(d.id, true);
    try {
      await send(await clientForHost(host));
      setData((cur) => (cur ? { ...cur, devices: patchDevice(cur.devices, d.id, patch) } : cur));
    } catch (e) {
      failed(e);
    } finally {
      markBusy(d.id, false);
    }
  }
  const setGrant = (d: Device, cap: DeviceGrant, on: boolean) => {
    if (!hasFileCapabilities(d)) return;
    const next = nextFileGrants(d, { [cap]: on });
    void change(d, (cl) => cl.updateDeviceFileCapabilities(d.id, next), next);
  };
  const setApps = (d: Device, flags: { viewApps?: boolean; launchApps?: boolean }) => {
    if (!hasAppCapabilities(d)) return;
    const next = nextAppCapabilities(d, flags);
    void change(d, (cl) => cl.updateDeviceAppCapabilities(d.id, next), next);
  };

  // Asks for a folder to keep the phone inside; blank lets it go anywhere. The computer checks the path and may refuse it.
  async function changeFolder(d: Device) {
    const value = await dialogs.prompt({
      title: t('phonesOnlyInsideTitle'),
      description: t('phonesOnlyInsideBody'),
      placeholder: t('phonesOnlyInsidePlaceholder'),
      initialValue: d.jailRoot,
      confirmLabel: t('phonesOnlyInsideSave'),
      allowEmpty: true,
      mono: true,
      validate: folderProblem,
    });
    if (value === null || value === d.jailRoot) return;
    void change(d, (cl) => cl.updateDeviceJailRoot(d.id, value), { jailRoot: value });
  }

  // Allow or Refuse for a waiting phone. The row leaves the list once the computer answered; after Allow the phone
  // list is reloaded so the new phone appears, and after a failure the lists are reloaded to drop a request that expired.
  async function answer(r: PairRequest, allow: boolean) {
    markBusy(r.id, true);
    try {
      const cl = await clientForHost(host);
      await (allow ? cl.approvePairRequest(r.id) : cl.rejectPairRequest(r.id));
      setData((cur) => (cur ? { ...cur, pending: withoutRequest(cur.pending, r.id) } : cur));
      if (allow) await load();
    } catch (e) {
      toast.error(t('phonesAllowFailed', { error: humanizeError(e) }));
      await load();
    } finally {
      markBusy(r.id, false);
    }
  }

  // Block keeps the phone on the list but shuts it out (revoke); Remove deletes its record for good (purge).
  async function drop(d: Device, purge: boolean) {
    const ok = await dialogs.confirm({
      title: purge ? t('phonesRemoveTitle') : t('phonesBlockTitle'),
      description: t(purge ? 'phonesRemoveBody' : 'phonesBlockBody', { label: d.label }),
      confirmLabel: purge ? t('phonesRemove') : t('phonesBlock'),
      cancelLabel: t('cancelButton'),
      destructive: true,
    });
    if (!ok) return;
    markBusy(d.id, true);
    try {
      await (await clientForHost(host)).deleteDevice(d.id, purge);
      if (purge) setData((cur) => (cur ? { ...cur, devices: cur.devices.filter((x) => x.id !== d.id) } : cur));
      else setData((cur) => (cur ? { ...cur, devices: patchDevice(cur.devices, d.id, { revoked: true }) } : cur));
      setOpenId(null);
    } catch (e) {
      failed(e);
    } finally {
      markBusy(d.id, false);
    }
  }

  if (error && !data) {
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ title: t('phonesTitle') }} />
        <ErrorRetry message={t('errorLabel', { error })} onRetry={() => void load()} />
      </View>
    );
  }
  if (!data) {
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ title: t('phonesTitle') }} />
        <Loading />
      </View>
    );
  }

  const viewerIsOwner = data.status.isAdmin;
  return (
    <ScrollView
      contentContainerStyle={{ paddingBottom: 48 }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={async () => {
            setRefreshing(true);
            await load();
            setRefreshing(false);
          }}
        />
      }
    >
      <Stack.Screen options={{ title: t('phonesTitle') }} />
      <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }]} muted>
        {host.label}
      </Text>
      {viewerIsOwner && data.pending.length > 0 ? (
        <>
          <View style={{ paddingHorizontal: Spacing.md2, paddingTop: Spacing.md2 }}>
            <SectionLabel title={t('phonesWaiting')} />
          </View>
          {data.pending.map((r) => (
            <View key={r.id}>
              <Row lead={<Emoji name="phone" size={32} />} title={r.label} subtitle={pendingPhoneSubtitle(r)} />
              <View style={{ flexDirection: 'row', gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingLeft: 72, paddingBottom: Spacing.sm }}>
                <Button size="s" kind="filled" label={t('phonesAllow')} disabled={busy.has(r.id)} onPress={() => void answer(r, true)} />
                <Button size="s" kind="tonal" label={t('phonesRefuse')} disabled={busy.has(r.id)} onPress={() => void answer(r, false)} />
              </View>
            </View>
          ))}
        </>
      ) : null}
      <View style={{ paddingHorizontal: Spacing.md2, paddingTop: Spacing.md2 }}>
        <SectionLabel title={t('phonesAllowed')} />
      </View>
      {!viewerIsOwner ? (
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm }]} muted>
          {t('phonesOwnerOnly')}
        </Text>
      ) : null}
      {data.devices.length === 0 ? (
        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md }]} muted>
          {t('phonesEmpty')}
        </Text>
      ) : null}
      {data.devices.map((d) => {
        const editable = canEditPhone(d, viewerIsOwner);
        const title = d.current ? `${d.label} ${t('phonesThisPhone')}` : d.label;
        const isOpen = openId === d.id;
        const working = busy.has(d.id);
        const lead = <Emoji name="phone" size={32} off={d.revoked} />;
        if (!editable && !(viewerIsOwner && d.revoked)) return <Row key={d.id} lead={lead} title={title} subtitle={phoneSubtitle(d)} />;
        return (
          <View key={d.id} style={isOpen ? { marginHorizontal: Spacing.sm, borderRadius: Radii.control, borderWidth: 1, borderColor: c.rule2, backgroundColor: c.surfaceContainer } : undefined}>
            <Row
              lead={lead}
              title={title}
              subtitle={phoneSubtitle(d)}
              onPress={() => setOpenId(isOpen ? null : d.id)}
              trailing={<DuoIcon icon={isOpen ? ChevronUp : ChevronDown} size={20} color={c.onSurfaceVariant} />}
              accessibilityLabel={`${title}. ${t('phonesRowHint')}`}
            />
            {isOpen ? (
              <View style={{ paddingBottom: Spacing.md2 }}>
                {editable ? (
                  <>
                    {phoneToggles(d).map((p) => (
                      <SwitchLine key={p.cap} label={p.label} value={p.on} disabled={working} onChange={(on) => setGrant(d, p.cap, on)} />
                    ))}
                    {hasAppCapabilities(d) ? (
                      <>
                        <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }]} muted>
                          {t('phonesApps')}
                        </Text>
                        <SwitchLine label={t('viewAppsLabel')} value={d.viewApps ?? false} disabled={working} onChange={(on) => setApps(d, { viewApps: on })} />
                        <SwitchLine label={t('launchAppsLabel')} value={(d.viewApps ?? false) && (d.launchApps ?? false)} disabled={working || !(d.viewApps ?? false)} onChange={(on) => setApps(d, { launchApps: on })} />
                      </>
                    ) : null}
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md2, paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }}>
                      <View style={{ flex: 1 }}>
                        <Text style={LumenType.meta} muted>
                          {t('phonesOnlyInside')}
                        </Text>
                        <Text style={{ fontSize: 15 }}>{onlyInsideLabel(d)}</Text>
                      </View>
                      <Button size="s" kind="text" label={t('phonesChange')} disabled={working} onPress={() => void changeFolder(d)} />
                    </View>
                  </>
                ) : null}
                <View style={{ flexDirection: 'row', gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }}>
                  {canBlockPhone(d) ? <Button size="s" kind="tonal" destructive label={t('phonesBlock')} disabled={working} onPress={() => void drop(d, false)} /> : null}
                  {!d.current ? <Button size="s" kind="tonal" destructive label={t('phonesRemove')} disabled={working} renderIcon={(k) => <Trash2 size={16} color={k} />} onPress={() => void drop(d, true)} /> : null}
                </View>
                <TechnicalFold device={d} />
              </View>
            ) : null}
          </View>
        );
      })}
    </ScrollView>
  );
}
