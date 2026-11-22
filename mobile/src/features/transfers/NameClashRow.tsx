import { useEffect, useRef, useState } from 'react';
import { View } from 'react-native';
import { File } from 'expo-file-system';

import type { Host } from '../../core/models/host';
import type { TransferRecord } from '../../core/native';
import { Button, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { folderLabel, parentDirOf } from '../explorer/paths';
import { fileUriOf, resolveClash } from './resolveClash';
import { MetaLine, TransferFrame } from './TransferRows';
import { clashColumns, type ClashColumn, type ClashSide } from './transferSections';
import type { Resolution } from './uploadPlan';

/** The phone-side file being sent: its size and last change, or nothing known if the staged copy cannot be read. */
function readSending(r: TransferRecord): ClashSide {
  try {
    const f = new File(fileUriOf(r.destPath));
    return f.exists ? { size: f.size, modifiedAt: f.lastModified ?? undefined } : {};
  } catch {
    return {};
  }
}

/** One half of `.cmp`: a muted heading, the size as a mono figure, then when it changed. */
function Column({ col, first }: { col: ClashColumn; first: boolean }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, paddingVertical: Spacing.sm, paddingHorizontal: Spacing.md2, borderLeftWidth: first ? 0 : 1, borderLeftColor: c.rule2 }}>
      <Text muted style={LumenType.meta}>
        {col.heading}
      </Text>
      <Text style={{ fontFamily: FontFamily.mono, fontSize: 15, lineHeight: 22 }}>{col.size}</Text>
      {col.when ? (
        <Text muted style={LumenType.meta}>
          {col.when}
        </Text>
      ) : null}
    </View>
  );
}

/**
 * An upload the computer refused because a file with this name is already there: says where, shows this file next to the
 * one already on the computer (size and when it changed), and offers Keep both, Replace or Skip.
 */
export function NameClashRow({ r, host }: { r: TransferRecord; host: Host | undefined }) {
  const c = useScheme();
  const toast = useToast();
  const [existing, setExisting] = useState<ClashSide | null>(null);
  const [busy, setBusy] = useState(false);
  const computer = host?.label || host?.address || '';
  const sending = readSending(r);

  useEffect(() => {
    let live = true;
    if (!host) return;
    void clientForHost(host)
      .then((cl) => cl.meta(r.remotePath))
      .then(
        (e) => live && setExisting({ size: e.size, modifiedAt: e.modified ? Date.parse(e.modified) : undefined }),
        // The comparison is a courtesy: if the other file cannot be read the choice still works, with its numbers shown as unknown.
        () => live && setExisting({}),
      );
    return () => {
      live = false;
    };
  }, [host, r.remotePath]);

  const inFlight = useRef(false);
  const choose = async (resolution: Resolution) => {
    if (!host || busy || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    let carriedOut = false;
    try {
      carriedOut = await resolveClash(host, r, resolution);
    } catch {
      toast.info(t('trClashFailed'));
    } finally {
      // A carried-out choice removes this row, so it stays busy; any other exit gives the buttons back.
      if (!carriedOut) {
        inFlight.current = false;
        setBusy(false);
      }
    }
  };

  const [left, right] = clashColumns(sending, existing, computer);
  return (
    <TransferFrame r={r}>
      <MetaLine color={c.wait} lines={3}>
        {t('trClashBody', { folder: folderLabel(parentDirOf(r.remotePath)), computer })}
      </MetaLine>
      <View style={{ flexDirection: 'row', marginTop: Spacing.md2, borderRadius: Radii.control, borderWidth: 1, borderColor: c.rule2 }}>
        <Column col={left} first />
        <Column col={right} first={false} />
      </View>
      <View style={{ flexDirection: 'row', gap: Spacing.sm, marginTop: Spacing.md2, flexWrap: 'wrap' }}>
        <Button size="s" kind="filled" label={t('keepBothButton')} busy={busy} onPress={() => void choose('keepBoth')} />
        <Button size="s" kind="neutral" label={t('trReplace')} disabled={busy} onPress={() => void choose('overwrite')} />
        <Button size="s" kind="text" label={t('trSkip')} disabled={busy} onPress={() => void choose('skip')} />
      </View>
    </TransferFrame>
  );
}
