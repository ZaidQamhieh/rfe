import { Stack, useRouter } from 'expo-router';
import { Code, FileText, Globe, Monitor, Play, RefreshCw, SearchX, Settings, ShieldAlert, Terminal, type LucideIcon } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, View } from 'react-native';

import { AgentApiError } from '../../core/api/agentClient';
import type { HostApp, HostAppCatalog } from '../../core/api/models';
import { AppBarIconButton, Button, ErrorRetry, GroupedCard, HintCard, PageHead, Pressable, SearchBar, StatePill, Text, TopBar, useDialogs, useToast } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useHostById } from '../hosts/useHostById';
import { humanizeError } from '../pairing/pairingService';
import { appIconKey, appMeta, filterApps, launchFailure, platformKey, type IconKey } from './appsLogic';

const ICONS: Record<IconKey, LucideIcon> = { browser: Globe, code: Code, office: FileText, media: Play, terminal: Terminal, system: Settings, monitor: Monitor };

type Failure = { kind: 'denied' | 'unsupported' | 'error'; message: string };
type Load = { catalog: HostAppCatalog } | { failure: Failure };

/**
 * Apps the computer chose to expose for one paired host (the Apps tab and the host route both render this). Launch sends
 * only the server-issued id and the agent re-checks it; the list is what `view_apps` allows (a 403 is the not-allowed
 * state) and the Run buttons are what `launch_apps` allows (`launchAllowed` from the same response).
 */
export function AppsScreen({ hostId, chrome = true }: { hostId: string; chrome?: boolean }) {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const dialogs = useDialogs();
  const host = useHostById(hostId);
  const [catalog, setCatalog] = useState<HostAppCatalog | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [query, setQuery] = useState('');
  const [launching, setLaunching] = useState<ReadonlySet<string>>(new Set());
  const [refreshing, setRefreshing] = useState(false);

  const fetchCatalog = useCallback(async (): Promise<Load> => {
    if (!host) return { failure: { kind: 'error', message: 'This computer is no longer paired.' } };
    try {
      return { catalog: await (await clientForHost(host)).listApps() };
    } catch (e) {
      if (e instanceof AgentApiError && e.statusCode === 403) return { failure: { kind: 'denied', message: '' } };
      if (e instanceof AgentApiError && e.code === 'APP_CATALOG_UNSUPPORTED') return { failure: { kind: 'unsupported', message: t('hostAppsUnsupported') } };
      return { failure: { kind: 'error', message: `${t('hostAppsLoadFailed')}\n${humanizeError(e)}` } };
    }
  }, [host]);
  const apply = useCallback((r: Load) => {
    if ('catalog' in r) {
      setCatalog(r.catalog);
      setFailure(null);
    } else {
      setFailure(r.failure);
    }
  }, []);
  useEffect(() => {
    if (host === undefined) return;
    let live = true;
    void fetchCatalog().then((r) => live && apply(r));
    return () => {
      live = false;
    };
  }, [apply, fetchCatalog, host]);

  const reload = () => {
    setCatalog(null);
    setFailure(null);
    void fetchCatalog().then(apply);
  };

  const launch = async (app: HostApp) => {
    if (launching.has(app.id) || !host) return;
    setLaunching((s) => new Set(s).add(app.id));
    try {
      await (await clientForHost(host)).launchApp(app.id);
      toast.success(t('hostAppLaunchStarted', { appName: app.name, hostLabel: host.label }));
    } catch (e) {
      const f = launchFailure(e);
      toast.error(t(f.key, { appName: app.name }));
      if (f.refresh) void fetchCatalog().then(apply);
    } finally {
      setLaunching((s) => {
        const next = new Set(s);
        next.delete(app.id);
        return next;
      });
    }
  };

  const more = async () => {
    const pick = await dialogs.choose({
      title: host?.label ?? t('hostAppsTitle'),
      options: [
        { value: 'refresh', label: t('hostAppsRefreshTooltip'), icon: <RefreshCw size={20} color={c.onSurface} /> },
        { value: 'settings', label: t('settingsTitle'), icon: <Settings size={20} color={c.onSurface} /> },
      ],
    });
    if (pick === 'refresh') reload();
    if (pick === 'settings') router.push({ pathname: '/host/[id]/settings', params: { id: hostId } });
  };

  const centered = (icon: React.ReactNode, title: string, message: string, action?: React.ReactNode) => (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 32, gap: 12 }}>
      {icon}
      <Text style={[LumenType.title, { textAlign: 'center' }]}>{title}</Text>
      <Text style={[LumenType.meta, { textAlign: 'center' }]} color={c.onSurfaceVariant}>{message}</Text>
      {action}
    </View>
  );

  let body: React.ReactNode;
  if (host === null) {
    body = centered(<Monitor size={48} color={c.onSurfaceVariant} />, 'Computer not found', 'This computer is no longer paired.');
  } else if (failure?.kind === 'denied') {
    body = centered(<ShieldAlert size={48} color={c.primary} />, t('hostAppsAccessDeniedTitle'), t('hostAppsAccessDeniedMessage'), <Button kind="neutral" label={t('retryButton')} onPress={reload} />);
  } else if (failure) {
    body = <ErrorRetry message={failure.message} onRetry={reload} />;
  } else if (catalog === null) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 }}>
        <ActivityIndicator color={c.primary} />
        <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('hostAppsLoading')}</Text>
      </View>
    );
  } else if (catalog.apps.length === 0) {
    body = centered(<Monitor size={48} color={c.onSurfaceVariant} />, t('hostAppsEmptyTitle'), t('hostAppsEmptyMessage'));
  } else {
    const shown = filterApps(catalog.apps, query);
    body = (
      <FlatList
        data={shown}
        keyExtractor={(a) => a.id}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            colors={[c.primary]}
            progressBackgroundColor={c.surfaceContainerHigh}
            onRefresh={async () => {
              setRefreshing(true);
              apply(await fetchCatalog());
              setRefreshing(false);
            }}
          />
        }
        contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24, gap: 10 }}
        ListHeaderComponent={
          <View style={{ gap: 12, marginBottom: 2 }}>
            {catalog.launchAllowed ? null : <HintCard kind="warning" text={t('hostAppsLaunchPermissionOffHint')} />}
            <SearchBar value={query} onChange={setQuery} placeholder={t('hostAppsSearchHint')} />
            {query.trim() !== '' ? (
              <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>{t('hostAppsMatches', { count: shown.length })}</Text>
            ) : null}
          </View>
        }
        ListEmptyComponent={
          <View style={{ alignItems: 'center', gap: 6, padding: 24 }}>
            <SearchX size={32} color={c.onSurfaceVariant} />
            <Text style={LumenType.rowTitle}>{t('hostAppsNoMatchesTitle')}</Text>
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('hostAppsNoMatchesMessage')}</Text>
          </View>
        }
        renderItem={({ item }) => <AppCard app={item} launching={launching.has(item.id)} launchAllowed={catalog.launchAllowed} onRun={() => void launch(item)} />}
      />
    );
  }

  const subtitle = catalog ? `${t('hostAppsCount', { count: catalog.apps.length })} · ${t(platformKey(catalog.platform))}` : undefined;
  return (
    <View style={{ flex: 1 }}>
      {chrome ? (
        <>
          <TopBar
            context={`${host?.label ?? ''}${host ? ' · ' : ''}Apps`}
            sub="Host workspace"
            right={catalog ? <StatePill label={catalog.launchAllowed ? 'Can launch' : 'View only'} tone={catalog.launchAllowed ? 'safe' : 'muted'} /> : undefined}
            onMore={host ? () => void more() : undefined}
          />
          <PageHead title="Applications" subtitle={subtitle} />
        </>
      ) : (
        <Stack.Screen
          options={{
            headerRight: () => (
              <AppBarIconButton label={t('hostAppsRefreshTooltip')} onPress={reload}>
                <RefreshCw size={19} color={c.onSurfaceVariant} />
              </AppBarIconButton>
            ),
          }}
        />
      )}
      {body}
    </View>
  );
}

