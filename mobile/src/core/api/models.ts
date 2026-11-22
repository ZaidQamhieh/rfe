// Response models ported from app/lib/core/models/*.dart. Parsers are total:
// missing fields take the same defaults the Dart fromJson used.

import { t } from '../../i18n';

type Json = Record<string, unknown>;

const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === 'number' ? v : undefined);
const bool = (v: unknown, d: boolean): boolean => (typeof v === 'boolean' ? v : d);
const nonEmpty = (v: unknown): string | undefined => {
  const s = str(v);
  return s === undefined || s.length === 0 ? undefined : s;
};

export type Health = {
  status: string;
  name: string;
  version: string;
  os: string;
  readOnly: boolean;
  address?: string;
  tailscaleAddress?: string;
  macAddress?: string;
  /** Agent API revision; a v1.42.x agent omits it, which reads as 1. */
  apiVersion: number;
  /** Optional capabilities the agent advertises (e.g. "pairRequest"); absent on older agents. */
  features: string[];
  /** Present only when this agent is an account server and the caller is authorized (`health.accountServer`). */
  accountServer?: { name: string; keyFingerprint: string };
};

/** Any parser input may be null, an array or a scalar when a proxy or odd agent answers; never throw on shape. */
const obj = (v: unknown): Json =>
  typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Json) : {};
const total =
  <T>(f: (j: Json) => T) =>
  (v: Json): T =>
    f(obj(v));

/** Like [total], but a body that is not an object is an error: a proxy's empty 200 must not read as a healthy agent. */
const objectOnly =
  <T>(f: (j: Json) => T) =>
  (v: Json): T => {
    if (typeof v !== "object" || v === null || Array.isArray(v))
      throw new Error(t("unexpectedResponseShape"));
    return f(v);
  };

export const parseHealth = objectOnly((j: Json): Health => {
  const api = num(j.apiVersion);
  return {
    status: str(j.status) ?? "unknown",
    name: str(j.name) ?? "",
    version: str(j.version) ?? "",
    os: str(j.os) ?? "",
    readOnly: bool(j.readOnly, false),
    address: nonEmpty(j.address),
    tailscaleAddress: nonEmpty(j.tailscaleAddress),
    macAddress: nonEmpty(j.macAddress),
    apiVersion:
      api !== undefined && Number.isInteger(api) && api >= 1 ? api : 1,
    features: Array.isArray(j.features)
      ? [
          ...new Set(
            (j.features as unknown[]).filter(
              (x): x is string => typeof x === "string" && x.length > 0,
            ),
          ),
        ]
      : [],
    accountServer: parseAccountServerHint(j.accountServer),
  };
});

function parseAccountServerHint(v: unknown): Health["accountServer"] {
  const o = obj(v as Json);
  const fp = str(o.keyFingerprint);
  return fp && /^[0-9a-f]{64}$/i.test(fp) ? { name: str(o.name) ?? "", keyFingerprint: fp.toLowerCase() } : undefined;
}

/** `GET /accounts/key`. */
export type AccountKey = { name: string; publicKey: string; keyFingerprint: string };

export const parseAccountKey = objectOnly((j: Json): AccountKey => {
  const publicKey = nonEmpty(j.publicKey);
  const fp = str(j.keyFingerprint);
  if (!publicKey || !fp || !/^[0-9a-f]{64}$/i.test(fp) || j.algorithm !== "Ed25519")
    throw new Error(t("unexpectedResponseShape"));
  return { name: str(j.name) ?? "", publicKey, keyFingerprint: fp.toLowerCase() };
});

/** `POST /accounts/assertion`: a bearer credential for its short lifetime; never logged or stored. */
export type AccountAssertion = { assertion: string; exp: number };

export const parseAccountAssertion = objectOnly((j: Json): AccountAssertion => {
  const assertion = nonEmpty(j.assertion);
  const exp = num(j.exp);
  if (!assertion || exp === undefined) throw new Error(t("unexpectedResponseShape"));
  return { assertion, exp };
});

