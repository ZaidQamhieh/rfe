import { useRouter } from 'expo-router';
import { useShareIntentContext } from 'expo-share-intent';
import { File as FileIcon, Monitor } from 'lucide-react-native';
import { useMemo, useState } from 'react';
import { ScrollView, View } from 'react-native';

import type { Host } from '../core/models/host';
import { formatSize } from '../core/format';
import { Button, Loading, Text, useToast } from '../design/components';
import { Spacing } from '../design/tokens';
import { t } from '../i18n';
import { DestinationPicker } from '../features/explorer/DestinationPicker';
import { folderLabel } from '../features/explorer/paths';
import { loadRoots } from '../features/explorer/rootsCache';
import { useConflictPrompt } from '../features/explorer/useConflictPrompt';
import { useHosts } from '../features/hosts/useHosts';
import { humanizeError } from '../features/pairing/pairingService';
import { InfoRow, NavRow, SettingsPage, SettingsSection } from '../features/settings/parts';
import { sharedFiles } from '../features/share/shareLogic';
import { enqueueUploads } from '../features/transfers/enqueueUploads';
import { cleanUploadName, planUploads, type Resolution } from '../features/transfers/uploadPlan';
import { clientForHost, keyValue } from '../services';

/** Files shared from another app: pick a paired computer and a folder, then upload with the usual name-clash prompt. */
export default function Share() {
  const router = useRouter();
  const toast = useToast();
  const askConflict = useConflictPrompt();
  const { shareIntent, resetShareIntent } = useShareIntentContext();
  const hosts = useHosts();
  const files = useMemo(() => sharedFiles(shareIntent.files), [shareIntent.files]);
  const [chosen, setChosen] = useState<string | null>(null);
  const [picking, setPicking] = useState<{ host: Host; start: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const host = hosts?.find((h) => h.id === chosen) ?? (hosts?.length === 1 ? hosts[0] : null);

  function close() {
    resetShareIntent();
    if (router.canGoBack()) router.back();
    else router.replace('/');
  }

  async function choose(h: Host) {
    const roots = await loadRoots(keyValue, h.id).catch(() => null);
    setPicking({ host: h, start: roots?.roots[0] ?? '/' });
  }

  async function send(h: Host, dest: string) {
    setPicking(null);
    setBusy(true);
    try {
      let resolution: Resolution = 'skip';
      const colliding = await collidingNames(h, dest, files.map((f) => cleanUploadName(f.name)));
      if (colliding.size > 0) {
        const res = await askConflict(colliding.size, files.length, dest);
        if (res === 'cancel') return;
        resolution = res;
      }
      const items = planUploads(files, colliding, resolution);
      if (items.length === 0) {
        toast.info(t('uploadAllExist', { folder: folderLabel(dest) }));
        return;
      }
      const queued = await enqueueUploads(h, dest, items);
      toast.success(queued === 1 ? t('uploadingFile', { name: items[0].targetName }) : t('uploadingNFiles', { count: queued }));
      resetShareIntent();
      router.replace('/transfers');
    } catch (e) {
      toast.error(t('operationFailed', { operation: t('uploadFileTooltip'), error: humanizeError(e) }));
    } finally {
      setBusy(false);
    }
  }

  if (hosts === undefined) return <Loading />;
  if (files.length === 0 || hosts.length === 0) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
        <Text style={{ textAlign: 'center' }}>{files.length === 0 ? t('shareNothingBody') : t('sharePairFirstBody')}</Text>
        <Button kind="tonal" label={t('cancelButton')} onPress={close} />
      </View>
    );
  }

  return (
    <View style={{ flex: 1 }}>
      <ScrollView>
        <SettingsPage>
          <SettingsSection title={t('shareFilesSection', { count: files.length })}>
            {files.map((f, i) => (
              <InfoRow key={`${f.uri}${i}`} icon={FileIcon} title={f.name} subtitle={f.size !== undefined ? formatSize(f.size) : undefined} />
            ))}
          </SettingsSection>
          {hosts.length > 1 && (
            <SettingsSection title={t('shareToComputerSection')}>
              {hosts.map((h) => (
                <NavRow key={h.id} icon={Monitor} title={h.label} subtitle={h.id === host?.id ? t('shareSelected') : h.address} onPress={() => setChosen(h.id)} />
              ))}
            </SettingsSection>
          )}
        </SettingsPage>
      </ScrollView>
      <View style={{ padding: Spacing.md, gap: Spacing.sm }}>
        <Button label={host ? t('shareChooseFolder', { host: host.label }) : t('shareChooseComputerFirst')} disabled={!host} busy={busy} onPress={() => host && void choose(host)} />
        <Button kind="text" label={t('cancelButton')} onPress={close} />
      </View>
      {picking && (
        <DestinationPicker visible host={picking.host} originPath={picking.start} title={t('shareChooseFolderTitle')} confirmLabel={t('shareUploadHere')} onPick={(dir) => void send(picking.host, dir)} onClose={() => setPicking(null)} />
      )}
    </View>
  );
}

async function collidingNames(host: Host, dest: string, names: string[]): Promise<Set<string>> {
  const client = await clientForHost(host);
  const existing = new Set<string>();
  let cursor: string | undefined;
  do {
    const l = await client.list(dest, { cursor });
    // Case-insensitive: a Windows or macOS host treats Photo.JPG and photo.jpg as one file; asking too often is safe (audit F-129).
    for (const e of l.entries) existing.add(e.name.toLowerCase());
    cursor = l.nextCursor ?? undefined;
  } while (cursor);
  return new Set(names.filter((n) => existing.has(n.toLowerCase())));
}
