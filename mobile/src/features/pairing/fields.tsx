import { Monitor } from 'lucide-react-native';
import { useRef, useState, type ReactNode } from 'react';
import { TextInput, View, type TextInputProps } from 'react-native';

import { Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily } from '../../design/tokens';
import { t } from '../../i18n';
import { CODE_LENGTH, sanitizeCode } from './codeInput';

type FieldProps = TextInputProps & { label: string; error?: string | null; helper?: string; leading?: ReactNode; trailing?: ReactNode; mono?: boolean };

/**
 * Lumen field: a flat raised tile with no outline. The label sits above in the caption style; focus only lifts the fill
 * towards the host colour, and an error turns the message (not a border) red.
 */
export function LumenField({ label, error, helper, leading, trailing, mono, style, onFocus, onBlur, ...rest }: FieldProps) {
  const c = useScheme();
  const [focused, setFocused] = useState(false);
  return (
    <View style={{ gap: 8 }}>
      <Text style={LumenType.meta} color={c.onSurfaceVariant}>
        {label}
      </Text>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: 12,
          minHeight: 56,
          paddingHorizontal: 16,
          borderRadius: LumenSize.tileRadius,
          backgroundColor: focused ? mix(c.primary, c.surfaceContainerHigh, 0.12) : c.surfaceContainerHigh,
        }}
      >
        {leading}
        <TextInput
          accessibilityLabel={label}
          placeholderTextColor={c.onSurfaceVariant}
          selectionColor={c.primary}
          {...rest}
          onFocus={(e) => {
            setFocused(true);
            onFocus?.(e);
          }}
          onBlur={(e) => {
            setFocused(false);
            onBlur?.(e);
          }}
          style={[{ flex: 1, color: c.onSurface, paddingVertical: 12, fontSize: 18, fontFamily: mono ? FontFamily.mono : FontFamily.regular }, style]}
        />
        {trailing}
      </View>
      {helper && !error ? (
        <Text style={LumenType.meta} color={c.onSurfaceVariant}>
          {helper}
        </Text>
      ) : null}
      {error ? (
        <Text style={LumenType.meta} color={c.error} accessibilityRole="alert">
          {error}
        </Text>
      ) : null}
    </View>
  );
}

export function AddressField({ value, onChange, error }: { value: string; onChange: (v: string) => void; error?: string | null }) {
  const c = useScheme();
  return (
    <LumenField
      label={t('agentAddressLabel')}
      placeholder={t('agentAddressHint')}
      value={value}
      onChangeText={onChange}
      autoCapitalize="none"
      autoCorrect={false}
      keyboardType="url"
      error={error}
      leading={<Monitor size={22} color={c.onSurfaceVariant} />}
    />
  );
}

/**
 * 8 display boxes over ONE real text input (the standard OTP pattern): fast typing, paste and IME
 * composition are handled by the native input rather than 8 racing controlled fields. Boxes are flat; the box under the
 * cursor is tinted with the host colour instead of outlined.
 */
export function CodeBoxRow({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const c = useScheme();
  const ref = useRef<TextInput>(null);
  const [focused, setFocused] = useState(false);
  const chars = value.padEnd(CODE_LENGTH, ' ').slice(0, CODE_LENGTH).split('');
  const cursor = Math.min(value.length, CODE_LENGTH - 1);
  return (
    <View style={{ height: 56 }}>
      <View style={{ flexDirection: 'row', justifyContent: 'center', gap: 6 }} pointerEvents="none" accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
        {chars.map((ch, i) => (
          <View
            key={i}
            style={{
              flex: 1,
              maxWidth: 40,
              height: 56,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: focused && i === cursor ? mix(c.primary, c.surfaceContainerHigh, 0.3) : c.surfaceContainerHigh,
              borderRadius: LumenSize.tileRadius,
            }}
          >
            <Text style={{ fontFamily: FontFamily.monoMedium, fontSize: 20 }}>{ch.trim()}</Text>
          </View>
        ))}
      </View>
      <TextInput
        ref={ref}
        accessibilityLabel={`Pairing code, ${CODE_LENGTH} characters`}
        value={value}
        onChangeText={(v) => onChange(sanitizeCode(v))}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        autoCapitalize="characters"
        autoCorrect={false}
        autoComplete="off"
        maxLength={CODE_LENGTH * 2}
        caretHidden
        style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, opacity: 0.02, color: 'transparent' }}
      />
    </View>
  );
}
