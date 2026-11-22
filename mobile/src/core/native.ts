// Adapters from the local Expo module (modules/rfe-transport) to core interfaces.
import { getRandomBytes } from 'expo-crypto';
import Rfe from '../../modules/rfe-transport/src/RfeTransportModule';
import type { Transport } from './api/agentClient';
import { normalizeDiscovered, parseDiscovered, type DiscoveredAgent } from './discovery';
import type { SecureStore } from './security/secureStore';

export const nativeTransport: Transport = {
  async request({ url, method, headers, bodyText, pin, timeoutMs }) {
    const r = await Rfe.request(url, method, headers, bodyText, pin, timeoutMs);
    return { status: r.status, headers: r.headers, bodyText: r.bodyText };
  },
};

export const nativeSecureStore: SecureStore = {
  read: (key) => Rfe.secureRead(key),
  write: (key, value) => Rfe.secureWrite(key, value),
  delete: (key) => Rfe.secureDelete(key),
};

export const readLegacyPrefs = () => Rfe.legacyPrefsReadAll();
export const probeFingerprint = (url: string, timeoutMs?: number) => Rfe.probeFingerprint(url, timeoutMs);
type TransferState = 'QUEUED' | 'RUNNING' | 'PAUSED' | 'DONE' | 'FAILED' | 'CANCELLED';
export type TransferRecord = {
  id: string;
  hostId: string;
  address: string;
  remotePath: string;
  destPath: string;
  state: TransferState;
  received: number;
  total: number;
  error: string | null;
  /** Absent in journals written before uploads existed, which are downloads. */
  direction?: 'DOWNLOAD' | 'UPLOAD';
  /** Uploads: `remotePath` is the target on the host and `destPath` the local source file. */
  sessionId?: string | null;
  sha256?: string | null;
  overwrite?: boolean;
  /** Downloads: MediaStore URI of the copy in the shared Downloads folder, when it was published. */
  publicUri?: string | null;
  /** Epoch millis of the last journal write; for a finished transfer, when it finished. Absent or 0 in older journals. */
  updatedAt?: number;
  /** Epoch millis when it was queued; absent or 0 in older journals. */
  enqueuedAt?: number;
  /** Downloads: the name the published copy was saved under (a duplicate gets "(1)"). */
  savedName?: string | null;
};

const parseRecord = (s: string) => JSON.parse(s) as TransferRecord;

/** Durable native downloads (foreground service, journal, Range resume). */
export const transfers = {
  enqueue: async (id: string, hostId: string, address: string, remotePath: string, destPath: string) =>
    parseRecord(await Rfe.transferEnqueue(id, hostId, address, remotePath, destPath)),
  enqueueUpload: async (id: string, hostId: string, address: string, localPath: string, remotePath: string, overwrite: boolean, deleteSource: boolean) =>
    parseRecord(await Rfe.transferEnqueueUpload(id, hostId, address, localPath, remotePath, overwrite, deleteSource)),
  resume: (id: string) => Rfe.transferResume(id),
  pause: (id: string) => Rfe.transferPause(id),
  cancel: (id: string) => Rfe.transferCancel(id),
  remove: (id: string) => Rfe.transferRemove(id),
  list: async () => (await Rfe.transfersList()).map(parseRecord),
  subscribe(cb: (r: TransferRecord) => void) {
    const sub = Rfe.addListener('onTransferUpdate', (e) => cb(parseRecord(e.record)));
    return () => sub.remove();
  },
};

/** Cryptographically secure random bytes from the OS CSPRNG (expo-crypto); never Math.random. */
export function secureRandomBytes(n: number): Uint8Array {
  return getRandomBytes(n);
}

/** Encrypted offline copies of files in pinned folders. */
export const offlineBodies = {
  put: (hostId: string, path: string, srcPath: string) => Rfe.offlineBodyPut(hostId, path, srcPath),
  restore: (hostId: string, path: string, destPath: string) => Rfe.offlineBodyRestore(hostId, path, destPath),
  has: (hostId: string, path: string) => Rfe.offlineBodyHas(hostId, path),
  totalBytes: () => Rfe.offlineBodyTotalBytes(),
  remove: (hostId: string, path: string) => Rfe.offlineBodyRemove(hostId, path),
  evictHost: (hostId: string) => Rfe.offlineBodyEvictHost(hostId),
};
export const openPublicUri = (uri: string, mime: string) => Rfe.openPublicUri(uri, mime);
export const openFileExternal = (path: string, mime: string) => Rfe.openFileExternal(path, mime);
export const shareFileExternal = (path: string, mime: string) => Rfe.shareFileExternal(path, mime);
export const appBuild = () => Rfe.appBuild();
export const publicDownload = (id: string, url: string, destPath: string, offset: number) => Rfe.publicDownload(id, url, destPath, offset);
export const sha256File = (path: string) => Rfe.sha256File(path);
export const canInstallPackages = () => Rfe.canInstallPackages();
export const openInstallSettings = () => Rfe.openInstallSettings();
export const installApk = (path: string) => Rfe.installApk(path);
/** Gives the home-screen storage widget its latest numbers. */
export const widgetSet = (title: string, detail: string, percent: number) => Rfe.widgetSet(title, detail, percent);
export const networkTransports = () => Rfe.networkTransports();
export const deviceIdNative = async () => (await Rfe.deviceId()) ?? null;

/** Searches the Wi-Fi for computers; [onFound] is called with each one as it is resolved, and the full list is returned when the search ends. */
export const scanLan = async (onFound?: (agent: DiscoveredAgent) => void) => {
  const sub = Rfe.addListener('onLanAgentFound', (e) => {
    const a = parseDiscovered(e);
    if (a) onFound?.(a);
  });
  try {
    return normalizeDiscovered(await Rfe.discoveryScan());
  } finally {
    sub.remove();
  }
};
export const stopLanScan = () => Rfe.discoveryStop();

export const sendWakeOnLan = (mac: string) => Rfe.sendWakeOnLan(mac);

export const fetchToFileNative = (id: string, url: string, headers: Record<string, string>, pin: string | null, destPath: string, timeoutMs?: number, maxBytes?: number) =>
  Rfe.fetchToFile(id, url, headers, pin, destPath, timeoutMs, maxBytes);
export const eventStreamNative = {
  run: (id: string, url: string, headers: Record<string, string>, pin: string | null) => Rfe.eventStreamRun(id, url, headers, pin),
  stop: (id: string) => Rfe.eventStreamStop(id),
  onEvent: (cb: (id: string, kind: string) => void) => Rfe.addListener('onAgentEvent', (e) => cb(e.id, e.kind)),
};
export const fetchCancelNative = (id: string) => Rfe.fetchCancel(id);
export const mediaProxy = {
  start: (id: string, url: string, headers: Record<string, string>, pin: string | null) => Rfe.mediaProxyStart(id, url, headers, pin),
  stop: (id: string) => Rfe.mediaProxyStop(id),
};
export const pdf = {
  pageCount: (path: string) => Rfe.pdfPageCount(path),
  renderPage: (path: string, index: number, widthPx: number, outPath: string) => Rfe.pdfRenderPage(path, index, widthPx, outPath),
  close: () => Rfe.pdfClose(),
};
