import { ArrowLeft, CircleAlert, File as FileIcon, MoreVertical, RefreshCw, Share2 } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { Entry } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { Button, Pressable, Text } from '../../design/components';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';

/** The mockup `.iconbtn`: a 40 dp round glyph button in a 48 dp touch target, colour-adjustable for dark media canvases. */
export function PreviewIconButton({ label, onPress, selected, children }: { label: string; onPress: () => void; selected?: boolean; children: ReactNode }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={label} accessibilityState={selected === undefined ? undefined : { selected }} pressedScale={0.9} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 40, height: 40, borderRadius: Radii.stadium, alignItems: 'center', justifyContent: 'center', backgroundColor: selected ? mix(c.primary, c.surface, 0.2) : 'transparent' }}>{children}</View>
    </Pressable>
  );
}

/**
 * Port of PreviewTopBar: back, name + size, per-viewer actions, then Share and "…". On image/video
 * it floats as translucent black with white foreground over the media.
 */
export function PreviewTopBar({ entry, onDark, onBack, onShare, onMore, leading }: { entry: Entry; onDark: boolean; onBack: () => void; onShare: () => void; onMore: () => void; leading?: ReactNode }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const fg = onDark ? '#FFFFFF' : c.onSurface;
  const dim = onDark ? 'rgba(255,255,255,0.7)' : c.onSurfaceVariant;
  const size = formatSize(entry.size);
  return (
    <View style={{ paddingTop: insets.top + 4, paddingBottom: 4, paddingHorizontal: 8, flexDirection: 'row', alignItems: 'center', gap: 2, backgroundColor: onDark ? 'rgba(0,0,0,0.45)' : c.surface }}>
      <PreviewIconButton label="Back" onPress={onBack}>
        <ArrowLeft size={19} color={fg} />
      </PreviewIconButton>
      <View style={{ flex: 1, marginLeft: 4 }}>
        <Text numberOfLines={1} style={[LumenType.rowTitle, { color: fg }]}>{entry.name}</Text>
        {size ? <Text numberOfLines={1} style={[LumenType.meta, { color: dim }]}>{size}</Text> : null}
      </View>
      {leading}
      <PreviewIconButton label="Share" onPress={onShare}>
        <Share2 size={19} color={fg} />
      </PreviewIconButton>
      <PreviewIconButton label={t('moreTooltip')} onPress={onMore}>
        <MoreVertical size={19} color={fg} />
      </PreviewIconButton>
    </View>
  );
}

export function PreviewLoading({ message, onDark }: { message?: string; onDark?: boolean }) {
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md }}>
      <ActivityIndicator color={onDark ? '#FFFFFF' : undefined} />
      {message ? <Text muted color={onDark ? 'rgba(255,255,255,0.8)' : undefined}>{message}</Text> : null}
    </View>
  );
}

export function PreviewError({ message, onRetry, onDark }: { message: string; onRetry?: () => void; onDark?: boolean }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
      <CircleAlert size={48} color={c.error} />
      <Text style={{ textAlign: 'center' }} color={onDark ? '#FFFFFF' : undefined}>{message}</Text>
      {onRetry ? (
        <Button kind="neutral" label={t('retryButton')} onPress={onRetry} renderIcon={(k) => <RefreshCw size={18} color={k} />} />
      ) : null}
    </View>
  );
}

export function PreviewTooLarge({ size, onDark }: { size: number; onDark?: boolean }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
      <FileIcon size={48} color={c.outline} />
      <Text style={{ textAlign: 'center' }} color={onDark ? '#FFFFFF' : undefined}>{t('fileTooLargeToPreview', { sizeLabel: formatSize(size) })}</Text>
    </View>
  );
}
