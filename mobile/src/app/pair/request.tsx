import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { useEffect, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';

import { humanizeError } from '../../core/humanizeError';
import { Text, useToast } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { GettingInBar, ProblemView, WaitingView } from '../../features/pairing/GettingInViews';
import { pairProblem, waitMinutes, type PairProblem } from '../../features/pairing/gettingInLogic';
import { groupFingerprint } from '../../features/pairing/identity';
import { awaitPairing, requestPairing, type PairRequestHandle } from '../../features/pairing/pairingService';
import { t } from '../../i18n';
import { pairingDeps } from '../../services';
import { Spacing } from '../../design/tokens';

type Phase = { kind: 'connecting' } | { kind: 'waiting'; handle: PairRequestHandle } | { kind: 'failed'; problem: PairProblem; detail: string; fingerprint?: string };

const PROBLEM_COPY: Record<PairProblem, { title: 'giDeclinedTitle' | 'giExpiredTitle' | 'giTooOldTitle' | 'giFailedTitle'; body: 'giDeclinedBody' | 'giExpiredBody' | 'giTooOldBody' | 'giFailedBody' }> = {
  declined: { title: 'giDeclinedTitle', body: 'giDeclinedBody' },
  expired: { title: 'giExpiredTitle', body: 'giExpiredBody' },
  tooOld: { title: 'giTooOldTitle', body: 'giTooOldBody' },
  failed: { title: 'giFailedTitle', body: 'giFailedBody' },
};

/**
 * Asks the computer to allow this phone and waits for the answer. The screen shows the match number to compare and
 * nothing technical; the address, the computer's identity and any error text are under a closed "Technical details" row. The request itself
 * (pin on first contact, polling, the match code) is unchanged.
 */
export default function PairRequest() {
  const router = useRouter();
  const toast = useToast();
  const c = useScheme();
  const { address } = useLocalSearchParams<{ address: string }>();
  const [phase, setPhase] = useState<Phase>({ kind: 'connecting' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const ctl = new AbortController();
    void (async () => {
      let fingerprint: string | undefined;
      try {
        const handle = await requestPairing(pairingDeps, address);
        if (ctl.signal.aborted) return;
        fingerprint = handle.target.fingerprint;
        setPhase({ kind: 'waiting', handle });
        const host = await awaitPairing(pairingDeps, handle, { signal: ctl.signal });
        toast.success(t('pairedWith', { name: host.label }));
        router.dismissTo('/');
      } catch (e) {
        if (ctl.signal.aborted) return;
        setPhase({ kind: 'failed', problem: pairProblem(e), detail: humanizeError(e), fingerprint });
      }
    })();
    return () => ctl.abort();
  }, [address, attempt, router, toast]);

  const retry = () => {
    setPhase({ kind: 'connecting' });
    setAttempt((a) => a + 1);
  };
  const cancel = () => router.back();

  let body;
  if (phase.kind === 'waiting') {
    body = (
      <WaitingView
        matchCode={phase.handle.matchCode}
        minutes={waitMinutes(phase.handle.expiresInSeconds)}
        details={[address ?? '', t('computerIdentityLine', { fingerprint: groupFingerprint(phase.handle.target.fingerprint) })]}
        onSignIn={() => router.replace({ pathname: '/pair/login', params: { address } })}
        onCancel={cancel}
      />
    );
  } else if (phase.kind === 'failed') {
    const copy = PROBLEM_COPY[phase.problem];
    body = (
      <ProblemView
        title={t(copy.title)}
        body={t(copy.body)}
        primary={phase.problem === 'tooOld' ? { label: t('giScanCode'), onPress: () => router.replace('/pair/scan') } : { label: t('giTryAgain'), onPress: retry }}
        onCancel={cancel}
        details={[address ?? '', ...(phase.fingerprint ? [t('computerIdentityLine', { fingerprint: groupFingerprint(phase.fingerprint) })] : []), phase.detail]}
      />
    );
  } else {
    body = (
      <View style={{ flex: 1, backgroundColor: c.surface }}>
        <GettingInBar onClose={cancel} />
        <View style={{ paddingTop: Spacing.lg, paddingHorizontal: Spacing.lg, flexDirection: 'row', alignItems: 'center', gap: Spacing.md2 }} accessibilityRole="progressbar" accessibilityLabel={t('giConnecting')}>
          <ActivityIndicator size="small" color={c.onSurfaceVariant} />
          <Text style={LumenType.name} muted>
            {t('giConnecting')}
          </Text>
        </View>
      </View>
    );
  }
  return (
    <>
      <Stack.Screen options={{ headerShown: false, title: t('pairRequestTitle') }} />
      {body}
    </>
  );
}
