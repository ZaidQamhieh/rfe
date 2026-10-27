import { EyeOff, Plus, X } from 'lucide-react-native';
import { useState } from 'react';
import { TextInput, View } from 'react-native';

import { GroupedCard, Pressable, SectionLabel, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { visibilityPresets, type VisibilityPrefs } from '../../core/visibility';
import { ToggleRow } from './parts';
import { addExtension, addName, customExtensions, removeExtension, removeName, setHideDotfiles } from './visibilityEdit';

/**
 * Which files the explorer hides: dotfiles, and per file type (extension or exact name) grouped by category. It edits
 * a value and reports the next one, so the same editor serves the app default and a device's own override.
 */
export function VisibilityEditor({ prefs, onChange }: { prefs: VisibilityPrefs; onChange: (next: VisibilityPrefs) => void }) {
  const c = useScheme();
  const [draft, setDraft] = useState('');
  const custom = customExtensions(prefs);
  const lowerNames = new Set([...prefs.hiddenNames].map((n) => n.toLowerCase()));

  const submit = () => {
    if (draft.trim() === '') return;
    onChange(addExtension(prefs, draft));
    setDraft('');
  };

  return (
    <View style={{ gap: 16 }}>
      <GroupedCard padded={false} style={{ paddingHorizontal: 14, paddingVertical: 4 }}>
        <ToggleRow icon={EyeOff} tint={c.primary} title={t('hideDotfiles')} subtitle={t('hideDotfilesSubtitle')} value={prefs.hideDotfiles} onChange={(v) => onChange(setHideDotfiles(prefs, v))} />
      </GroupedCard>
      {visibilityPresets.map((preset) => (
        <View key={preset.label}>
          <SectionLabel title={preset.label} />
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 8 }}>
            {[...preset.extensions].sort().map((ext) => {
              const on = prefs.hiddenExtensions.has(ext);
              return <Chip key={ext} label={`.${ext}`} selected={on} onPress={() => onChange(on ? removeExtension(prefs, ext) : addExtension(prefs, ext))} />;
            })}
            {[...preset.names].sort().map((name) => {
              const on = lowerNames.has(name.toLowerCase());
              return <Chip key={name} label={name} selected={on} onPress={() => onChange(on ? removeName(prefs, name) : addName(prefs, name))} />;
            })}
          </View>
        </View>
      ))}
      <View>
        <SectionLabel title={t('customLabel')} />
        {custom.length === 0 ? (
          <Text style={[LumenType.meta, { marginBottom: 8, paddingHorizontal: 4 }]} muted>{t('noCustomExtensions')}</Text>
        ) : (
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 8 }}>
            {custom.map((ext) => (
              <Chip key={ext} label={`.${ext}`} selected onPress={() => onChange(removeExtension(prefs, ext))} trailing={<X size={14} color={c.onPrimary} />} />
            ))}
          </View>
        )}
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.xs, borderRadius: Radii.chip, paddingLeft: 14, minHeight: 48, backgroundColor: c.surfaceContainer }}>
          <Text muted style={LumenType.name}>.</Text>
          <TextInput
            accessibilityLabel={t('addExtensionHint')}
            value={draft}
            onChangeText={setDraft}
            onSubmitEditing={submit}
            placeholder={t('addExtensionHint')}
            placeholderTextColor={c.onSurfaceVariant}
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="done"
            style={{ flex: 1, paddingVertical: 10, color: c.onSurface, fontFamily: FontFamily.regular, fontSize: 16 }}
          />
          <Pressable onPress={submit} pressedScale={0.92} accessibilityLabel={t('addExtensionTooltip')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
            <Plus size={18} color={c.onSurface} />
          </Pressable>
        </View>
      </View>
    </View>
  );
}

function Chip({ label, selected, onPress, trailing }: { label: string; selected: boolean; onPress: () => void; trailing?: React.ReactNode }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={label} accessibilityState={{ selected }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, minHeight: 40, borderRadius: Radii.stadium, backgroundColor: selected ? c.primary : c.surfaceContainer }}>
        <Text style={LumenType.pill} color={selected ? c.onPrimary : c.onSurfaceVariant}>{label}</Text>
        {selected ? trailing : null}
      </View>
    </Pressable>
  );
}
