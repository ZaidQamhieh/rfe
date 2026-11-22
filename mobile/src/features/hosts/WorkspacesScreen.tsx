import { useRouter } from 'expo-router';
import { MoreVertical, Pencil, StickyNote, Plug, Plus, SlidersHorizontal, Trash2, Zap } from 'lucide-react-native';
import { useState } from 'react';
import { FlatList, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { Host } from '../../core/models/host';
import { AppBarIconButton, Button, DuoIcon, ErrorRetry, Loading, Text, useDialogs, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { EmptyPairPrompt } from './EmptyPairPrompt';
import { OverflowSheet, type OverflowItem } from './OverflowSheet';
import { WorkspaceRow } from './WorkspaceRow';
import { activateHost, useWorkspaces, wakeHost } from './useWorkspaces';
import { Spacing } from '../../design/tokens';

/** Workspaces: every paired computer, the selected one first. Selecting makes it the computer Home, Files, Apps and Activity use. */
export function WorkspacesScreen() {
  const router = useRouter();
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const toast = useToast();
  const dialogs = useDialogs();
  const { hosts, selected, error, reload } = useWorkspaces();
  const [rowMenu, setRowMenu] = useState<Host | null>(null);
  const [pageMenu, setPageMenu] = useState(false);

  const select = (host: Host) => {
    if (host.id !== selected?.id) activateHost(host);
    router.dismissTo('/');
  };
  const hostRoute = (host: Host, pathname: string) => router.push({ pathname, params: { id: host.id } } as never);

  const rename = async (host: Host) => {
    const label = await dialogs.prompt({
      title: t('renameComputerTitle'),
      initialValue: host.label,
      placeholder: host.label,
      confirmLabel: t('renameButton'),
      validate: (v) => (v.trim().length === 0 ? t('enterNameError') : null),
    });
    if (!label || label === host.label) return;
    const updated = { ...host, label };
    // Patch the one field: a learned address that landed while the dialog was open must survive.
    await hostStore.patchHost(host.id, { label });
    const active = useActiveHost.getState().active;
    if (active?.host.id === host.id) useActiveHost.getState().setActive({ ...active, host: updated });
    await reload();
  };
  const editNote = async (host: Host) => {
    const note = await dialogs.prompt({
      title: t('workspaceNoteTitle'),
      description: t('workspaceNoteDescription'),
      initialValue: host.note ?? '',
      placeholder: t('noteFieldPlaceholder'),
      confirmLabel: t('saveButton'),
      allowEmpty: true,
    });
    if (note === null || note === (host.note ?? '')) return;
    const { note: _old, ...rest } = host;
    const updated: Host = note === '' ? rest : { ...rest, note };
    await hostStore.patchHost(host.id, { note: note === '' ? undefined : note });
    const active = useActiveHost.getState().active;
    if (active?.host.id === host.id) useActiveHost.getState().setActive({ ...active, host: updated });
    await reload();
  };
  const forget = async (host: Host) => {
    const ok = await dialogs.confirm({ title: t('forgetComputerTitle'), description: t('forgetComputerConfirm', { hostLabel: host.label }), confirmLabel: t('forgetButton'), cancelLabel: t('cancelButton'), destructive: true });
    if (!ok) return;
    await hostStore.removeHost(host.id);
    if (useActiveHost.getState().active?.host.id === host.id) useActiveHost.getState().setActive(null);
    await reload();
  };
  const wake = (host: Host) => wakeHost(host, toast);

  const rowItems = (host: Host): OverflowItem[] => [
    { key: 'settings', label: t('settingsMenuItem'), icon: SlidersHorizontal, onPress: () => hostRoute(host, '/host/[id]/settings') },
    { key: 'connection', label: t('aboutComputerConnection'), icon: Plug, onPress: () => hostRoute(host, '/host/[id]/connect') },
    ...(host.macAddress ? [{ key: 'wol', label: t('aboutComputerWake'), icon: Zap, onPress: () => void wake(host) }] : []),
    { key: 'rename', label: t('renameButton'), icon: Pencil, onPress: () => void rename(host) },
    { key: 'note', label: host.note ? t('editNoteMenuItem') : t('addNoteMenuItem'), icon: StickyNote, onPress: () => void editNote(host) },
    { key: 'forget', label: t('forgetComputerMenuItem'), icon: Trash2, destructive: true, onPress: () => void forget(host) },
  ];

  const others = (hosts ?? []).filter((h) => h.id !== selected?.id);
  const selectedHost = hosts?.find((h) => h.id === selected?.id) ?? null;

  let body;
  if (error) body = <ErrorRetry message={t('errorLabel', { error })} onRetry={reload} />;
  else if (hosts === null) body = <Loading />;
  else if (hosts.length === 0) body = <EmptyPairPrompt />;
  else {
    body = (
      <FlatList
        data={others}
        keyExtractor={(h) => h.id}
        contentContainerStyle={{ paddingBottom: Spacing.md }}
        ListHeaderComponent={
          <View>
            {selectedHost ? <WorkspaceRow host={selectedHost} selected onSelect={() => select(selectedHost)} onMore={() => setRowMenu(selectedHost)} onChanged={reload} /> : null}
            {others.length > 0 ? (
              <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { paddingTop: Spacing.md3, paddingBottom: 6, paddingHorizontal: Spacing.md }]}>
                {t('otherComputersHeader')}
              </Text>
            ) : null}
          </View>
        }
        renderItem={({ item }) => <WorkspaceRow host={item} selected={false} onSelect={() => select(item)} onMore={() => setRowMenu(item)} onChanged={reload} />}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <View style={{ paddingTop: insets.top, paddingLeft: Spacing.md, paddingRight: Spacing.xs, flexDirection: 'row', alignItems: 'center', minHeight: 56 + insets.top }}>
        <View style={{ flex: 1 }}>
          <Text style={LumenType.appbar} accessibilityRole="header">
            {t('switcherTitle')}
          </Text>
          <Text style={LumenType.meta} muted>
            {t('chooseComputerToOpen')}
          </Text>
        </View>
        <AppBarIconButton label={t('moreOptionsTooltip')} onPress={() => setPageMenu(true)}>
          <DuoIcon icon={MoreVertical} size={24} />
        </AppBarIconButton>
      </View>
      <View style={{ flex: 1 }}>{body}</View>
      {hosts && hosts.length > 0 && (
        <View style={{ paddingHorizontal: Spacing.md, paddingTop: Spacing.md2, paddingBottom: insets.bottom + 12, borderTopWidth: 1, borderTopColor: c.rule, backgroundColor: c.surface }}>
          <Button kind="tonal" label={t('switcherAdd')} renderIcon={(k) => <Plus size={20} color={k} />} onPress={() => router.push('/pair')} />
        </View>
      )}
      <OverflowSheet visible={rowMenu !== null} title={rowMenu?.label} items={rowMenu ? rowItems(rowMenu) : []} onClose={() => setRowMenu(null)} />
      <OverflowSheet
        visible={pageMenu}
        onClose={() => setPageMenu(false)}
        items={[
          { key: 'add', label: t('addComputerButton'), icon: Plus, onPress: () => router.push('/pair') },
        ]}
      />
    </View>
  );
}
