import { ArrowLeft, ChevronDown, EllipsisVertical, Search } from 'lucide-react-native';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { DuoIcon, Emoji, Pressable, Text, type EmojiName } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import type { Lamp } from './nameplateLogic';
import { Spacing } from '../../design/tokens';

/** Nameplate `.lamp`: an 8 px dot, solid for go, wait and stop, hollow (a 1.5 px ring) when off. */
function LampDot({ lamp }: { lamp: Lamp }) {
  const c = useScheme();
  const fill = lamp === 'go' ? c.go : lamp === 'wait' ? c.wait : lamp === 'stop' ? c.stop : 'transparent';
  return <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: fill, borderWidth: lamp === 'off' ? 1.5 : 0, borderColor: c.outline }} />;
}

/** One `.state` line: a lamp and a 14/20 phrase in ink-2, indented to sit under the computer's name. */
function StateRow({ lamp, text }: { lamp: Lamp; text: string }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, marginTop: 2, paddingLeft: 34 }}>
      <LampDot lamp={lamp} />
      <Text style={[LumenType.caption, { flexShrink: 1 }]} muted numberOfLines={2}>
        {text}
      </Text>
    </View>
  );
}

/**
 * Nameplate `.plate`: a picture of the computer, its name (22/28, tap to switch computers) with a down chevron, a lamp
 * and a short state phrase, then search and more on the right. A computer that is off or unreachable is drawn grey.
 */
export function Nameplate({
  name,
  picture,
  off,
  lamp,
  phrase,
  notes = [],
  onSwitch,
  onBack,
  onSearch,
  onMore,
}: {
  name: string;
  picture: EmojiName;
  off: boolean;
  lamp: Lamp;
  phrase: string;
  /** Extra state lines under the phrase (read-only, low disk). */
  notes?: { lamp: Lamp; text: string }[];
  /** Opens the computer switcher; without it the name is plain text with no chevron (a screen about one computer). */
  onSwitch?: () => void;
  /** Adds a back arrow at the left, for a pushed screen. */
  onBack?: () => void;
  onSearch?: () => void;
  onMore: () => void;
}) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const title = (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs }}>
      <View style={{ marginRight: 2 }}>
        <Emoji name={picture} size={28} off={off} />
      </View>
      <Text style={[LumenType.hostName, { flexShrink: 1 }]} numberOfLines={1} accessibilityRole="header">
        {name}
      </Text>
      {onSwitch ? <DuoIcon icon={ChevronDown} size={20} color={c.onSurfaceVariant} /> : null}
    </View>
  );
  return (
    <View style={{ flexDirection: 'row', alignItems: 'flex-start', paddingTop: insets.top + 4, paddingRight: Spacing.xs, paddingBottom: Spacing.md2, paddingLeft: onBack ? 4 : 16 }}>
      {onBack ? (
        <Pressable onPress={onBack} accessibilityLabel={t('searchBack')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
          <DuoIcon icon={ArrowLeft} size={24} />
        </Pressable>
      ) : null}
      <View style={{ flex: 1, minWidth: 0, paddingTop: onBack ? 12 : 8 }}>
        {onSwitch ? (
          <Pressable onPress={onSwitch} accessibilityRole="button" accessibilityLabel={`${name}. ${t('switchComputerLabel')}`} style={{ alignSelf: 'flex-start', maxWidth: '100%' }}>
            {title}
          </Pressable>
        ) : (
          title
        )}
        <StateRow lamp={lamp} text={phrase} />
        {notes.map((n) => (
          <StateRow key={n.text} lamp={n.lamp} text={n.text} />
        ))}
      </View>
      {onSearch ? (
        <Pressable onPress={onSearch} accessibilityLabel={t('searchButton')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
          <DuoIcon icon={Search} size={24} />
        </Pressable>
      ) : null}
      <Pressable onPress={onMore} accessibilityLabel={t('moreTooltip')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={EllipsisVertical} size={24} />
      </Pressable>
    </View>
  );
}
