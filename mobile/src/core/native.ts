// Adapters from the local Expo module (modules/rfe-transport) to core interfaces.
import { getRandomBytes } from 'expo-crypto';
import Rfe from '../../modules/rfe-transport/src/RfeTransportModule';
import type { Transport } from './api/agentClient';
import { normalizeDiscovered } from './discovery';
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
export type TransferState = 'QUEUED' | 'RUNNING' | 'PAUSED' | 'DONE' | 'FAILED' | 'CANCELLED';
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

export const downloadToFile = Rfe.downloadToFile.bind(Rfe);

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
export const backupEncrypt = (payloadJson: string, passphrase: string) => Rfe.backupEncrypt(payloadJson, passphrase);
export const backupDecrypt = (envelopeJson: string, passphrase: string) => Rfe.backupDecrypt(envelopeJson, passphrase);
export const secureReadAll = () => Rfe.secureReadAll();
export const appBuild = () => Rfe.appBuild();
export const publicDownload = (id: string, url: string, destPath: string, offset: number) => Rfe.publicDownload(id, url, destPath, offset);
export const publicDownloadCancel = (id: string) => Rfe.publicDownloadCancel(id);
export const sha256File = (path: string) => Rfe.sha256File(path);
export const canInstallPackages = () => Rfe.canInstallPackages();
export const openInstallSettings = () => Rfe.openInstallSettings();
export const installApk = (path: string) => Rfe.installApk(path);
export const networkTransports = () => Rfe.networkTransports();
export const deviceIdNative = async () => (await Rfe.deviceId()) ?? null;

export const scanLan = async () => normalizeDiscovered(await Rfe.discoveryScan());
export const stopLanScan = () => Rfe.discoveryStop();

export const sendWakeOnLan = (mac: string) => Rfe.sendWakeOnLan(mac);

export const fetchToFileNative = (id: string, url: string, headers: Record<string, string>, pin: string | null, destPath: string, timeoutMs?: number, maxBytes?: number) =>
  Rfe.fetchToFile(id, url, headers, pin, destPath, timeoutMs, maxBytes);
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
