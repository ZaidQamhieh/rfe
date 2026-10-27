import { Stack, useLocalSearchParams } from 'expo-router';
import { BadgeCheck, Check, File as FileIcon, FileArchive, FileText, Image as ImageIcon, type LucideIcon } from 'lucide-react-native';
import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, View } from 'react-native';

import { archiveExtensions, docExtensions, imageExtensions, videoExtensions } from '../core/entryCategory';
import { formatSize } from '../core/format';
import { Button, Pressable, SectionLabel, Text, useDialogs, useToast } from '../design/components';
import { useScheme } from '../design/theme';
import { Brand, FontFamily, Radii, Spacing } from '../design/tokens';
import { computeWaste, defaultDeletions, scanForDuplicates, withoutPaths, type DupGroup } from '../features/dups/dupFinder';
import { explorerFor } from '../features/explorer/useExplorer';
import { basenameOf } from '../features/explorer/paths';
import { humanizeError } from '../features/pairing/pairingService';
import { clientForHost } from '../services';
import { useActiveHost } from '../state/activeHost';

type Result = { groups: DupGroup[]; sizes: Record<string, number> };

/** Icon and tint from the extension alone (checksums carry no MIME type), matching the explorer's palette. */
function rowIconFor(path: string): { icon: LucideIcon; color: string | null } {
  const dot = path.lastIndexOf('.');
  const ext = dot < 0 ? '' : path.slice(dot + 1).toLowerCase();
  if (imageExtensions.has(ext) || videoExtensions.has(ext)) return { icon: ImageIcon, color: Brand.accent };
  if (archiveExtensions.has(ext)) return { icon: FileArchive, color: Brand.amber };
  if (ext === 'pdf') return { icon: FileText, color: Brand.red };
  if (docExtensions.has(ext)) return { icon: FileText, color: Brand.seed };
  return { icon: FileIcon, color: null };
}

/** Recursively hashes everything under a folder and offers to trash extra copies (the first copy of each group is kept). */
export default function Duplicates() {
  const c = useScheme();
  const toast = useToast();
  const dialogs = useDialogs();
  const { path } = useLocalSearchParams<{ path: string }>();
  const active = useActiveHost((s) => s.active);
  const [phase, setPhase] = useState<'idle' | 'scanning' | 'done'>('idle');
  const [scanned, setScanned] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<Result | null>(null);
  const [toDelete, setToDelete] = useState<Set<string>>(new Set());
  const [deleting, setDeleting] = useState(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  if (!active || !path) return null;
  const { host, rootPath } = active;

  async function scan() {
    setPhase('scanning');
    setError(null);
    setScanned(0);
    try {
      const r = await scanForDuplicates(await clientForHost(host), path, (n) => alive.current && setScanned(n));
      if (!alive.current) return;
      setResult(r);
      setToDelete(defaultDeletions(r.groups));
      setPhase('done');
    } catch (e) {
      if (!alive.current) return;
      setError(humanizeError(e));
      setPhase('idle');
    }
  }

  async function deleteSelected() {
    if (!result || toDelete.size === 0 || deleting) return;
    const ok = await dialogs.confirm({ title: 'Delete duplicates?', description: `Move ${toDelete.size} duplicate file${toDelete.size === 1 ? '' : 's'} to Trash? You can restore them from Trash.`, confirmLabel: 'Move to Trash', cancelLabel: 'Cancel', destructive: true });
    if (!ok) return;
    setDeleting(true);
    try {
      const deleted = [...toDelete];
      const freed = deleted.reduce((n, p) => n + (result.sizes[p] ?? 0), 0);
      await (await clientForHost(host)).delete(deleted);
      // The delete already succeeded; a failed background refresh of the open folder must not become an unhandled rejection.
      if (rootPath) void explorerFor(host, rootPath, () => clientForHost(host)).refresh().catch(() => {});
      if (!alive.current) return;
      setResult({ ...result, groups: withoutPaths(result.groups, new Set(deleted)) });
      setToDelete(new Set());
      toast.success(`Deleted ${deleted.length} duplicates — ${formatSize(freed)} freed`);
    } catch (e) {
      toast.error(humanizeError(e));
    } finally {
      if (alive.current) setDeleting(false);
    }
  }

  const toggle = (p: string) =>
    setToDelete((s) => {
      const next = new Set(s);
      if (!next.delete(p)) next.add(p);
      return next;
    });

  let body: React.ReactNode;
  if (phase === 'scanning') {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md }}>
        <ActivityIndicator />
        <Text>{`Scanning ${scanned} files...`}</Text>
      </View>
    );
  } else if (error) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md, padding: Spacing.lg }}>
        <Text accessibilityRole="alert" style={{ textAlign: 'center' }}>{`Error: ${error}`}</Text>
        <Button kind="filled" label="Retry" onPress={scan} />
      </View>
    );
  } else if (phase === 'idle') {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md, padding: Spacing.lg }}>
        <Text muted style={{ textAlign: 'center' }}>{`Looks for identical files under ${basenameOf(path) || path} by comparing checksums.`}</Text>
        <Button label="Scan for Duplicates" onPress={scan} />
      </View>
    );
  } else if (!result || result.groups.length === 0) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md }}>
        <BadgeCheck size={64} color={c.primary} />
        <Text>No duplicates found</Text>
      </View>
    );
  } else {
    const { groups, sizes } = result;
    body = (
      <ScrollView contentContainerStyle={{ paddingBottom: Spacing.lg }}>
        <View style={{ margin: Spacing.md, marginBottom: Spacing.sm, paddingVertical: Spacing.md, borderRadius: Radii.card, backgroundColor: c.surfaceContainerHigh, flexDirection: 'row', justifyContent: 'space-around' }}>
          <Stat value={String(groups.length)} label="groups" />
          <Stat value={formatSize(computeWaste(groups, sizes))} label="reclaimable" color={Brand.online} />
        </View>
        {groups.map((g) => (
          <View key={g.hash}>
            <View style={{ paddingHorizontal: Spacing.md }}>
              <SectionLabel title={`${basenameOf(g.paths[0])} · ${g.paths.length} copies · ${g.hash.slice(0, 4)}…`} />
            </View>
            {g.paths.map((p, i) => (
              <DupRow key={p} path={p} size={sizes[p]} kept={i === 0} selected={toDelete.has(p)} onToggle={i === 0 ? undefined : () => toggle(p)} />
            ))}
          </View>
        ))}
        <View style={{ padding: Spacing.md }}>
          <Button label={`Delete ${toDelete.size} selected duplicates`} disabled={toDelete.size === 0} busy={deleting} onPress={deleteSelected} />
        </View>
      </ScrollView>
    );
  }

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ title: 'Duplicate Finder' }} />
      {body}
    </View>
  );
}

