import { CloudOff, HardDrive, ShieldCheck, ShieldX } from 'lucide-react-native';
import { useCallback, useEffect, useState } from 'react';
import { ScrollView, View } from 'react-native';

import type { AgentStatus, Drive, Health } from '../../core/api/models';
import { formatSize } from '../../core/format';
import type { Host } from '../../core/models/host';
import { EmptyState, OfflineBanner, ErrorRetry, ListingSkeleton, Loading, PageHead, Pressable, StatePill, Text, TopBar } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { AgentApiError } from '../../core/api/agentClient';
import { CertPinMismatch } from '../../core/api/pin';
import { clientForHost, keyValue } from '../../services';
import { humanizeError } from '../pairing/pairingService';
import { t } from '../../i18n';
import { folderLabel } from './paths';
import { resolveRoots, type RootResolution } from './rootResolution';
import { loadRoots, saveRoots } from './rootsCache';

/** Resolves what this device may browse on a host (allowed roots, Windows drives, or nothing) and lets the user pick a root. */
export function HostRootView({ host, health, initialPath, onSelectRoot }: { host: Host; health: Health | null; initialPath?: string; onSelectRoot: (root: string, initialPath?: string) => void }) {
  const [res, setRes] = useState<RootResolution | null>(null);
  const [drives, setDrives] = useState<Drive[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [offline, setOffline] = useState(false);
  const [untrusted, setUntrusted] = useState(false);

  const load = useCallback(() => {
    let live = true;
    (async () => {
      const client = await clientForHost(host);
      try {
        const settings: AgentStatus = await client.status();
        const r = resolveRoots(settings, health, initialPath);
        const drives = r.kind === 'drives' ? await client.drives() : null;
        if (!settings.accessDenied) await saveRoots(keyValue, host.id, { roots: settings.roots, os: health?.os, drives: drives ?? undefined }).catch(() => {});
        return { r, drives, offline: false };
      } catch (e) {
        // Unreachable host: fall back to what it last allowed, so cached listings stay browsable.
        const cached = e instanceof AgentApiError && e.code === 'CONNECTION' ? await loadRoots(keyValue, host.id) : null;
        if (!cached) throw e;
        return { r: resolveRoots({ roots: cached.roots, accessDenied: false }, health ?? (cached.os ? { os: cached.os } : null), initialPath), drives: cached.drives ?? null, offline: true };
      }
    })().then(
      ({ r, drives, offline: off }) => {
        if (!live) return;
        setOffline(off);
        if (r.kind === 'select') return onSelectRoot(r.rootPath, r.initialPath);
        setDrives(drives);
        setRes(r);
      },
      (e) => {
        if (!live) return;
        setUntrusted(e instanceof CertPinMismatch);
        setError(humanizeError(e));
      },
    );
    return () => {
      live = false;
    };
  }, [host, health, initialPath, onSelectRoot]);
  useEffect(() => load(), [load]);

  if (error) return <Shell host={host} title="Files" offline untrusted={untrusted}><ErrorRetry
          message={t('errorLabel', { error })}
          onRetry={() => {
            setError(null);
            setUntrusted(false);
            setRes(null);
            load();
          }}
        /></Shell>;
  if (!res) return <Shell host={host} title="Files"><Loading /></Shell>;
  if (res.kind === 'denied') return <Shell host={host} title="Files"><Denied /></Shell>;
  const banner = offline ? <OfflineBanner text={t('offlineBannerText')} /> : null;
  if (res.kind === 'drives') return <Shell host={host} title="Drives" subtitle={drives ? `${drives.length} ${drives.length === 1 ? 'drive' : 'drives'}` : undefined} offline={offline}>{banner}{drives === null ? <ListingSkeleton /> : drives.length === 0 ? <EmptyState /> : <DrivesList drives={drives} onSelect={(p) => onSelectRoot(p)} />}</Shell>;
  if (res.kind === 'roots') return <Shell host={host} title="Shared folders" subtitle={`${res.roots.length} ${res.roots.length === 1 ? 'folder' : 'folders'}`} offline={offline}>{banner}<Roots roots={res.roots} unavailablePath={res.unavailablePath} onSelect={(r) => onSelectRoot(r)} /></Shell>;
  return null;
}

function Shell({ host, title, subtitle, offline, untrusted, children }: { host: Host; title: string; subtitle?: string; offline?: boolean; untrusted?: boolean; children: React.ReactNode }) {
  return (
    <View style={{ flex: 1 }}>
      <TopBar context={`${host.label} · Files`} sub="Workspace" right={untrusted ? <StatePill label="Not trusted" tone="warn" icon={ShieldX} /> : offline ? <StatePill label="Offline" tone="warn" icon={CloudOff} /> : <StatePill label="Connected" tone="safe" icon={ShieldCheck} />} />
      <PageHead title={title} subtitle={subtitle} />
      <View style={{ flex: 1 }}>{children}</View>
    </View>
  );
}

function Denied() {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
      <ShieldX size={48} color={c.error} />
      <Text style={{ textAlign: 'center' }}>This device has no file access on this computer.</Text>
    </View>
  );
}

/** A root or drive as a Lumen `.collection` card: name in the folder colour, the real path or capacity under it. */
function CollectionCard({ icon, name, meta, onPress }: { icon?: React.ReactNode; name: string; meta: string; onPress: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  return (
    <Pressable onPress={onPress} accessibilityLabel={name}>
      <View style={{ minHeight: 64, flexDirection: 'row', alignItems: 'center', gap: 12, padding: LumenSize.cardPadding, borderRadius: LumenSize.cardRadius, backgroundColor: c.surfaceContainer }}>
        {icon}
        <View style={{ flex: 1 }}>
          <Text style={LumenType.title} color={roles.folder} numberOfLines={1}>{name}</Text>
          <Text style={[LumenType.meta, { marginTop: 2 }]} muted numberOfLines={2}>{meta}</Text>
        </View>
      </View>
    </Pressable>
  );
}

function Roots({ roots, unavailablePath, onSelect }: { roots: string[]; unavailablePath?: string; onSelect: (r: string) => void }) {
  const c = useScheme();
  const sorted = [...roots].sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()));
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 18, gap: 10 }}>
      {unavailablePath ? <Text style={LumenType.meta} color={c.error}>This saved location is no longer available to this device. Choose a shared folder below.</Text> : null}
      {sorted.map((r) => (
        <CollectionCard key={r} name={folderLabel(r)} meta={r} onPress={() => onSelect(r)} />
      ))}
    </ScrollView>
  );
}

function DrivesList({ drives, onSelect }: { drives: Drive[]; onSelect: (p: string) => void }) {
  const c = useScheme();
  const roles = useRoles();
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 18, gap: 10 }}>
      {drives.map((d) => {
        const label = d.label ? d.label : folderLabel(d.path);
        const cap = d.totalBytes != null && d.freeBytes != null;
        return (
          <CollectionCard
            key={d.path}
            icon={
              <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>
                <HardDrive size={22} color={roles.folder} />
              </View>
            }
            name={label}
            meta={cap ? `${formatSize(d.freeBytes)} free of ${formatSize(d.totalBytes)}  ·  ${d.path}` : d.path}
            onPress={() => onSelect(d.path)}
          />
        );
      })}
    </ScrollView>
  );
}
