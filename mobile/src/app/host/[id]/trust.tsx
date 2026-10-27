import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { Activity, Download, Folder, LayoutGrid, Link as LinkIcon, Lock, Monitor, Network, Pencil, Play, RefreshCw, ScrollText, Settings, ShieldAlert, ShieldCheck, Trash2, Upload, UserRound, type LucideIcon } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, RefreshControl, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { canManageHost, hasAppCapabilities, type AgentStatus, type AuditEntry, type Device } from '../../../core/api/models';
import { AgentApiError } from '../../../core/api/agentClient';
import { CertPinMismatch, normalizeFingerprint } from '../../../core/api/pin';
import { formatDate, formatRelative } from '../../../core/format';
import type { Host } from '../../../core/models/host';
import { GroupedCard, IconTile, Loading, PageHead, SectionLabel, StatePill, Text, useDialogs } from '../../../design/components';
import { LumenSize, LumenType } from '../../../design/lumen';
import { useRoles, useScheme } from '../../../design/theme';
import { MethodRow, MetaPill, PermissionRow, StackTopBar } from '../../../features/hosts/LumenRows';
import { useHostById } from '../../../features/hosts/useHostById';
import { buildPermissions, type AppsProbe, identityState, PERMISSION_COPY, type IdentityState, type PermissionKey } from '../../../features/hosts/trustLogic';
import { humanizeError } from '../../../features/pairing/pairingService';
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
  if (host === null) {
    return (
      <View style={{ flex: 1 }}>
        <Stack.Screen options={{ headerShown: false }} />
        <StackTopBar context="Trust" />
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <Text muted>This computer is no longer paired.</Text>
        </View>
      </View>
    );
  }
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
  const goAudit = () => router.push({ pathname: '/host/[id]/audit', params: { id: host.id } });
  const goSettings = () => router.push({ pathname: '/host/[id]/settings', params: { id: host.id } });
  const goConnect = () => router.push({ pathname: '/host/[id]/connect', params: { id: host.id } });

  const more = async () => {
    const picked = await dialogs.choose<'refresh' | 'audit' | 'connect' | 'settings'>({
      title: host.label,
      options: [
        { value: 'refresh', label: t('refreshTooltip'), icon: <RefreshCw size={18} color={c.onSurfaceVariant} /> },
        { value: 'audit', label: t('activityLogTitle'), icon: <ScrollText size={18} color={c.onSurfaceVariant} /> },
        { value: 'connect', label: 'Connection', icon: <Activity size={18} color={c.onSurfaceVariant} /> },
        { value: 'settings', label: t('settingsMenuItem'), icon: <Settings size={18} color={c.onSurfaceVariant} /> },
      ],
    });
    if (picked === 'refresh') await reload();
    else if (picked === 'audit') goAudit();
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

  const headline = IDENTITY_COPY[identity];
  const pill =
    identity === 'verified' ? <StatePill label="Protected" icon={ShieldCheck} tone="safe" /> : identity === 'checking' ? <StatePill label="Checking" tone="muted" /> : identity === 'unreachable' ? <StatePill label="Offline" tone="muted" /> : <StatePill label="Not verified" icon={ShieldAlert} tone="warn" />;
  const pinnedLine =
    snap.pinned === false
      ? 'No identity is pinned for this computer. Pair it again to pin it.'
      : snap.pinned === null
        ? 'Checking the saved identity…'
        : loaded?.me?.created
          ? `Identity pinned on this phone when you paired, ${formatDate(new Date(loaded.me.created))}`
          : 'Identity pinned on this phone when you paired';

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StackTopBar context={`${host.label} · Trust`} sub="Identity" right={pill} onMore={more} />
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
          <PageHead title="Trust" subtitle="Review this phone’s identity and access." />
        </View>

        <GroupedCard style={{ gap: 12 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
            <IconTile icon={identity === 'verified' || identity === 'checking' ? ShieldCheck : ShieldAlert} color={identity === 'verified' ? roles.safe : identity === 'checking' || identity === 'unreachable' ? c.onSurfaceVariant : roles.warn} size={46} radius={16} />
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
            {snap.outcome === 'loading' && snap.loaded === null ? <ActivityIndicator style={{ alignSelf: 'flex-start', marginTop: 8 }} color={c.primary} /> : null}
          </View>
          {snap.error ? (
            <Text style={LumenType.meta} color={identity === 'mismatch' ? roles.warn : c.onSurfaceVariant}>
              {snap.error}
            </Text>
          ) : null}
        </GroupedCard>

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="This phone can" />
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
                    right={<MetaPill label={p.allowed === null ? 'Not reported' : p.allowed ? 'Allowed' : 'Not allowed'} strong={p.allowed === true} />}
                  />
                );
              })}
            </GroupedCard>
            {loaded.me?.jailRoot ? (
              <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
                {t('limitedTo', { path: loaded.me.jailRoot })}
              </Text>
            ) : null}
          </>
        ) : snap.outcome === 'loading' ? (
          <View style={{ paddingVertical: 24, alignItems: 'center' }}>
            <ActivityIndicator color={c.primary} />
          </View>
        ) : (
          <GroupedCard>
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>
              Access is shown once the computer answers. Pull down to try again.
            </Text>
          </GroupedCard>
        )}

        {loaded ? (
          <>
            <View style={{ marginTop: 6 }}>
              <SectionLabel title="Access and safety" />
            </View>
            <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 2 }}>
              <PermissionRow icon={Lock} tone={loaded.status.readOnly ? roles.warn : c.onSurfaceVariant} title={t('readOnlyMode')} subtitle={loaded.status.readOnly ? t('writesRejected') : t('phoneCanModify')} right={<MetaPill label={loaded.status.readOnly ? 'On' : 'Off'} strong />} />
              <PermissionRow icon={LinkIcon} tone={roles.route} title={t('enableShareLinks')} subtitle={loaded.status.allowSharing ? t('shareLinksEnabledHint') : t('shareLinksDisabledHint')} right={<MetaPill label={loaded.status.allowSharing ? 'On' : 'Off'} strong />} />
              <PermissionRow
                icon={UserRound}
                tone={c.primary}
                title="Signed in with"
                subtitle={loaded.me ? (loaded.me.viaLogin ? 'A host account (owner)' : 'A one-time pairing code') : undefined}
                right={<MetaPill label={loaded.me ? (loaded.me.viaLogin ? 'Account' : 'Pairing code') : 'Not reported'} strong />}
              />
              <PermissionRow
                icon={Monitor}
                tone={c.onSurfaceVariant}
                title="Paired"
                subtitle={loaded.me?.created ? `${formatDate(new Date(loaded.me.created))}${loaded.me.lastVersion ? ` · app v${loaded.me.lastVersion}` : ''}` : undefined}
                last
                right={<MetaPill label={loaded.me?.created ? formatRelative(new Date(loaded.me.created)) : 'Not reported'} strong />}
              />
            </GroupedCard>
          </>
        ) : null}

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="Recent activity" />
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
                right={<MetaPill label={formatRelative(e.at)} />}
              />
            ))}
          </GroupedCard>
        ) : loaded ? (
          <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
            {owner ? t('activityLogEmpty') : t('activityLogAdminOnly')}
          </Text>
        ) : null}
        <MethodRow icon={ScrollText} tone={roles.warn} title={t('activityLogTitle')} subtitle="Pairings, sign-ins, device changes and share links" onPress={goAudit} />

        <View style={{ marginTop: 6 }}>
          <SectionLabel title="More" />
        </View>
        <MethodRow icon={Network} tone={roles.route} title="Connection" subtitle="Routes, reachability and latency" onPress={goConnect} />
        <MethodRow icon={Settings} tone={c.primary} title={t('settingsMenuItem')} subtitle="Access, read-only mode, share links, paired devices and disconnecting" onPress={goSettings} />
      </ScrollView>
    </View>
  );
}

const IDENTITY_COPY: Record<IdentityState, { subtitle: string; pill: string; tone: 'safe' | 'warn' | 'muted' }> = {
  verified: { subtitle: 'Identity verified · this connection', pill: 'Trusted', tone: 'safe' },
  checking: { subtitle: 'Checking this computer’s identity…', pill: 'Checking', tone: 'muted' },
  mismatch: { subtitle: 'This computer does not match the identity pinned at pairing. Do not continue unless you changed its certificate.', pill: 'Mismatch', tone: 'warn' },
  unpinned: { subtitle: 'No identity is pinned for this computer', pill: 'Not pinned', tone: 'warn' },
  unreachable: { subtitle: 'Could not reach this computer to verify it', pill: 'Offline', tone: 'muted' },
};
