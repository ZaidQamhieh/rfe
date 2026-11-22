import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { Activity, Download, Folder, LayoutGrid, Link as LinkIcon, Lock, Monitor, Network, Pencil, Play, RefreshCw, ScrollText, Settings, ShieldCheck, Trash2, Upload, UserRound, type LucideIcon } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, RefreshControl, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { canManageHost, hasAppCapabilities, type AgentStatus, type AuditEntry, type Device } from '../../../core/api/models';
import { AgentApiError } from '../../../core/api/agentClient';
import { CertPinMismatch, normalizeFingerprint } from '../../../core/api/pin';
import { formatDate, relativeOrDate } from '../../../core/format';
import type { Host } from '../../../core/models/host';
import { GroupedCard, IconTile, Loading, PageHead, SectionLabel, StatePill, Text, useDialogs } from '../../../design/components';
import { LumenSize, LumenType } from '../../../design/lumen';
import { Spacing } from '../../../design/tokens';
import { useRoles, useScheme } from '../../../design/theme';
import { MethodRow, MetaPill, PermissionRow, StackTopBar } from '../../../features/hosts/LumenRows';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';
import { buildPermissions, type AppsProbe, identityState, PERMISSION_COPY, type IdentityState, type PermissionKey } from '../../../features/hosts/trustLogic';
import { humanizeError } from '../../../core/humanizeError';
import { ComputerStateView } from '../../../features/hosts/ComputerStateView';
import { GettingInBar, IdentityChangedView, TechnicalDetails } from '../../../features/pairing/GettingInViews';
import { useActiveHost } from '../../../state/activeHost';
import { auditLabel } from '../../../features/settings/hostSettingsLogic';
import { t } from '../../../i18n';
import { clientForHost, hostStore } from '../../../services';

type Loaded = { status: AgentStatus; me: Device | undefined; audit: AuditEntry[] | null; apps: AppsProbe };
type Snapshot = { pinned: boolean | null; outcome: 'loading' | 'ok' | 'pinMismatch' | 'failed'; loaded: Loaded | null; error: string | null };

const AUDIT_SHOWN = 4;

/** Trust: what this phone is to the computer. Identity pin, this phone's grants, safety switches and recent activity, from the host's real answers. */
export default function TrustRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone context={t('trustTitle')} />;
  return <Trust host={host} />;
}

/** What the apps endpoint itself says this phone may do: a catalog means it may view, `launchAllowed` says whether it may launch, a 403 means neither. */
async function probeApps(client: { listApps: () => Promise<{ launchAllowed: boolean }> }): Promise<AppsProbe> {
  try {
    const catalog = await client.listApps();
    return { view: true, launch: catalog.launchAllowed === true };
  } catch (e) {
    if (e instanceof AgentApiError && e.statusCode === 403) return { view: false, launch: false };
    return null;
  }
}

