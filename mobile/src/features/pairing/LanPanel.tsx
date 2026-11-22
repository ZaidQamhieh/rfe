import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Platform, ScrollView, View } from 'react-native';

import { mergeDiscovered, type DiscoveredAgent } from '../../core/discovery';
import type { Host } from '../../core/models/host';
import { ignoreError } from '../../core/log';
import { scanLan, stopLanScan } from '../../core/native';
import { Button, Emoji, Row, SectionLabel, Text } from '../../design/components';
import { LumenSize, LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { Spacing } from '../../design/tokens';
import { savedHostAt } from './savedComputers';

/**
 * Fixed height of the result area. The notes and the found list scroll inside it, so the button below never moves when a
 * search ends or finds more computers. Only the container has a height; the text inside is not sized, so large fonts wrap.
 */
const LAN_RESULTS_HEIGHT = 192;

export type LanScan = { agents: DiscoveredAgent[]; scanning: boolean; searched: boolean; failed: boolean; supported: boolean; toggle: () => Promise<void> };

/** Local-network discovery state: one search at a time, stopped when the screen goes away. [toggle] starts or stops a search. */
export function useLanScan(): LanScan {
  const [agents, setAgents] = useState<DiscoveredAgent[]>([]);
  const [scanning, setScanning] = useState(false);
  const [searched, setSearched] = useState(false);
  const [failed, setFailed] = useState(false);
  const live = useRef(true);
  const scanningRef = useRef(false);

  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
      if (scanningRef.current) stopLanScan().catch(ignoreError('stop LAN scan'));
    };
  }, []);

  const toggle = useCallback(async () => {
    if (scanningRef.current) {
      await stopLanScan().catch(ignoreError('stop LAN scan'));
      return;
    }
    scanningRef.current = true;
    setScanning(true);
    setSearched(true);
    setFailed(false);
    setAgents([]);
    try {
      // Each computer is listed the moment it answers; the final list only fills in any that were missed.
      const found = await scanLan((a) => {
        if (live.current) setAgents((cur) => mergeDiscovered(cur, a));
      });
      if (live.current) setAgents((cur) => found.reduce((list, a) => mergeDiscovered(list, a), cur));
    } catch {
      if (live.current) setFailed(true);
    } finally {
      scanningRef.current = false;
      if (live.current) setScanning(false);
    }
  }, []);

  return { agents, scanning, searched, failed, supported: Platform.OS === 'android', toggle };
}

/**
 * "On this Wi-Fi": one row per computer found, each with a Connect button, plus a quiet line for searching, nothing found
 * or a failed search and a text button to search again. The computer's address is not shown here; only its name. A computer
 * that is already in [saved] reads "Already added" and its button is off, so it cannot be added a second time.
 */
export function LanPanel({ scan, onSelect, saved = [] }: { scan: LanScan; onSelect: (authority: string) => void; saved?: readonly Host[] }) {
  const c = useScheme();
  const { agents, scanning, searched, failed, supported } = scan;
  const note = (text: string) => (
    <Text style={[LumenType.meta, { paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm }]} color={c.onSurfaceVariant}>
      {text}
    </Text>
  );
  return (
    <View>
      <View style={{ paddingHorizontal: Spacing.xs }}>
        <SectionLabel title={t('giOnWifi')} />
      </View>
      <ScrollView style={{ height: LAN_RESULTS_HEIGHT }} nestedScrollEnabled>
        {!supported ? note(t('giWifiUnavailable')) : null}
        {supported && scanning ? (
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md2, paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm }} accessibilityRole="progressbar">
            <ActivityIndicator size="small" color={c.onSurfaceVariant} />
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>
              {t('giSearching')}
            </Text>
          </View>
        ) : null}
        {supported && failed ? note(t('giSearchFailed')) : null}
        {supported && !failed && !scanning && searched && agents.length === 0 ? note(t('giNoneFound')) : null}
        {agents.map((a) => {
          const known = savedHostAt(saved, a.authority) !== undefined;
          return (
            <Row
              key={a.authority}
              lead={<Emoji name="monitor" size={32} />}
              title={a.name}
              subtitle={known ? t('giAlreadyAdded') : t('giRfeRunning')}
              trailing={known ? <Button size="s" kind="neutral" label={t('giAdded')} disabled onPress={() => undefined} /> : <Button size="s" kind="neutral" label={t('giConnect')} onPress={() => onSelect(a.authority)} />}
            />
          );
        })}
      </ScrollView>
      <View style={{ paddingHorizontal: Spacing.xs, minHeight: LumenSize.actionHeightSmall, alignItems: 'flex-start', justifyContent: 'center' }}>
        {supported && !scanning && searched ? <Button size="s" kind="text" label={t('giSearchAgain')} onPress={() => void scan.toggle()} /> : null}
      </View>
    </View>
  );
}
