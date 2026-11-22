import * as Clipboard from 'expo-clipboard';
import { useEffect, useRef, useState } from 'react';
import { View } from 'react-native';

import type { AgentClient } from '../../core/api/agentClient';
import type { ShareLink } from '../../core/api/models';
import { BottomSheet, Button, Segmented, SheetHead, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { humanizeError } from '../../core/humanizeError';
import { liveLinkForPath, rememberResolvedPath } from './activeShares';
import { formatRemaining, remainingMs } from './shareLink';
import { presetIndexFor, SHARE_EXPIRY_DEFAULT_INDEX, SHARE_EXPIRY_PRESETS } from './shareLogic';

/** How often the expiry countdown refreshes. */
const COUNTDOWN_TICK_MS = 1000;

/**
 * The share sheet: pick how long the link works (1 hour, 1 day or 1 week), then the link shows in a field with Copy,
 * a live countdown and "Stop sharing". Give it `link` to open on an already minted link, or `path` to start at "Create link".
 * Choosing another length on a minted link makes a new link and stops the old one.
 */
export function ShareLinkSheet({ visible, client, link: initial, path, fileName, computerName, onClose }: { visible: boolean; client: AgentClient; link?: ShareLink; path?: string; fileName: string; computerName?: string; onClose: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const [link, setLink] = useState<ShareLink | undefined>(initial);
  const [choice, setChoice] = useState(() => (initial ? presetIndexFor(remainingMs(initial.expiresAt) / 1000) : SHARE_EXPIRY_DEFAULT_INDEX));
  const [busy, setBusy] = useState(false);
  // Starts true when the lookup will run on open (the sheet is mounted per open), so Create waits for it.
  const [loading, setLoading] = useState(() => visible && !initial && !!path);
  const [now, setNow] = useState(() => Date.now());
  const expiresAt = link?.expiresAt;

  useEffect(() => {
    if (!visible || expiresAt === undefined) return;
    const id = setInterval(() => setNow(Date.now()), COUNTDOWN_TICK_MS);
    return () => clearInterval(id);
  }, [visible, expiresAt]);

  // Opened on a file with no link in hand: show its live link, if one exists, so reopening shows the same link.
  useEffect(() => {
    if (!visible || initial || !path) return;
    let current = true;
    client
      .listShareLinks()
      .then((links) => {
        if (!current) return;
        const found = liveLinkForPath(links, path);
        if (!found) return;
        setLink(found);
        setChoice(presetIndexFor(remainingMs(found.expiresAt) / 1000));
      })
      .catch((e) => {
        if (current) toast.error(t('shareLinkFailed', { error: humanizeError(e) }));
      })
      .finally(() => {
        if (current) setLoading(false);
      });
    return () => {
      current = false;
    };
  }, [visible, client, path, initial, toast]);

  /** Mints a link of preset [i] for the file; when one already exists it is stopped once the new one is ready. */
  const inFlight = useRef(false);
  async function mint(i: number) {
    const target = link?.path ?? path;
    if (!target || busy || inFlight.current) return;
    inFlight.current = true;
    const previous = link;
    setChoice(i);
    setBusy(true);
    try {
      const made = await client.mintShareLink(target, SHARE_EXPIRY_PRESETS[i].seconds);
      rememberResolvedPath(target, made.path);
      setLink(made);
      if (previous) {
        await client
          .revokeShareLink(previous.tokenHash)
          .catch((e) => toast.error(t('shareLinkFailed', { error: humanizeError(e) })));
      }
    } catch (e) {
      toast.error(t('shareLinkFailed', { error: humanizeError(e) }));
      if (previous) setChoice(presetIndexFor(remainingMs(previous.expiresAt) / 1000));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  async function copy() {
    if (!link) return;
    await Clipboard.setStringAsync(link.url);
    toast.info(t('copiedPath', { path: link.url }));
  }

  async function stop() {
    if (!link) return;
    try {
      await client.revokeShareLink(link.tokenHash);
      onClose();
      toast.info(t('shareLinkRevoked'));
    } catch (e) {
      toast.error(t('shareLinkFailed', { error: humanizeError(e) }));
    }
  }

  // The clock is read by the one-second tick, so a freshly minted link may show up to a second of lag.
  const left = expiresAt === undefined ? 0 : remainingMs(expiresAt, now);
  const expired = left <= 0;
  const lede = computerName ? t('shareSheetLede', { name: fileName, computer: computerName }) : t('shareSheetLedeNoComputer', { name: fileName });
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('shareLinkSheetTitle')} />
      <View style={{ paddingHorizontal: Spacing.md, gap: Spacing.sm }}>
        <Text style={LumenType.caption} muted>{lede}</Text>
        <Text style={[LumenType.sectionLabel, { marginTop: Spacing.sm }]} muted>{t('shareSheetWorksFor')}</Text>
        <Segmented options={SHARE_EXPIRY_PRESETS.map((p) => p.label)} selectedIndex={choice} onChange={(i) => (link ? void mint(i) : setChoice(i))} />
        {link ? (
          <View style={{ gap: Spacing.sm, marginTop: Spacing.sm }}>
            {link.url ? (
              <View style={{ minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 6, paddingLeft: Spacing.md2, paddingRight: 6, borderRadius: Radii.control, borderWidth: 1, borderColor: c.rule2, backgroundColor: c.surfaceContainerLowest, opacity: busy ? 0.5 : 1 }}>
                <Text selectable accessibilityRole="text" accessibilityLabel={link.url} style={[LumenType.name, { flex: 1, flexShrink: 1 }]}>{link.url}</Text>
                <Button label={t('copyButton')} kind="tonal" size="s" onPress={copy} disabled={busy} />
              </View>
            ) : (
              <Text style={LumenType.meta} muted>{t('shareSheetAddressHidden')}</Text>
            )}
            <Text style={LumenType.meta} color={expired ? c.stop : c.onSurfaceVariant}>{expired ? t('shareLinkExpired') : t('shareLinkExpiresIn', { time: formatRemaining(left) })}</Text>
            <Text style={LumenType.meta} muted>{t('shareSheetOnce')}</Text>
            <Button label={t('shareSheetStop')} kind="text" destructive onPress={stop} disabled={busy} />
          </View>
        ) : (
          <View style={{ marginTop: Spacing.sm }}>
            <Button label={t('shareSheetCreate')} onPress={() => void mint(choice)} busy={busy || loading} />
          </View>
        )}
      </View>
    </BottomSheet>
  );
}
