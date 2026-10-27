import { NativeModule, requireNativeModule } from 'expo';

import type { RfeResponse } from './RfeTransport.types';

type Events = { onTransferUpdate: (e: { record: string }) => void };

declare class RfeTransportModule extends NativeModule<Events> {
  /** Durable native downloads; each returns/emits JSON-encoded TransferRecord. */
  transferEnqueue(id: string, hostId: string, address: string, remotePath: string, destPath: string): Promise<string>;
  /** Durable chunked upload: [localPath] to [remotePath] on the host. `deleteSource` removes the local file when done or cancelled. */
  transferEnqueueUpload(id: string, hostId: string, address: string, localPath: string, remotePath: string, overwrite: boolean, deleteSource: boolean): Promise<string>;
  transferResume(id: string): Promise<void>;
  transferPause(id: string): Promise<void>;
  transferCancel(id: string): Promise<void>;
  /** Forgets a finished, failed or cancelled transfer; running ones are ignored. */
  transferRemove(id: string): Promise<void>;
  transfersList(): Promise<string[]>;
  /** Cancellable pinned GET to a file; a non-2xx status is a normal result (404 no thumbnail, 429 busy + Retry-After). Rejects ERR_TOO_LARGE past maxBytes. */
  fetchToFile(id: string, url: string, headers: Record<string, string>, pin: string | null, destPath: string, timeoutMs?: number, maxBytes?: number): Promise<{ status: number; retryAfter: number | null; bytes: number }>;
  fetchCancel(id: string): Promise<void>;
  /** Local PDF via PdfRenderer; rejects ERR_PDF for corrupt or password-protected files. */
  pdfPageCount(path: string): Promise<number>;
  /** Renders a page to a PNG at [outPath], [widthPx] wide on white; resolves with the pixel size. */
  pdfRenderPage(path: string, index: number, widthPx: number, outPath: string): Promise<{ width: number; height: number }>;
  pdfClose(): Promise<void>;
  /** Loopback bridge (127.0.0.1, random path, GET/HEAD + Range) so the media player can stream a pinned file. */
  mediaProxyStart(id: string, url: string, headers: Record<string, string>, pin: string | null): Promise<string>;
  mediaProxyStop(id: string): Promise<void>;
  /** Broadcasts a WoL magic packet; false on a malformed MAC or send failure. */
  sendWakeOnLan(mac: string): Promise<boolean>;
  /** Opens/shares a local file (under cache/open or cache/share) with another app; false when nothing can handle it. */
  openFileExternal(path: string, mime: string): Promise<boolean>;
  shareFileExternal(path: string, mime: string): Promise<boolean>;
  /** Opens a finished download published to the shared Downloads folder (a MediaStore content URI). */
  openPublicUri(uri: string, mime: string): Promise<boolean>;
  /** Encrypted-at-rest bodies of pinned folders (AES-GCM streaming, key in secure storage). Restore rejects ERR_INTEGRITY and deletes the entry when it fails authentication. */
  offlineBodyPut(hostId: string, path: string, srcPath: string): Promise<void>;
  offlineBodyRestore(hostId: string, path: string, destPath: string): Promise<boolean>;
  offlineBodyHas(hostId: string, path: string): Promise<boolean>;
  offlineBodyTotalBytes(): Promise<number>;
  offlineBodyRemove(hostId: string, path: string): Promise<void>;
  offlineBodyEvictHost(hostId: string): Promise<void>;
  /** Transports of the active network: any of wifi, ethernet, cellular, vpn, bluetooth; empty when offline. */
  networkTransports(): Promise<string[]>;
  deviceId(): Promise<string | null>;
  /** mDNS search for `_rfe._tcp` agents (8 s); results are untrusted hints. */
  discoveryScan(): Promise<{ name: string; address: string; port: number }[]>;
  discoveryStop(): Promise<void>;
  /** Flutter shared_preferences (`flutter.` stripped); each value is JSON-encoded. */
  legacyPrefsReadAll(): Promise<Record<string, string>>;
  secureRead(key: string): Promise<string | null>;
  secureWrite(key: string, value: string): Promise<void>;
  secureDelete(key: string): Promise<void>;
  secureContains(key: string): Promise<boolean>;
  /** Every secure-storage entry (keys without the plugin prefix). */
  secureReadAll(): Promise<Record<string, string>>;
  /** Encrypted settings backup, format shared with the Flutter app. Rejects ERR_BACKUP with a user-readable message. */
  backupEncrypt(payloadJson: string, passphrase: string): Promise<string>;
  backupDecrypt(envelopeJson: string, passphrase: string): Promise<string>;
  /** This build's versionCode. */
  appBuild(): Promise<number>;
  /** Plain-HTTPS download with Range resume from [offset] (the partial file's size). Rejects ERR_RANGE when the server ignored the resume (the partial file is deleted). Resolves with the file size. */
  publicDownload(id: string, url: string, destPath: string, offset: number): Promise<number>;
  publicDownloadCancel(id: string): Promise<void>;
  sha256File(path: string): Promise<string>;
  /** Whether "install unknown apps" is allowed for this app. */
  canInstallPackages(): Promise<boolean>;
  openInstallSettings(): Promise<boolean>;
  /** Hands an APK under cache/updates to the system installer. */
  installApk(path: string): Promise<boolean>;
  probeFingerprint(url: string, timeoutMs?: number): Promise<string>;
  request(
    url: string,
    method: string,
    headers: Record<string, string>,
    bodyText: string | null,
    pin: string | null,
    timeoutMs?: number,
  ): Promise<RfeResponse>;
  downloadToFile(
    url: string,
    headers: Record<string, string>,
    pin: string | null,
    destPath: string,
    offset?: number,
    timeoutMs?: number,
  ): Promise<number>;
}

export default requireNativeModule<RfeTransportModule>('RfeTransport');
