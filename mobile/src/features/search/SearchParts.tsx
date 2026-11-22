import { useRouter } from 'expo-router';
import { ArrowLeft, Info, Search, X } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, Text as RNText, TextInput, View } from 'react-native';

import type { Entry, Snippet } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { Button, Chip, DuoIcon, Emoji, Pressable, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { emojiNameFor } from '../explorer/entryEmoji';
import { highlightRange } from './searchLogic';
import { parentFolderName } from './searchScope';

/**
 * Search bar (`.bar` with a `.field`): a back button and a 48 high field with the search glyph, the input and a clear
 * button once there is text. The field has a 1px hairline and a 2px accent border while it has focus.
 */
export function SearchBarRow({ value, onChange, onSubmit, placeholder, onClear }: { value: string; onChange: (v: string) => void; onSubmit?: () => void; placeholder: string; onClear: () => void }) {
  const c = useScheme();
  const router = useRouter();
  const [focused, setFocused] = useState(false);
  return (
    <View style={{ height: 64, flexDirection: 'row', alignItems: 'center', paddingLeft: Spacing.xs, paddingRight: Spacing.md2 }}>
      <Pressable onPress={() => router.back()} accessibilityLabel={t('searchBack')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={ArrowLeft} size={24} />
      </Pressable>
      <View style={{ flex: 1, height: 48, flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: focused ? 11 : Spacing.md2, borderRadius: Radii.control, borderWidth: focused ? 2 : 1, borderColor: focused ? c.primary : c.rule2, backgroundColor: c.surfaceContainerLowest }}>
        <Search size={20} color={c.onSurfaceVariant} />
        <TextInput
          accessibilityLabel={placeholder}
          autoFocus
          value={value}
          onChangeText={onChange}
          onSubmitEditing={onSubmit}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder={placeholder}
          placeholderTextColor={c.onSurfaceVariant}
          returnKeyType="search"
          autoCapitalize="none"
          autoCorrect={false}
          style={{ flex: 1, padding: 0, fontSize: 16, fontFamily: FontFamily.regular, color: c.onSurface }}
        />
        {value !== '' && (
          <Pressable onPress={onClear} hitSlop={12} accessibilityLabel={t('searchClear')}>
            <X size={20} color={c.onSurfaceVariant} />
          </Pressable>
        )}
      </View>
    </View>
  );
}

/** One scope chip: its label, whether it is the active choice, and what a tap does. */
export type ScopeChipSpec = { key: string; label: string; selected?: boolean; dropdown?: boolean; onPress: () => void };

/** Scrolling row of scope chips (`.chips`): 16 side padding, 8 gap; a chip with `dropdown` opens a choice list, shown by a small chevron. */
export function ScopeChips({ chips }: { chips: ScopeChipSpec[] }) {
  const c = useScheme();
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0 }} contentContainerStyle={{ paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm, gap: Spacing.sm, alignItems: 'center' }} keyboardShouldPersistTaps="handled">
      {chips.map((x) => (
        <Chip key={x.key} label={x.label} selected={x.selected} onPress={x.onPress} icon={x.dropdown ? <RNText style={{ color: c.onSurfaceVariant, fontSize: 12 }}>{'▾'}</RNText> : undefined} />
      ))}
    </ScrollView>
  );
}

/** Name with the matched part in 600 weight (no coloured background); long names are cut in the middle. */
function HighlightedName({ name, query, highlight }: { name: string; query: string; highlight: boolean }) {
  const c = useScheme();
  const base = { ...LumenType.name, color: c.onSurface } as const;
  const r = highlight ? highlightRange(name, query) : null;
  if (!r) return <RNText numberOfLines={1} ellipsizeMode="middle" style={base}>{name}</RNText>;
  return (
    <RNText numberOfLines={1} ellipsizeMode="middle" style={base}>
      {name.slice(0, r.start)}
      <RNText style={{ fontFamily: FontFamily.semibold }}>{name.slice(r.start, r.end)}</RNText>
      {name.slice(r.end)}
    </RNText>
  );
}

/** The matching line of a content hit, the match in 600 weight; one line, cut by the row. */
function SnippetLine({ snippet }: { snippet: Snippet }) {
  const { text, start, length } = snippet;
  return (
    <Text numberOfLines={1} muted style={LumenType.meta} accessibilityLabel={t('searchSnippetLabel', { text })}>
      {text.slice(0, start)}
      <RNText style={{ fontFamily: FontFamily.semibold }}>{text.slice(start, start + length)}</RNText>
      {text.slice(start + length)}
    </Text>
  );
}