/** `.app-card` + `.app-row`: a 46 dp role-tinted icon tile, name and real meta, then the Run button. */
function AppCard({ app, launching, launchAllowed, onRun }: { app: HostApp; launching: boolean; launchAllowed: boolean; onRun: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  const key = appIconKey(app);
  const Icon = ICONS[key];
  const tone = appTone(key, roles, c.onSurfaceVariant);
  const canRun = launchAllowed && app.launchable;
  const meta = appMeta(app, launchAllowed);
  return (
    <GroupedCard padded={false} style={{ padding: 12 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
        <View style={{ width: LumenSize.appIconTile, height: LumenSize.appIconTile, borderRadius: 14, alignItems: 'center', justifyContent: 'center', backgroundColor: mix(tone, c.surfaceContainerHigh, 0.19) }}>
          <Icon size={24} color={tone} />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={LumenType.name} numberOfLines={1}>{app.name}</Text>
          {meta ? <Text style={[LumenType.meta, { marginTop: 2 }]} color={c.onSurfaceVariant} numberOfLines={2}>{meta}</Text> : null}
        </View>
        <RunButton label={launching ? t('hostAppStartingButton') : t('hostAppRunButton')} off={!canRun} busy={launching} onPress={onRun} />
      </View>
    </GroupedCard>
  );
}

/** `.run` / `.run.off`: flat, green (`safe`) with button text, or the raised surface with muted text when it cannot run. */
function RunButton({ label, off, busy, onPress }: { label: string; off: boolean; busy: boolean; onPress: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  const fg = off ? c.onSurfaceVariant : c.onPrimary;
  return (
    <Pressable onPress={off || busy ? undefined : onPress} disabled={off || busy} accessibilityLabel={off ? `${label} unavailable` : label} accessibilityState={{ disabled: off, busy }} hitSlop={4}>
      <View style={{ minWidth: 76, minHeight: 48, paddingHorizontal: 14, borderRadius: 12, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 4, backgroundColor: off ? c.surfaceContainerHigh : roles.safe, opacity: busy ? 0.7 : 1 }}>
        {busy ? <ActivityIndicator size="small" color={fg} /> : off ? null : <Play size={16} color={fg} />}
        <Text style={LumenType.pill} color={fg} numberOfLines={1}>{off ? 'Off' : label}</Text>
      </View>
    </Pressable>
  );
}

function appTone(key: IconKey, roles: ReturnType<typeof useRoles>, neutral: string): string {
  switch (key) {
    case 'code':
      return roles.transfer;
    case 'browser':
      return roles.warn;
    case 'media':
      return roles.route;
    case 'terminal':
      return roles.folder;
    case 'office':
      return roles.doc;
    default:
      return neutral;
  }
}
