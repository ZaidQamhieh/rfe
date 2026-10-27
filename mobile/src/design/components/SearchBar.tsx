import { Search } from 'lucide-react-native';
import { TextInput, View } from 'react-native';

import { useScheme } from '../theme';
import { FontFamily, Radii } from '../tokens';

/** Mockup `.searchbar`: flat pill, 1px border, 14/9 padding, 13.5px text. */
export function SearchBar({ value, onChange, placeholder, autoFocus }: { value: string; onChange: (v: string) => void; placeholder: string; autoFocus?: boolean }) {
  const c = useScheme();
  return (
    <View style={{ minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 14, paddingVertical: 9, backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.stadium }}>
      <Search size={16} color={c.onSurfaceVariant} />
      <TextInput
        accessibilityLabel={placeholder}
        autoFocus={autoFocus}
        value={value}
        onChangeText={onChange}
        placeholder={placeholder}
        placeholderTextColor={c.onSurfaceVariant}
        style={{ flex: 1, padding: 0, fontSize: 13.5, fontFamily: FontFamily.regular, color: c.onSurface }}
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
      />
    </View>
  );
}