export type PairResponse = {
  deviceToken: string;
  deviceId: string;
  agentName: string;
  certFingerprint?: string;
  address?: string;
  tailscaleAddress?: string;
};

export const parsePairResponse = total((j: Json): PairResponse => ({
  deviceToken: str(j.deviceToken) ?? "",
  deviceId: str(j.deviceId) ?? "",
  agentName: str(j.agentName) ?? "",
  certFingerprint: str(j.certFingerprint),
  address: nonEmpty(j.address),
  tailscaleAddress: nonEmpty(j.tailscaleAddress),
}));

export type Drive = { path: string; label?: string; totalBytes?: number; freeBytes?: number; isOS: boolean };

export const parseDrive = total((j: Json): Drive => ({
  path: str(j.path) ?? "",
  label: str(j.label),
  totalBytes: num(j.totalBytes),
  freeBytes: num(j.freeBytes),
  isOS: bool(j.isOS, false),
}));

export type Entry = {
  name: string;
  path: string;
  isDir: boolean;
  size?: number;
  mimeType?: string;
  mode?: string;
  modified?: string;
  created?: string;
  isSymlink: boolean;
  symlinkTarget?: string;
  /** Items directly inside a folder, capped at 1000 by the agent; only present in listings. */
  childCount?: number;
  /** First matching line of a content-search hit; `start`/`length` index `text` in UTF-16 units. */
  snippet?: Snippet;
};

export type Snippet = { text: string; start: number; length: number };

const parseSnippet = (v: unknown): Snippet | undefined => {
  if (typeof v !== 'object' || v === null) return undefined;
  const j = v as Json;
  const text = str(j.text);
  const start = num(j.start);
  const length = num(j.length);
  if (text === undefined || start === undefined || length === undefined) return undefined;
  if (start < 0 || length < 0 || start + length > text.length) return undefined;
  return { text, start, length };
};

export const parseEntry = objectOnly((j: Json): Entry => ({
  name: str(j.name) ?? "",
  path: str(j.path) ?? "",
  isDir: bool(j.isDir, false),
  size: num(j.size),
  mimeType: str(j.mimeType),
  mode: str(j.mode),
  modified: str(j.modified),
  created: str(j.created),
  isSymlink: bool(j.isSymlink, false),
  symlinkTarget: str(j.symlinkTarget),
  childCount: num(j.childCount),
  snippet: parseSnippet(j.snippet),
}));

export type Listing = { path: string; entries: Entry[]; nextCursor?: string };

export const parseListing = total((j: Json): Listing => ({
  path: str(j.path) ?? "",
  entries: Array.isArray(j.entries)
    ? (j.entries as Json[]).map(parseEntry)
    : [],
  nextCursor: str(j.nextCursor),
}));

export type AgentStatus = {
  agentName: string;
  roots: string[];
  readOnly: boolean;
  /** Whether owner-created one-time share links are enabled on the host. */
  allowSharing: boolean;
  isAdmin: boolean;
  /** False when an older agent omitted `isAdmin`; the UI then shows the owner controls and lets the agent enforce. */
  isAdminKnown: boolean;
  /** Photo backup destination configured on the host (empty when none). */
  photoBackupRoot: string;
  photoBackupConfigured: boolean;
  photoBackupAvailable?: boolean;
  /** What this device may do with files; absent on older agents (then nothing is hidden and the agent enforces). */
  fileCapabilities?: DeviceGrants;
  effectiveScope: 'global' | 'device';
  /** True when the device's jail is outside all host roots: no filesystem access at all. */
  accessDenied: boolean;
};

function parseFileCapabilities(v: unknown): DeviceGrants | undefined {
  if (typeof v !== 'object' || v === null) return undefined;
  const o = v as Record<string, unknown>;
  const grants = Object.fromEntries(FILE_CAPABILITIES.map((c) => [c, o[c] === true])) as Record<FileCapability, boolean>;
  return typeof o.wake === 'boolean' ? { ...grants, wake: o.wake } : grants;
}

