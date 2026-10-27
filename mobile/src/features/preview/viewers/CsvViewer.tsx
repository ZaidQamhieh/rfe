import { useMemo } from 'react';
import { FlatList, ScrollView, View } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Spacing } from '../../../design/tokens';
import { parseCsv } from '../csv';
import { textStateView, usePreviewText } from './TextViewer';

const COL_WIDTH = 140;

/** Port of CsvPreviewScreen: header row + up to 1000 rows in a two-way scrolling table, row count on top. */
export function CsvViewer({ host, entry }: { host: Host; entry: Entry }) {
  const c = useScheme();
  const { file, text, retry } = usePreviewText(host, entry);
  const data = useMemo(() => (text.status === 'ready' ? parseCsv(text.text) : null), [text]);
  const pending = textStateView(file, text, retry, 'Loading CSV…');
  if (pending) return pending;
  if (!data || (data.headers.length === 0 && data.rows.length === 0)) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Text color={c.outline}>(empty file)</Text>
      </View>
    );
  }
  const cols = data.headers.length;
  const width = cols * COL_WIDTH;
  const cell = (v: string, header: boolean, key: number) => (
    <Text key={key} numberOfLines={header ? 1 : 3} style={{ width: COL_WIDTH, paddingHorizontal: 10, paddingVertical: 8, fontSize: 13, fontFamily: header ? FontFamily.semibold : FontFamily.regular }}>
      {v}
    </Text>
  );
  return (
    <View style={{ flex: 1 }}>
      <Text variant="bodySmall" muted style={{ alignSelf: 'flex-end', paddingHorizontal: Spacing.md, paddingTop: Spacing.xs }}>
        {`${data.totalRows} row${data.totalRows === 1 ? '' : 's'}`}
      </Text>
      <ScrollView horizontal contentContainerStyle={{ padding: Spacing.md }}>
        <View style={{ width }}>
          <View style={{ flexDirection: 'row', backgroundColor: c.surfaceContainerHighest }}>{data.headers.map((h, i) => cell(h, true, i))}</View>
          <FlatList
            data={data.rows}
            keyExtractor={(_, i) => String(i)}
            style={{ width }}
            initialNumToRender={40}
            renderItem={({ item }) => (
              <View style={{ flexDirection: 'row', borderBottomWidth: 1, borderColor: c.outlineVariant }}>{Array.from({ length: cols }, (_, i) => cell(item[i] ?? '', false, i))}</View>
            )}
          />
        </View>
      </ScrollView>
    </View>
  );
}
