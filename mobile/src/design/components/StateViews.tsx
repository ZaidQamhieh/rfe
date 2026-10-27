import { CircleAlert, CloudOff, FolderOpen, FilterX, RefreshCw } from 'lucide-react-native';
import { ActivityIndicator, View } from 'react-native';

import { useScheme } from '../theme';
import { Spacing } from '../tokens';
import { Button } from './Button';
import { Text } from './Text';

export function EmptyState({ kind = 'emptyFolder', message, action }: { kind?: 'emptyFolder' | 'noMatches'; message?: string; action?: { label: string; onPress: () => void } }) {
  const c = useScheme();
  const Icon = kind === 'noMatches' ? FilterX : FolderOpen;
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: 12 }} accessibilityRole="summary">
      <Icon size={64} color={c.outline} />
      <Text variant="titleMedium" style={{ textAlign: 'center' }}>
        {message ?? (kind === 'noMatches' ? 'No matches' : 'This folder is empty')}
      </Text>
      {action ? <Button size="lg" kind="filled" label={action.label} onPress={action.onPress} /> : null}
    </View>
  );
}

export function ErrorRetry({ message, onRetry }: { message: string; onRetry: () => void }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: 12 }}>
      <CircleAlert size={56} color={c.error} />
      <Text style={{ textAlign: 'center' }} accessibilityRole="alert">
        {message}
      </Text>
      <Button kind="filled" label="Retry" icon={<RefreshCw size={18} color={c.onPrimary} />} onPress={onRetry} />
    </View>
  );
}

export function OfflineBanner({ text = 'You are offline. Showing saved data.' }: { text?: string }) {
  const c = useScheme();
  return (
    <View style={{ backgroundColor: c.tertiaryContainer, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 12, paddingVertical: 6 }}>
      <CloudOff size={16} color={c.onTertiaryContainer} />
      <Text style={{ flex: 1, fontSize: 13 }} color={c.onTertiaryContainer}>
        {text}
      </Text>
    </View>
  );
}

export function Loading({ label = 'Loading' }: { label?: string }) {
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }} accessibilityLabel={label} accessibilityRole="progressbar">
      <ActivityIndicator />
    </View>
  );
}

/** Port of ListingSkeleton: circle avatar + two bars per row. */
export function ListingSkeleton({ rows = 8 }: { rows?: number }) {
  const c = useScheme();
  const bar = (w: number) => <View style={{ height: 12, width: w, borderRadius: 6, backgroundColor: c.surfaceContainerHighest }} />;
  return (
    <View accessibilityLabel="Loading files" accessibilityRole="progressbar">
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ flexDirection: 'row', alignItems: 'center', paddingHorizontal: 16, paddingVertical: 8, gap: 16 }}>
          <View style={{ width: 40, height: 40, borderRadius: 20, backgroundColor: c.surfaceContainerHighest }} />
          <View style={{ gap: 6 }}>
            {bar(160)}
            {bar(90)}
          </View>
        </View>
      ))}
    </View>
  );
}
