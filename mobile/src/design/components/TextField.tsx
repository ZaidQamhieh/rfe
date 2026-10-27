import { useState, type ReactNode } from 'react';
import { TextInput, View, type TextInputProps } from 'react-native';

import { useScheme } from '../theme';
import { FontFamily, Radii } from '../tokens';
import { Text } from './Text';

type Props = TextInputProps & { label: string; hideLabel?: boolean; mono?: boolean; error?: string | null; helper?: string; leading?: ReactNode; trailing?: ReactNode };

/** Filled input (surfaceContainerHighest), 8px radius, 1px outlineVariant, 2px primary when focused. */
export function TextField({ label, hideLabel, mono, error, helper, leading, trailing, style, onFocus, onBlur, ...rest }: Props) {
  const c = useScheme();
  const [focused, setFocused] = useFocus();
  return (
    <View style={{ gap: 6 }}>
      {hideLabel ? null : (
        <Text variant="labelMedium" muted>
          {label}
        </Text>
      )}
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          backgroundColor: c.surfaceContainerHigh,
          borderRadius: Radii.chip,
          borderWidth: focused || error ? 2 : 0,
          borderColor: error ? c.error : c.primary,
          paddingHorizontal: focused || error ? 12 : 14,
          gap: 10,
        }}
      >
        {leading}
        <TextInput
          accessibilityLabel={label}
          placeholderTextColor={c.onSurfaceVariant}
          {...rest}
          onFocus={(e) => {
            setFocused(true);
            onFocus?.(e);
          }}
          onBlur={(e) => {
            setFocused(false);
            onBlur?.(e);
          }}
          style={[
            {
              flex: 1,
              color: c.onSurface,
              paddingVertical: focused ? 11 : 12,
              fontSize: 16,
              fontFamily: mono ? FontFamily.mono : FontFamily.regular,
            },
            style,
          ]}
        />
        {trailing}
      </View>
      {helper && !error ? (
        <Text variant="bodySmall" muted>
          {helper}
        </Text>
      ) : null}
      {error ? (
        <Text variant="bodySmall" color={c.error} accessibilityRole="alert">
          {error}
        </Text>
      ) : null}
    </View>
  );
}

const useFocus = () => useState(false);
