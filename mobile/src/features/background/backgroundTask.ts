import * as BackgroundTask from 'expo-background-task';
import * as TaskManager from 'expo-task-manager';

import { transfers } from '../../core/native';
import { assetIdFromTransferId } from '../photoBackup/photoBackupLogic';
import { photoBackupStore, runPhotoBackup, watchPhotoBackup } from '../photoBackup/photoBackupService';
import { sessionUpdateCheck } from '../update/useUpdate';
import { runBackgroundWork } from './backgroundWork';

export const BACKGROUND_TASK = 'rfe-background';

/** Roughly how often the system may run the job (WorkManager treats it as a minimum; six hours is the Flutter update-check cadence). */
const INTERVAL_MINUTES = 6 * 60;

const photoUploadsActive = async () => (await transfers.list()).some((r) => r.direction === 'UPLOAD' && assetIdFromTransferId(r.id) !== null && (r.state === 'RUNNING' || r.state === 'QUEUED'));

async function waitForPhotoUploads(budgetMs: number): Promise<void> {
  const end = Date.now() + budgetMs;
  while (Date.now() < end && (await photoUploadsActive())) await new Promise((r) => setTimeout(r, 2000));
}

// Defined at module load, which the app's entry file (index.js) imports before the router: the headless run that
// WorkManager starts evaluates only the entry, so the task must be defined there.
TaskManager.defineTask(BACKGROUND_TASK, async () => {
  // Finished uploads are recorded as backed up by this watcher, which normally lives in the root layout.
  const stopWatching = watchPhotoBackup();
  try {
    const report = await runBackgroundWork({
      photoBackupScheduled: async () => {
        const p = await photoBackupStore.load();
        return p.enabled && p.scheduled;
      },
      runPhotoBackup: () => runPhotoBackup({ interactive: false }),
      waitForPhotoUploads,
      updateCheck: async () => void (await sessionUpdateCheck()),
    });
    // eslint-disable-next-line no-console -- the only trace of a run with no screen
    console.log('[rfe-background]', JSON.stringify(report));
    // Let the last completion event reach the watcher before it is removed.
    await new Promise((r) => setTimeout(r, 500));
    return BackgroundTask.BackgroundTaskResult.Success;
  } catch (e) {
    // eslint-disable-next-line no-console -- see above
    console.log('[rfe-background] failed', String(e));
    return BackgroundTask.BackgroundTaskResult.Failed;
  } finally {
    stopWatching();
  }
});

/** Registers the periodic job (idempotent; the system keeps it across restarts). Best effort: an OEM quirk must not block startup. */
export async function ensureBackgroundTask(): Promise<void> {
  try {
    if ((await BackgroundTask.getStatusAsync()) !== BackgroundTask.BackgroundTaskStatus.Available) return;
    if (!(await TaskManager.isTaskRegisteredAsync(BACKGROUND_TASK))) await BackgroundTask.registerTaskAsync(BACKGROUND_TASK, { minimumInterval: INTERVAL_MINUTES });
  } catch {
    // Not fatal: the once-per-launch update check and Back up now still work.
  }
}