/** Whether an action is offered: unknown capabilities (older agent, not loaded yet) never hide anything. */
export const can = (caps: DeviceGrants | undefined, c: DeviceGrant): boolean => caps === undefined || (caps[c] ?? c === 'wake');

export const parseStatus = objectOnly((j: Json): AgentStatus => ({
  agentName: str(j.agentName) ?? "",
  roots: Array.isArray(j.roots)
    ? (j.roots as unknown[]).filter((x): x is string => typeof x === "string")
    : [],
  readOnly: bool(j.readOnly, false),
  allowSharing: bool(j.allowSharing, false),
  isAdmin: bool(j.isAdmin, false),
  isAdminKnown: 'isAdmin' in j,
  photoBackupRoot: str(j.photoBackupRoot) ?? '',
  photoBackupConfigured: typeof j.photoBackupConfigured === 'boolean' ? j.photoBackupConfigured : (str(j.photoBackupRoot) ?? '').length > 0,
  photoBackupAvailable: typeof j.photoBackupAvailable === 'boolean' ? j.photoBackupAvailable : undefined,
  fileCapabilities: parseFileCapabilities(j.fileCapabilities),
  effectiveScope: j.effectiveScope === 'device' ? 'device' : 'global',
  accessDenied: bool(j.accessDenied, false),
}));

export type BatchItemResult = { path: string; ok: boolean; errorCode?: string; errorMessage?: string };
export type BatchResult = { results: BatchItemResult[]; failed: BatchItemResult[] };

export const parseBatchResult = (raw: Json): BatchResult => {
  const j = obj(raw);
  const results = (Array.isArray(j.results) ? (j.results as Json[]) : []).map(
    (r0): BatchItemResult => {
      const r = obj(r0);
      const err =
        typeof r.error === "object" && r.error !== null
          ? (r.error as Json)
          : undefined;
      return {
        path: str(r.path) ?? "",
        ok: r.ok === true,
        errorCode: err ? str(err.code) : undefined,
        errorMessage: err ? str(err.message) : undefined,
      };
    },
  );
  return { results, failed: results.filter((r) => !r.ok) };
};

export type TrashEntry = { id: string; name: string; originalPath: string; deletedAt?: string; size?: number; isDir: boolean };

export const parseTrashEntry = total((j: Json): TrashEntry => ({
  id: str(j.id) ?? "",
  name: str(j.name) ?? "",
  originalPath: str(j.originalPath) ?? "",
  deletedAt: str(j.deletedAt),
  size: num(j.size),
  isDir: bool(j.isDir, false),
}));

export type ArchiveEntry = { path: string; size: number; modified?: string; isDir: boolean };

export const parseArchiveEntry = total((j: Json): ArchiveEntry => ({
  path: str(j.path) ?? "",
  size: num(j.size) ?? 0,
  modified: str(j.modified),
  isDir: bool(j.isDir, false),
}));

export type SearchResult = { entries: Entry[]; truncated: boolean; timeBudgetHit: boolean };

const flagSet = (headers: Record<string, string>, name: string) =>
  Object.entries(headers).some(([k, v]) => k.toLowerCase() === name && v.trim() === '1');

/** Search and recent bodies are bare arrays; truncation state arrives in `X-Search-*` response headers (case-insensitive). */
export const parseSearchResult = (body: unknown, headers: Record<string, string>): SearchResult => ({
  entries: Array.isArray(body) ? (body as Json[]).map(parseEntry) : [],
  truncated: flagSet(headers, 'x-search-truncated'),
  timeBudgetHit: flagSet(headers, 'x-search-time-budget'),
});

/** One child of a scanned folder with the size of everything under it. */
export type UsageEntry = { name: string; isDir: boolean; bytes: number };
/** Answer of GET /fs/usage: per-child sizes, largest first. `partial` means the scan hit its time budget. */
export type UsageResult = { path: string; totalBytes: number; partial: boolean; entries: UsageEntry[] };

