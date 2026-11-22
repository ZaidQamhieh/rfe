import { Pin as PinIcon, Star } from 'lucide-react-native';
import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import type { EntryDensity } from '../../core/models/sort';
import { Checkbox, Emoji, Pressable, Row, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { emojiNameFor } from './entryEmoji';
import { isImageEntry } from './EntryIcon';
import { entryMeta, entrySize } from './entryMeta';
import { Thumbnail } from './Thumbnail';

/** Side of the lead slot (mockup `.row .lead`) and of an image thumbnail inside it. */
const LEAD = 40;

/**
 * One flat listing row (mockup `.row`): a 40x40 lead (Fluent emoji for folders and file types, a 4-radius thumbnail for
 * images, a checkbox in select mode), the name cut in the middle, the item count or time below and a file's size on the
 * right. Folders also show their favourite and pinned marks. Details are one long-press (selection) away. A selected row is the `Row` selected state.
 */
export function EntryTile({
  entry, hostId, selected, multiSelect, isFavorite, isPinned, onPress, onLongPress, onSelect, onPeek, density,
}: {
  entry: Entry; hostId: string; selected: boolean; multiSelect: boolean; isFavorite?: boolean; isPinned?: boolean;
  onPress: () => void; onLongPress: () => void; onSelect: () => void; onPeek?: () => void; density?: EntryDensity;
}) {
  const c = useScheme();
  const meta = entryMeta(entry);
  const size = entrySize(entry);
  const emoji = <Emoji name={emojiNameFor(entry, { open: entry.isDir && selected })} size={LEAD} />;
  const picture = isImageEntry(entry) ? <Thumbnail hostId={hostId} entry={entry} size={128} style={{ width: LEAD, height: LEAD, borderRadius: Radii.file, overflow: 'hidden' }} fallback={emoji} /> : emoji;

  const lead = multiSelect ? (
    <Pressable onPress={onSelect} accessibilityRole="checkbox" accessibilityState={{ checked: selected }} style={{ width: LEAD, height: LEAD }}>
      <View style={{ width: LEAD, height: LEAD, alignItems: 'center', justifyContent: 'center' }}>
        <Checkbox checked={selected} />
      </View>
    </Pressable>
  ) : onPeek && !entry.isDir ? (
    <Pressable onPress={onPress} onLongPress={onPeek} accessibilityLabel={entry.name} accessibilityHint={t('peekHint')} style={{ width: LEAD, height: LEAD }}>
      {picture}
    </Pressable>
  ) : (
    picture
  );

  const marks = entry.isDir && (isFavorite || isPinned);
  const trailing = marks || size ? (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs }}>
      {isFavorite ? <Star size={14} color={c.onSurfaceVariant} fill={c.onSurfaceVariant} /> : null}
      {isPinned ? <PinIcon size={14} color={c.onSurfaceVariant} /> : null}
      {size ? <Text style={LumenType.meta} muted>{size}</Text> : null}
    </View>
  ) : undefined;

  const spoken = [meta, size].filter(Boolean).join(', ');
  return <Row title={entry.name} subtitle={meta || undefined} lead={lead} trailing={trailing} selected={selected} density={density} onPress={onPress} onLongPress={onLongPress} accessibilityLabel={`${entry.name}${spoken ? `, ${spoken}` : ''}`} />;
}
