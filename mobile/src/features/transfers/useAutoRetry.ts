import { useEffect, useState } from 'react';
import { AppState } from 'react-native';

import { transfers } from '../../core/native';
import { useTransfers } from '../hosts/useTransfers';
import { AUTO_RETRY_DELAYS_MS, AUTO_RETRY_TICK_MS, resetAutoRetry, resumeQuietly, scheduleAutoRetries, type AutoRetryTracks } from './autoRetry';

// One store for the whole app: the attempts used survive screen changes, and only this hook resumes transfers on its own.
let tracks: AutoRetryTracks = new Map();

/** The owner tapped Retry on [id]: its automatic attempts start again. */
export function restartAutoRetry(id: string): void {
  tracks = resetAutoRetry(tracks, [id]);
}

/** True while [id] still has automatic attempts left (a transfer not seen yet has all of them). */
export const hasAutoRetryLeft = (id: string): boolean => (tracks.get(id)?.attempts ?? 0) < AUTO_RETRY_DELAYS_MS.length;

/** Resumes dropped transfers on their own, a bounded number of times (see autoRetry.ts), whichever screen is open and while the app is in the foreground. Mount once, at the root. */
export function useAutoRetry(): void {
  const items = useTransfers();
  const [foreground, setForeground] = useState(AppState.currentState === 'active');
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => setForeground(s === 'active'));
    return () => sub.remove();
  }, []);
  useEffect(() => {
    if (!foreground || items === null) return;
    const check = () => {
      const step = scheduleAutoRetries(items, tracks, Date.now(), true);
      tracks = step.tracks;
      void resumeQuietly(step.resume, (id) => transfers.resume(id));
    };
    check();
    const timer = setInterval(check, AUTO_RETRY_TICK_MS);
    return () => clearInterval(timer);
  }, [items, foreground]);
}
