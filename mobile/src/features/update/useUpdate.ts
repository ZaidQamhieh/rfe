import { useEffect, useState } from 'react';

import { networkTransports } from '../../core/native';
import { checkForUpdate, downloadUpdate, isApkReady } from './updateService';
import type { AppRelease } from './updateLogic';

let session: Promise<AppRelease | null> | undefined;

/**
 * The once-per-launch update check. Any failure counts as "no update", so a failed check never nags. When a release is
 * found and the phone is on Wi-Fi or ethernet, its APK is fetched quietly so tapping Update is instant.
 */
export function sessionUpdateCheck(): Promise<AppRelease | null> {
  session ??= checkForUpdate()
    .then(async (release) => {
      if (release && !(await isApkReady(release))) {
        const nets = await networkTransports().catch(() => [] as string[]);
        if (nets.includes('wifi') || nets.includes('ethernet')) void downloadUpdate(release).catch(() => {});
      }
      return release;
    })
    .catch(() => null);
  return session;
}

/** A fresh check on demand (the Check for updates row); throws when the check fails. */
export function recheckUpdate(): Promise<AppRelease | null> {
  const check = checkForUpdate();
  session = check.catch(() => null); // the background users of the session never see a failure
  return check;
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
