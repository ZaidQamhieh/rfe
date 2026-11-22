import { ArrowLeft, Plus, X } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Button, Disclosure, DuoIcon, Emoji, Pressable, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { ExplainerText, Spacing, Radii } from '../../design/tokens';
import { t } from '../../i18n';
import { spokenCode } from './gettingInLogic';

/** Nameplate `.bar`: a 56 high row under the status bar with one 48 dp icon button (close or back) and an optional title. */
export function GettingInBar({ onClose, back = false, title }: { onClose: () => void; back?: boolean; title?: string }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <View style={{ paddingTop: insets.top, flexDirection: 'row', alignItems: 'center', height: 56 + insets.top, paddingHorizontal: Spacing.xs }}>
      <Pressable onPress={onClose} accessibilityLabel={back ? t('searchBack') : t('cancelButton')} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        <DuoIcon icon={back ? ArrowLeft : X} size={24} color={c.onSurface} />
      </Pressable>
      {title ? (
        <Text style={[LumenType.appbar, { flex: 1, paddingLeft: Spacing.xs }]} numberOfLines={1} accessibilityRole="header">
          {title}
        </Text>
      ) : null}
    </View>
  );
}

/** `.state`: a small coloured dot and a short word, for what is happening now (waiting, not connected). */
function StateLine({ label, tone }: { label: string; tone: 'wait' | 'stop' | 'go' }) {
  const c = useScheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.sm, marginBottom: 14 }}>
      <View style={{ width: 8, height: 8, borderRadius: Radii.file, backgroundColor: c[tone] }} />
      <Text style={LumenType.caption} color={tone === 'stop' ? c.stop : c.onSurfaceVariant}>
        {label}
      </Text>
    </View>
  );
}

/** `.empty`: padded 24 on the sides, the content left-aligned, scrolling when the phone is small. */
function EmptyBody({ children, top = 24 }: { children: ReactNode; top?: number }) {
  return (
    <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingTop: top, paddingHorizontal: Spacing.lg, paddingBottom: Spacing.lg }} keyboardShouldPersistTaps="handled">
      {children}
    </ScrollView>
  );
}

/** `.empty h5` and `.empty p`: a heading and a muted sentence; [size] 24 is the first-run headline. */
function Heading({ children, big = false }: { children: string; big?: boolean }) {
  return (
    <Text style={[big ? { fontSize: 24, lineHeight: 32, fontFamily: LumenType.pageTitle.fontFamily } : LumenType.pageTitle, { marginBottom: Spacing.sm }]} accessibilityRole="header">
      {children}
    </Text>
  );
}

function Para({ children, quiet = false, gap = Spacing.md3 }: { children: ReactNode; quiet?: boolean; gap?: number }) {
  return (
    <Text style={[quiet ? LumenType.meta : { ...ExplainerText, fontFamily: LumenType.name.fontFamily }, { maxWidth: ExplainerText.maxWidth, marginBottom: gap }]} muted>
      {children}
    </Text>
  );
}

/** A full-width button; `primary` is the one filled action, `quiet` a bare text button, `danger` the red outlined one. */
function Action({ label, onPress, kind = 'neutral', icon, destructive }: { label: string; onPress: () => void; kind?: 'filled' | 'neutral' | 'text'; icon?: ReactNode; destructive?: boolean }) {
  return <Button label={label} kind={kind} onPress={onPress} icon={icon} destructive={destructive} style={{ alignSelf: 'stretch' }} />;
}

/** The first thing a new person sees: what RFE is for, one button to add a computer, and what the computer needs. */
export function FirstRunView({ onAdd }: { onAdd: () => void }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  return (
    <View style={{ flex: 1, backgroundColor: c.surface, paddingTop: insets.top, paddingBottom: insets.bottom }}>
      <EmptyBody top={64}>
        <View style={{ marginBottom: Spacing.lg }}>
          <Emoji name="monitor" size={72} />
        </View>
        <Heading big>{t('giFirstRunTitle')}</Heading>
        <Para>{t('giFirstRunBody')}</Para>
        <Action label={t('giAddComputer')} kind="filled" onPress={onAdd} icon={<DuoIcon icon={Plus} size={20} color={c.onPrimary} />} />
        <Text style={[LumenType.meta, { marginTop: Spacing.md2 }]} muted>
          {t('giFirstRunNeedsApp')}
        </Text>
      </EmptyBody>
    </View>
  );
}

