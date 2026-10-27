import { Check, Pin as PinIcon, Star } from 'lucide-react-native';
import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { Pressable, Text } from '../../design/components';
import { useRoles, useScheme } from '../../design/theme';
import { EntryLeading, useIconChipBg } from './EntryIcon';
import { Thumbnail } from './Thumbnail';

/** Grid cell: a flat `.collection` card (no outline) with a role-tinted tile, the name (folders in the folder colour) and a selection check. */
export function EntryGridCell({
  entry, hostId, selected, multiSelect, isFavorite, isPinned, onPress, onLongPress, onPeek,
}: {
  entry: Entry; hostId: string; selected: boolean; multiSelect: boolean; isFavorite?: boolean; isPinned?: boolean;
  onPress: () => void; onLongPress: () => void; onPeek?: () => void;
}) {
  const c = useScheme();
  const roles = useRoles();
  const chip = useIconChipBg(entry);
  const isImage = !entry.isDir && (entry.mimeType ?? '').startsWith('image/');
  const fallback = (
    <View style={{ width: 56, height: 56, borderRadius: 14, backgroundColor: chip, alignItems: 'center', justifyContent: 'center' }}>
      <EntryLeading entry={entry} size={30} />
    </View>
  );
  const thumb = isImage ? <Thumbnail hostId={hostId} entry={entry} size={256} style={{ width: 56, height: 56, borderRadius: 14 }} fallback={fallback} /> : fallback;
  const badge = (pos: object, bg: string, children: React.ReactNode) => (
    <View style={{ position: 'absolute', width: 22, height: 22, borderRadius: 11, backgroundColor: bg, alignItems: 'center', justifyContent: 'center', ...pos }}>{children}</View>
  );
  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      accessibilityLabel={entry.name}
      accessibilityState={{ selected }}
      style={{ flex: 1, minHeight: 132, padding: 10, alignItems: 'center', justifyContent: 'center', backgroundColor: selected ? mix(c.primary, c.surfaceContainer, 0.18) : c.surfaceContainer, borderRadius: LumenSize.cardRadius }}
    >
      {!multiSelect && !entry.isDir && onPeek ? <Pressable onPress={onPress} onLongPress={onPeek} style={{}}>{thumb}</Pressable> : thumb}
      <Text style={[LumenType.name, { textAlign: 'center', marginTop: 8 }]} color={entry.isDir ? roles.folder : c.onSurface} numberOfLines={2}>{entry.name}</Text>
      {multiSelect && badge({ top: 6, right: 6 }, selected ? c.primary : c.surfaceContainerHigh, selected ? <Check size={14} color={c.onPrimary} /> : null)}
      {isFavorite && entry.isDir && badge({ top: 6, left: 6 }, c.surfaceContainerHigh, <Star size={13} color={roles.folder} fill={roles.folder} />)}
      {isPinned && entry.isDir && badge({ bottom: 6, left: 6 }, c.surfaceContainerHigh, <PinIcon size={12} color={c.primary} />)}
    </Pressable>
  );
}
