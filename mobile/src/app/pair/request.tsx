import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { useEffect, useState } from 'react';
import { ActivityIndicator, ScrollView, View } from 'react-native';

import { isUnsupportedByAgent } from "../../core/api/compat";
import {
  GroupedCard,
  InlineError,
  PageHead,
  Text,
  useToast,
} from "../../design/components";
import { LumenType } from "../../design/lumen";
import { useScheme } from "../../design/theme";
import { FontFamily } from "../../design/tokens";
import { LumenFooter, StackTopBar } from "../../features/hosts/LumenRows";
import {
  awaitPairing,
  humanizeError,
  PairExpired,
  PairRejected,
  requestPairing,
  type PairRequestHandle,
} from "../../features/pairing/pairingService";
import { t } from "../../i18n";
import { pairingDeps } from "../../services";

type Phase =
  | { kind: "connecting" }
  | { kind: "waiting"; handle: PairRequestHandle }
  | { kind: "failed"; message: string; tooOld?: boolean };

/** Asks the computer to approve this phone and waits for the answer; the only thing shown is a match code to compare. */
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
      try {
        const handle = await requestPairing(pairingDeps, address);
        if (ctl.signal.aborted) return;
        setPhase({ kind: 'waiting', handle });
        const host = await awaitPairing(pairingDeps, handle, { signal: ctl.signal });
        toast.success(t('pairedWith', { name: host.label }));
        router.dismissTo('/');
      } catch (e) {
        if (ctl.signal.aborted) return;
        const message =
          e instanceof PairRejected
            ? t("pairRequestRejected")
            : e instanceof PairExpired
              ? t("pairRequestExpired")
              : t("pairRequestFailed", { error: humanizeError(e) });
        if (isUnsupportedByAgent(e))
          setPhase({
            kind: "failed",
            message: t("pairRequestUnsupported"),
            tooOld: true,
          });
        else setPhase({ kind: "failed", message });
      }
    })();
    return () => ctl.abort();
  }, [address, attempt, router, toast]);

  const retry = () => {
    setPhase({ kind: 'connecting' });
    setAttempt((a) => a + 1);
  };

  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false, title: t('pairRequestTitle') }} />
      <StackTopBar context="RFE · Pair" />
      <ScrollView contentContainerStyle={{ paddingHorizontal: 18, paddingBottom: 24, gap: 14 }}>
        <View style={{ marginHorizontal: -18 }}>
          <PageHead title={t('pairRequestTitle')} subtitle={address} />
        </View>
        {phase.kind === 'connecting' && (
          <View style={{ alignItems: 'center', gap: 12, paddingVertical: 32 }}>
            <ActivityIndicator color={c.primary} />
            <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('pairRequestConnecting')}</Text>
          </View>
        )}
        {phase.kind === 'waiting' && (
          <>
            <GroupedCard>
              <View style={{ alignItems: 'center', gap: 8, paddingVertical: 14 }}>
                <Text style={LumenType.meta} color={c.onSurfaceVariant}>{t('pairRequestMatchCode')}</Text>
                <Text style={{ fontFamily: FontFamily.monoMedium, fontSize: 40, lineHeight: 48, letterSpacing: 2, color: c.primary }} accessibilityLabel={phase.handle.matchCode.replace(/\D/g, ' ').trim()}>
                  {phase.handle.matchCode}
                </Text>
              </View>
            </GroupedCard>
            <View style={{ alignItems: 'center', gap: 10 }}>
              <ActivityIndicator color={c.primary} />
              <Text style={LumenType.title}>{t('pairRequestApprove')}</Text>
            </View>
            <Text style={[LumenType.meta, { textAlign: 'center' }]} color={c.onSurfaceVariant}>{t('pairRequestApproveBody', { name: address })}</Text>
          </>
        )}
        {phase.kind === 'failed' && <InlineError message={phase.message} />}
      </ScrollView>
      <LumenFooter
        buttons={
          phase.kind === 'failed'
            ? [
                phase.tooOld
                  ? {
                      key: "code",
                      label: t("pairRequestUseCode"),
                      primary: true,
                      onPress: () => router.replace("/pair/scan"),
                    }
                  : {
                      key: "retry",
                      label: t("pairRequestRetry"),
                      primary: true,
                      onPress: retry,
                    },
                {
                  key: "cancel",
                  label: t("pairRequestCancel"),
                  primary: false,
                  onPress: () => router.back(),
                },
              ]
            : [
                {
                  key: "cancel",
                  label: t("pairRequestCancel"),
                  primary: false,
                  onPress: () => router.back(),
                },
              ]
        }
      />
    </View>
  );
}
