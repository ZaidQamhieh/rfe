import { CircleAlert, CloudOff, FolderOpen, FilterX, RefreshCw } from 'lucide-react-native';
import { ActivityIndicator, View } from 'react-native';

import { useScheme } from '../theme';
import { Spacing } from '../tokens';
import { Button } from './Button';
import { Text } from './Text';
import { t } from '../../i18n';

export function EmptyState({ kind = 'emptyFolder', message, action }: { kind?: 'emptyFolder' | 'noMatches'; message?: string; action?: { label: string; onPress: () => void } }) {
  const c = useScheme();
  const Icon = kind === 'noMatches' ? FilterX : FolderOpen;
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md2 }} accessibilityRole="summary">
      <Icon size={64} color={c.outline} />
      <Text variant="titleMedium" style={{ textAlign: 'center' }}>
        {message ?? (kind === 'noMatches' ? t('noMatchesTitle') : t('emptyFolderMessage'))}
      </Text>
      {action ? <Button size="lg" kind="filled" label={action.label} onPress={action.onPress} /> : null}
    </View>
  );
}

export function ErrorRetry({ message, onRetry }: { message: string; onRetry: () => void }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md2 }}>
      <CircleAlert size={56} color={c.error} />
      <Text style={{ textAlign: 'center' }} accessibilityRole="alert">
        {message}
      </Text>
      <Button kind="filled" label={t('retryButton')} icon={<RefreshCw size={18} color={c.onPrimary} />} onPress={onRetry} />
    </View>
  );
}

export function OfflineBanner({ text = t('offlineBannerText') }: { text?: string }) {
  const c = useScheme();
  return (
    <View style={{ backgroundColor: c.tertiaryContainer, flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingHorizontal: Spacing.md2, paddingVertical: 6 }}>
      <CloudOff size={16} color={c.onTertiaryContainer} />
      <Text style={{ flex: 1, fontSize: 13 }} color={c.onTertiaryContainer}>
        {text}
      </Text>
    </View>
  );
}

export function Loading({ label = t('loadingLabel') }: { label?: string }) {
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }} accessibilityLabel={label} accessibilityRole="progressbar">
      <ActivityIndicator />
    </View>
  );
}

/** How wide each row's name bar is, as a share of the text column; the rows cycle through these so the list looks uneven. */
const SKELETON_NAME_WIDTHS = ['70%', '52%', '84%', '60%'] as const;

/**
 * A folder still being read (`loading` screen): flat 64 dp rows, each a 32 x 40 square block where the emoji will go,
 * then a name bar and a shorter, thinner line under it. No circles.
 */
export function ListingSkeleton({ rows = 8 }: { rows?: number }) {
  const c = useScheme();
  return (
    <View accessibilityLabel={t('loadingFiles')} accessibilityRole="progressbar">
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ minHeight: 64, flexDirection: 'row', alignItems: 'center', paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm, gap: Spacing.md }}>
          <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>
            <View style={{ width: 32, height: 40, borderRadius: 3, backgroundColor: c.fill2 }} />
          </View>
          <View style={{ flex: 1 }}>
            <View style={{ height: 12, width: SKELETON_NAME_WIDTHS[i % SKELETON_NAME_WIDTHS.length], borderRadius: 3, backgroundColor: c.fill2 }} />
            <View style={{ height: 10, width: '30%', borderRadius: 3, marginTop: 10, backgroundColor: c.fill2 }} />
          </View>
        </View>
      ))}
    </View>
  );
}
