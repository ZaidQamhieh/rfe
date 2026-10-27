import { useRouter } from 'expo-router';
import { Monitor, Network, Pencil, StickyNote, Plug, Plus, QrCode, SlidersHorizontal, Trash2, Zap } from 'lucide-react-native';
import { useState } from 'react';
import { FlatList, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { Host } from '../../core/models/host';
import { sendWakeOnLan } from '../../core/native';
import { Button, ErrorRetry, Loading, SectionLabel, Text, useDialogs, useToast } from '../../design/components';
import { mix } from '../../design/color';
import { PageHead, StatePill, TopBar } from '../../design/components/LumenBits';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { OverflowSheet, type OverflowItem } from './OverflowSheet';
import { WorkspaceRow } from './WorkspaceRow';
import { activateHost, useWorkspaces } from './useWorkspaces';

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
      title: 'Rename computer',
      initialValue: host.label,
      placeholder: host.label,
      confirmLabel: t('renameButton'),
      validate: (v) => (v.trim().length === 0 ? 'Enter a name' : null),
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
      title: 'Workspace note',
      description: 'A short label shown under the name, for example "Home office". Kept on this phone only.',
      initialValue: host.note ?? '',
      placeholder: 'Note',
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
  const wake = async (host: Host) => {
    if (!host.macAddress) return;
    const sent = await sendWakeOnLan(host.macAddress).catch(() => false);
    if (sent) toast.info(t('wolPacketSent', { hostname: host.label }));
    else toast.error(t('wolPacketFailed'));
  };

  const rowItems = (host: Host): OverflowItem[] => [
    { key: 'settings', label: `${t('settingsMenuItem')}`, icon: SlidersHorizontal, onPress: () => hostRoute(host, '/host/[id]/settings') },
    { key: 'connection', label: 'Connection', icon: Plug, onPress: () => hostRoute(host, '/host/[id]/connect') },
    ...(host.macAddress ? [{ key: 'wol', label: 'Wake on LAN', icon: Zap, onPress: () => void wake(host) }] : []),
    { key: 'rename', label: t('renameButton'), icon: Pencil, onPress: () => void rename(host) },
    { key: 'note', label: host.note ? 'Edit note' : 'Add note', icon: StickyNote, onPress: () => void editNote(host) },
    { key: 'forget', label: t('forgetComputerMenuItem'), icon: Trash2, destructive: true, onPress: () => void forget(host) },
  ];

  const others = (hosts ?? []).filter((h) => h.id !== selected?.id);
  const selectedHost = hosts?.find((h) => h.id === selected?.id) ?? null;

  let body;
  if (error) body = <ErrorRetry message={t('errorLabel', { error })} onRetry={reload} />;
  else if (hosts === null) body = <Loading />;
  else if (hosts.length === 0) {
    body = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 18, gap: 14 }}>
        <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ width: 108, height: 108, borderRadius: 54, backgroundColor: mix(c.primary, c.surface, 0.17), alignItems: 'center', justifyContent: 'center' }}>
          <Monitor size={44} color={c.primary} />
        </View>
        <Text style={[LumenType.title, { textAlign: 'center' }]}>{t('emptyStatePairTitle')}</Text>
        <Text style={[LumenType.caption, { textAlign: 'center' }]} muted>
          {t('emptyStatePairBody')}
        </Text>
      </View>
    );
  } else {
    body = (
      <FlatList
        data={others}
        keyExtractor={(h) => h.id}
        contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 16 }}
        ListHeaderComponent={
          <View>
            {selectedHost ? (
              <View style={{ marginBottom: 14 }}>
                <WorkspaceRow host={selectedHost} selected position="only" onSelect={() => select(selectedHost)} onMore={() => setRowMenu(selectedHost)} onChanged={reload} />
              </View>
            ) : null}
            {others.length > 0 ? <SectionLabel title="Other computers" /> : null}
          </View>
        }
        renderItem={({ item, index }) => (
          <WorkspaceRow
            host={item}
            selected={false}
            position={others.length === 1 ? 'only' : index === 0 ? 'first' : index === others.length - 1 ? 'last' : 'middle'}
            onSelect={() => select(item)}
            onMore={() => setRowMenu(item)}
            onChanged={reload}
          />
        )}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <TopBar context="RFE · Workspaces" sub="Your computers" right={hosts && hosts.length > 0 ? <StatePill label={`${hosts.length} saved`} tone="safe" icon={Network} /> : undefined} onMore={() => setPageMenu(true)} />
      <PageHead title="Workspaces" subtitle="Choose a computer to open." />
      <View style={{ flex: 1 }}>{body}</View>
      <View style={{ paddingHorizontal: 18, paddingTop: 10, paddingBottom: insets.bottom + 12, borderTopWidth: 1, borderTopColor: mix(c.onSurfaceVariant, c.surface, 0.18), backgroundColor: c.surface }}>
        <Button size="lg" kind="filled" label="Add a workspace" renderIcon={(k) => <Plus size={20} color={k} />} onPress={() => router.push('/pair')} />
      </View>
      <OverflowSheet visible={rowMenu !== null} title={rowMenu?.label} items={rowMenu ? rowItems(rowMenu) : []} onClose={() => setRowMenu(null)} />
      <OverflowSheet
        visible={pageMenu}
        onClose={() => setPageMenu(false)}
        items={[
          { key: 'add', label: t('addComputerButton'), icon: Plus, onPress: () => router.push('/pair') },
          { key: 'receive', label: t('receiveFileTooltip'), icon: QrCode, onPress: () => router.push('/receive') },
        ]}
      />
    </View>
  );
}
