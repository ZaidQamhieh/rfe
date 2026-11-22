import { ChevronDown, ChevronUp, LayoutGrid, List } from 'lucide-react-native';
import { View } from 'react-native';

import type { SortField, SortOrder } from '../../core/models/sort';
import { DuoIcon, Pressable, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { Radii, Spacing } from '../../design/tokens';

/** Height of the sort button and the list/grid switch. */
const CONTROL_HEIGHT = 40;
/** Width of the list/grid switch. */
const GRID_TOGGLE_WIDTH = 48;

/** The visible name of a sort field ("Name", "Size", "Date", "Type"). */
export const sortFieldLabel = (f: SortField) => t(f === 'name' ? 'sortFieldName' : f === 'size' ? 'sortFieldSize' : f === 'date' ? 'sortFieldDate' : 'sortFieldType');

/**
 * Mockup `.tools`: a 44 dp row under the path with the item count on the left (14/20 ink-2), the sort button (field name
 * and a direction chevron) and the list/grid switch on the right, and a hairline below. While the folder is being read
 * the count is replaced by [reading] and a short ink bar sits on the hairline.
 */
export function ToolsRow({ summary, reading = false, sort, gridView, onSort, onToggleGrid }: { summary: string; reading?: boolean; sort: SortOrder; gridView: boolean; onSort: () => void; onToggleGrid: () => void }) {
  const c = useScheme();
  const Chevron = sort.ascending ? ChevronDown : ChevronUp;
  return (
    <View style={{ height: 44, flexDirection: 'row', alignItems: 'center', paddingLeft: Spacing.md, paddingRight: Spacing.xs, borderBottomWidth: 1, borderBottomColor: c.rule }}>
      <Text numberOfLines={1} style={[LumenType.meta, { flex: 1 }]} muted>
        {reading ? t('readingFolder') : summary}
      </Text>
      <Pressable onPress={onSort} accessibilityLabel={`${t('sortByLabel')}: ${sortFieldLabel(sort.field)}`} style={{ height: CONTROL_HEIGHT, borderRadius: Radii.control }}>
        <View style={{ height: CONTROL_HEIGHT, flexDirection: 'row', alignItems: 'center', gap: Spacing.xs, paddingHorizontal: 10 }}>
          <Text style={LumenType.pill}>{sortFieldLabel(sort.field)}</Text>
          <Chevron size={16} strokeWidth={2} color={c.onSurface} />
        </View>
      </Pressable>
      <Pressable onPress={onToggleGrid} accessibilityLabel={gridView ? t('showAsList') : t('showAsGrid')} style={{ width: GRID_TOGGLE_WIDTH, height: CONTROL_HEIGHT, borderRadius: Radii.control }}>
        <View style={{ width: GRID_TOGGLE_WIDTH, height: CONTROL_HEIGHT, alignItems: 'center', justifyContent: 'center' }}>
          <DuoIcon icon={gridView ? List : LayoutGrid} size={20} />
        </View>
      </Pressable>
      {reading ? <View style={{ position: 'absolute', left: 0, bottom: -1, height: 2, width: '30%', backgroundColor: c.onSurface }} /> : null}
    </View>
  );
}
