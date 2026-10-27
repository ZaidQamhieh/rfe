import { TriangleAlert } from 'lucide-react-native';
import { useState, type ReactNode } from 'react';
import { KeyboardAvoidingView, Modal, Platform, Pressable as RNPressable, ScrollView, View } from 'react-native';

import { t } from '../../i18n';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Button } from './Button';
import { withAlpha } from './Callouts';
import { SheetHero } from './Sheet';
import { Text } from './Text';
import { TextField } from './TextField';

function Center({ visible, onClose, children }: { visible: boolean; onClose: () => void; children: ReactNode }) {
  const c = useScheme();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose} statusBarTranslucent>
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={{ flex: 1 }}>
        <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.5)', justifyContent: 'center', padding: 24 }} onPress={onClose} accessibilityLabel="Dismiss">
          <RNPressable accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderRadius: Radii.lg, overflow: 'hidden', maxHeight: '90%' }}>
            {children}
          </RNPressable>
        </RNPressable>
      </KeyboardAvoidingView>
    </Modal>
  );
}

/** Single text input dialog (name/tag/path prompts). Confirm stays disabled while empty unless `allowEmpty`. */
export function PromptDialog({
  visible, title, description, placeholder, initialValue = '', confirmLabel, cancelLabel = t('cancelButton'), allowEmpty, mono, helper, validate, keyboardType, onSubmit, onCancel,
}: {
  visible: boolean; title: string; description?: string; placeholder?: string; initialValue?: string; confirmLabel: string; cancelLabel?: string; allowEmpty?: boolean; mono?: boolean; helper?: string; validate?: (value: string) => string | null; keyboardType?: 'default' | 'url' | 'number-pad'; onSubmit: (value: string) => void; onCancel: () => void;
}) {
  const [value, setValue] = useState(initialValue);
  // Reset the field each time the dialog opens (state adjusted during render, not in an effect).
  const [wasVisible, setWasVisible] = useState(visible);
  if (visible !== wasVisible) {
    setWasVisible(visible);
    if (visible) setValue(initialValue);
  }
  const problem = validate && value.trim() !== '' ? validate(value.trim()) : null;
  const ok = (allowEmpty || value.trim().length > 0) && problem === null;
  return (
    <Center visible={visible} onClose={onCancel}>
      <View style={{ padding: 24, gap: 12 }}>
        <Text variant="titleLarge" accessibilityRole="header">{title}</Text>
        {description ? <Text muted>{description}</Text> : null}
        <TextField label={placeholder ?? title} hideLabel placeholder={placeholder} value={value} onChangeText={setValue} autoFocus autoCapitalize="none" autoCorrect={false} mono={mono} helper={helper} error={problem} keyboardType={keyboardType} onSubmitEditing={() => ok && onSubmit(value.trim())} />
        <View style={{ flexDirection: 'row', justifyContent: 'flex-end', gap: 8 }}>
          <Button kind="text" label={cancelLabel} onPress={onCancel} />
          <Button kind="filled" label={confirmLabel} disabled={!ok} onPress={() => onSubmit(value.trim())} />
        </View>
      </View>
    </Center>
  );
}

export type ChoiceOption<T extends string> = { value: T; label: string; icon?: ReactNode; tint?: string };

/** Dialog with a hero and a list of choices (conflict resolution, three-way delete). */
export function ChoiceDialog<T extends string>({ visible, title, subtitle, options, icon, tint, onChoose, onDismiss }: { visible: boolean; title: string; subtitle?: string; options: ChoiceOption<T>[]; icon?: ReactNode; tint?: string; onChoose: (v: T) => void; onDismiss: () => void }) {
  const c = useScheme();
  return (
    <Center visible={visible} onClose={onDismiss}>
      <SheetHero showGrabber={false} badge={icon ?? <TriangleAlert size={24} color={c.primary} />} badgeColor={tint ? withAlpha(tint, 0.2) : undefined} tint={tint} title={title} subtitle={subtitle} />
      <View style={{ padding: Spacing.lg, paddingTop: 0 }}>
        <View style={{ backgroundColor: c.surfaceContainerHighest, borderRadius: Radii.card, overflow: 'hidden' }}>
          {options.map((o, i) => (
            <RNPressable key={o.value} accessibilityRole="button" accessibilityLabel={o.label} onPress={() => onChoose(o.value)} style={{ flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: Spacing.md, minHeight: 48, borderTopWidth: i ? 1 : 0, borderColor: withAlpha(c.outlineVariant, 0.5) }}>
              {o.icon}
              <Text variant="bodyLarge" color={o.tint}>{o.label}</Text>
            </RNPressable>
          ))}
        </View>
      </View>
    </Center>
  );
}

/** Scrollable message dialog with an OK button (batch failure report). */
export function ReportDialog({ visible, title, items, onClose }: { visible: boolean; title: string; items: { primary: string; secondary?: string }[]; onClose: () => void }) {
  const c = useScheme();
  return (
    <Center visible={visible} onClose={onClose}>
      <SheetHero showGrabber={false} badge={<TriangleAlert size={24} color={c.error} />} tint={c.error} title={title} />
      <ScrollView style={{ maxHeight: 320 }}>
        {items.map((it, i) => (
          <View key={i} style={{ paddingHorizontal: Spacing.lg, paddingVertical: 6 }}>
            <Text style={{ fontSize: 14, fontFamily: 'Lato_400Regular' }}>{it.primary}</Text>
            {it.secondary ? <Text muted style={{ fontSize: 11.5 }}>{it.secondary}</Text> : null}
          </View>
        ))}
      </ScrollView>
      <View style={{ flexDirection: 'row', justifyContent: 'flex-end', padding: Spacing.md }}>
        <Button kind="text" label={t('okButton')} onPress={onClose} />
      </View>
    </Center>
  );
}
