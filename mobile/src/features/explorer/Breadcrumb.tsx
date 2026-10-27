import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { ChevronRight } from 'lucide-react-native';
import { useRef } from 'react';
import { ScrollView, View } from 'react-native';

import { Menu, Pressable, Text, useToast } from '../../design/components';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { crumbLabel } from './paths';

export const MAX_VISIBLE_CRUMBS = 4;

/** Indices of the middle crumbs folded into the "…" chip (index 0 and the tail always stay visible). */
export function collapsedCrumbIndices(stackLength: number, maxVisible = MAX_VISIBLE_CRUMBS): number[] {
  if (stackLength <= maxVisible) return [];
  const visibleTail = Math.min(Math.max(maxVisible - 1, 1), stackLength);
  const firstVisibleTail = stackLength - visibleTail;
  return Array.from({ length: firstVisibleTail - 1 }, (_, i) => i + 1);
}

export { crumbLabel };

export function useCopyPath() {
  const toast = useToast();
  return async (path: string) => {
    await Clipboard.setStringAsync(path);
    Haptics.selectionAsync().catch(() => {});
    toast.info(t('copiedPath', { path }));
  };
}

export function BreadcrumbBar({ pathStack, onNavigateTo, onJumpTo }: { pathStack: string[]; onNavigateTo: (i: number) => void; onJumpTo?: (path: string) => void }) {
  const c = useScheme();
  const toast = useToast();
  const copyPath = useCopyPath();
  const collapsed = collapsedCrumbIndices(pathStack.length);
  const last = pathStack.length - 1;
  const scroller = useRef<ScrollView>(null);

  const items: React.ReactNode[] = [];
  for (let i = 0; i < pathStack.length; i++) {
    if (collapsed.includes(i)) {
      if (i === collapsed[0]) {
        items.push(<Sep key={`s${i}`} show={i > 0} />);
        items.push(
          <Menu
            key="collapsed"
            accessibilityLabel={t('showHiddenFoldersTooltip')}
            trigger={
              <View style={{ minHeight: 36, justifyContent: 'center', paddingHorizontal: 14, borderRadius: Radii.stadium, backgroundColor: c.surfaceContainerHigh }}>
                <Text style={LumenType.pill} muted>…</Text>
              </View>
            }
            items={[
              ...collapsed.map((idx) => ({ label: crumbLabel(pathStack, idx), onPress: () => onNavigateTo(idx) })),
              { label: t('copyPathAction'), onPress: () => copyPath(pathStack[pathStack.length - 1]) },
              ...(onJumpTo
                ? [
                    {
                      label: t('pastePathAction'),
                      onPress: async () => {
                        const p = (await Clipboard.getStringAsync()).trim();
                        if (!p) toast.info(t('clipboardEmptyMessage'));
                        else onJumpTo(p);
                      },
                    },
                  ]
                : []),
            ]}
          />,
        );
      }
      continue;
    }
    items.push(<Sep key={`s${i}`} show={i > 0} />);
    const current = i === last;
    items.push(
      <Pressable key={`c${i}`} onPress={() => onNavigateTo(i)} onLongPress={() => copyPath(pathStack[i])} accessibilityLabel={crumbLabel(pathStack, i)} hitSlop={6}>
        <View style={{ minHeight: 36, justifyContent: 'center', paddingHorizontal: 14, borderRadius: Radii.stadium, backgroundColor: current ? mix(c.primary, c.surfaceContainer, 0.2) : c.surfaceContainerHigh }}>
          <Text style={LumenType.pill} color={current ? c.primary : c.onSurfaceVariant} numberOfLines={1}>
            {crumbLabel(pathStack, i)}
          </Text>
        </View>
      </Pressable>,
    );
  }
  return (
    <ScrollView ref={scroller} onContentSizeChange={() => scroller.current?.scrollToEnd({ animated: false })} horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ paddingVertical: Spacing.xs, paddingRight: 18, alignItems: 'center' }}>
      {items}
    </ScrollView>
  );
}

function Sep({ show }: { show: boolean }) {
  const c = useScheme();
  return show ? <ChevronRight size={16} color={c.outline} style={{ marginHorizontal: Spacing.xs }} /> : null;
}
