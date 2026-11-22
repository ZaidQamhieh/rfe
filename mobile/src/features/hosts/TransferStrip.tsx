import { useRouter } from 'expo-router';
import { Pause, Play } from 'lucide-react-native';
import { useEffect, useMemo, useState } from 'react';
import { View } from 'react-native';

import { formatSize } from '../../core/format';
import { humanizeError } from '../../core/humanizeError';
import { isUserCancel } from '../../core/userCancel';
import { transfers, type TransferRecord } from '../../core/native';
import { DuoIcon, Pressable, Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { stripModel } from './transferStrip';
import { Spacing } from '../../design/tokens';

/**
 * Nameplate `.moving`: a 56 high strip above the nav while anything is transferring. A 2 px bar on its top edge shows
 * the share of bytes done, the text says what is moving and where, and the button pauses (or resumes) all of it.
 * Renders nothing when no transfer is in progress. Tapping the text opens the Transfers tab.
 */
export function TransferStrip({ records }: { records: TransferRecord[] | null }) {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const [hostNames, setHostNames] = useState<Record<string, string>>({});
  useEffect(() => {
    let live = true;
    hostStore.listHosts().then(
      (hs) => live && setHostNames(Object.fromEntries(hs.map((h) => [h.id, h.label || h.address]))),
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, []);
  const model = useMemo(() => stripModel(records ?? [], hostNames, formatSize), [records, hostNames]);
  if (!model) return null;

  const toggle = () => {
    const act = model.paused ? transfers.resume : transfers.pause;
    Promise.all(model.targetIds.map((id) => act(id))).catch((e) => {
      if (!isUserCancel(e)) toast.error(humanizeError(e));
    });
  };

  return (
    <View style={{ height: 56, flexDirection: 'row', alignItems: 'center', gap: Spacing.md2, paddingLeft: Spacing.md, paddingRight: Spacing.xs, backgroundColor: c.surfaceContainerLowest, borderTopWidth: 1, borderTopColor: c.rule }}>
      <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ position: 'absolute', left: 0, top: -1, height: 2, width: `${Math.round(model.progress * 100)}%`, backgroundColor: c.primary }} />
      <Pressable onPress={() => router.navigate('/transfers')} accessibilityLabel={model.title} pressedScale={1} style={{ flex: 1, minWidth: 0 }}>
        <View>
          <Text style={LumenType.name} numberOfLines={1}>
            {model.title}
          </Text>
          {model.line ? (
            <Text style={LumenType.meta} muted numberOfLines={1}>
              {model.line}
            </Text>
          ) : null}
        </View>
      </Pressable>
      <Pressable onPress={toggle} accessibilityLabel={model.paused ? t('stripResumeLabel') : t('stripPauseLabel')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={model.paused ? Play : Pause} size={24} />
      </Pressable>
    </View>
  );
}