export const parseUsage = (body: unknown, headers: Record<string, string>): UsageResult => {
  const j = obj(body);
  return {
    path: str(j.path) ?? '',
    totalBytes: num(j.totalBytes) ?? 0,
    partial: j.partial === true || flagSet(headers, 'x-search-time-budget'),
    entries: Array.isArray(j.entries)
      ? (j.entries as Json[]).map((e) => ({ name: str(e.name) ?? '', isDir: e.isDir === true, bytes: num(e.bytes) ?? 0 }))
      : [],
  };
};

/** A one-time share link. `token` and `url` exist only in the mint response; list entries carry `tokenHash` and `path`. */
export type ShareLink = { token: string; tokenHash: string; path: string; expiresAt: number; url: string };

export const parseShareLink = total((j: Json): ShareLink => ({
  token: str(j.token) ?? "",
  tokenHash: str(j.tokenHash) ?? "",
  path: str(j.path) ?? "",
  expiresAt: num(j.expiresAt) ?? 0,
  url: str(j.url) ?? "",
}));

/** A phone asking to be let in, waiting for the owner to allow or refuse it (`GET /pair/requests`). */
export type PairRequest = {
  id: string;
  /** The name the phone gave itself. */
  label: string;
  /** The short number both the computer and the phone show, as the agent sends it. */
  matchCode: string;
  remoteIp: string;
  /** Label of the already paired phone this approval would take over; empty for a new phone. */
  replaces: string;
  /** Unix ms; 0 when the agent sent no readable time. */
  expiresAt: number;
};

/** Reads one waiting pairing request; a missing or unreadable field becomes an empty string or 0. */
export const parsePairRequest = total((j: Json): PairRequest => {
  const at = typeof j.expiresAt === 'string' ? Date.parse(j.expiresAt) : NaN;
  return {
    id: str(j.id) ?? '',
    label: str(j.label) ?? '',
    matchCode: str(j.matchCode) ?? '',
    remoteIp: str(j.remoteIp) ?? '',
    replaces: str(j.replaces) ?? '',
    expiresAt: Number.isNaN(at) ? 0 : at,
  };
});

/** Photo or video details read from the file header; every field is absent when the file does not say. */
export type MediaInfo = {
  width?: number;
  height?: number;
  /** Capture time, unix ms. */
  takenAt?: number;
  durationMs?: number;
};

/** Reads `GET /fs/media-info`; a field of the wrong type or an unreadable date is left out, so `{}` is a valid answer. */
export const parseMediaInfo = total((j: Json): MediaInfo => {
  const out: MediaInfo = {};
  const w = num(j.width);
  const h = num(j.height);
  const d = num(j.durationMs);
  const at = typeof j.takenAt === 'string' ? Date.parse(j.takenAt) : NaN;
  if (w !== undefined) out.width = w;
  if (h !== undefined) out.height = h;
  if (!Number.isNaN(at)) out.takenAt = at;
  if (d !== undefined) out.durationMs = d;
  return out;
});

/** Owners manage the host; devices paired by code manage only themselves. Older agents omit `isAdmin`, so unknown counts as owner. */
export const canManageHost = (s: Pick<AgentStatus, 'isAdmin' | 'isAdminKnown'>) => s.isAdmin || !s.isAdminKnown;

export type FileCapability = 'browse' | 'download' | 'upload' | 'modify' | 'delete' | 'share';
export const FILE_CAPABILITIES: FileCapability[] = ['browse', 'download', 'upload', 'modify', 'delete', 'share'];

/** A per-device grant: the six file permissions plus `wake` (Wake-on-LAN through the agent, POST /wol), which the agent lists with them but older agents do not send. */
export type DeviceGrant = FileCapability | 'wake';
export const DEVICE_GRANTS: DeviceGrant[] = [...FILE_CAPABILITIES, 'wake'];

/** The grants a device holds: the six file permissions always, `wake` only when the agent reports it. */
export type DeviceGrants = Record<FileCapability, boolean> & { wake?: boolean };

