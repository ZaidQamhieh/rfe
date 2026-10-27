import { FilePen } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { BottomSheet, Button, Segmented, SheetHero, Text, TextField } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';
import { Radii, Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { computeBatchRenames, type BatchRenameMode } from './batchRename';

const PREVIEW_ROWS = 3;

/** Batch rename for the selection: pattern numbering or find/replace, with a live preview of the first names. `onApply` gets the new basenames in input order. */
export function BatchRenameSheet({ visible, names, onClose, onApply }: { visible: boolean; names: string[]; onClose: () => void; onApply: (newNames: string[]) => void }) {
  const c = useScheme();
  const [mode, setMode] = useState<BatchRenameMode>('pattern');
  const [base, setBase] = useState('File');
  const [start, setStart] = useState('1');
  const [find, setFind] = useState('');
  const [replace, setReplace] = useState('');

  const startNumber = Number.parseInt(start.trim(), 10);
  const preview = computeBatchRenames({ names, mode, base: base.trim(), startNumber: Number.isNaN(startNumber) ? 1 : startNumber, find, replace });
  const shown = Math.min(PREVIEW_ROWS, names.length);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <ScrollView keyboardShouldPersistTaps="handled">
        <SheetHero badge={<FilePen size={24} color={c.primary} />} title={t('renameNItemsTitle', { count: names.length })} subtitle={mode === 'pattern' ? t('patternLabel') : t('findAndReplaceLabel')} onClose={onClose} />
        <View style={{ padding: Spacing.lg, paddingTop: 0, gap: Spacing.md }}>
          <Segmented options={[t('findAndReplaceLabel'), t('patternLabel')]} selectedIndex={mode === 'findReplace' ? 0 : 1} onChange={(i) => setMode(i === 0 ? 'findReplace' : 'pattern')} />
          {mode === 'pattern' ? (
            <>
              <TextField label={t('baseNameLabel')} value={base} onChangeText={setBase} helper={t('baseNameHelperText')} autoCorrect={false} />
              <TextField label={t('startNumberLabel')} value={start} onChangeText={setStart} keyboardType="number-pad" />
            </>
          ) : (
            <>
              <TextField label={t('findLabel')} value={find} onChangeText={setFind} autoCapitalize="none" autoCorrect={false} />
              <TextField label={t('replaceWithLabel')} value={replace} onChangeText={setReplace} autoCapitalize="none" autoCorrect={false} />
            </>
          )}
          <View style={{ backgroundColor: c.surfaceContainerHigh, borderRadius: Radii.card, padding: Spacing.md, gap: 2 }} accessibilityLabel="Preview">
            {names.slice(0, shown).map((n, i) => (
              <Text key={i} style={LumenType.meta} numberOfLines={1}>{`${n}  →  ${preview[i]}`}</Text>
            ))}
            {names.length > shown && <Text style={LumenType.meta} muted>{t('andNMore', { count: names.length - shown })}</Text>}
          </View>
          <Button size="lg" label={t('renameNItems', { count: names.length })} onPress={() => onApply(preview)} />
        </View>
      </ScrollView>
    </BottomSheet>
  );
}
