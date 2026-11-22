import { ArrowLeft, CircleAlert, File as FileIcon, MoreVertical, RefreshCw } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { Entry } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { Button, DuoIcon, Pressable, Text } from '../../design/components';
import { PREVIEW_BAR_HEIGHT } from './pagerChip';
import { formatDateTime } from './previewFacts';
import { mix } from '../../design/color';
import { LumenType } from '../../design/lumen';
import { ON_DARK } from '../../design/overlayPalette';
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
 * Preview top bar (`.bar`): back, the file name cut in the middle with "size · modified" under it, any per-viewer
 * actions, then "…". On image/video it floats as translucent black with white foreground over the media.
 */
export function PreviewTopBar({ entry, onDark, onBack, onMore, leading }: { entry: Entry; onDark: boolean; onBack: () => void; onMore: () => void; leading?: ReactNode }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const fg = onDark ? ON_DARK.ink : c.onSurface;
  const dim = onDark ? ON_DARK.inkDim : c.onSurfaceVariant;
  const subtitle = [formatSize(entry.size), entry.modified ? formatDateTime(Date.parse(entry.modified)) : ''].filter(Boolean).join(' · ');
  return (
    <View style={{ paddingTop: insets.top, minHeight: PREVIEW_BAR_HEIGHT + insets.top, paddingHorizontal: Spacing.xs, flexDirection: 'row', alignItems: 'center', backgroundColor: onDark ? ON_DARK.bar : c.surfaceContainerLowest }}>
      <PreviewIconButton label={t('searchBack')} onPress={onBack}>
        <DuoIcon icon={ArrowLeft} size={24} color={fg} />
      </PreviewIconButton>
      <View style={{ flex: 1, minWidth: 0, paddingLeft: Spacing.xs }}>
        <Text numberOfLines={1} ellipsizeMode="middle" style={[LumenType.rowTitle, { fontSize: 17 }]} color={fg}>{entry.name}</Text>
        {subtitle ? <Text numberOfLines={1} style={LumenType.meta} color={dim}>{subtitle}</Text> : null}
      </View>
      {leading}
      <PreviewIconButton label={t('moreTooltip')} onPress={onMore}>
        <DuoIcon icon={MoreVertical} size={24} color={fg} />
      </PreviewIconButton>
    </View>
  );
}

export function PreviewLoading({ message, onDark }: { message?: string; onDark?: boolean }) {
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: Spacing.md }}>
      <ActivityIndicator color={onDark ? ON_DARK.ink : undefined} />
      {message ? <Text muted color={onDark ? ON_DARK.inkSoft : undefined}>{message}</Text> : null}
    </View>
  );
}

export function PreviewError({ message, onRetry, onDark }: { message: string; onRetry?: () => void; onDark?: boolean }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.md }}>
      <CircleAlert size={48} color={c.error} />
      <Text style={{ textAlign: 'center' }} color={onDark ? ON_DARK.ink : undefined}>{message}</Text>
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
      <Text style={{ textAlign: 'center' }} color={onDark ? ON_DARK.ink : undefined}>{t('fileTooLargeToPreview', { sizeLabel: formatSize(size) })}</Text>
    </View>
  );
}