function Trust({ host }: { host: Host }) {
  const c = useScheme();
  const roles = useRoles();
  const router = useRouter();
  const dialogs = useDialogs();
  const insets = useSafeAreaInsets();
  const [snap, setSnap] = useState<Snapshot>({ pinned: null, outcome: 'loading', loaded: null, error: null });
  const [refreshing, setRefreshing] = useState(false);

  const fetchAll = useCallback(async (): Promise<Snapshot> => {
    // The pin lives in secure storage; only whether it exists is read here, never its value shown.
    const pinned = normalizeFingerprint(await hostStore.getPin(host.id)) !== null;
    try {
      const client = await clientForHost(host, true);
      const status = await client.status();
      const devices = await client.listDevices().catch(() => [] as Device[]);
      const audit = canManageHost(status) ? await client.audit({ limit: AUDIT_SHOWN }).catch(() => null) : null;
      const me = devices.find((d) => d.current);
      // Only the owner can read this phone's device record; otherwise ask the apps endpoint what it allows.
      const apps = me && hasAppCapabilities(me) ? null : await probeApps(client);
      return { pinned, outcome: 'ok', loaded: { status, me, audit, apps }, error: null };
    } catch (e) {
      return { pinned, outcome: e instanceof CertPinMismatch ? 'pinMismatch' : 'failed', loaded: null, error: humanizeError(e) };
    }
  }, [host]);

  useEffect(() => {
    let live = true;
    void fetchAll().then((s) => live && setSnap(s));
    return () => {
      live = false;
    };
  }, [fetchAll]);
  const reload = async () => {
    setSnap((s) => ({ ...s, outcome: 'loading' }));
    setSnap(await fetchAll());
  };

  const identity = identityState({ pinned: snap.pinned, outcome: snap.outcome });
  const { loaded } = snap;
  const owner = loaded ? canManageHost(loaded.status) : false;
  const goSettings = () => router.push({ pathname: '/host/[id]/settings', params: { id: host.id } });
  const goConnect = () => router.push({ pathname: '/host/[id]/connect', params: { id: host.id } });

  const more = async () => {
    const picked = await dialogs.choose<'refresh' | 'connect' | 'settings'>({
      title: host.label,
      options: [
        { value: 'refresh', label: t('refreshTooltip'), icon: <RefreshCw size={18} color={c.onSurfaceVariant} /> },
        { value: 'connect', label: t('trustConnection'), icon: <Activity size={18} color={c.onSurfaceVariant} /> },
        { value: 'settings', label: t('settingsMenuItem'), icon: <Settings size={18} color={c.onSurfaceVariant} /> },
      ],
    });
    if (picked === 'refresh') await reload();
    else if (picked === 'connect') goConnect();
    else if (picked === 'settings') goSettings();
  };

  const icons: Record<PermissionKey, { icon: LucideIcon; tone: string }> = {
    browse: { icon: Folder, tone: roles.folder },
    download: { icon: Download, tone: roles.transfer },
    upload: { icon: Upload, tone: roles.transfer },
    modify: { icon: Pencil, tone: roles.doc },
    delete: { icon: Trash2, tone: roles.warn },
    share: { icon: LinkIcon, tone: roles.route },
    viewApps: { icon: LayoutGrid, tone: c.primary },
    launchApps: { icon: Play, tone: roles.transfer },
  };

  /** Forget the computer after one confirmation; its saved identity, token and cached files go with it. */
  const forget = async () => {
    const ok = await dialogs.confirm({ title: t('forgetComputerTitle'), description: t('forgetComputerConfirm', { hostLabel: host.label }), confirmLabel: t('forgetButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    await hostStore.removeHost(host.id);
    if (useActiveHost.getState().active?.host.id === host.id) useActiveHost.getState().setActive(null);
    router.dismissTo('/');
  };

  if (identity === 'mismatch' || identity === 'unpinned') {
    // Addresses, the error text and (for an owner) the last few account events go under "Technical details", closed.
    const activity = loaded && owner && loaded.audit ? loaded.audit.slice(0, AUDIT_SHOWN).map((e) => `${auditLabel(e.action)} · ${relativeOrDate(e.at)}`) : [];
    const details = [host.address, snap.error ?? '', ...(activity.length > 0 ? [t('trustActivityTitle'), ...activity] : [])];
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ headerShown: false }} />
        <IdentityChangedView name={host.label} kind={identity === 'mismatch' ? 'changed' : 'unknown'} details={details} onAddAgain={() => router.push('/pair')} onForget={() => void forget()} onNotNow={() => router.back()} />
      </View>
    );
  }
  if (identity === 'unreachable') {
    return (
      <View style={{ flex: 1, backgroundColor: c.surface }}>
        <Stack.Screen options={{ headerShown: false }} />
        <GettingInBar onClose={() => router.back()} back />
        <ComputerStateView kind="lost" name={host.label} onRetry={() => void reload()} onCheck={goConnect} />
        <TechnicalDetails lines={[host.address, snap.error ?? '']} />
      </View>
    );
  }

  const headline = IDENTITY_COPY[identity];
  const pill = identity === 'verified' ? <StatePill label={t('trustProtected')} icon={ShieldCheck} tone="safe" /> : <StatePill label={t('trustChecking')} tone="muted" />;
  const pinnedLine =
    snap.pinned === null
      ? t('trustCheckingSaved')
      : loaded?.me?.created
        ? t('trustPinnedWithDate', { date: formatDate(new Date(loaded.me.created)) })
        : t('trustPinned');

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StackTopBar context={t('trustContext', { host: host.label })} sub={t('trustIdentitySub')} right={pill} onMore={more} />
      <ScrollView
        style={{ flex: 1 }}
        contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: insets.bottom + 28, gap: 10 }}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={async () => {
              setRefreshing(true);
              setSnap(await fetchAll());
              setRefreshing(false);
            }}
          />
        }
      >
        <View style={{ marginHorizontal: -18 }}>
          <PageHead title={t('trustTitle')} subtitle={t('trustPageSubtitle')} />
        </View>

        <GroupedCard style={{ gap: Spacing.md2 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md2 }}>
            <IconTile icon={ShieldCheck} color={identity === 'verified' ? roles.safe : c.onSurfaceVariant} size={46} radius={16} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Text style={LumenType.title} numberOfLines={1}>
                {host.label}
              </Text>
              <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant} numberOfLines={3}>
                {headline.subtitle}
              </Text>
            </View>
            <StatePill label={headline.pill} tone={headline.tone} />
          </View>
          <View style={{ padding: 14, borderRadius: LumenSize.tileRadius, backgroundColor: c.surfaceContainerHigh }}>
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>
              {pinnedLine}
            </Text>
            {snap.outcome === 'loading' && snap.loaded === null ? <ActivityIndicator style={{ alignSelf: 'flex-start', marginTop: Spacing.sm }} color={c.primary} /> : null}
          </View>
          {snap.error ? (
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>
              {snap.error}
            </Text>
          ) : null}
        </GroupedCard>

        <View style={{ marginTop: 6 }}>
          <SectionLabel title={t('trustThisPhoneCan')} />
        </View>
        {loaded ? (
          <>
            <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 2 }}>
              {buildPermissions(loaded.status, loaded.me, loaded.apps).map((p, i, all) => {
                const copy = PERMISSION_COPY[p.key];
                const v = icons[p.key];
                return (
                  <PermissionRow
                    key={p.key}
                    icon={v.icon}
                    tone={v.tone}
                    title={copy.title}
                    subtitle={p.note ?? copy.subtitle}
                    last={i === all.length - 1}
                    right={<MetaPill label={p.allowed === null ? t('trustNotReported') : p.allowed ? t('phonesAllowed') : t('trustNotAllowed')} strong={p.allowed === true} />}
                  />
                );
              })}
            </GroupedCard>
            {loaded.me?.jailRoot ? (
              <Text style={[LumenType.meta, { paddingHorizontal: Spacing.xs }]} color={c.onSurfaceVariant}>
                {t('limitedTo', { path: loaded.me.jailRoot })}
              </Text>
            ) : null}
          </>
        ) : snap.outcome === 'loading' ? (
          <View style={{ paddingVertical: Spacing.lg, alignItems: 'center' }}>
            <ActivityIndicator color={c.primary} />
          </View>
        ) : (
          <GroupedCard>
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>
              {t('trustAccessShownLater')}
            </Text>
          </GroupedCard>
        )}

        {loaded ? (
          <>
            <View style={{ marginTop: 6 }}>
              <SectionLabel title={t('trustAccessAndSafety')} />
            </View>
            <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 2 }}>
              <PermissionRow icon={Lock} tone={loaded.status.readOnly ? roles.warn : c.onSurfaceVariant} title={t('readOnlyMode')} subtitle={loaded.status.readOnly ? t('writesRejected') : t('phoneCanModify')} right={<MetaPill label={loaded.status.readOnly ? t('switchOn') : t('switchOff')} strong />} />
              <PermissionRow icon={LinkIcon} tone={roles.route} title={t('enableShareLinks')} subtitle={loaded.status.allowSharing ? t('shareLinksEnabledHint') : t('shareLinksDisabledHint')} right={<MetaPill label={loaded.status.allowSharing ? t('switchOn') : t('switchOff')} strong />} />
              <PermissionRow
                icon={UserRound}
                tone={c.primary}
                title={t('trustSignedInWith')}
                subtitle={loaded.me ? (loaded.me.viaLogin ? t('trustHostAccount') : t('trustOneTimeCode')) : undefined}
                right={<MetaPill label={loaded.me ? (loaded.me.viaLogin ? t('trustAccount') : t('pairingCodeLabel')) : t('trustNotReported')} strong />}
              />
              <PermissionRow
                icon={Monitor}
                tone={c.onSurfaceVariant}
                title={t('trustPaired')}
                subtitle={loaded.me?.created ? `${formatDate(new Date(loaded.me.created))}${loaded.me.lastVersion ? ` · ${t('trustAppVersion', { version: loaded.me.lastVersion })}` : ''}` : undefined}
                last
                right={<MetaPill label={loaded.me?.created ? relativeOrDate(new Date(loaded.me.created)) : t('trustNotReported')} strong />}
              />
            </GroupedCard>
          </>
        ) : null}

        <View style={{ marginTop: 6 }}>
          <SectionLabel title={t('trustActivityTitle')} />
        </View>
        {loaded && owner && loaded.audit && loaded.audit.length > 0 ? (
          <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 2 }}>
            {loaded.audit.slice(0, AUDIT_SHOWN).map((e, i, all) => (
              <PermissionRow
                key={e.id}
                icon={ScrollText}
                tone={c.onSurfaceVariant}
                title={auditLabel(e.action)}
                subtitle={[e.actor, e.target, e.detail].filter(Boolean).join(' · ') || undefined}
                last={i === all.length - 1}
                right={<MetaPill label={relativeOrDate(e.at)} />}
              />
            ))}
          </GroupedCard>
        ) : loaded ? (
          <Text style={[LumenType.meta, { paddingHorizontal: Spacing.xs }]} color={c.onSurfaceVariant}>
            {owner ? t('activityLogEmpty') : t('activityLogAdminOnly')}
          </Text>
        ) : null}

        <View style={{ marginTop: 6 }}>
          <SectionLabel title={t('trustMore')} />
        </View>
        <MethodRow icon={Network} tone={roles.route} title={t('trustConnection')} subtitle={t('trustConnectionSubtitle')} onPress={goConnect} />
        <MethodRow icon={Settings} tone={c.primary} title={t('settingsMenuItem')} subtitle={t('trustSettingsSubtitle')} onPress={goSettings} />
      </ScrollView>
    </View>
  );
}

/** The header copy for the two states that keep the full trust page; the others have their own screens above. */
const IDENTITY_COPY: Record<Extract<IdentityState, 'verified' | 'checking'>, { subtitle: string; pill: string; tone: 'safe' | 'muted' }> = {
  verified: { subtitle: t('trustVerifiedSubtitle'), pill: t('trustTrusted'), tone: 'safe' },
  checking: { subtitle: t('trustCheckingIdentity'), pill: t('trustChecking'), tone: 'muted' },
};
