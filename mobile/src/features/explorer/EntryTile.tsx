import { Check, EllipsisVertical, Pin as PinIcon, Star } from 'lucide-react-native';
import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { entryMeta } from './entryMeta';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { Pressable, Text } from '../../design/components';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { EntryLeading, useIconChipBg } from './EntryIcon';
import type { RowShape } from './rowShape';
import { Thumbnail } from './Thumbnail';
import type { EntryDensity } from './sort';

const TILE = 38;

/**
 * One listing row in the Lumen layout: a folder is its own `.collection` card with the name in the folder colour, files
 * share one `.filelist` card with a 38 dp role-tinted `.filemark` tile, name and "size · date".
 */
export function EntryTile({
  entry, hostId, selected, multiSelect, density = 'comfortable', shape = { first: true, last: true }, isFavorite, isPinned, onPress, onLongPress, onSelect, onShowMeta, onPeek,
}: {
  entry: Entry; hostId: string; selected: boolean; multiSelect: boolean; density?: EntryDensity; shape?: RowShape; isFavorite?: boolean; isPinned?: boolean;
  onPress: () => void; onLongPress: () => void; onSelect: () => void; onShowMeta?: () => void; onPeek?: () => void;
}) {
  const c = useScheme();
  const roles = useRoles();
  const compact = density === 'compact';
  const meta = entryMeta(entry);
  const chip = useIconChipBg(entry);
  const isImage = !entry.isDir && (entry.mimeType ?? '').startsWith('image/');
  const r = LumenSize.cardRadius;
  const rowPad = compact ? 2 : 6;

  const tile = (
    <View style={{ width: TILE, height: TILE }}>
      {isImage ? (
        <Thumbnail hostId={hostId} entry={entry} size={128} style={{ width: TILE, height: TILE, borderRadius: 10 }} fallback={<Chip bg={chip}><EntryLeading entry={entry} size={22} /></Chip>} />
      ) : (
        <Chip bg={chip}>
          <EntryLeading entry={entry} size={22} />
        </Chip>
      )}
    </View>
  );

  const leading = multiSelect ? <SelBox checked={selected} onPress={onSelect} /> : entry.isDir ? null : onPeek ? <Pressable onPress={onPress} onLongPress={onPeek} accessibilityLabel={entry.name} accessibilityHint={t('peekHint')} style={{}}>{tile}</Pressable> : tile;

  return (
    <View
      style={{
        marginTop: shape.first ? 10 : 0,
        paddingHorizontal: 8,
        paddingTop: shape.first ? 8 : 0,
        paddingBottom: shape.last ? 8 : 0,
        backgroundColor: c.surfaceContainer,
        borderTopLeftRadius: shape.first ? r : 0,
        borderTopRightRadius: shape.first ? r : 0,
        borderBottomLeftRadius: shape.last ? r : 0,
        borderBottomRightRadius: shape.last ? r : 0,
      }}
    >
      <Pressable
        onPress={onPress}
        onLongPress={onLongPress}
        accessibilityLabel={`${entry.name}${meta ? `, ${meta}` : ''}`}
        accessibilityState={{ selected }}
        style={{ minHeight: 48, paddingVertical: rowPad, paddingHorizontal: 6, borderRadius: 12, backgroundColor: selected ? mix(c.primary, c.surfaceContainer, 0.16) : 'transparent', flexDirection: 'row', alignItems: 'center', gap: 12 }}
      >
        {leading}
        <View style={{ flex: 1 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
            <Text style={[entry.isDir ? LumenType.title : LumenType.name, { flexShrink: 1 }]} color={entry.isDir ? roles.folder : c.onSurface} numberOfLines={1}>{entry.name}</Text>
            {isFavorite && entry.isDir ? <Star size={14} color={roles.folder} fill={roles.folder} /> : null}
            {isPinned && entry.isDir ? <PinIcon size={14} color={c.primary} /> : null}
          </View>
          {meta ? <Text style={[LumenType.meta, { marginTop: 2 }]} muted numberOfLines={1}>{meta}</Text> : null}
        </View>
        {entry.isDir &&
          (onShowMeta && !multiSelect ? (
            // A "more" glyph, not a chevron: this opens the folder's actions sheet, while tapping the row opens the folder.
            <Pressable onPress={onShowMeta} pressedScale={0.92} accessibilityLabel={`${t('folderDetailsTooltip')}: ${entry.name}`} style={{ width: 48, height: 48, marginRight: -8, alignItems: 'center', justifyContent: 'center' }}>
              <EllipsisVertical size={20} color={c.onSurfaceVariant} />
            </Pressable>
          ) : null)}
      </Pressable>
    </View>
  );
}

function Chip({ bg, children }: { bg: string; children: React.ReactNode }) {
  return <View style={{ width: TILE, height: TILE, borderRadius: 10, backgroundColor: bg, alignItems: 'center', justifyContent: 'center' }}>{children}</View>;
}

/** Selection marker: a flat rounded square, filled with the host colour when checked. No outline. */
export function SelBox({ checked, onPress }: { checked: boolean; onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityRole="checkbox" accessibilityState={{ checked }} style={{ width: 48, height: 48, marginHorizontal: -6, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 26, height: 26, borderRadius: 9, backgroundColor: checked ? c.primary : c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>
        {checked ? <Check size={16} color={c.onPrimary} /> : null}
      </View>
    </Pressable>
  );
}
