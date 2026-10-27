import { Monitor, MonitorSmartphone, Rocket, Smartphone, Wifi, type LucideIcon } from 'lucide-react-native';
import { useRef, useState } from 'react';
import { ScrollView, useWindowDimensions, View, type NativeScrollEvent, type NativeSyntheticEvent } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { mix } from '../../design/color';
import { Button, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useRoles, useScheme } from '../../design/theme';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';

type Page = { icon: LucideIcon; title: string; body: string; hero?: 'link' };

/** The three first-run pages: what the app is, how it works, and the call to pair a first computer. */
export function OnboardingScreen({ onComplete }: { onComplete: () => void }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const scroller = useRef<ScrollView>(null);
  const [page, setPage] = useState(0);

  const pages: Page[] = [
    { icon: MonitorSmartphone, title: t('onboardingWelcomeTitle'), body: t('onboardingWelcomeBody'), hero: 'link' },
    { icon: Wifi, title: t('onboardingHowTitle'), body: t('onboardingHowBody') },
    { icon: Rocket, title: t('onboardingReadyTitle'), body: t('onboardingReadyBody') },
  ];
  const isLast = page === pages.length - 1;

  const goTo = (i: number) => {
    scroller.current?.scrollTo({ x: i * width, animated: true });
    setPage(i);
  };
  const onSettle = (e: NativeSyntheticEvent<NativeScrollEvent>) => setPage(Math.round(e.nativeEvent.contentOffset.x / width));

  return (
    <View style={{ flex: 1, backgroundColor: c.surface, paddingTop: insets.top, paddingBottom: insets.bottom }}>
      <ScrollView ref={scroller} horizontal pagingEnabled showsHorizontalScrollIndicator={false} onMomentumScrollEnd={onSettle} style={{ flex: 1 }}>
        {pages.map((p) => (
          <View key={p.title} style={{ width, paddingHorizontal: Spacing.xl, alignItems: 'center', justifyContent: 'center' }}>
            {p.hero === 'link' ? <DeviceLinkHero /> : <BlobHero icon={p.icon} />}
            <Text style={[LumenType.pageTitle, { textAlign: 'center', marginTop: Spacing.xl }]}>{p.title}</Text>
            <Text muted style={[LumenType.title, { fontFamily: LumenType.caption.fontFamily, lineHeight: 28, textAlign: 'center', marginTop: Spacing.md }]}>{p.body}</Text>
          </View>
        ))}
      </ScrollView>
      <View style={{ flexDirection: 'row', alignItems: 'center', paddingHorizontal: Spacing.lg, paddingTop: Spacing.md, paddingBottom: Spacing.lg }}>
        <View style={{ flexDirection: 'row', gap: Spacing.xs }} accessibilityLabel={`${page + 1} / ${pages.length}`}>
          {pages.map((p, i) => (
            <View key={p.title} style={{ width: i === page ? 20 : 8, height: 8, borderRadius: 4, backgroundColor: i === page ? c.primary : c.outlineVariant }} />
          ))}
        </View>
        <View style={{ flex: 1 }} />
        {page > 0 ? <Button size="lg" kind="text" label={t('onboardingBack')} onPress={() => goTo(page - 1)} /> : null}
        <Button size="lg" label={isLast ? t('onboardingGetStarted') : t('onboardingNext')} onPress={isLast ? onComplete : () => goTo(page + 1)} />
      </View>
    </View>
  );
}

function BlobHero({ icon: Icon }: { icon: LucideIcon }) {
  const c = useScheme();
  return (
    <View style={{ width: 140, height: 140, borderRadius: 70, alignItems: 'center', justifyContent: 'center', backgroundColor: mix(c.primary, c.surface, 0.18) }}>
      <Icon size={58} color={c.primary} />
    </View>
  );
}

/** Phone chip, dashed connector, monitor chip: the mockup's welcome art. */
function DeviceLinkHero() {
  const c = useScheme();
  const roles = useRoles();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md2 }}>
      <Chip width={64} height={88} tint={c.primary} Icon={Smartphone} />
      <View style={{ width: 30, height: 2, flexDirection: 'row', gap: 4 }}>
        {[0, 1, 2, 3].map((i) => (
          <View key={i} style={{ flex: 1, backgroundColor: c.outline }} />
        ))}
      </View>
      <Chip width={104} height={74} tint={roles.route} Icon={Monitor} />
    </View>
  );
}

function Chip({ width, height, tint, Icon }: { width: number; height: number; tint: string; Icon: LucideIcon }) {
  const c = useScheme();
  return (
    <View style={{ width, height, borderRadius: 18, alignItems: 'center', justifyContent: 'center', backgroundColor: c.surfaceContainer }}>
      <Icon size={height * 0.28} color={tint} />
    </View>
  );
}
