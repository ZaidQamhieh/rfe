import { EyeOff, Plus, X } from 'lucide-react-native';
import { useState } from 'react';
import { TextInput, View } from 'react-native';

import { Chip, DuoIcon, Pressable, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { visibilityPresets, type VisibilityPrefs } from '../../core/visibility';
import { ToggleRow } from './parts';
import { addExtension, addName, customExtensions, removeExtension, removeName, setHideDotfiles } from './visibilityEdit';

/** A `.sec` style label for one group of chips, with the 16 side padding the flat lists use. */
function GroupLabel({ title }: { title: string }) {
  return (
    <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { paddingHorizontal: Spacing.md, paddingTop: Spacing.md2, paddingBottom: 6 }]}>
      {title}
    </Text>
  );
}

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
    <View>
      <ToggleRow icon={EyeOff} title={t('hideDotfiles')} subtitle={t('hideDotfilesSubtitle')} value={prefs.hideDotfiles} onChange={(v) => onChange(setHideDotfiles(prefs, v))} />
      {visibilityPresets.map((preset) => (
        <View key={preset.labelKey}>
          <GroupLabel title={t(preset.labelKey)} />
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.sm, paddingHorizontal: Spacing.md }}>
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
      <GroupLabel title={t('customLabel')} />
      {custom.length === 0 ? (
        <Text style={[LumenType.meta, { marginBottom: Spacing.sm, paddingHorizontal: Spacing.md }]} muted>
          {t('noCustomExtensions')}
        </Text>
      ) : (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.sm, marginBottom: Spacing.sm, paddingHorizontal: Spacing.md }}>
          {custom.map((ext) => (
            <Chip key={ext} label={`.${ext}`} selected icon={<DuoIcon icon={X} size={14} color={c.onSurface} />} onPress={() => onChange(removeExtension(prefs, ext))} />
          ))}
        </View>
      )}
      <View style={{ marginHorizontal: Spacing.md, flexDirection: 'row', alignItems: 'center', gap: Spacing.xs, borderRadius: Radii.control, borderWidth: 1, borderColor: c.rule2, paddingLeft: 14, minHeight: 48 }}>
        <Text muted style={LumenType.name}>
          .
        </Text>
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
          <DuoIcon icon={Plus} size={20} />
        </Pressable>
      </View>
    </View>
  );
}
