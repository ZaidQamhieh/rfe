import { ChevronDown, ChevronRight, Download, ExternalLink, Share2, Trash2, type LucideIcon } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import type { Entry, MediaInfo } from '../../core/api/models';
import { DuoIcon, Pressable, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { ON_DARK } from '../../design/overlayPalette';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import type { Host } from '../../core/models/host';
import { factRows, type FactRow } from './previewFacts';
import { Spacing } from '../../design/tokens';

/** Width of the label column in the info block (`.facts`: 96 px labels, 12 px gap). */
const FACT_LABEL_WIDTH = 96;

/**
 * Photo or video details (taken, dimensions, length) for [entry], fetched once when [enabled] and never retried (the request is aborted when the view goes away): a file
 * that does not carry them, or an agent that is not reachable, simply leaves the info block with fewer rows.
 */
function useMediaInfo(host: Host, entry: Entry, enabled: boolean): MediaInfo | undefined {
  const [info, setInfo] = useState<{ path: string; value: MediaInfo } | null>(null);
  useEffect(() => {
    if (!enabled) return;
    const abort = new AbortController();
    clientForHost(host)
      .then((client) => client.mediaInfo(entry.path, abort.signal))
      .then((value) => !abort.signal.aborted && setInfo({ path: entry.path, value }))
      .catch(() => undefined);
    return () => abort.abort();
  }, [host, entry.path, enabled]);
  return info?.path === entry.path ? info.value : undefined;
}

/** The info block (`.facts`): a muted label column and a value column, one line per row; draws nothing when there are no rows. */
export function FactsList({ rows, onDark = false }: { rows: FactRow[]; onDark?: boolean }) {
  const c = useScheme();
  if (rows.length === 0) return null;
  return (
    <View style={{ paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm, gap: 6 }} accessibilityRole="summary">
      {rows.map((r) => (
        <View key={r.labelKey} style={{ flexDirection: 'row', gap: Spacing.md2 }}>
          <Text style={[LumenType.name, { width: FACT_LABEL_WIDTH, fontSize: 15, lineHeight: 22 }]} color={onDark ? ON_DARK.inkDim : c.onSurfaceVariant}>{t(r.labelKey)}</Text>
          <Text style={[LumenType.name, { flex: 1, fontSize: 15, lineHeight: 22 }]} color={onDark ? ON_DARK.ink : c.onSurface}>{r.value}</Text>
        </View>
      ))}
    </View>
  );
}

/** Info block for a preview: size and modified for any file, plus taken, dimensions and length for a photo or video once they load. */
export function PreviewFacts({ host, entry, media, onDark }: { host: Host; entry: Entry; media: boolean; onDark: boolean }) {
  const info = useMediaInfo(host, entry, media);
  return <FactsList rows={factRows(entry, info)} onDark={onDark} />;
}

/** "Technical details", closed until tapped: the file's location on the computer, kept off the main view. */
export function TechnicalDetails({ path }: { path: string }) {
  const c = useScheme();
  const [open, setOpen] = useState(false);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <View>
      <Pressable onPress={() => setOpen((v) => !v)} accessibilityLabel={t('previewTechnicalDetails')} accessibilityState={{ expanded: open }}>
        <View style={{ minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingHorizontal: Spacing.md }}>
          <Chevron size={18} color={c.onSurfaceVariant} />
          <Text style={LumenType.sectionLabel} muted>{t('previewTechnicalDetails')}</Text>
        </View>
      </Pressable>
      {open ? (
        <View style={{ paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm, gap: 2 }}>
          <Text style={LumenType.meta} muted>{t('previewTechnicalLocation')}</Text>
          <Text selectable style={LumenType.meta}>{path}</Text>
        </View>
      ) : null}
    </View>
  );
}

type ActionSpec = { key: string; label: string; icon: LucideIcon; onPress: () => void; danger?: boolean };

/**
 * The bottom action row (`.acts`): 76 high with a hairline on top and equal columns of a duo icon over a 13/500 label.
 * Delete is the only red one. Actions the device may not do are left out by the caller (pass `undefined`).
 */
export function PreviewActions({ onShare, onSave, onOpen, onDelete, onDark, bottomInset }: { onShare: () => void; onSave?: () => void; onOpen?: () => void; onDelete?: () => void; onDark: boolean; bottomInset: number }) {
  const c = useScheme();
  const actions: ActionSpec[] = [
    { key: 'share', label: t('previewActionShare'), icon: Share2, onPress: onShare },
    ...(onSave ? [{ key: 'save', label: t('previewActionSave'), icon: Download, onPress: onSave }] : []),
    ...(onOpen ? [{ key: 'open', label: t('previewActionOpen'), icon: ExternalLink, onPress: onOpen }] : []),
    ...(onDelete ? [{ key: 'delete', label: t('deleteButton'), icon: Trash2, onPress: onDelete, danger: true }] : []),
  ];
  const ink = onDark ? ON_DARK.ink : c.onSurface;
  return (
    <View style={{ flexDirection: 'row', minHeight: 76 + bottomInset, paddingBottom: bottomInset, backgroundColor: onDark ? ON_DARK.canvas : c.surfaceContainerLowest, borderTopWidth: 1, borderColor: onDark ? ON_DARK.hairline : c.rule }}>
      {actions.map((a) => {
        const tone = a.danger ? c.stop : ink;
        return (
          <Pressable key={a.key} onPress={a.onPress} accessibilityLabel={a.label} style={{ flex: 1 }}>
            <View style={{ flex: 1, minHeight: 76, alignItems: 'center', justifyContent: 'center', gap: 6 }}>
              <DuoIcon icon={a.icon} size={24} color={tone} />
              <Text numberOfLines={1} style={LumenType.dock} color={tone}>{a.label}</Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}
