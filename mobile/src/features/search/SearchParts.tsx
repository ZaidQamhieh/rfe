import { ChevronRight, File as FileIcon, FileArchive, FileText, Folder, Globe, Image as ImageIcon, Info, Music, Regex, Video, type LucideIcon } from 'lucide-react-native';
import { ScrollView, Text as RNText, View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { roleOf } from '../../core/entryCategory';
import { formatDate, formatSize } from '../../core/format';
import { Pressable, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenSize, LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { FontFamily, Radii, type Roles } from '../../design/tokens';
import { t } from '../../i18n';
import { folderLabel } from '../explorer/paths';
import { CATEGORY_LABEL, highlightRange, SEARCH_CATEGORIES, type SearchCategory } from './searchLogic';

const CATEGORY_ICON: Record<SearchCategory, LucideIcon> = { folder: Folder, image: ImageIcon, video: Video, audio: Music, document: FileText, archive: FileArchive, other: FileIcon };
const CATEGORY_ROLE: Record<SearchCategory, keyof Roles | null> = { folder: 'folder', image: 'photo', video: 'photo', audio: 'route', document: 'doc', archive: 'warn', other: null };

/** Icon of a result row by MIME type; the colour comes from the entry's role (see ResultIconTile). */
export function resultIcon(e: Pick<Entry, 'isDir' | 'mimeType'>): { icon: LucideIcon } {
  if (e.isDir) return { icon: Folder };
  const mime = e.mimeType ?? '';
  if (mime.startsWith('image/')) return { icon: ImageIcon };
  if (mime.startsWith('video/')) return { icon: Video };
  if (mime.startsWith('audio/')) return { icon: Music };
  if (mime.includes('pdf') || mime.startsWith('text/') || mime.includes('json')) return { icon: FileText };
  if (mime.includes('zip') || mime.includes('archive')) return { icon: FileArchive };
  return { icon: FileIcon };
}

/** `.filemark`: 38 dp tile, the entry's role colour at 17% over the raised surface (neutral when the type is unknown). */
export function ResultIconTile({ entry }: { entry: Pick<Entry, 'isDir' | 'mimeType'> }) {
  const c = useScheme();
  const roles = useRoles();
  const { icon: Icon } = resultIcon(entry);
  const role = roleOf(entry);
  const tint = role ? roles[role] : c.onSurfaceVariant;
  return (
    <View style={{ width: 38, height: 38, borderRadius: 10, backgroundColor: role ? mix(tint, c.surfaceContainerHigh, 0.17) : c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>
      <Icon size={22} color={tint} />
    </View>
  );
}

/** Row wrapper that joins consecutive results into one `.filelist` card: rounded at the first and last row only. */
export function ResultCardRow({ index, count, children }: { index: number; count: number; children: React.ReactNode }) {
  const c = useScheme();
  const r = LumenSize.cardRadius;
  const first = index === 0;
  const last = index === count - 1;
  return (
    <View
      style={{
        marginHorizontal: 18,
        paddingHorizontal: 14,
        paddingTop: first ? 8 : 0,
        paddingBottom: last ? 8 : 0,
        backgroundColor: c.surfaceContainer,
        borderTopLeftRadius: first ? r : 0,
        borderTopRightRadius: first ? r : 0,
        borderBottomLeftRadius: last ? r : 0,
        borderBottomRightRadius: last ? r : 0,
      }}
    >
      {children}
    </View>
  );
}

/** Flat search field (no outline): a pill on the card surface holding the icon, the input and any trailing actions. */
export function SearchField({ children, style }: { children: React.ReactNode; style?: object }) {
  const c = useScheme();
  return (
    <View style={[{ minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 16, backgroundColor: c.surfaceContainer, borderRadius: Radii.stadium }, style]}>
      {children}
    </View>
  );
}

function HighlightedName({ name, query, highlight }: { name: string; query: string; highlight: boolean }) {
  const c = useScheme();
  const base = { ...LumenType.name, color: c.onSurface } as const;
  const r = highlight ? highlightRange(name, query) : null;
  if (!r) return <Text numberOfLines={1} style={base}>{name}</Text>;
  return (
    <RNText numberOfLines={1} style={base}>
      {name.slice(0, r.start)}
      <RNText style={{ fontFamily: FontFamily.semibold, backgroundColor: c.primaryContainer, color: c.onPrimaryContainer }}>{name.slice(r.start, r.end)}</RNText>
      {name.slice(r.end)}
    </RNText>
  );
}

/** One search hit: name with the matched part highlighted, and path, size and date under it. */
export function SearchResultTile({ entry, query, highlight, onPress }: { entry: Entry; query: string; highlight: boolean; onPress: () => void }) {
  const c = useScheme();
  const sub = [entry.path, !entry.isDir ? formatSize(entry.size) : '', entry.modified ? formatDate(new Date(entry.modified)) : ''].filter(Boolean).join('  ·  ');
  return (
    <Pressable onPress={onPress} accessibilityLabel={entry.name}>
      <View style={{ minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 }}>
        <ResultIconTile entry={entry} />
        <View style={{ flex: 1, gap: 2 }}>
          <HighlightedName name={entry.name} query={query} highlight={highlight} />
          <Text numberOfLines={2} muted style={[LumenType.meta, { fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 18 }]}>{sub}</Text>
        </View>
        {entry.isDir && <ChevronRight size={18} color={c.onSurfaceVariant} />}
      </View>
    </Pressable>
  );
}

/** Horizontally scrolling category filter chips; a selected chip takes its role colour at 17% over the card surface. */
export function CategoryChips({ selected, onToggle }: { selected: SearchCategory[]; onToggle: (c: SearchCategory) => void }) {
  const c = useScheme();
  const roles = useRoles();
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0 }} contentContainerStyle={{ paddingHorizontal: 18, paddingVertical: 6, gap: 8, alignItems: 'center' }}>
      {SEARCH_CATEGORIES.map((cat) => {
        const on = selected.includes(cat);
        const role = CATEGORY_ROLE[cat];
        const tint = role ? roles[role] : c.primary;
        const Icon = CATEGORY_ICON[cat];
        return (
          <Pressable key={cat} onPress={() => onToggle(cat)} hitSlop={4} accessibilityLabel={t(CATEGORY_LABEL[cat])} accessibilityState={{ selected: on }}>
            <View style={{ minHeight: 40, flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, borderRadius: Radii.stadium, backgroundColor: on ? mix(tint, c.surfaceContainer, 0.2) : c.surfaceContainer }}>
              <Icon size={16} color={on ? tint : role ? tint : c.onSurfaceVariant} />
              <Text style={LumenType.pill} color={on ? tint : c.onSurfaceVariant}>{t(CATEGORY_LABEL[cat])}</Text>
            </View>
          </Pressable>
        );
      })}
    </ScrollView>
  );
}

/** Static chip shown when the query is treated as a glob or regex. */
export function GlobIndicator() {
  const c = useScheme();
  return (
    <View style={{ paddingHorizontal: 18, paddingBottom: 6, flexDirection: 'row' }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, paddingVertical: 8, borderRadius: Radii.stadium, backgroundColor: mix(c.primary, c.surfaceContainer, 0.2) }}>
        <Regex size={16} color={c.primary} />
        <Text style={LumenType.pill} color={c.primary}>{t('globPattern')}</Text>
      </View>
    </View>
  );
}

