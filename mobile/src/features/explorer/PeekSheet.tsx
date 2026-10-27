import { View } from 'react-native';

import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { BottomSheet, SheetHead } from '../../design/components';
import { PreviewPage } from '../preview/viewers/PreviewPage';

const noop = () => {};

/** Quick look: the file's viewer in a bottom sheet without leaving the folder. Opened by long-pressing a file's icon or thumbnail. */
export function PeekSheet({ host, entry, onClose }: { host: Host; entry: Entry; onClose: () => void }) {
  return (
    <BottomSheet visible onClose={onClose}>
      <SheetHead title={entry.name} />
      <View style={{ height: 420, overflow: 'hidden' }}>
        <PreviewPage host={host} entry={entry} isCurrent lineNumbers={false} rawMarkdown={false} follow={false} onZoomChange={noop} onText={noop} />
      </View>
    </BottomSheet>
  );
}
