import { queryOptions } from '@tanstack/react-query';
import { api, type Device } from './api';

/** How often the pages that poll refetch, in milliseconds. */
export const REFRESH_INTERVAL_MS = 10000;
/** Refetch intervals of the metrics, devices and logs queries, in milliseconds. */
export const METRICS_REFRESH_MS = 5000;
export const DEVICES_REFRESH_MS = 15000;
export const LOGS_REFRESH_MS = 8000;

/** A device counts as online when it was seen within this many seconds. */
const ONLINE_WINDOW_SECONDS = 300;

export function isOnline(d: Device): boolean {
  return Date.now() / 1000 - d.lastSeen < ONLINE_WINDOW_SECONDS;
}

/** The agent status query. Each caller sets how often it polls. */
export const statusQuery = (refetchInterval: number) =>
  queryOptions({ queryKey: ['status'], queryFn: api.status, refetchInterval });