/**
 * "Allow this phone": the 8-digit number is the biggest thing on screen (`.sas`, mono 40/48). The person compares it with
 * the one on the computer and presses Allow there. Cancel is a quiet text button; the address stays under Technical details.
 */
export function WaitingView({ matchCode, minutes, details, onSignIn, onCancel }: { matchCode: string; minutes: number; details: string[]; onSignIn: () => void; onCancel: () => void }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <GettingInBar onClose={onCancel} />
      <EmptyBody>
        <StateLine label={t('giWaiting')} tone="wait" />
        <Heading>{t('giAllowTitle')}</Heading>
        <Para gap={8}>{t('giAllowBody')}</Para>
        <Text style={{ fontFamily: LumenType.monoCode.fontFamily, fontSize: 40, lineHeight: 48, marginVertical: Spacing.xs }} accessibilityLabel={spokenCode(matchCode)} selectable>
          {matchCode}
        </Text>
        <Para quiet gap={32}>
          {minutes === 1 ? t('giWaitsForOne') : t('giWaitsFor', { count: minutes })}
        </Para>
        <Para gap={12}>{t('giNotNear')}</Para>
        <View style={{ flexDirection: 'row', gap: Spacing.sm }}>
          <Button label={t('giSignInInstead')} kind="neutral" onPress={onSignIn} />
          <Button label={t('cancelButton')} kind="text" onPress={onCancel} />
        </View>
      </EmptyBody>
      <TechnicalDetails lines={details} />
    </View>
  );
}

/** The closed "Technical details" row at the foot of a screen, listing [lines] (addresses, error text) once opened. */
export function TechnicalDetails({ lines }: { lines: string[] }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const shown = lines.filter((l) => l.length > 0);
  if (shown.length === 0) return null;
  return (
    <View style={{ paddingBottom: insets.bottom }}>
      <Disclosure label={t('giTechnical')}>
        {shown.map((l, i) => (
          <Text key={i} style={LumenType.meta} color={c.onSurfaceVariant} selectable>
            {l}
          </Text>
        ))}
      </Disclosure>
    </View>
  );
}

/** A pairing request that ended without a computer: a short title, one sentence, a primary action and a way out. */
export function ProblemView({ title, body, primary, onCancel, details }: { title: string; body: string; primary: { label: string; onPress: () => void }; onCancel: () => void; details: string[] }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <GettingInBar onClose={onCancel} />
      <EmptyBody>
        <StateLine label={t('giIdentityState')} tone="stop" />
        <Heading>{title}</Heading>
        <Para gap={24}>{body}</Para>
        <View style={{ flexDirection: 'row', gap: Spacing.sm }}>
          <Button label={primary.label} kind="filled" onPress={primary.onPress} />
          <Button label={t('cancelButton')} kind="text" onPress={onCancel} />
        </View>
      </EmptyBody>
      <TechnicalDetails lines={details} />
    </View>
  );
}

/**
 * The identity-changed screen: the computer answered with a different key than the one saved when it was added. RFE has
 * already stopped; the person may add it again, forget it (the only red), or leave it. Fingerprints are not shown here.
 */
export function IdentityChangedView({ name, details, kind = 'changed', onAddAgain, onForget, onNotNow }: { name: string; details: string[]; kind?: 'changed' | 'unknown'; onAddAgain: () => void; onForget: () => void; onNotNow: () => void }) {
  const c = useScheme();
  const changed = kind === 'changed';
  return (
    <View style={{ flex: 1, backgroundColor: c.surface }}>
      <GettingInBar onClose={onNotNow} back />
      <EmptyBody top={8}>
        <StateLine label={changed ? t('giIdentityState') : t('giUnknownState')} tone={changed ? 'stop' : 'wait'} />
        <View style={{ marginBottom: Spacing.md }}>
          <Emoji name="lock" size={48} />
        </View>
        <Heading>{changed ? t('giIdentityTitle', { name }) : t('giUnknownTitle', { name })}</Heading>
        <Para gap={12}>{changed ? t('giIdentityBody1') : t('giUnknownBody1', { name })}</Para>
        <Para gap={24}>{changed ? t('giIdentityBody2') : t('giUnknownBody2')}</Para>
        <View style={{ gap: Spacing.sm }}>
          <Action label={t('giAddAgain')} kind="filled" onPress={onAddAgain} />
          <Action label={t('giForget', { name })} kind="neutral" destructive onPress={onForget} />
          <Action label={t('giNotNow')} kind="text" onPress={onNotNow} />
        </View>
      </EmptyBody>
      <TechnicalDetails lines={details} />
    </View>
  );
}