/**
 * One search hit as a flat row (`.row`): the thing's emoji, the name with the match in 600 weight, the parent folder's
 * name ("in Documents") under it and the size on the right. The full path is not shown. Same metrics as the shared Row, which only takes a plain title.
 */
export function ResultRow({ entry, query, highlight, onPress, label }: { entry: Entry; query: string; highlight: boolean; onPress: () => void; label?: string }) {
  const c = useScheme();
  const size = entry.isDir ? '' : formatSize(entry.size);
  const folder = parentFolderName(entry.path);
  return (
    <Pressable onPress={onPress} accessibilityLabel={label ?? entry.name} pressedScale={1}>
      <View style={{ minHeight: LumenSize.rowMinHeight, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md }}>
        <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
          <Emoji name={emojiNameFor(entry)} size={32} />
        </View>
        <View style={{ flex: 1, minWidth: 0 }}>
          <HighlightedName name={entry.name} query={query} highlight={highlight} />
          {entry.snippet ? <SnippetLine snippet={entry.snippet} /> : null}
          {folder ? <Text numberOfLines={1} muted style={LumenType.meta}>{t('searchInFolder', { folder })}</Text> : null}
        </View>
        {size ? <Text style={[LumenType.monoSmall, { fontSize: 13 }]} color={c.onSurfaceVariant}>{size}</Text> : null}
      </View>
    </Pressable>
  );
}

/** Sentence-case 14/500 label above a group of rows (`.sec`). */
export function SectionHeading({ text }: { text: string }) {
  return <Text accessibilityRole="header" style={[LumenType.sectionLabel, { paddingHorizontal: Spacing.md, paddingTop: Spacing.md3, paddingBottom: 6 }]} muted>{text}</Text>;
}

/** Nothing matched: an emoji, one sentence, a hint and short buttons (`.empty`). */
export function NoResults({ title, hint, actions }: { title: string; hint?: string; actions?: { label: string; onPress: () => void; primary?: boolean }[] }) {
  return (
    <View style={{ paddingTop: Spacing.xl, paddingHorizontal: Spacing.lg, gap: Spacing.sm }} accessibilityRole="summary">
      <Emoji name="card_file_box" size={48} />
      <Text style={[LumenType.pageTitle, { marginTop: Spacing.sm }]}>{title}</Text>
      {hint ? <Text style={[LumenType.name, { lineHeight: 24 }]} muted>{hint}</Text> : null}
      {actions && actions.length > 0 ? (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.sm, marginTop: Spacing.md2 }}>
          {actions.map((a) => (
            <Button key={a.label} label={a.label} kind={a.primary ? 'neutral' : 'text'} onPress={a.onPress} />
          ))}
        </View>
      ) : null}
    </View>
  );
}

/** Placeholder rows shown while a search runs: a round-cornered emoji slot and two grey bars per row. */
export function ResultSkeleton({ rows = 8 }: { rows?: number }) {
  const c = useScheme();
  return (
    <View accessibilityRole="progressbar" accessibilityLabel={t('searchingProgress')}>
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ minHeight: LumenSize.rowMinHeight, flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.md }}>
          <View style={{ width: 40, height: 40, borderRadius: Radii.control, backgroundColor: c.surfaceContainerHigh }} />
          <View style={{ gap: 6 }}>
            <View style={{ height: 14, width: i % 2 ? 130 : 190, borderRadius: Radii.file, backgroundColor: c.surfaceContainerHigh }} />
            <View style={{ height: 12, width: 100, borderRadius: Radii.file, backgroundColor: c.surfaceContainerHigh }} />
          </View>
        </View>
      ))}
    </View>
  );
}

/** Static chip shown when the query is treated as a glob or regex. */
export function GlobIndicator() {
  return (
    <View style={{ paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm, flexDirection: 'row' }}>
      <Chip label={t('globPattern')} selected />
    </View>
  );
}

/** Notice above a result list: the agent stopped early (result cap or time budget). A warn-tinted card. */
export function TruncationBanner({ message }: { message: string }) {
  const c = useScheme();
  const roles = useRoles();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, marginHorizontal: 18, marginVertical: 6, paddingHorizontal: 14, paddingVertical: 10, borderRadius: LumenSize.cardRadius, backgroundColor: mix(roles.warn, c.surfaceContainer, 0.15) }}>
      <Info size={18} color={roles.warn} />
      <Text style={[LumenType.meta, { flex: 1 }]} color={c.onSurface}>{message}</Text>
    </View>
  );
}
