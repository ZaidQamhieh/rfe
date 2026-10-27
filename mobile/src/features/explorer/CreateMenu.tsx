import { ClipboardPaste, FilePlus, FolderPlus, Upload } from 'lucide-react-native';
import { View } from 'react-native';

import { ActionListCard, ActionListTile, BottomSheet, IconTile, SheetHead } from '../../design/components';
import { useRoles } from '../../design/theme';
import { t } from '../../i18n';

/** "New" sheet: new folder / new file / upload / paste (paste only when the clipboard belongs to this host); each row is gated by the caller. */
export function CreateMenu({ visible, onClose, onNewFolder, onNewFile, onUpload, onPaste, pasteLabel, canModify = true }: { visible: boolean; onClose: () => void; onNewFolder: () => void; onNewFile: () => void; onUpload?: () => void; onPaste?: () => void; pasteLabel?: string; canModify?: boolean }) {
  const roles = useRoles();
  const go = (fn?: () => void) => () => {
    onClose();
    fn?.();
  };
  const rows = [
    canModify && <ActionListTile key="folder" icon={<IconTile icon={FolderPlus} color={roles.folder} size={38} />} label={t('newFolderButton')} onPress={go(onNewFolder)} />,
    canModify && <ActionListTile key="file" icon={<IconTile icon={FilePlus} color={roles.doc} size={38} />} label={t('newFileButton')} onPress={go(onNewFile)} />,
    onUpload && <ActionListTile key="upload" icon={<IconTile icon={Upload} color={roles.transfer} size={38} />} label={t('uploadFileTooltip')} onPress={go(onUpload)} />,
    onPaste && pasteLabel && <ActionListTile key="paste" icon={<IconTile icon={ClipboardPaste} color={roles.route} size={38} />} label={pasteLabel} onPress={go(onPaste)} />,
  ].filter(Boolean) as React.ReactElement[];
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <SheetHead title={t('newButton')} />
      <View style={{ paddingHorizontal: 18, paddingBottom: 18 }}>
        <ActionListCard>{rows}</ActionListCard>
      </View>
    </BottomSheet>
  );
}