/** A device paired with a host. Capability flags are absent on agents that predate them. */
export type Device = {
  id: string;
  label: string;
  /** Unix seconds -> ms. */
  created: number;
  lastSeen: number;
  revoked: boolean;
  current: boolean;
  viaLogin: boolean;
  lastAddress: string;
  lastVersion: string;
  jailRoot: string;
  viewApps?: boolean;
  launchApps?: boolean;
  browse?: boolean;
  download?: boolean;
  upload?: boolean;
  modify?: boolean;
  delete?: boolean;
  share?: boolean;
  /** Absent on agents that predate the Wake-on-LAN grant. */
  wake?: boolean;
};

const optBool = (v: unknown) => (typeof v === 'boolean' ? v : undefined);

export const parseDevice = total((j: Json): Device => ({
  id: str(j.id) ?? "",
  label: str(j.label) ?? "",
  created: (num(j.created) ?? 0) * 1000,
  lastSeen: (num(j.lastSeen) ?? 0) * 1000,
  revoked: bool(j.revoked, false),
  current: bool(j.current, false),
  viaLogin: bool(j.viaLogin, false),
  lastAddress: str(j.lastAddress) ?? '',
  lastVersion: str(j.lastVersion) ?? '',
  jailRoot: str(j.jailRoot) ?? '',
  viewApps: optBool(j.viewApps),
  launchApps: optBool(j.launchApps),
  browse: optBool(j.browse),
  download: optBool(j.download),
  upload: optBool(j.upload),
  modify: optBool(j.modify),
  delete: optBool(j.delete),
  share: optBool(j.share),
  wake: optBool(j.wake),
}));

export const hasAppCapabilities = (d: Device) => d.viewApps !== undefined && d.launchApps !== undefined;
export const hasFileCapabilities = (d: Device) => FILE_CAPABILITIES.every((c) => d[c] !== undefined);

export type BandwidthSettings = {
  maxUploadBytesPerSec: number;
  maxDownloadBytesPerSec: number;
};
export const parseBandwidth = total((j: Json): BandwidthSettings => ({
  maxUploadBytesPerSec: num(j.maxUploadBytesPerSec) ?? 0,
  maxDownloadBytesPerSec: num(j.maxDownloadBytesPerSec) ?? 0,
}));

export type AuditEntry = {
  id: number;
  at: Date;
  action: string;
  actor: string;
  target: string;
  detail: string;
};
export const parseAuditEntry = total((j: Json): AuditEntry => {
  const at = new Date(str(j.at) ?? "");
  return {
    id: num(j.id) ?? 0,
    at: Number.isNaN(at.getTime()) ? new Date(0) : at,
    action: str(j.action) ?? "",
    actor: str(j.actor) ?? "",
    target: str(j.target) ?? "",
    detail: str(j.detail) ?? "",
  };
});

/** One launchable entry of a host's app catalog. `id` is opaque and server-issued: never a path or command line. */
export type HostApp = { id: string; name: string; launchable: boolean; description?: string; icon?: string; category?: string };
export type HostAppCatalog = { platform: string; apps: HostApp[]; launchAllowed: boolean };

export const parseHostAppCatalog = (raw: Json): HostAppCatalog => {
  const j = obj(raw);
  if (!Array.isArray(j.apps))
    throw new Error(t("invalidAppCatalogMessage"));
  const apps = (j.apps as Json[]).map((a0): HostApp => {
    const a = obj(a0);
    const id = str(a.id);
    const name = str(a.name);
    if (!id || !name || name.trim() === '') throw new Error(t('invalidAppCatalogEntryMessage'));
    // Older agents listed only apps they could launch.
    return { id, name, launchable: bool(a.launchable, true), description: str(a.description), icon: str(a.icon), category: str(a.category) };
  });
  // A missing flag from an older agent means launching is off, the safe reading.
  return { platform: str(j.platform) ?? '', apps, launchAllowed: bool(j.launchAllowed, false) };
};
