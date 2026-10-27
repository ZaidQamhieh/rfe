import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { BottomSheet, Button, MockupSwitch, Pressable, SheetHead, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { DATE_PRESETS, defaultFilters, SIZE_PRESETS, type SearchFilters, type SearchMode } from './searchLogic';

const MODES: { key: SearchMode; label: 'searchModeSubstring' | 'searchModeGlob' | 'searchModeRegex' }[] = [
  { key: 'substring', label: 'searchModeSubstring' },
  { key: 'glob', label: 'searchModeGlob' },
  { key: 'regex', label: 'searchModeRegex' },
];

/** Search mode, size, date, scope and hidden-items filters. Edits stay local until Apply; Reset restores the defaults. */
export function SearchFilterSheet({ visible, filters, currentPath, onApply, onClose }: { visible: boolean; filters: SearchFilters; currentPath: string; onApply: (f: SearchFilters) => void; onClose: () => void }) {
  const [draft, setDraft] = useState(filters);
  // Start from the applied filters each time the sheet opens.
  const [wasVisible, setWasVisible] = useState(visible);
  if (visible !== wasVisible) {
    setWasVisible(visible);
    if (visible) setDraft(filters);
  }
  const set = (p: Partial<SearchFilters>) => setDraft((d) => ({ ...d, ...p }));

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('searchFiltersTooltip')} />
      <ScrollView>
        <View style={{ paddingHorizontal: 18, paddingBottom: 18, gap: Spacing.md }}>
          <Group label={t('searchModeLabel')}>
            {MODES.map((m) => (
              <Chip key={m.key} label={t(m.label)} selected={draft.mode === m.key} onPress={() => set({ mode: m.key })} />
            ))}
          </Group>
          <Group label={t('fileSize')}>
            {SIZE_PRESETS.map((p) => (
              <Chip key={p.key} label={t(p.label)} selected={draft.size === p.key} onPress={() => set({ size: p.key })} />
            ))}
          </Group>
          <Group label={t('sortFieldDate')}>
            {DATE_PRESETS.map((p) => (
              <Chip key={p.key} label={t(p.label)} selected={draft.date === p.key} onPress={() => set({ date: p.key })} />
            ))}
          </Group>
          <View style={{ gap: Spacing.sm }}>
            <Label text={t('searchScope')} />
            <ToggleRow title={draft.fromHere ? t('searchingIn', { path: currentPath }) : t('searchingEverywhere')} value={!draft.fromHere} onToggle={() => set({ fromHere: !draft.fromHere })} />
            <ToggleRow title={t('includeHiddenItems')} subtitle={t('includeHiddenSubtitle')} value={draft.includeHidden} onToggle={() => set({ includeHidden: !draft.includeHidden })} />
          </View>
          <View style={{ flexDirection: 'row', gap: Spacing.sm, marginTop: Spacing.sm }}>
            <Button size="lg" kind="neutral" label={t('resetButton')} style={{ flex: 2 }} onPress={() => setDraft(defaultFilters())} />
            <Button size="lg" label={t('applyButton')} style={{ flex: 3 }} onPress={() => onApply(draft)} />
          </View>
        </View>
      </ScrollView>
    </BottomSheet>
  );
}

function Label({ text }: { text: string }) {
  return <Text style={LumenType.sectionLabel} muted>{text}</Text>;
}

function Group({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <View style={{ gap: Spacing.xs }}>
      <Label text={label} />
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 8 }}>{children}</View>
    </View>
  );
}

function Chip({ label, selected, onPress }: { label: string; selected: boolean; onPress: () => void }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} hitSlop={4} accessibilityLabel={label} accessibilityState={{ selected }}>
      <View style={{ minHeight: 40, justifyContent: 'center', paddingHorizontal: 16, borderRadius: Radii.stadium, backgroundColor: selected ? mix(c.primary, c.surfaceContainer, 0.2) : c.surfaceContainerHigh }}>
        <Text style={LumenType.pill} color={selected ? c.primary : c.onSurfaceVariant}>{label}</Text>
      </View>
    </Pressable>
  );
}

function ToggleRow({ title, subtitle, value, onToggle }: { title: string; subtitle?: string; value: boolean; onToggle: () => void }) {
  return (
    <Pressable onPress={onToggle} accessibilityLabel={title} accessibilityRole="switch" accessibilityState={{ checked: value }}>
      <View style={{ minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: Spacing.sm }}>
        <View style={{ flex: 1 }}>
          <Text numberOfLines={2} style={LumenType.name}>{title}</Text>
          {subtitle ? <Text muted style={LumenType.meta}>{subtitle}</Text> : null}
        </View>
        <MockupSwitch value={value} />
      </View>
    </Pressable>
  );
}
