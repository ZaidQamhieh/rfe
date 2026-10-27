import { Check, Image as ImageIcon } from 'lucide-react-native';
import { View } from 'react-native';

import { BottomSheet, Button, Pressable, SheetHead, SheetScroll, Text } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import type { AlbumInfo } from './photoBackupService';

/** Multi-select list of device albums; an empty selection means every photo. */
export function AlbumPickerSheet({ visible, albums, selected, onToggle, onClear, onClose }: { visible: boolean; albums: AlbumInfo[]; selected: ReadonlySet<string>; onToggle: (id: string) => void; onClear: () => void; onClose: () => void }) {
  const c = useScheme();
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('selectAlbums')} subtitle={selected.size === 0 ? t('allPhotos') : t('albumsSelected', { count: selected.size })} />
      <SheetScroll>
        {albums.map((a) => {
          const on = selected.has(a.id);
          return (
            <Pressable key={a.id} onPress={() => onToggle(a.id)} accessibilityLabel={a.title} accessibilityRole="checkbox" accessibilityState={{ checked: on }}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.lg, paddingVertical: 11 }}>
                <ImageIcon size={18} color={c.onSurfaceVariant} />
                <View style={{ flex: 1 }}>
                  <Text style={{ fontSize: 14, fontFamily: FontFamily.medium }}>{a.title}</Text>
                  <Text muted style={{ fontSize: 11.5 }}>{t('albumPhotoCount', { count: a.count })}</Text>
                </View>
                {on && <Check size={18} color={c.primary} />}
              </View>
            </Pressable>
          );
        })}
      </SheetScroll>
      <View style={{ padding: Spacing.md, gap: Spacing.sm }}>
        <Button kind="tonal" label={t('allPhotos')} disabled={selected.size === 0} onPress={onClear} />
        <Button label={t('okButton')} onPress={onClose} />
      </View>
    </BottomSheet>
  );
}
