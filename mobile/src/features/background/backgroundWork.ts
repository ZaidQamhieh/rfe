import type { BackupResult } from '../photoBackup/photoBackupService';

/** The scheduled job's pieces, injected so the order and failure handling can be tested without a device. */
export interface BackgroundDeps {
  photoBackupScheduled(): Promise<boolean>;
  runPhotoBackup(): Promise<BackupResult>;
  /** Resolves when the photo uploads queued by the run have finished, or after [budgetMs]. */
  waitForPhotoUploads(budgetMs: number): Promise<void>;
  /** Looks for a newer app build and quietly downloads it when the phone is on Wi-Fi. */
  updateCheck(): Promise<void>;
}

/** Time the worker may keep the process busy; WorkManager cuts a job off at ten minutes. */
export const UPLOAD_BUDGET_MS = 8 * 60 * 1000;

export type BackgroundReport = { photos: BackupResult | 'off' | 'error'; update: 'done' | 'error' };

/**
 * One scheduled pass: the automatic photo backup (only when it is switched on) and the update check. The two are
 * independent, so a failure in one never skips the other. Uploads are given time to finish before the job ends so the
 * system does not stop the process halfway through a photo.
 */
export async function runBackgroundWork(d: BackgroundDeps, budgetMs = UPLOAD_BUDGET_MS): Promise<BackgroundReport> {
  const report: BackgroundReport = { photos: 'off', update: 'done' };
  try {
    if (await d.photoBackupScheduled()) {
      report.photos = await d.runPhotoBackup();
      if (report.photos.kind === 'enqueued') await d.waitForPhotoUploads(budgetMs);
    }
  } catch {
    report.photos = 'error';
  }
  try {
    await d.updateCheck();
  } catch {
    report.update = 'error';
  }
  return report;
}
