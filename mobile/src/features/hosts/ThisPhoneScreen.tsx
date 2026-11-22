import { useFocusEffect, useRouter } from 'expo-router';
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { FlatList, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { formatSize } from '../../core/format';
import { offlineBodies, openPublicUri, type TransferRecord } from '../../core/native';
import { Emoji, Row, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { useCollections } from '../../state/collections';
import { backupStatus } from '../photoBackup/photoBackupStatus';
import { photoBackupStore } from '../photoBackup/photoBackupService';
import { emojiNameFor } from '../explorer/entryEmoji';
import { externalMime } from '../preview/externalFiles';
import { finishedWhen, groupTransfers, isUpload, transferName } from '../transfers/transferLogic';
import { keptFolders } from './thisPhoneLogic';
import { useHosts } from './useHosts';
import { useTransfers } from './useTransfers';

/** Files this phone has downloaded and still has (finished downloads, newest first). */
function savedDownloads(all: readonly TransferRecord[]): TransferRecord[] {
  return groupTransfers([...all]).finished.filter((r) => r.state === 'DONE' && !isUpload(r));
}

/** `.sec`: a 14/500 ink-2 sentence-case label with an optional figure at the end. */
function Section({ title, figure }: { title: string; figure?: string }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'baseline', paddingTop: Spacing.md3, paddingBottom: 6, paddingHorizontal: Spacing.md }}>
      <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { flex: 1 }]}>
        {title}
      </Text>
      {figure ? (
        <Text muted style={{ fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 20 }}>
          {figure}
        </Text>
      ) : null}
    </View>
  );
}

/** A quiet one-sentence note under a section whose list is empty. */
function Note({ children }: { children: ReactNode }) {
  return (
    <Text muted style={[LumenType.caption, { paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }]}>
      {children}
    </Text>
  );
}

/**
 * This phone: what the phone holds, as flat rows. Storage (used and free in words), Photo backup (opens its settings),
 * the folders kept for offline use with their total size, and the files downloaded from computers (tap to open).
 */
export function ThisPhoneScreen() {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const insets = useSafeAreaInsets();
  const records = useTransfers();
  const hosts = useHosts();
  const pins = useCollections((s) => s.pins);
  const saved = useMemo(() => savedDownloads(records ?? []), [records]);
  const [keptBytes, setKeptBytes] = useState<number | null>(null);
  // Saved backup settings and the backed-up count, re-read each time this screen is shown so the Photo backup row stays current.
  const [backup, setBackup] = useState<{ enabled: boolean; hostId: string | null; count: number } | null>(null);
  useFocusEffect(
    useCallback(() => {
      let live = true;
      void photoBackupStore
        .load()
        .then(async (p) => ({ enabled: p.enabled, hostId: p.hostId, count: await photoBackupStore.doneCountFor(p.hostId) }))
        .then((b) => live && setBackup(b))
        // The line is a nicety: without it the row keeps its plain hint.
        .catch(() => live && setBackup(null));
      return () => {
        live = false;
      };
    }, []),
  );
  const backupRow = useMemo(() => {
    if (!backup) return t('phPhotoBackupHint');
    const computer = (hosts ?? []).find((h) => h.id === backup.hostId);
    const { row } = backupStatus({ hostChosen: backup.hostId !== null, enabled: backup.enabled, computer: computer?.label || computer?.address || '', backedUp: backup.count, pending: 0, last: null });
    return t(row.key, row.params);
  }, [backup, hosts]);
  const kept = useMemo(() => keptFolders(pins, new Map((hosts ?? []).map((h) => [h.id, h.label || h.address]))), [pins, hosts]);

  useEffect(() => {
    let live = true;
    offlineBodies.totalBytes().then(
      (n) => live && setKeptBytes(n),
      // The size is a nicety: without it the section simply has no figure.
      () => live && setKeptBytes(null),
    );
    return () => {
      live = false;
    };
  }, [pins]);

  const open = async (r: TransferRecord) => {
    if (!r.publicUri) return;
    if (!(await openPublicUri(r.publicUri, externalMime({ name: transferName(r) })))) toast.info(t('phoneNoAppToOpen'));
  };

  const header = (
    <View>
      <View style={{ paddingTop: insets.top + 12, paddingRight: Spacing.md, paddingBottom: Spacing.md2, paddingLeft: Spacing.md }}>
        <Text style={LumenType.pageTitle} numberOfLines={1} accessibilityRole="header">
          {t('navThisPhone')}
        </Text>
        <Text style={[LumenType.caption, { marginTop: 2 }]} muted numberOfLines={1}>
          {t('thisPhoneState')}
        </Text>
      </View>
      <Row lead={<Emoji name="image" size={28} />} title={t('phPhotoBackup')} subtitle={backupRow} onPress={() => router.push('/settings/photo-backup')} />
      <Section title={t('phKeptTitle')} figure={kept.length > 0 && keptBytes !== null ? formatSize(keptBytes) : undefined} />
      {kept.length === 0 ? (
        <Note>{t('phKeptEmpty')}</Note>
      ) : (
        kept.map((k) => <Row key={k.key} lead={<Emoji name="file_folder" size={28} />} title={k.name} subtitle={k.computer ? t('phKeptFrom', { computer: k.computer }) : t('phKeptFolder')} />)
      )}
      <Section title={t('phDownloadsTitle')} />
    </View>
  );

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <FlatList
        data={saved}
        keyExtractor={(r) => r.id}
        ListHeaderComponent={header}
        ListEmptyComponent={records === null ? null : <Note>{t('phDownloadsEmpty')}</Note>}
        renderItem={({ item }) => {
          const name = transferName(item);
          const when = finishedWhen(item);
          const size = item.total > 0 ? formatSize(item.total) : formatSize(item.received);
          return <Row lead={<Emoji name={emojiNameFor({ isDir: false, name })} size={28} />} title={name} subtitle={when ? t('phoneSavedSubtitle', { size, when }) : size} onPress={item.publicUri ? () => void open(item) : undefined} />;
        }}
        contentContainerStyle={{ paddingBottom: Spacing.lg }}
      />
    </View>
  );
}
