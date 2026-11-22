import { Monitor } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import { humanizeError } from '../../core/humanizeError';
import type { Host } from '../../core/models/host';
import { BottomSheet, Pressable, SheetHead, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { destinationStart, startRelayCopy } from '../transfers/relayCopy';
import { DestinationPicker } from './DestinationPicker';

/**
 * Copy files to another paired computer through this phone: pick the computer, then a folder on it. The files are
 * downloaded here and uploaded there by the existing transfer engines (see transfers/relayCopy.ts).
 */
export function RelayCopySheet({ source, paths, onDone, onClose }: { source: Host; paths: string[]; onDone: () => void; onClose: () => void }) {
  const c = useScheme();
  const toast = useToast();
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loadFailed, setLoadFailed] = useState(false);
  const [dest, setDest] = useState<{ host: Host; start: string } | null>(null);

  useEffect(() => {
    let live = true;
    hostStore.listHosts().then(
      (all) => live && setHosts(all.filter((h) => h.id !== source.id)),
      // A failed lookup is not "no other computer": say it could not be read.
      () => live && setLoadFailed(true),
    );
    return () => {
      live = false;
    };
  }, [source.id]);

  async function choose(host: Host) {
    try {
      setDest({ host, start: await destinationStart(host) });
    } catch (e) {
      toast.error(humanizeError(e));
    }
  }

  if (dest) {
    return (
      <DestinationPicker
        visible
        host={dest.host}
        originPath=""
        startPath={dest.start}
        title={t('relayCopyTitle', { name: dest.host.label })}
        confirmLabel={t('relayCopyConfirm')}
        onPick={(dir) => {
          startRelayCopy(source, paths, dest.host, dir).then(
            (n) => {
              toast.success(t('relayCopyQueued', { count: n, name: dest.host.label }));
              onDone();
            },
            (e) => toast.error(humanizeError(e)),
          );
        }}
        onClose={onClose}
      />
    );
  }
  return (
    <BottomSheet visible onClose={onClose}>
      <SheetHead title={t('relayCopyChoose')} subtitle={t('relayCopyHint')} />
      {loadFailed ? (
        <Text style={{ padding: 18, color: c.onSurfaceVariant }}>{t('relayCopyLoadFailed')}</Text>
      ) : hosts.length === 0 ? (
        <Text style={{ padding: 18, color: c.onSurfaceVariant }}>{t('relayCopyNone')}</Text>
      ) : (
        hosts.map((h) => (
          <Pressable key={h.id} onPress={() => void choose(h)} accessibilityLabel={h.label}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, minHeight: 56, paddingHorizontal: 18 }}>
              <Monitor size={22} color={c.onSurfaceVariant} />
              <Text numberOfLines={1} style={[LumenType.name, { flex: 1 }]}>{h.label}</Text>
            </View>
          </Pressable>
        ))
      )}
    </BottomSheet>
  );
}
