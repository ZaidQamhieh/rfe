import { useState } from 'react';
import { FlatList, View } from 'react-native';

import { Pressable, SectionLabel, Text } from '../../design/components';
import { useScheme } from '../../design/theme';
import { LumenType } from '../../design/lumen';
import { FontFamily } from '../../design/tokens';
import { t } from '../../i18n';
import { bundledPackages, licenseText, nativeComponents } from '../../features/support/licenses';

/** Open-source licenses of everything built into the app. Tap a package to read its license text. */
export default function Licenses() {
  const c = useScheme();
  const [open, setOpen] = useState<string | null>(null);
  return (
    <FlatList
      data={bundledPackages}
      keyExtractor={(p) => `${p.n}@${p.v}`}
      contentContainerStyle={{ paddingHorizontal: 18, paddingVertical: 12, gap: 4, paddingBottom: 32 }}
      ListHeaderComponent={
        <View style={{ gap: 6, marginBottom: 8 }}>
          <Text style={LumenType.meta} muted>{t('licensesIntro')}</Text>
          <SectionLabel title={t('licensesNative')} />
          {nativeComponents.map((n) => (
            <View key={n.name} style={{ paddingVertical: 4 }}>
              <Text style={LumenType.name}>{n.name}</Text>
              <Text muted style={LumenType.meta}>{n.license}</Text>
            </View>
          ))}
          <SectionLabel title={t('licensesPackages', { count: bundledPackages.length })} />
        </View>
      }
      renderItem={({ item }) => {
        const key = `${item.n}@${item.v}`;
        const expanded = open === key;
        const text = licenseText(item.t);
        return (
          <Pressable onPress={() => setOpen(expanded ? null : key)} accessibilityRole="button" accessibilityLabel={`${item.n} ${item.v}, ${item.l}`} accessibilityState={{ expanded }}>
            <View style={{ paddingVertical: 10, minHeight: 48, borderBottomWidth: 1, borderBottomColor: c.outlineVariant }}>
              <Text style={LumenType.name}>{item.n}</Text>
              <Text muted style={LumenType.meta}>{`${item.v} · ${item.l}`}</Text>
              {expanded ? <Text style={{ fontSize: 12, marginTop: 8, fontFamily: FontFamily.mono }}>{text ?? t('licenseTextMissing', { license: item.l })}</Text> : null}
            </View>
          </Pressable>
        );
      }}
    />
  );
}
