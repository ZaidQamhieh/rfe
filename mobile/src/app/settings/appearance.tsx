import { Check, Gauge, Smartphone } from 'lucide-react-native';
import { View } from 'react-native';

import { ACCENT_PRESETS, schemeFromSeed } from '../../design/accent';
import { Pressable, Segmented, Text, useDialogs } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { lightScheme, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import type { EntryDensity, SortField } from '../../features/explorer/sort';
import { SettingsPage, SettingsSection, ToggleRow, ValueRow } from '../../features/settings/parts';
import { useSettings } from '../../state/settings';
import type { ThemeMode } from '../../core/settings/settings';

const THEME_ORDER: ThemeMode[] = ['system', 'light', 'dark'];
const DENSITY_ORDER: EntryDensity[] = ['comfortable', 'compact'];
const SORT_LABEL: Record<SortField, () => string> = { name: () => t('sortName'), size: () => t('sortSize'), date: () => t('sortDate'), type: () => t('sortType') };

/** Theme, accent, AMOLED and the default explorer layout, density and sort. There is no wallpaper-color option: React Native has no Material You source. */
export default function AppearanceSettings() {
  const c = useScheme();
  const dialogs = useDialogs();
  const app = useSettings((s) => s.state.app);
  const setApp = useSettings((s) => s.setApp);

  const pickSort = async () => {
    const field = await dialogs.choose<SortField>({
      title: t('defaultSortLabel'),
      options: (Object.keys(SORT_LABEL) as SortField[]).map((f) => ({ value: f, label: SORT_LABEL[f]() })),
    });
    if (field) void setApp('sort', { ...app.sort, field });
  };

  return (
    <SettingsPage>
      <SettingsSection title={t('themeLabel')}>
        <View style={{ paddingVertical: Spacing.md2 }}>
          <Segmented options={[t('systemTheme'), t('lightTheme'), t('darkTheme')]} selectedIndex={THEME_ORDER.indexOf(app.themeMode)} onChange={(i) => void setApp('themeMode', THEME_ORDER[i])} />
        </View>
        <ToggleRow icon={Smartphone} tint={c.primary} title={t('amoledBlackTitle')} subtitle={t('amoledBlackSubtitle')} value={app.amoledDark} onChange={(v) => void setApp('amoledDark', v)} />
        <View style={{ paddingVertical: Spacing.md2, gap: Spacing.sm }}>
          <Text style={LumenType.name}>{t('accentColorLabel')}</Text>
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.md2 }}>
            {ACCENT_PRESETS.map((p) => {
              const swatch = p.color === null ? lightScheme.primary : schemeFromSeed(p.color, false).primary;
              const on = p.color === app.seedColor;
              return (
                <Pressable key={p.label} onPress={() => void setApp('seedColor', p.color)} accessibilityRole="radio" accessibilityLabel={p.label} accessibilityState={{ selected: on }}>
                  <View style={{ width: 48, height: 48, borderRadius: Radii.stadium, backgroundColor: swatch, alignItems: 'center', justifyContent: 'center', borderWidth: on ? 3 : 0, borderColor: c.onSurface }}>
                    {on ? <Check size={18} color={lightScheme.onPrimary} /> : null}
                  </View>
                </Pressable>
              );
            })}
          </View>
        </View>
      </SettingsSection>
      <SettingsSection title={t('displaySection')}>
        <View style={{ paddingVertical: Spacing.md2, gap: Spacing.sm }}>
          <Text style={LumenType.name}>{t('layoutLabel')}</Text>
          <Segmented options={[t('listLayout'), t('gridLayout')]} selectedIndex={app.gridView ? 1 : 0} onChange={(i) => void setApp('gridView', i === 1)} />
        </View>
        <View style={{ paddingVertical: Spacing.md2, gap: Spacing.sm }}>
          <Text style={LumenType.name}>{t('densityLabel')}</Text>
          <Segmented options={[t('comfortableDensity'), t('compactDensity')]} selectedIndex={DENSITY_ORDER.indexOf(app.density)} onChange={(i) => void setApp('density', DENSITY_ORDER[i])} />
        </View>
        <ValueRow icon={Gauge} tint={c.primary} title={t('defaultSortLabel')} value={SORT_LABEL[app.sort.field]()} onPress={() => void pickSort()} />
        <View style={{ paddingVertical: Spacing.md2 }}>
          <Segmented options={[t('sortAscending'), t('sortDescending')]} selectedIndex={app.sort.ascending ? 0 : 1} onChange={(i) => void setApp('sort', { ...app.sort, ascending: i === 0 })} />
        </View>
        <ToggleRow icon={Smartphone} tint={c.primary} title={t('preloadCellularTitle')} subtitle={t('preloadCellularSubtitle')} value={app.preloadPreviewOnCellular} onChange={(v) => void setApp('preloadPreviewOnCellular', v)} />
      </SettingsSection>
    </SettingsPage>
  );
}
