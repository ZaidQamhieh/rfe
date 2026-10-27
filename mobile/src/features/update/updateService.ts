import { Directory, File, Paths } from 'expo-file-system';

import { canInstallPackages, installApk, openInstallSettings, publicDownload, publicDownloadCancel, sha256File, appBuild } from '../../core/native';
import { keyValue } from '../../services';
import { isUpdateAvailable, LATEST_MANIFEST_URL, LATEST_SIGNATURE_URL, manifestSignatureOk, parseRelease, percent, type AppRelease } from './updateLogic';

const DISMISSED_KEY = 'rfe_update_dismissed_code_v1';

/** Fetches the newest published release; null when it is not newer than this build. Throws when the check itself fails. */
export async function checkForUpdate(): Promise<AppRelease | null> {
  const res = await fetch(LATEST_MANIFEST_URL, { headers: { Accept: 'application/json' } });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const body = await res.text();
  const text = body.trim();
  if (text === '') return null;
  // The manifest is trusted only with the project's signature over its exact bytes (audit F-106).
  const sigRes = await fetch(LATEST_SIGNATURE_URL);
  if (!sigRes.ok || !manifestSignatureOk(body, await sigRes.text())) throw new Error('The update manifest is not signed by the project');
  const release = parseRelease(JSON.parse(text));
  return isUpdateAvailable(await appBuild(), release) ? release : null;
}

export async function dismissedCode(): Promise<number> {
  const raw = await keyValue.get(DISMISSED_KEY);
  const n = raw === null ? 0 : Number(raw);
  return Number.isFinite(n) ? n : 0;
}

/** Records a dismissal; it never lowers the stored value. */
export async function dismissUpdate(code: number): Promise<void> {
  if (code > (await dismissedCode())) await keyValue.set(DISMISSED_KEY, String(code));
}

const apkFile = (code: number) => {
  const dir = new Directory(Paths.cache, 'updates');
  dir.create({ idempotent: true, intermediates: true });
  return new File(dir, `update-${code}.apk`);
};
const nativePath = (f: File) => decodeURIComponent(f.uri.replace('file://', ''));

async function verified(release: AppRelease, file: File): Promise<boolean> {
  if (!file.exists || (release.size > 0 && file.size !== release.size)) return false;
  return (await sha256File(nativePath(file))).toLowerCase() === release.sha256;
}

/** True when a complete, verified copy of this release is already on disk. */
export async function isApkReady(release: AppRelease): Promise<boolean> {
  return release.size > 0 && (await verified(release, apkFile(release.versionCode)));
}

const active = new Map<number, Promise<void>>();

/**
 * Downloads (or resumes) the release's APK and verifies it. A second call for the same build joins the running one, so
 * the background pre-download and a tap on Update never write the same file twice. A failed check deletes the file.
 */
export function downloadUpdate(release: AppRelease, onProgress?: (pct: number) => void): Promise<void> {
  const running = active.get(release.versionCode);
  if (running) return running;
  const job = (async () => {
    const file = apkFile(release.versionCode);
    if (await verified(release, file)) return;
    const id = `update${release.versionCode}`;
    let offset = file.exists ? file.size : 0;
    if (release.size > 0 && offset > release.size) {
      file.delete();
      offset = 0;
    }
    const timer = onProgress ? setInterval(() => file.exists && onProgress(percent(file.size, release.size)), 300) : undefined;
    try {
      try {
        await publicDownload(id, release.url ?? '', nativePath(file), offset);
      } catch (e) {
        // The server ignored the resume: the partial file was removed, so start over from the beginning.
        if ((e as { code?: string }).code !== 'ERR_RANGE') throw e;
        await publicDownload(id, release.url ?? '', nativePath(file), 0);
      }
    } finally {
      if (timer) clearInterval(timer);
    }
    if (!(await verified(release, file))) {
      file.delete();
      throw new Error('The downloaded update failed verification.');
    }
    onProgress?.(100);
  })().finally(() => active.delete(release.versionCode));
  active.set(release.versionCode, job);
  return job;
}

export const cancelUpdateDownload = (release: AppRelease) => publicDownloadCancel(`update${release.versionCode}`);

export type InstallOutcome = 'started' | 'needsPermission' | 'failed';

/** Hands the verified APK to the system installer. Without the "install unknown apps" switch, opens its settings page instead. */
export async function installUpdate(release: AppRelease): Promise<InstallOutcome> {
  if (!(await canInstallPackages())) {
    await openInstallSettings();
    return 'needsPermission';
  }
  return (await installApk(nativePath(apkFile(release.versionCode)))) ? 'started' : 'failed';
}
