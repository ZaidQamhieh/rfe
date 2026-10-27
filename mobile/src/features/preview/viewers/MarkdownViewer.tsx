import { useEffect, useMemo, type ReactNode } from 'react';
import { ScrollView, Text as RNText, View } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Radii, Spacing } from '../../../design/tokens';
import { parseMarkdown, type Block, type Inline } from '../markdown';
import { PAGER_CHIP_CLEARANCE } from '../previewFile';
import { MONO, textStateView, usePreviewText } from './TextViewer';

const HEADING_SIZE = [0, 26, 22, 19, 17, 15.5, 14.5];

/** Port of MarkdownPreviewScreen: rendered markdown, or the raw source when [raw] is on. */
export function MarkdownViewer({ host, entry, raw, onText }: { host: Host; entry: Entry; raw: boolean; onText?: (text: string | null) => void }) {
  const c = useScheme();
  const { file, text, retry } = usePreviewText(host, entry);
  const ready = text.status === 'ready' ? text.text : null;
  useEffect(() => onText?.(ready), [ready, onText]);
  const blocks = useMemo(() => (text.status === 'ready' && !raw ? parseMarkdown(text.text) : []), [text, raw]);
  const pending = textStateView(file, text, retry, 'Loading markdown…');
  if (pending) return pending;
  const src = text.status === 'ready' ? text.text : '';
  if (src === '') {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Text color={c.outline}>(empty file)</Text>
      </View>
    );
  }
  return (
    <ScrollView contentContainerStyle={{ padding: Spacing.md, gap: Spacing.md, paddingBottom: PAGER_CHIP_CLEARANCE }}>
      {raw ? (
        <Text selectable style={MONO}>{src}</Text>
      ) : (
        blocks.map((b, i) => <BlockView key={i} b={b} />)
      )}
    </ScrollView>
  );
}

function InlineView({ nodes }: { nodes: Inline[] }): ReactNode {
  const c = useScheme();
  return nodes.map((n, i) => {
    switch (n.t) {
      case 'text':
        return n.v;
      case 'code':
        return (
          <RNText key={i} style={{ fontFamily: FontFamily.mono, fontSize: 13, backgroundColor: c.surfaceContainerHighest }}>
            {n.v}
          </RNText>
        );
      case 'strong':
        return (
          <RNText key={i} style={{ fontFamily: FontFamily.semibold }}>
            <InlineView nodes={n.c} />
          </RNText>
        );
      case 'em':
        return (
          <RNText key={i} style={{ fontStyle: 'italic' }}>
            <InlineView nodes={n.c} />
          </RNText>
        );
      case 'del':
        return (
          <RNText key={i} style={{ textDecorationLine: 'line-through' }}>
            <InlineView nodes={n.c} />
          </RNText>
        );
      case 'link':
        // Shown as a link but inert, like the Flutter viewer: a previewed file never opens URLs.
        return (
          <RNText key={i} style={{ color: c.primary, textDecorationLine: 'underline' }}>
            <InlineView nodes={n.c} />
          </RNText>
        );
    }
  });
}

function BlockView({ b }: { b: Block }) {
  const c = useScheme();
  switch (b.t) {
    case 'heading':
      return (
        <Text selectable accessibilityRole="header" style={{ fontSize: HEADING_SIZE[b.level], lineHeight: HEADING_SIZE[b.level] * 1.3, fontFamily: FontFamily.semibold }}>
          <InlineView nodes={b.c} />
        </Text>
      );
    case 'para':
      return (
        <Text selectable variant="bodyLarge">
          <InlineView nodes={b.c} />
        </Text>
      );
    case 'code':
      return (
        <ScrollView horizontal style={{ backgroundColor: c.surfaceContainerHighest, borderRadius: Radii.sm }} contentContainerStyle={{ padding: Spacing.md2 }}>
          <Text selectable style={MONO}>{b.v}</Text>
        </ScrollView>
      );
    case 'list':
      return (
        <View style={{ gap: 4 }}>
          {b.items.map((it, i) => (
            <View key={i} style={{ flexDirection: 'row', gap: 8 }}>
              <Text variant="bodyLarge" style={{ minWidth: 18 }}>{b.ordered ? `${b.start + i}.` : '•'}</Text>
              <Text selectable variant="bodyLarge" style={{ flex: 1 }}>
                <InlineView nodes={it} />
              </Text>
            </View>
          ))}
        </View>
      );
    case 'quote':
      return (
        <View style={{ borderLeftWidth: 3, borderColor: c.outlineVariant, paddingLeft: Spacing.md2, gap: Spacing.sm }}>
          {b.c.map((x, i) => (
            <BlockView key={i} b={x} />
          ))}
        </View>
      );
    case 'table':
      return (
        <ScrollView horizontal>
          <View style={{ borderWidth: 1, borderColor: c.outlineVariant, borderRadius: Radii.chip }}>
            {[b.head, ...b.rows].map((row, r) => (
              <View key={r} style={{ flexDirection: 'row', backgroundColor: r === 0 ? c.surfaceContainerHighest : undefined, borderTopWidth: r ? 1 : 0, borderColor: c.outlineVariant }}>
                {b.head.map((_, i) => (
                  <Text key={i} selectable style={{ width: 130, padding: 8, fontSize: 13, fontFamily: r === 0 ? FontFamily.semibold : FontFamily.regular }}>
                    <InlineView nodes={row[i] ?? []} />
                  </Text>
                ))}
              </View>
            ))}
          </View>
        </ScrollView>
      );
    case 'hr':
      return <View style={{ height: 1, backgroundColor: c.outlineVariant }} />;
  }
}
