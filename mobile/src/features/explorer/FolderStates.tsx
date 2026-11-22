import { View } from 'react-native';

import { Button, Emoji, Text, type EmojiName } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { ExplainerText, Spacing, TypeScale } from '../../design/tokens';
import { t } from '../../i18n';

/** Widths (percent) of the name bars in the loading skeleton, varied so the rows do not look stamped. */
const SKELETON_NAME_WIDTHS = [70, 52, 84, 60, 66, 48, 76, 56];

/**
 * Mockup `.skel` rows shown while a folder is read: per row a 32x40 lead block, a name bar and a shorter meta bar, in the
 * translucent ink fill with 3 dp corners. Same 64 dp rhythm as a real row.
 */
export function FolderSkeleton({ rows = 6 }: { rows?: number }) {
  const c = useScheme();
  return (
    <View accessibilityRole="progressbar" accessibilityLabel={t('loadingFiles')} style={{ paddingTop: Spacing.xs }}>
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ minHeight: 64, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md }}>
          <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
            <View style={{ width: 32, height: 40, borderRadius: 3, backgroundColor: c.fill2 }} />
          </View>
          <View style={{ flex: 1, minWidth: 0 }}>
            <View style={{ height: 12, width: `${SKELETON_NAME_WIDTHS[i % SKELETON_NAME_WIDTHS.length]}%`, borderRadius: 3, backgroundColor: c.fill2 }} />
            <View style={{ height: 10, width: '30%', marginTop: 10, borderRadius: 3, backgroundColor: c.fill2 }} />
          </View>
        </View>
      ))}
    </View>
  );
}

/** One quiet line for the folder: previews are off because this device has no permission to download from that computer. */
export function PreviewsOffNote() {
  return (
    <View style={{ paddingHorizontal: Spacing.lg, paddingVertical: Spacing.sm }}>
      <Text style={LumenType.meta} muted>
        {t('previewsNeedPermission')}
      </Text>
    </View>
  );
}

/** A button under a status block: the first is the raised primary, the rest are outlined. */
export type StatusAction = { label: string; onPress: () => void; icon?: (color: string) => React.ReactNode };

const STATUS_BLOCK_TOP = 40;
const STATUS_EMOJI_SIZE = 48;

/**
 * Mockup `.empty`: left-aligned block with 40/24 padding, an optional 48 dp Fluent emoji, a 20/28 title, a 16/24 ink-2
 * sentence and a row of buttons. One pattern for an empty folder, no filter matches, and a folder that failed to open.
 */
export function StatusBlock({ emoji, off = false, title, hint, actions = [], top = STATUS_BLOCK_TOP }: { emoji?: EmojiName; off?: boolean; title: string; hint?: string; actions?: StatusAction[]; top?: number }) {
  return (
    <View style={{ paddingTop: top, paddingHorizontal: Spacing.lg }} accessibilityRole="summary">
      {emoji ? (
        <View style={{ marginBottom: Spacing.md, alignSelf: 'flex-start' }}>
          <Emoji name={emoji} size={STATUS_EMOJI_SIZE} off={off} />
        </View>
      ) : null}
      <Text accessibilityRole="header" style={{ ...TypeScale.screenTitle, marginBottom: Spacing.sm }}>
        {title}
      </Text>
      {hint ? (
        <Text muted style={{ ...ExplainerText, marginBottom: Spacing.md3 }}>
          {hint}
        </Text>
      ) : (
        <View style={{ height: Spacing.md2 }} />
      )}
      {actions.length > 0 ? (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.sm }}>
          {actions.map((a, i) => (
            <Button key={a.label} kind={i === 0 ? 'filled' : 'outlined'} label={a.label} onPress={a.onPress} renderIcon={a.icon} />
          ))}
        </View>
      ) : null}
    </View>
  );
}
