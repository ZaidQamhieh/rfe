import { ed25519 } from '@noble/curves/ed25519.js';

/** The release manifest (`latest.json`) published next to each GitHub release. */
export type AppRelease = { versionName: string; versionCode: number; size: number; url: string | null; sha256: string };

/** Same repository and stable "latest release" redirect the Flutter app reads, so both apps follow one release channel. */
export const GITHUB_REPO = 'ZaidQamhieh/rfe';
export const LATEST_MANIFEST_URL = `https://github.com/${GITHUB_REPO}/releases/latest/download/latest.json`;

export const LATEST_SIGNATURE_URL = `${LATEST_MANIFEST_URL}.sig`;

/** The release signing public key (ed25519, hex). The private key signs `latest.json` in the release workflow and is never in the repository (audit F-106). */
export const RELEASE_PUBLIC_KEY = '0c7b69ac0e0b08ca6c6cf0c12c528870e99506aeeecf59fce1076014a481bac2';

const hexBytes = (hex: string): Uint8Array | null => (/^([0-9a-fA-F]{2})+$/.test(hex) ? Uint8Array.from(hex.match(/../g) as string[], (h) => parseInt(h, 16)) : null);

/** True when `signatureHex` is a valid ed25519 signature of exactly `manifestText` under `publicKeyHex`. */
export function manifestSignatureOk(manifestText: string, signatureHex: string, publicKeyHex: string = RELEASE_PUBLIC_KEY): boolean {
  const sig = hexBytes(signatureHex.trim());
  const key = hexBytes(publicKeyHex);
  if (sig === null || key === null || sig.length !== 64 || key.length !== 32) return false;
  try {
    return ed25519.verify(sig, new TextEncoder().encode(manifestText), key);
  } catch {
    return false;
  }
}

/** Hosts an APK may be fetched from: GitHub, and the storage host its release downloads redirect to. */
const APK_HOSTS = ['github.com', 'githubusercontent.com'];

export function isTrustedApkUrl(url: string): boolean {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return false;
  }
  return u.protocol === 'https:' && APK_HOSTS.some((h) => u.hostname === h || u.hostname.endsWith(`.${h}`));
}

/** Parses a manifest; null when it is not a usable release (no version code, no APK address on GitHub, or no SHA-256 to check the download against). */
export function parseRelease(raw: unknown): AppRelease | null {
  if (typeof raw !== 'object' || raw === null) return null;
  const j = raw as Record<string, unknown>;
  const code = typeof j.versionCode === 'number' ? Math.trunc(j.versionCode) : NaN;
  if (!Number.isFinite(code) || code <= 0) return null;
  const url = typeof j.url === 'string' && isTrustedApkUrl(j.url) ? j.url : null;
  if (url === null) return null;
  // The release workflow always publishes the hash; a manifest without one is not trusted to install.
  if (typeof j.sha256 !== 'string' || !/^[0-9a-fA-F]{64}$/.test(j.sha256)) return null;
  const sha = j.sha256.toLowerCase();
  return { versionName: typeof j.versionName === 'string' ? j.versionName : '', versionCode: code, size: typeof j.size === 'number' && j.size > 0 ? Math.trunc(j.size) : 0, url, sha256: sha };
}

export const isUpdateAvailable = (installedBuild: number, release: AppRelease | null): release is AppRelease => release !== null && release.versionCode > installedBuild;

/** The banner shows a release only until the user dismisses that build; a newer build shows again. */
export const shouldSurfaceUpdate = (release: AppRelease | null, dismissedCode: number) => release !== null && release.versionCode > dismissedCode;

/** Percent (0-100) of a download; unknown totals report 0. */
export const percent = (received: number, total: number) => (total > 0 ? Math.min(100, Math.floor((received * 100) / total)) : 0);
