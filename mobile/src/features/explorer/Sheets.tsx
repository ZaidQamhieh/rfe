import { ArrowDown, ArrowUp, Folder, Plus, Star } from 'lucide-react-native';
import { ScrollView, View } from 'react-native';

import type { Host } from '../../core/models/host';
import { BottomSheet, Button, GroupedCard, MockupSwitch, Pressable, Segmented, SectionLabel, SheetHead, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { useSettings } from '../../state/settings';
import { useCollections } from '../../state/collections';
import type { ExplorerState } from './explorerStore';
import { currentPath } from './explorerStore';
import { folderLabel } from './paths';
import { sortFieldLabel } from './ToolsRow';
import type { EntryDensity, SortField, SortOrder } from '../../core/models/sort';

const SORT_FIELDS: SortField[] = ['name', 'size', 'date', 'type'];

export function ViewOptionsSheet({ visible, onClose, gridView, density, sort, showHidden, hiddenCount, onToggleShowHidden }: { visible: boolean; onClose: () => void; gridView: boolean; density: EntryDensity; sort: SortOrder; showHidden: boolean; hiddenCount: number; onToggleShowHidden: () => void }) {
  const c = useScheme();
  const setApp = useSettings((s) => s.setApp);
  const foldersFirst = useSettings((s) => s.state.app.foldersFirst);
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('viewOptionsTitle')} />
      <ScrollView contentContainerStyle={{ paddingHorizontal: Spacing.md, paddingBottom: Spacing.lg, gap: Spacing.sm }}>
        <Text style={LumenType.name}>{t('layoutLabel')}</Text>
        <Segmented options={[t('listLabel'), t('gridLabel')]} selectedIndex={gridView ? 1 : 0} onChange={(i) => setApp('gridView', i === 1)} />
        <View style={{ height: Spacing.sm }} />
        <Text style={LumenType.name}>{t('densityLabel')}</Text>
        <Segmented options={[t('comfortableLabel'), t('compactLabel')]} selectedIndex={density === 'compact' ? 1 : 0} onChange={(i) => setApp('density', i === 1 ? 'compact' : 'comfortable')} />
        <View style={{ height: Spacing.sm }} />
        <SectionLabel title={t('sortByLabel')} />
        <GroupedCard padded={false} style={{ backgroundColor: c.surfaceContainerHigh }}>
          {SORT_FIELDS.map((f, i) => (
            <Pressable
              key={f}
              onPress={() => setApp('sort', sort.field === f ? { ...sort, ascending: !sort.ascending } : { field: f, ascending: true })}
              accessibilityLabel={sortFieldLabel(f)}
              accessibilityState={{ selected: sort.field === f }}
            >
              <View style={{ flexDirection: 'row', alignItems: 'center', minHeight: 48, paddingHorizontal: 14 }}>
                <Text style={[LumenType.name, { flex: 1 }]} color={sort.field === f ? c.primary : c.onSurface}>{sortFieldLabel(f)}</Text>
                {sort.field === f && (sort.ascending ? <ArrowUp size={18} color={c.primary} /> : <ArrowDown size={18} color={c.primary} />)}
              </View>
            </Pressable>
          ))}
        </GroupedCard>
        <View style={{ height: Spacing.sm }} />
        <SectionLabel title={t('optionsLabel')} />
        <GroupedCard padded={false} style={{ backgroundColor: c.surfaceContainerHigh }}>
          <ToggleRow title={t('foldersFirstLabel')} value={foldersFirst} onPress={() => void setApp('foldersFirst', !foldersFirst)} divider />
          <ToggleRow title={t('showHiddenItems')} subtitle={hiddenCount > 0 ? t('nHiddenByVisibility', { count: hiddenCount }) : undefined} value={showHidden} onPress={onToggleShowHidden} />
        </GroupedCard>
      </ScrollView>
    </BottomSheet>
  );
}

function ToggleRow({ title, subtitle, value, onPress, divider }: { title: string; subtitle?: string; value: boolean; onPress?: () => void; divider?: boolean }) {
  const row = (
    <View style={{ flexDirection: 'row', alignItems: 'center', minHeight: 56, paddingVertical: Spacing.sm, paddingHorizontal: 14, marginBottom: divider ? 2 : 0 }}>
      <View style={{ flex: 1 }}>
        <Text style={LumenType.name}>{title}</Text>
        {subtitle ? <Text muted style={[LumenType.meta, { marginTop: 1 }]}>{subtitle}</Text> : null}
      </View>
      <MockupSwitch value={value} />
    </View>
  );
  return onPress ? <Pressable onPress={onPress} accessibilityLabel={title}>{row}</Pressable> : row;
}

export function FavoritesSheet({ visible, onClose, host, state, onOpen }: { visible: boolean; onClose: () => void; host: Host; state: ExplorerState; onOpen: (path: string) => void }) {
  const c = useScheme();
  const roles = useRoles();
  const favs = useCollections((s) => s.favorites).filter((f) => f.hostId === host.id);
  const toggle = useCollections((s) => s.toggleFavorite);
  const remove = useCollections((s) => s.removeFavorite);
  const path = currentPath(state);
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('favoritesTitle')} subtitle={t('favoritesSubtitle')} />
      {favs.length === 0 ? (
        <Text muted style={[LumenType.meta, { textAlign: 'center', paddingHorizontal: Spacing.md3, paddingBottom: Spacing.lg }]}>{t('noFavoritesYet')}</Text>
      ) : (
        <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingVertical: Spacing.xs }}>
          {favs.map((f) => (
            <Pressable key={f.path} onPress={() => { onClose(); onOpen(f.path); }} accessibilityLabel={f.label}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.xs, paddingVertical: Spacing.sm }}>
                <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: mix(roles.folder, c.surfaceContainerHigh, 0.17), alignItems: 'center', justifyContent: 'center' }}>
                  <Folder size={22} color={roles.folder} />
                </View>
                <View style={{ flex: 1 }}>
                  <Text numberOfLines={1} style={LumenType.name}>{f.label}</Text>
                  <Text numberOfLines={1} muted style={LumenType.meta}>{f.path}</Text>
                </View>
                <Pressable onPress={() => remove(f.hostId, f.path)} pressedScale={0.92} accessibilityLabel={t('removeFavoriteTooltip')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
                  <Star size={18} color={roles.folder} fill={roles.folder} />
                </Pressable>
              </View>
            </Pressable>
          ))}
        </ScrollView>
      )}
      <View style={{ padding: 18 }}>
        <Button size="lg" kind="neutral" label={t('addCurrentFolderLabel', { name: folderLabel(path) })} onPress={() => void toggle({ hostId: host.id, path, label: folderLabel(path) })} renderIcon={(k) => <Plus size={20} color={k} />} />
      </View>
    </BottomSheet>
  );
}
