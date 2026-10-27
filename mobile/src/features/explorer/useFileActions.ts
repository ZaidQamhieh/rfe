import * as DocumentPicker from 'expo-document-picker';

import type { Entry , BatchResult } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { useDialogs, useToast } from '../../design/components';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { humanizeError } from '../pairing/pairingService';
import { enqueueUploads } from '../transfers/enqueueUploads';
import { cleanUploadName, planUploads, type Resolution } from '../transfers/uploadPlan';
import { formatSize } from '../../core/format';
import { clientForHost } from '../../services';
import { useFileClipboard } from './clipboard';
import { uploadSpaceShortfall } from './freeSpace';
import { useConflictPrompt } from './useConflictPrompt';
import { currentPath, type Explorer, type ExplorerState } from './explorerStore';
import { basenameOf, folderLabel, renameDestination } from './paths';
import { deleteSubtitle } from './selectionLogic';
import { compareVerdict, sizeVerdict, type Verdict } from './compareFiles';
import { moveUndo, renameUndo, type UndoOp } from './undoPlan';
import { destinationProblem, pasteSources, transferFullySucceeded } from './pasteLogic';

/** Ports the explorer screen's copy/cut/paste/delete/compress flows, keeping their dialogs, messages and precedence rules. */
export function useFileActions(host: Host, ex: Explorer & { getState(): ExplorerState }) {
  const dialogs = useDialogs();
  const toast = useToast();
  const c = useScheme();
  const askConflict = useConflictPrompt();
  const clip = useFileClipboard();

  /** Runs an undo from the toast and says how it went. */
  async function undo(op: UndoOp) {
    try {
      await ex.undo(op);
      toast.success(t('undoDone'));
    } catch (e) {
      toast.error(t('undoFailed', { error: humanizeError(e) }));
    }
  }

  async function reportBatch(res: BatchResult, verb: string, undoOp?: UndoOp | null) {
    if (res.failed.length === 0) {
      toast.success(t('batchSuccessNItems', { verb, count: res.results.length }), undoOp ? () => void undo(undoOp) : undefined);
      return;
    }
    await dialogs.report({
      title: t('batchResultWithErrors', { verb, errorCount: res.failed.length }),
      items: res.failed.map((f) => ({ primary: f.path, secondary: f.errorMessage ?? f.errorCode ?? 'failed' })),
    });
  }

  const copySelection = () => {
    const paths = [...ex.getState().selected];
    if (paths.length === 0) return;
    clip.copy(paths, host.id);
    ex.clearSelection();
    toast.success(t('clipboardCopiedHint', { count: paths.length }));
  };

  const cutSelection = () => {
    const paths = [...ex.getState().selected];
    if (paths.length === 0) return;
    clip.cut(paths, host.id);
    ex.clearSelection();
    toast.success(t('clipboardCutHint', { count: paths.length }));
  };

  /**
   * Moves or copies [paths] into [dest] with a pre-flight collision check. Resolves true once the host was asked,
   * false when the user cancelled, nothing was left to do or a step failed.
   */
  async function transferInto(dest: string, paths: readonly string[], isCut: boolean, retry: () => void): Promise<boolean> {
    if (destinationProblem(paths, dest)) {
      toast.error(t('cannotPasteIntoItself'));
      return false;
    }
    let sources = pasteSources(paths, dest, isCut);
    if (sources.length === 0) {
      toast.info(t('alreadyInThisFolder'));
      return false;
    }
    let duplicate = false;
    let overwrite = false;
    try {
      const colliding = await ex.collidingBasenames(dest, sources);
      if (colliding.size > 0) {
        const res = await askConflict(colliding.size, sources.length, dest);
        if (res === 'cancel') return false;
        if (res === 'keepBoth') duplicate = true;
        if (res === 'overwrite') overwrite = true;
        if (res === 'skip') {
          sources = sources.filter((p) => !colliding.has(basenameOf(p)));
          if (sources.length === 0) {
            toast.info(t('clipboardAllExistNothing', { folder: folderLabel(dest), operation: isCut ? t('moveLabel') : t('copyLabel') }));
            return false;
          }
        }
      }
    } catch (e) {
      toast.error(t('couldNotCheckFolder', { folder: folderLabel(dest), error: humanizeError(e) }), retry);
      return false;
    }
    try {
      const res = isCut ? await ex.moveSelected(dest, { sources, duplicate, overwrite }) : await ex.copySelected(dest, { sources, duplicate, overwrite });
      // A plain move can be put back; keep-both and overwrite changed names or removed files, so they cannot.
      await reportBatch(res, isCut ? t('movedLabel') : t('copiedLabel'), isCut && !duplicate && !overwrite ? moveUndo(sources, dest, res.results) : null);
      // A partial failure keeps the clipboard (and selection) so the failed items can be pasted again.
      return transferFullySucceeded(res);
    } catch (e) {
      toast.error(t('operationFailed', { operation: isCut ? t('moveLabel') : t('copyLabel'), error: humanizeError(e) }), retry);
      return false;
    }
  }

  /** Pastes the in-app clipboard into the current folder. */
  async function paste() {
    const clipboard = useFileClipboard.getState().clip;
    if (!clipboard) return;
    const isCut = clipboard.mode === 'cut';
    const done = await transferInto(currentPath(ex.getState()), clipboard.paths, isCut, () => void paste());
    if (done && isCut) clip.clear();
  }

  /** "Move to…" / "Copy to…": the selection goes to a folder picked in the destination sheet. */
  async function transferSelectionTo(dest: string, isCut: boolean) {
    const paths = [...ex.getState().selected];
    if (paths.length === 0) return;
    const done = await transferInto(dest, paths, isCut, () => void transferSelectionTo(dest, isCut));
    if (done) ex.clearSelection();
  }

  /** Copies the selection next to the originals, each under a free name. */
  async function duplicateSelection() {
    if (ex.getState().selected.size === 0) return;
    try {
      const res = await ex.duplicateSelected();
      ex.clearSelection();
      await reportBatch(res, t('duplicatedLabel'));
    } catch (e) {
      toast.error(t('duplicateFailed', { error: humanizeError(e) }));
    }
  }

  /** With two files selected, says whether their contents are identical (sizes first, then SHA-256 from the host). */
  async function compareSelection() {
    const picked = new Set(ex.getState().selected);
    const files = ex.getState().entries.filter((e) => picked.has(e.path));
    if (files.length !== 2 || files.some((e) => e.isDir)) return;
    const [a, b] = files;
    let verdict: Verdict = sizeVerdict(a, b) ?? 'unknown';
    if (verdict === 'unknown') {
      toast.info(t('comparing'));
      try {
        verdict = compareVerdict(await (await clientForHost(host)).batchChecksums([a.path, b.path]), a.path, b.path);
      } catch (e) {
        toast.error(t('compareFailed', { error: humanizeError(e) }));
        return;
      }
    }
    await dialogs.report({
      title: verdict === 'same' ? t('compareSame') : verdict === 'different' ? t('compareDifferent') : t('compareUnknown'),
      items: [a, b].map((e) => ({ primary: e.name, secondary: e.size != null ? formatSize(e.size) : undefined })),
    });
  }

  /** Warns when the picked files will not fit on the host's drive; the user can still go ahead. A failed lookup never blocks. */
  async function roomForUpload(dest: string, bytes: number): Promise<boolean> {
    let short: { needed: number; free: number } | null = null;
    try {
      short = uploadSpaceShortfall(await (await clientForHost(host)).drives(), dest, bytes);
    } catch {
      return true;
    }
    if (!short) return true;
    return dialogs.confirm({ title: t('notEnoughSpaceTitle'), description: t('notEnoughSpaceBody', { needed: formatSize(short.needed), free: formatSize(short.free) }), confirmLabel: t('uploadAnywayButton'), cancelLabel: t('cancelButton') });
  }

  /** Picks files on the phone and queues them for upload into the current folder, asking first about name clashes. */
  async function upload() {
    const dest = currentPath(ex.getState());
    let picked: DocumentPicker.DocumentPickerResult;
    try {
      picked = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
    } catch (e) {
      toast.error(t('operationFailed', { operation: t('uploadFileTooltip'), error: humanizeError(e) }));
      return;
    }
    if (picked.canceled || picked.assets.length === 0) return;
    const files = picked.assets.map((a) => ({ name: a.name, uri: a.uri, size: a.size }));
    let resolution: Resolution = 'skip';
    try {
      const colliding = await ex.collidingBasenames(dest, files.map((f) => cleanUploadName(f.name)));
      if (colliding.size > 0) {
        const res = await askConflict(colliding.size, files.length, dest);
        if (res === 'cancel') return;
        resolution = res;
      }
      const items = planUploads(files, colliding, resolution);
      if (items.length === 0) {
        toast.info(t('uploadAllExist', { folder: folderLabel(dest) }));
        return;
      }
      if (!(await roomForUpload(dest, items.reduce((n, i) => n + (i.source.size ?? 0), 0)))) return;
      const queued = await enqueueUploads(host, dest, items);
      toast.success(items.length === 1 ? t('uploadingFile', { name: items[0].targetName }) : t('uploadingNFiles', { count: queued }));
    } catch (e) {
      toast.error(t('operationFailed', { operation: t('uploadFileTooltip'), error: humanizeError(e) }), () => void upload());
    }
  }

  /** Delete: three-way dialog (cancel / delete forever / move to trash). */
  async function confirmDelete() {
    const picked = [...ex.getState().selected];
    const count = picked.length;
    if (count === 0) return;
    const choice = await dialogs.choose<'forever' | 'trash'>({
      title: t('deleteTitle'),
      subtitle: deleteSubtitle({ count, hostLabel: host.label, name: count === 1 ? basenameOf(picked[0]) : undefined }),
      tint: c.error,
      options: [
        { value: 'trash', label: t('moveToTrashButton') },
        { value: 'forever', label: t('deleteForeverButton'), tint: c.error },
      ],
    });
    if (choice === null) return;
    const permanent = choice === 'forever';
    try {
      const res = await ex.deleteSelected({ permanent });
      await reportBatch(res, permanent ? t('deletedLabel') : t('movedToTrashLabel'));
    } catch (e) {
      toast.error(t('deleteFailed', { error: humanizeError(e) }));
    }
  }

  async function compressSelected() {
    const paths = [...ex.getState().selected];
    if (paths.length === 0) return;
    const dir = currentPath(ex.getState());
    const sep = dir.includes('\\') ? '\\' : '/';
    const stem = paths.length === 1 ? basenameOf(paths[0]) : folderLabel(dir) === 'Root' ? 'Archive' : folderLabel(dir);
    const dest = dir.endsWith(sep) ? `${dir}${stem}.zip` : `${dir}${sep}${stem}.zip`;
    try {
      const entry = await ex.compressSelected(dest, paths);
      ex.clearSelection();
      toast.success(t('compressedTo', { name: entry.name }));
    } catch (e) {
      toast.error(t('compressFailed', { error: humanizeError(e) }));
    }
  }

  async function createNamed(isFolder: boolean) {
    const title = isFolder ? t('newFolderButton') : t('newFileButton');
    const name = await dialogs.prompt({ title, placeholder: t('nameHint'), confirmLabel: t('createButton') });
    if (!name) return;
    try {
      if (isFolder) await ex.createFolder(name, { open: true });
      else await ex.createFile(name);
      toast.success(t('createdName', { name }));
    } catch (e) {
      toast.error(t('createFailed', { name, error: humanizeError(e) }));
    }
  }

  async function rename(entry: Entry) {
    const name = await dialogs.prompt({ title: 'Rename', initialValue: entry.name, confirmLabel: 'Rename' });
    if (!name || name === entry.name) return;
    try {
      await ex.rename(entry.path, name);
      const op = renameUndo(entry.path, renameDestination(entry.path, name));
      toast.success(t('renamedLabel'), op ? () => void undo(op) : undefined);
    } catch (e) {
      toast.error(t('renameFailed', { error: humanizeError(e) }));
    }
  }

  /** Applies [newNames] (aligned with [paths]) through the store's two-phase rename, then reports per-item failures. */
  async function applyBatchRename(paths: string[], newNames: string[]) {
    try {
      const res = await ex.batchRename(paths.map((path, i) => ({ path, newName: newNames[i] })));
      ex.clearSelection();
      await reportBatch(res, t('renamedLabel'));
    } catch (e) {
      toast.error(t('renameFailed', { error: humanizeError(e) }));
    }
  }

  return { copySelection, cutSelection, paste, transferSelectionTo, duplicateSelection, compareSelection, confirmDelete, compressSelected, createNamed, rename, applyBatchRename, upload, askConflict, reportBatch };
}