function Stat({ value, label, color }: { value: string; label: string; color?: string }) {
  return (
    <View style={{ alignItems: 'center' }}>
      <Text variant="titleLarge" color={color} style={{ fontFamily: FontFamily.mono }}>{value}</Text>
      <Text variant="bodySmall" muted>{label}</Text>
    </View>
  );
}

function DupRow({ path, size, kept, selected, onToggle }: { path: string; size: number | undefined; kept: boolean; selected: boolean; onToggle?: () => void }) {
  const c = useScheme();
  const spec = rowIconFor(path);
  const Icon = spec.icon;
  const tint = spec.color ?? c.onSurfaceVariant;
  const row = (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingVertical: Spacing.xs }}>
      {!kept && (
        <View style={{ width: 20, height: 20, borderRadius: 6, borderWidth: 1.5, borderColor: selected ? Brand.seed : c.outlineVariant, backgroundColor: selected ? Brand.seed : 'transparent', alignItems: 'center', justifyContent: 'center' }}>
          {selected && <Check size={13} color="#fff" />}
        </View>
      )}
      <View style={{ width: 38, height: 38, borderRadius: Radii.sm, backgroundColor: `${tint}24`, alignItems: 'center', justifyContent: 'center' }}>
        <Icon size={18} color={tint} />
      </View>
      <View style={{ flex: 1 }}>
        <Text numberOfLines={1} style={{ fontSize: 14, fontFamily: FontFamily.medium }}>{path}</Text>
        <Text muted style={{ fontSize: 11.5 }}>{formatSize(size)}</Text>
      </View>
      {kept && (
        <View style={{ paddingHorizontal: 7, paddingVertical: 2, borderRadius: Radii.stadium, backgroundColor: `${Brand.online}24` }}>
          <Text style={{ fontSize: 10.5, fontFamily: FontFamily.semibold }} color={Brand.online}>Keep</Text>
        </View>
      )}
    </View>
  );
  return onToggle ? (
    <Pressable onPress={onToggle} accessibilityRole="checkbox" accessibilityLabel={path} accessibilityState={{ checked: selected }}>{row}</Pressable>
  ) : (
    row
  );
}
