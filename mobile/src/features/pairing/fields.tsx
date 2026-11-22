import { Monitor } from 'lucide-react-native';
import { useRef, useState, type ReactNode } from 'react';
import { TextInput, View, type TextInputProps } from 'react-native';

import { DuoIcon, Text } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
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
    <View style={{ gap: Spacing.sm }}>
      <Text style={LumenType.meta} color={c.onSurfaceVariant}>
        {label}
      </Text>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: Spacing.md2,
          height: 48,
          paddingHorizontal: Spacing.md2,
          borderRadius: LumenSize.tileRadius,
          borderWidth: focused ? 2 : 1,
          borderColor: focused ? c.primary : c.rule2,
          backgroundColor: c.surface,
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
          style={[{ flex: 1, color: c.onSurface, paddingVertical: 0, fontSize: 16, fontFamily: mono ? FontFamily.mono : FontFamily.regular }, style]}
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
      label={t('giAddressLabel')}
      placeholder={t('agentAddressHint')}
      value={value}
      onChangeText={onChange}
      autoCapitalize="none"
      autoCorrect={false}
      keyboardType="url"
      error={error}
      leading={<DuoIcon icon={Monitor} size={22} color={c.onSurfaceVariant} />}
    />
  );
}

/**
 * 8 display boxes over ONE real text input (the standard OTP pattern): fast typing, paste and IME
 * composition are handled by the native input rather than 8 racing controlled fields. Boxes are `.code i`: up to 44x56,
 * 1px outline, mono 24; the box under the cursor gets a 2px ink outline.
 */
export function CodeBoxRow({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const c = useScheme();
  const ref = useRef<TextInput>(null);
  const [focused, setFocused] = useState(false);
  const chars = value.padEnd(CODE_LENGTH, ' ').slice(0, CODE_LENGTH).split('');
  const cursor = Math.min(value.length, CODE_LENGTH - 1);
  return (
    <View style={{ height: 56 }}>
      <View style={{ flexDirection: 'row', gap: 6 }} pointerEvents="none" accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
        {chars.map((ch, i) => {
          const current = focused && i === cursor;
          return (
            <View
              key={i}
              style={{
                flex: 1,
                maxWidth: 44,
                height: 56,
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: c.surface,
                borderWidth: current ? 2 : 1,
                borderColor: current ? c.onSurface : c.rule2,
                borderRadius: LumenSize.tileRadius,
              }}
            >
              <Text style={LumenType.monoCode}>{ch.trim()}</Text>
            </View>
          );
        })}
      </View>
      <TextInput
        ref={ref}
        accessibilityLabel={t('pairingCodeA11yLabel', { length: CODE_LENGTH })}
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
