import { Power, RefreshCw } from 'lucide-react-native';
import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { Button, Emoji, Row, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { FontFamily, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { emojiNameFor } from '../explorer/entryEmoji';
import { StatusBlock } from '../explorer/FolderStates';
import { relativeLabel } from './relative';

type Props = {
  kind: 'asleep' | 'lost' | 'refused';
  name: string;
  /** When the computer last answered; the lost sentence says how long ago, or nothing about time when unknown. */
  lastSeen?: Date | null;
  /** Only given for a sleeping computer that can be woken. */
  onWake?: () => void;
  onRetry: () => void;
  onCheck?: () => void;
  /** Refused view only: opens pairing again. */
  onPair?: () => void;
  /** Files that open without the computer (asleep view only); null or empty hides the block. */
  kept?: Entry[] | null;
  onOpenKept?: (e: Entry) => void;
};

/**
 * A computer that is not answering. Asleep (mockup `asleep`): a grey monitor, "{name} is not running", Wake it and Try again,
 * then the files kept on this phone. Lost (mockup `lost`): no picture or heading, just a sentence saying since when,
 * one line of reassurance and two small buttons over a hairline.
 */
export function ComputerStateView({ kind, name, lastSeen = null, onWake, onRetry, onCheck, onPair, kept, onOpenKept }: Props) {
  const c = useScheme();
  if (kind === 'refused') {
    return (
      <View style={{ paddingTop: Spacing.md2, paddingHorizontal: Spacing.md, paddingBottom: 14, borderBottomWidth: 1, borderBottomColor: c.rule }} accessibilityRole="alert">
        <Text style={LumenType.name}>{t('stateRefusedTitle')}</Text>
        <Text muted style={[LumenType.meta, { marginTop: 2 }]}>
          {t('stateRefusedBody')}
        </Text>
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: Spacing.sm, marginTop: Spacing.md2 }}>
          {onPair ? <Button label={t('pairAgain')} kind="tonal" size="s" onPress={onPair} /> : null}
          <Button label={t('giTryNow')} kind="text" size="s" onPress={onRetry} />
        </View>
      </View>
    );
  }
  if (kind === 'lost') {
    const since = lastSeen ? relativeLabel(lastSeen) : null;
    return (
      <View style={{ paddingTop: Spacing.md2, paddingHorizontal: Spacing.md, paddingBottom: 14, borderBottomWidth: 1, borderBottomColor: c.rule }}>
        <Text style={LumenType.name}>{since ? t('stateLostSince', { name, relative: since }) : t('connectStopped', { name })}</Text>
        <Text muted style={[LumenType.meta, { marginTop: 2 }]}>
          {t('giLostBody')}
        </Text>
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: Spacing.sm, marginTop: Spacing.md2 }}>
          <Button label={t('giTryNow')} kind="outlined" size="s" onPress={onRetry} renderIcon={(k) => <RefreshCw size={18} color={k} />} />
          {onCheck ? <Button label={t('giCheckConnection')} kind="text" size="s" onPress={onCheck} /> : null}
        </View>
      </View>
    );
  }
  return (
    <View>
      <StatusBlock
        top={28}
        emoji="monitor"
        off
        title={t('giAsleepTitle', { name })}
        hint={t('giAsleepBody')}
        actions={[
          ...(onWake ? [{ label: t('giWake'), onPress: onWake, icon: (k: string) => <Power size={18} color={k} /> }] : []),
          { label: t('giTryAgain'), onPress: onRetry },
        ]}
      />
      {kept && kept.length > 0 ? (
        <View>
          <Text muted accessibilityRole="header" style={[LumenType.sectionLabel, { paddingTop: Spacing.lg, paddingBottom: 6, paddingHorizontal: Spacing.md }]}>
            {t('keptFromComputer', { computer: name })}
          </Text>
          {kept.map((e) => (
            <Row
              key={e.path}
              lead={<Emoji name={emojiNameFor(e)} size={32} />}
              title={e.name}
              subtitle={t('keptOpensOffline')}
              trailing={
                e.size != null ? (
                  <Text muted style={{ fontFamily: FontFamily.mono, fontSize: 13, lineHeight: 20 }}>
                    {formatSize(e.size)}
                  </Text>
                ) : undefined
              }
              onPress={onOpenKept ? () => onOpenKept(e) : undefined}
            />
          ))}
        </View>
      ) : null}
    </View>
  );
}
