import { Monitor } from 'lucide-react-native';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Platform, View } from 'react-native';

import type { DiscoveredAgent } from '../../core/discovery';
import { scanLan, stopLanScan } from '../../core/native';
import { Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { MethodRow, MetaPill } from '../hosts/LumenRows';

export type LanScan = { agents: DiscoveredAgent[]; scanning: boolean; searched: boolean; failed: boolean; supported: boolean; toggle: () => Promise<void> };

/** Local-network discovery state: one search at a time, stopped when the screen goes away. The footer button drives [toggle]. */
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
      if (scanningRef.current) stopLanScan().catch(() => {});
    };
  }, []);

  const toggle = useCallback(async () => {
    if (scanningRef.current) {
      await stopLanScan().catch(() => {});
      return;
    }
    scanningRef.current = true;
    setScanning(true);
    setSearched(true);
    setFailed(false);
    setAgents([]);
    try {
      const found = await scanLan();
      if (live.current) setAgents(found);
    } catch {
      if (live.current) setFailed(true);
    } finally {
      scanningRef.current = false;
      if (live.current) setScanning(false);
    }
  }, []);

  return { agents, scanning, searched, failed, supported: Platform.OS === 'android', toggle };
}

/** The "Find on local network" body: the intro, search progress and one row per computer found. */
export function LanPanel({ scan, onSelect }: { scan: LanScan; onSelect: (authority: string) => void }) {
  const c = useScheme();
  const { agents, scanning, searched, failed, supported } = scan;
  if (!supported) {
    return (
      <Text style={[LumenType.meta, { padding: 8 }]} color={c.onSurfaceVariant}>
        {t('lanDiscoveryUnavailable')}
      </Text>
    );
  }
  return (
    <View style={{ gap: 10 }}>
      <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
        {t('lanDiscoveryIntro')}
      </Text>
      {scanning && (
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 4 }}>
          <ActivityIndicator color={c.primary} />
          <Text style={LumenType.meta} color={c.onSurfaceVariant}>
            {t('searchingLocalNetwork')}
          </Text>
        </View>
      )}
      {failed ? (
        <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.error}>
          {t('lanDiscoveryFailed')}
        </Text>
      ) : !scanning && searched && agents.length === 0 ? (
        <Text style={[LumenType.meta, { paddingHorizontal: 4 }]} color={c.onSurfaceVariant}>
          {t('noLocalAgentsFound')}
        </Text>
      ) : null}
      {agents.map((a) => (
        <MethodRow
          key={a.authority}
          icon={Monitor}
          tone={c.primary}
          title={a.name}
          subtitle={a.authority}
          onPress={() => onSelect(a.authority)}
          accessibilityLabel={`${t('useAddress')} ${a.name}`}
          right={<MetaPill label={t('useAddress')} color={c.primary} />}
        />
      ))}
    </View>
  );
}
