import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { ChevronRight } from 'lucide-react-native';
import { useRef } from 'react';
import { ScrollView, View } from 'react-native';

import { Menu, Pressable, Text, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { crumbLabel } from './paths';

const MAX_VISIBLE_CRUMBS = 4;
/** Mockup `.path`: the row is 36 high; each crumb has 6 dp of side padding and a 6 dp radius. */
const PATH_HEIGHT = 36;

/** Indices of the middle crumbs folded into the "…" crumb (index 0 and the tail always stay visible). */
function collapsedCrumbIndices(stackLength: number, maxVisible = MAX_VISIBLE_CRUMBS): number[] {
  if (stackLength <= maxVisible) return [];
  const visibleTail = Math.min(Math.max(maxVisible - 1, 1), stackLength);
  const firstVisibleTail = stackLength - visibleTail;
  return Array.from({ length: firstVisibleTail - 1 }, (_, i) => i + 1);
}

/** Returns a function that copies a path to the clipboard with a tick of haptics and a toast. */
function useCopyPath() {
  const toast = useToast();
  return async (path: string) => {
    await Clipboard.setStringAsync(path);
    Haptics.selectionAsync().catch(() => {});
    toast.info(t('copiedPath', { path }));
  };
}

/**
 * Mockup `.path`: one 36 dp row of folder names (14/20 ink-2) with ink-3 chevrons between them; the current folder is
 * ink and weight 600. A long chain folds its middle into a "…" menu; tap a name to go there, long-press to copy its path.
 */
export function BreadcrumbBar({ pathStack, onNavigateTo, onJumpTo, rootLabel }: { pathStack: string[]; onNavigateTo: (i: number) => void; onJumpTo?: (path: string) => void; rootLabel?: string }) {
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
              <Text style={{ fontSize: 14, lineHeight: 20 }} muted>
                …
              </Text>
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
    // The first crumb is the shared folder; when the computer's name is given it stands in for that folder's name.
    const label = i === 0 && rootLabel ? rootLabel : crumbLabel(pathStack, i);
    items.push(
      <Pressable key={`c${i}`} onPress={() => onNavigateTo(i)} onLongPress={() => copyPath(pathStack[i])} accessibilityLabel={label} hitSlop={6}>
        <View style={{ paddingVertical: Spacing.xs, paddingHorizontal: 6, borderRadius: 6 }}>
          <Text numberOfLines={1} style={{ fontSize: 14, lineHeight: 20, fontFamily: current ? FontFamily.semibold : FontFamily.regular }} color={current ? c.onSurface : c.onSurfaceVariant}>
            {label}
          </Text>
        </View>
      </Pressable>,
    );
  }
  return (
    <ScrollView
      ref={scroller}
      onContentSizeChange={() => scroller.current?.scrollToEnd({ animated: false })}
      horizontal
      showsHorizontalScrollIndicator={false}
      style={{ flexGrow: 0, height: PATH_HEIGHT }}
      contentContainerStyle={{ alignItems: 'center', gap: 2, paddingLeft: 10, paddingRight: Spacing.md2 }}
    >
      {items}
    </ScrollView>
  );
}

/** Chevron between two crumbs (ink-3); nothing before the first crumb. */
function Sep({ show }: { show: boolean }) {
  const c = useScheme();
  return show ? <ChevronRight size={16} color={c.outline} /> : null;
}
