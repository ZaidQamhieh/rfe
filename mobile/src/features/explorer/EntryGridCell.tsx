import { Check, Pin as PinIcon, Star } from 'lucide-react-native';
import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { Emoji, Pressable, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { emojiNameFor } from './entryEmoji';
import { isImageEntry } from './EntryIcon';
import { formatDuration } from './gridLogic';
import { Thumbnail } from './Thumbnail';
import { useVideoDuration } from './videoDuration';

/** Over a photo the check box and the duration are drawn in fixed light/dark colours, whatever the theme (mockup `.grid3 .cell`). */
const CHECK_OFF_FILL = 'rgba(255,255,255,0.85)';
const CHECK_INK = '#1D1E1B';
const CHECK_ON_FILL = '#1A1A1A';
const CHECK_RING = '#FFFFFF';
/** The soft dark shadow under a duration label on a photo. */
const DURATION_SHADOW = 'rgba(0,0,0,0.6)';
const DURATION_SHADOW_RADIUS = 3;
/** A selected picture shrinks to 86% inside its cell (mockup `.cell.sel svg`). */
const SELECTED_SCALE = 0.86;
const CHECK_SIZE = 22;
const CHECK_INSET = 8;

/** Mockup `.day`: a day's name on the left and how many items it holds on the right (a figure, so mono), 14/20 weight 500 in ink-2. */
export function DayHeader({ label, count }: { label: string; count: number }) {
  return (
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', paddingTop: Spacing.md, paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm }} accessibilityRole="header">
      <Text muted style={LumenType.sectionLabel}>
        {label}
      </Text>
      <Text muted style={[LumenType.sectionLabel, { fontFamily: FontFamily.monoMedium }]}>
        {count}
      </Text>
    </View>
  );
}

/** The 22 dp selection box in a photo's top-left corner: a light box with a dark outline, a dark box with a white check and ring when picked. */
function CellCheck({ checked }: { checked: boolean }) {
  return (
    <View
      accessibilityRole="checkbox"
      accessibilityState={{ checked }}
      style={{
        position: 'absolute',
        top: CHECK_INSET,
        left: CHECK_INSET,
        width: CHECK_SIZE,
        height: CHECK_SIZE,
        borderRadius: Radii.file,
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: checked ? CHECK_ON_FILL : CHECK_OFF_FILL,
        borderWidth: checked ? 1.5 : 1.75,
        borderColor: checked ? CHECK_RING : CHECK_INK,
      }}
    >
      {checked ? <Check size={16} color={CHECK_RING} strokeWidth={2.5} /> : null}
    </View>
  );
}

/**
 * One square cell of the grid view. A picture fills the cell (it shrinks and gets a 4 dp radius when selected); a folder
 * or other file shows its Fluent emoji with the name under it. A selection box sits top-left in select mode, and a video
 * length shows bottom-right in mono: [durationMs] when the caller gives one, else the length looked up lazily from the
 * computer for a video (the listing itself carries none).
 */
export function EntryGridCell({
  entry, hostId, size, selected, multiSelect, isFavorite, isPinned, durationMs, onPress, onLongPress,
}: {
  entry: Entry; hostId: string; size: number; selected: boolean; multiSelect: boolean; isFavorite?: boolean; isPinned?: boolean; durationMs?: number;
  onPress: () => void; onLongPress: () => void;
}) {
  const c = useScheme();
  const picture = isImageEntry(entry);
  const lookedUp = useVideoDuration(hostId, entry);
  const duration = formatDuration(durationMs ?? lookedUp);
  const emoji = (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center', gap: 6, paddingHorizontal: Spacing.sm, backgroundColor: c.surfaceContainer }}>
      <Emoji name={emojiNameFor(entry, { open: entry.isDir && selected })} size={48} />
      <Text numberOfLines={2} ellipsizeMode="middle" style={[LumenType.meta, { textAlign: 'center' }]}>
        {entry.name}
      </Text>
    </View>
  );
  const body = picture ? (
    <View style={{ width: size, height: size, backgroundColor: selected ? c.fill2 : c.surfaceContainer, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: size, height: size, transform: [{ scale: selected ? SELECTED_SCALE : 1 }], borderRadius: selected ? Radii.file : 0, overflow: 'hidden' }}>
        <Thumbnail hostId={hostId} entry={entry} size={256} style={{ width: size, height: size }} fallback={emoji} />
      </View>
    </View>
  ) : (
    <View style={{ width: size, height: size, borderRadius: selected ? Radii.file : 0, borderWidth: selected ? 1 : 0, borderColor: c.selBd, overflow: 'hidden' }}>{emoji}</View>
  );
  const mark = (pos: object, children: React.ReactNode) => (
    <View style={{ position: 'absolute', width: CHECK_SIZE, height: CHECK_SIZE, borderRadius: CHECK_SIZE / 2, backgroundColor: c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center', ...pos }}>{children}</View>
  );
  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      accessibilityLabel={entry.name}
      accessibilityState={{ selected }}
      pressedScale={1}
      style={{ width: size }}
    >
      <View style={{ width: size, height: size }}>
        {body}
        {multiSelect ? <CellCheck checked={selected} /> : null}
        {duration ? (
          <Text style={[LumenType.monoSmall, { position: 'absolute', right: CHECK_INSET, bottom: 4, textShadowColor: DURATION_SHADOW, textShadowRadius: DURATION_SHADOW_RADIUS, textShadowOffset: { width: 0, height: 0 } }]} color={CHECK_RING}>
            {duration}
          </Text>
        ) : null}
        {isFavorite && entry.isDir ? mark({ top: 6, right: 6 }, <Star size={13} color={c.onSurfaceVariant} fill={c.onSurfaceVariant} />) : null}
        {isPinned && entry.isDir ? mark({ bottom: 6, right: 6 }, <PinIcon size={12} color={c.onSurfaceVariant} />) : null}
      </View>
    </Pressable>
  );
}
