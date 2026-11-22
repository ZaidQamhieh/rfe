import { useEffect, useState } from 'react';

import { ignoreError } from '../../core/log';
import { networkTransports } from '../../core/native';
import { checkForUpdate, downloadUpdate, isApkReady } from './updateService';
import type { AppRelease } from './updateLogic';

const UPDATE_CHECK_LABEL = 'update check';

let session: Promise<AppRelease | null> | undefined;

/**
 * The once-per-launch update check. Any failure counts as "no update", so a failed check never nags; the cause (an unsigned manifest, no network) is logged. When a release is
 * found and the phone is on Wi-Fi or ethernet, its APK is fetched quietly so tapping Update is instant.
 */
export function sessionUpdateCheck(): Promise<AppRelease | null> {
  session ??= checkForUpdate()
    .then(async (release) => {
      if (release && !(await isApkReady(release))) {
        const nets = await networkTransports().catch(() => [] as string[]);
        if (nets.includes('wifi') || nets.includes('ethernet')) void downloadUpdate(release).catch(ignoreError('update download'));
      }
      return release;
    })
    .catch((e: unknown) => {
      ignoreError(UPDATE_CHECK_LABEL)(e);
      return null;
    });
  return session;
}

export function useSessionUpdate(): AppRelease | null {
  const [release, setRelease] = useState<AppRelease | null>(null);
  useEffect(() => {
    let live = true;
    void sessionUpdateCheck().then((r) => live && setRelease(r));
    return () => {
      live = false;
    };
  }, []);
  return release;
}
