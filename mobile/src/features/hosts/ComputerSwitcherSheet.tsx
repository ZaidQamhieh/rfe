import { Check, Plus, Power } from 'lucide-react-native';
import { useEffect, useRef, useState } from 'react';
import { View } from 'react-native';

import { sendWakeOnLan } from '../../core/native';
import { sleep } from '../../core/sleep';
import type { Host } from '../../core/models/host';
import { BottomSheet, Button, DuoIcon, Emoji, Row, SheetHead, SheetScroll, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { clientForHost } from '../../services';
import { pairAgainAddress, switcherRowState } from './switcherLogic';
import { useHostStatus } from './useHostStatus';
import { wakeThenOpen } from './wakeThenOpen';
import { Spacing } from '../../design/tokens';
import { useAppDensity } from '../../state/density';

/** How long a Wake waits for the computer to answer, and how often it asks. */
const WAKE_TIMEOUT_MS = 90_000;
const WAKE_POLL_MS = 3_000;
/** One quick "are you there" check while waiting for a wake. */
const WAKE_PING_MS = 3_000;

/**
 * One computer in the switcher: its picture (grey when asleep or unreachable), name, a plain state phrase and, when it
 * applies, a short Wake or Review button; the selected one is filled with a 1px border and a check mark.
 */
function SwitcherRow({ host, selected, onSelect, onReview, onPair, onChanged }: { host: Host; selected: boolean; onSelect: () => void; onReview: () => void; onPair: (address: string) => void; onChanged: () => void }) {
  const c = useScheme();
  const density = useAppDensity();
  const toast = useToast();
  const st = useHostStatus(host, onChanged);
  const state = switcherRowState(st, host);
  const [waking, setWaking] = useState(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Sends the magic packet, then waits for the computer to answer; the row refreshes its state when it does.
  const wake = async () => {
    setWaking(true);
    try {
      const result = await wakeThenOpen({
        mac: host.macAddress,
        isUp: () => clientForHost(host, false, WAKE_PING_MS).then((cl) => cl.health()).then(() => true, () => false),
        wake: (mac) => sendWakeOnLan(mac).catch(() => false),
        sleep,
        now: Date.now,
        timeoutMs: WAKE_TIMEOUT_MS,
        intervalMs: WAKE_POLL_MS,
        isCancelled: () => !mounted.current,
        onWaking: () => toast.info(t('wakingHost', { hostname: host.label })),
      });
      if (result === 'timeout') toast.error(t('switcherWakeTimedOut', { hostname: host.label }));
      else if (result === 'send-failed' || result === 'cannot-wake') toast.error(t('wolPacketFailed'));
      if (mounted.current) st.refresh();
    } finally {
      if (mounted.current) setWaking(false);
    }
  };

  const trailing =
    state.action === 'wake' ? (
      <Button size="s" kind="neutral" label={t('switcherWake')} busy={waking} renderIcon={(k) => <Power size={18} color={k} />} onPress={() => void wake()} />
    ) : state.action === 'review' ? (
      <Button size="s" kind="neutral" label={t('switcherReview')} onPress={onReview} />
    ) : state.action === 'pair' ? (
      <Button size="s" kind="neutral" label={t('pairAgain')} onPress={() => onPair(pairAgainAddress(st, host))} />
    ) : selected ? (
      <DuoIcon icon={Check} size={20} color={c.primary} />
    ) : undefined;

  return <Row density={density} selected={selected} lead={<Emoji name="desktop_computer" size={28} off={state.off} />} title={host.label} subtitle={state.phrase} subtitleColor={state.kind === 'online' ? c.go : undefined} trailing={trailing} onPress={onSelect} accessibilityLabel={`${host.label}, ${state.phrase}${selected ? t('selectedSuffix') : ''}`} />;
}

/**
 * The computer switcher: a bottom sheet titled Computers with one row per paired computer, then "Add a computer" and
 * "Manage". Picking a row makes that computer the one Home shows; the sheet closes before anything else happens.
 */
export function ComputerSwitcherSheet({
  visible,
  hosts,
  selectedId,
  onClose,
  onSelect,
  onReview,
  onPair,
  onAdd,
  onManage,
  onChanged,
}: {
  visible: boolean;
  hosts: Host[];
  selectedId: string;
  onClose: () => void;
  onSelect: (host: Host) => void;
  onReview: (host: Host) => void;
  onPair: (address: string) => void;
  onAdd: () => void;
  onManage: () => void;
  onChanged: () => void;
}) {
  const c = useScheme();
  const then = (fn: () => void) => () => {
    onClose();
    fn();
  };
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('switcherTitle')} />
      <SheetScroll>
        {hosts.map((h) => (
          <SwitcherRow key={h.id} host={h} selected={h.id === selectedId} onSelect={then(() => onSelect(h))} onReview={then(() => onReview(h))} onPair={(address) => then(() => onPair(address))()} onChanged={onChanged} />
        ))}
      </SheetScroll>
      <View style={{ height: 1, backgroundColor: c.rule, marginTop: Spacing.xs }} />
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingTop: Spacing.md2 }}>
        <Button kind="tonal" label={t('switcherAdd')} renderIcon={(k) => <Plus size={20} color={k} />} onPress={then(onAdd)} style={{ flex: 1 }} />
        <Button kind="text" label={t('switcherManage')} onPress={then(onManage)} />
      </View>
    </BottomSheet>
  );
}
