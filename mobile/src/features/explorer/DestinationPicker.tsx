import { ChevronRight, Folder, FolderPlus } from 'lucide-react-native';
import { useEffect, useMemo } from 'react';
import { ActivityIndicator, FlatList, View } from 'react-native';
import { useStore } from 'zustand';

import type { Host } from '../../core/models/host';
import { BottomSheet, Button, EmptyState, ErrorRetry, ListingSkeleton, Pressable, SheetHead, Text, useDialogs, useToast } from '../../design/components';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { useResolvedVisibility } from '../../state/settings';
import { humanizeError } from '../pairing/pairingService';
import { BreadcrumbBar } from './Breadcrumb';
import { createDestinationPicker, pickerPath } from './destinationPicker';

/**
 * Folder browser sheet for choosing where Move/Copy/Save goes. `onPick` gets the current directory; the
 * confirm button stays disabled while it is `originPath` (moving or copying into the same folder is a no-op).
 */
export function DestinationPicker({ visible, host, originPath, title, confirmLabel, onPick, onClose }: { visible: boolean; host: Host; originPath: string; title: string; confirmLabel: string; onPick: (dir: string) => void; onClose: () => void }) {
  const c = useScheme();
  const roles = useRoles();
  const toast = useToast();
  const dialogs = useDialogs();
  const vis = useResolvedVisibility(host.id);
  const picker = useMemo(
    () => createDestinationPicker({ hostId: host.id, startPath: originPath, getClient: () => clientForHost(host), visibility: () => vis, humanize: humanizeError }),
    [host, originPath, vis],
  );
  const state = useStore(picker);

  useEffect(() => {
    if (visible) void picker.load();
  }, [visible, picker]);

  async function newFolder() {
    const name = await dialogs.prompt({ title: t('newFolderButton'), placeholder: t('nameHint'), confirmLabel: t('createButton') });
    if (!name) return;
    try {
      await picker.createFolder(name);
      toast.success(t('createdName', { name }));
    } catch (e) {
      toast.error(t('createFailed', { name, error: humanizeError(e) }));
    }
  }

  const here = pickerPath(state);
  let body: React.ReactNode;
  if (state.loading && state.folders.length === 0) body = <ListingSkeleton rows={5} />;
  else if (state.error && state.folders.length === 0) body = <ErrorRetry message={state.error} onRetry={() => void picker.load()} />;
  else if (state.folders.length === 0) body = <EmptyState message={t('emptyFolderMessage')} />;
  else {
    body = (
      <FlatList
        data={state.folders}
        keyExtractor={(f) => f.path}
        onEndReached={() => void picker.loadMore()}
        onEndReachedThreshold={0.6}
        ListFooterComponent={state.loadingMore ? <ActivityIndicator style={{ padding: Spacing.lg }} /> : null}
        renderItem={({ item }) => (
          <Pressable onPress={() => picker.navigate(item.path)} accessibilityLabel={item.name}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, minHeight: 56, paddingHorizontal: 18, paddingVertical: 6 }}>
              <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: mix(roles.folder, c.surfaceContainerHigh, 0.17), alignItems: 'center', justifyContent: 'center' }}>
                <Folder size={22} color={roles.folder} />
              </View>
              <Text numberOfLines={1} style={[LumenType.name, { flex: 1 }]}>{item.name}</Text>
              <ChevronRight size={18} color={c.onSurfaceVariant} />
            </View>
          </Pressable>
        )}
      />
    );
  }

  return (
    <BottomSheet visible={visible} onClose={onClose} maxHeight="90%">
      <View style={{ height: 560, maxHeight: '100%' }}>
        <SheetHead title={title} subtitle={originPath} />
        <View style={{ paddingHorizontal: 18, height: 48 }}>
          <BreadcrumbBar pathStack={state.pathStack} onNavigateTo={picker.navigateTo} />
        </View>
        <View style={{ flex: 1 }}>{body}</View>
        <View style={{ padding: 18, gap: 10 }}>
          <Button size="lg" kind="neutral" label={t('newFolderButton')} renderIcon={(k) => <FolderPlus size={20} color={k} />} onPress={newFolder} />
          <Button size="lg" label={confirmLabel} disabled={here === originPath} onPress={() => onPick(here)} />
        </View>
      </View>
    </BottomSheet>
  );
}