/** Notice above a result list: the agent stopped early (result cap or time budget). A warn-tinted card. */
export function TruncationBanner({ message }: { message: string }) {
  const c = useScheme();
  const roles = useRoles();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, marginHorizontal: 18, marginVertical: 6, paddingHorizontal: 14, paddingVertical: 10, borderRadius: LumenSize.cardRadius, backgroundColor: mix(roles.warn, c.surfaceContainer, 0.15) }}>
      <Info size={18} color={roles.warn} />
      <Text style={[LumenType.meta, { flex: 1 }]} color={c.onSurface}>{message}</Text>
    </View>
  );
}

/** Pill showing where the search runs (this folder or everywhere); tapping opens the filters. */
export function ScopePill({ fromHere, currentPath, onPress }: { fromHere: boolean; currentPath: string; onPress: () => void }) {
  const c = useScheme();
  const Icon = fromHere ? Folder : Globe;
  return (
    <Pressable onPress={onPress} hitSlop={4} accessibilityLabel={fromHere ? folderLabel(currentPath) : t('searchingEverywhere')} style={{ marginLeft: 18 }}>
      <View style={{ minHeight: 40, flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, borderRadius: Radii.stadium, backgroundColor: mix(c.primary, c.surfaceContainer, 0.2) }}>
        <Icon size={16} color={c.primary} />
        <Text numberOfLines={1} style={[LumenType.pill, { maxWidth: 140 }]} color={c.primary}>{fromHere ? folderLabel(currentPath) : t('searchingEverywhere')}</Text>
      </View>
    </Pressable>
  );
}
