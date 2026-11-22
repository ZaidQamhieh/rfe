// Typed client for the real agent REST API (protocol/openapi.yaml — the
// contract's source of truth). Same-origin (this bundle is served by the
// agent itself via go:embed), so requests are relative to "/v1".
import { sha256 } from '@noble/hashes/sha2.js';
import { deviceProofMessageV2, getDevicePublicKeyB64, signNonce } from './deviceIdentity';
import { mintBody, renameBody, searchParams, trashIdsBody } from './requests';

// Remove the JavaScript-readable bearer token stored by older companion
// builds. Existing browser sessions must sign in again to receive an HttpOnly
// session cookie.
export function clearLegacyBrowserToken(): void {
  try { localStorage.removeItem('rfe_device_token'); } catch { /* storage may be disabled */ }
}

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// The device name a browser session is paired under.
const BROWSER_LABEL = 'Browser';

// Every request from this client carries this header; the agent uses it to tell the web companion from other callers.
const WEB_SESSION_HEADERS: Record<string, string> = { 'X-RFE-Web-Session': '1' };

/** The error for a failed response, from the agent's JSON error body when it has one. */
function apiErrorFrom(res: Response, data: { code?: string; message?: string } | null | undefined): ApiError {
  return new ApiError(res.status, data?.code ?? 'UNKNOWN', data?.message ?? res.statusText);
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = { ...WEB_SESSION_HEADERS };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const res = await fetch(`/v1${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (res.status === 204) return undefined as T;
  const isJson = res.headers.get('content-type')?.includes('application/json');
  const data = isJson ? await res.json() : undefined;
  if (!res.ok) throw apiErrorFrom(res, data);
  return data as T;
}

const get = <T>(path: string) => request<T>('GET', path);
const post = <T>(path: string, body?: unknown) => request<T>('POST', path, body);
const patch = <T>(path: string, body?: unknown) => request<T>('PATCH', path, body);
const put = <T>(path: string, body?: unknown) => request<T>('PUT', path, body);
const del = <T>(path: string, body?: unknown) => request<T>('DELETE', path, body);

/** The error for a response whose body may or may not be JSON. */
async function failedResponse(res: Response): Promise<ApiError> {
  const data = await res.json().catch(() => ({}));
  return apiErrorFrom(res, data);
}

// Chunk uploads are raw octet-stream bodies with a custom header, not JSON, so they bypass the
// JSON-only `request` helper above.
async function rawPut(path: string, body: ArrayBuffer, headers: Record<string, string>): Promise<void> {
  const res = await fetch(`/v1${path}`, {
    method: 'PUT',
    headers: { ...headers, ...WEB_SESSION_HEADERS },
    body,
    credentials: 'same-origin',
  });
  if (!res.ok) throw await failedResponse(res);
}

/** The message to show for a failed call: the agent's text for an ApiError, otherwise the fallback. */
export function apiMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

/** Fetches a file's content as a blob for the caller to save; failures are ApiErrors like the rest of this client. */
export async function fetchContentBlob(path: string): Promise<Blob> {
  const response = await fetch(`/v1/content?path=${encodeURIComponent(path)}`, {
    headers: { ...WEB_SESSION_HEADERS },
    credentials: 'same-origin',
  });
  if (!response.ok) throw await failedResponse(response);
  return response.blob();
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function sha256Hex(data: Uint8Array): string {
  return toHex(sha256(data));
}

const UPLOAD_CHUNK_SIZE = 4 * 1024 * 1024; // 4MiB, under the agent's 32MiB cap

/** Drives the full resumable-upload contract: open session, hash + PUT each
 * chunk, then complete. The required whole-file SHA-256 is computed in a
 * bounded-memory first pass; upload then rereads and sends one chunk at a time. */
async function uploadFile(path: string, file: File, onProgress?: (fraction: number) => void): Promise<void> {
  const wholeHasher = sha256.create();
  for (let start = 0; start < file.size; start += UPLOAD_CHUNK_SIZE) {
    const end = Math.min(start + UPLOAD_CHUNK_SIZE, file.size);
    const slice = await file.slice(start, end).arrayBuffer();
    wholeHasher.update(new Uint8Array(slice));
  }
  const wholeHash = toHex(wholeHasher.digest());

  const session = await post<UploadSession>('/transfers', {
    path,
    size: file.size,
    sha256: wholeHash,
    chunkSize: UPLOAD_CHUNK_SIZE,
  });
  const totalChunks = session.totalChunks;
  for (let n = 0; n < totalChunks; n++) {
    const start = n * UPLOAD_CHUNK_SIZE;
    const chunk = await file.slice(start, Math.min(start + UPLOAD_CHUNK_SIZE, file.size)).arrayBuffer();
    const chunkHash = sha256Hex(new Uint8Array(chunk));
    await rawPut(`/transfers/${session.id}/chunks/${n}`, chunk, { 'X-Chunk-Sha256': chunkHash });
    onProgress?.((n + 1) / totalChunks);
  }
  await post(`/transfers/${session.id}/complete`);
}

// ---------- device identity / proof-of-possession ----------

async function proofFields() {
  const challenge = await post<{ nonce: string; certFingerprint?: string }>('/auth/challenge');
  const devicePublicKey = await getDevicePublicKeyB64();
  // /pair, /register and /login accept only the certificate-bound proof; the agent tells us its fingerprint.
  if (!challenge.certFingerprint) throw new Error('This computer did not send its certificate fingerprint.');
  const signature = await signNonce(deviceProofMessageV2(challenge.certFingerprint, challenge.nonce));
  return { devicePublicKey, nonce: challenge.nonce, signature };
}

// ---------- schemas (protocol/openapi.yaml) ----------

interface Health {
  status: string;
  name?: string;
  version?: string;
  os?: 'windows' | 'linux';
  readOnly?: boolean;
  address?: string;
  tailscaleAddress?: string;
  macAddress?: string;
}
export interface AgentStatus {
  version: string;
  uptimeSeconds: number;
  platform: string;
  freeBytes: number;
  totalBytes: number;
  /** What this device may do; `wake` is the Wake-on-LAN grant (absent on older agents). */
  fileCapabilities?: { wake?: boolean };
}
export interface Metrics {
  rxBytes: number;
  txBytes: number;
  cpuPercent: number;
  ramPercent: number;
  tsMs: number;
}
interface PairResponse {
  deviceToken?: string;
  deviceId: string;
  agentName: string;
  certFingerprint: string;
  address?: string;
  tailscaleAddress?: string;
}
export interface Device {
  id: string;
  label: string;
  created: number;
  lastSeen: number;
  revoked: boolean;
  current: boolean;
  lastAddress?: string;
  lastVersion?: string;
  jailRoot?: string;
  readOnly?: boolean;
  viaLogin?: boolean;
  browse?: boolean;
  download?: boolean;
  upload?: boolean;
  modify?: boolean;
  delete?: boolean;
  share?: boolean;
  wake?: boolean;
}
export interface Entry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  mimeType?: string;
  mode?: string;
  modified?: string;
  created?: string;
  isSymlink?: boolean;
  symlinkTarget?: string;
}
interface Listing {
  path: string;
  entries: Entry[];
  nextCursor?: string | null;
}
export interface TrashEntry {
  id: string;
  name: string;
  originalPath: string;
  deletedAt: string;
  size: number;
  isDir: boolean;
}
export interface ShareLink {
  token: string;
  tokenHash: string;
  expiresAt: number;
  url: string;
}
/** A batch answer: 200 even when some items failed, so each result must be read. */
export interface BatchResponse {
  results?: { path: string; ok: boolean; error?: { code?: string; message?: string } }[];
}
export interface ShareLinkSummary {
  tokenHash: string;
  path: string;
  expiresAt: number;
}
export interface AgentSettings {
  readOnly: boolean;
  roots: string[];
  agentName: string;
  allowSharing: boolean;
  photoBackupRoot: string;
}
export interface BandwidthSettings {
  maxUploadBytesPerSec: number;
  maxDownloadBytesPerSec: number;
}
interface TransfersList {
  total: number;
  counts: Record<string, number>;
  transfers: TransferRow[];
  activeNow: number;
  devices: { id: string; label: string; username: string }[];
  users: string[];
}
export interface TransferRow {
  id: string;
  name: string;
  path: string;
  totalSize: number;
  receivedBytes: number;
  progress: number;
  status: 'open' | 'completed' | 'failed';
  deviceId: string;
  updatedAt: number;
}
export interface UserAccount {
  username: string;
  created: number;
}
interface LogLine {
  ts: string;
  message: string;
}
interface UploadSession {
  id: string;
  path: string;
  size: number;
  chunkSize: number;
  totalChunks: number;
  receivedChunks: number[];
  status: 'open' | 'completed' | 'failed';
}

// ---------- auth flows (unauthenticated) ----------

export const api = {
  health: () => get<Health>('/health'),
  status: () => get<AgentStatus>('/status'),
  metrics: () => get<Metrics>('/metrics'),

  async login(username: string, password: string): Promise<PairResponse> {
    const proof = await proofFields();
    return post<PairResponse>('/login', { username, password, deviceLabel: BROWSER_LABEL, ...proof });
  },
  async register(
    pairingCode: string,
    username: string,
    password: string,
  ): Promise<PairResponse> {
    const proof = await proofFields();
    return post<PairResponse>('/register', { pairingCode, username, password, deviceLabel: BROWSER_LABEL, ...proof });
  },
  async pair(pairingCode: string): Promise<PairResponse> {
    const proof = await proofFields();
    return post<PairResponse>('/pair', { pairingCode, deviceLabel: BROWSER_LABEL, ...proof });
  },
  logoutSession: () => request<void>('POST', '/auth/logout'),

  // devices
  listDevices: () => get<Device[]>('/devices'),
  patchDevice: (id: string, body: Partial<Pick<Device,
    'jailRoot' | 'readOnly' | 'browse' | 'download' | 'upload' | 'modify' | 'delete' | 'share' | 'wake'>>) =>
    patch<Device>(`/devices/${id}`, body),
  deleteDevice: (id: string) => del<void>(`/devices/${id}`),
  generatePairingCode: async () => {
    const res = await post<{ pairingCode: string; expiresInSeconds: number; qrPngBase64?: string }>('/pairing/generate');
    return {
      code: res.pairingCode,
      expiresAt: Math.floor(Date.now() / 1000) + res.expiresInSeconds,
      qrPngBase64: res.qrPngBase64,
    };
  },

  // users
  listUsers: () => get<UserAccount[]>('/users'),
  deleteUser: (username: string) => del<void>(`/users/${encodeURIComponent(username)}`),

  // logs
  listLogs: () => get<LogLine[]>('/logs'),

  // transfers
  listTransfers: (params?: { device?: string }) => {
    const qs = new URLSearchParams(params as Record<string, string>).toString();
    return get<TransfersList>(`/transfers/list${qs ? `?${qs}` : ''}`);
  },
  deleteTransfer: (id: string) => del<void>(`/transfers/${id}`),

  uploadFile,

  // settings
  getSettings: () => get<AgentSettings>('/settings'),
  patchSettings: (body: Partial<AgentSettings>) => patch<AgentSettings>('/settings', body),
  getBandwidth: () => get<BandwidthSettings>('/settings/bandwidth'),
  putBandwidth: (body: BandwidthSettings) => put<BandwidthSettings>('/settings/bandwidth', body),

  // filesystem
  listDir: (path: string, cursor?: string) => {
    const qs = new URLSearchParams({ path, ...(cursor ? { cursor } : {}) }).toString();
    return get<Listing>(`/fs?${qs}`);
  },
  mkdir: (path: string) => post<void>('/fs/folder', { path }),
  rename: (path: string, newName: string) => patch<Entry>('/fs/rename', renameBody(path, newName)),
  recent: () => get<Entry[]>('/fs/recent'),
  search: (query: string, root?: string) => get<Entry[]>(`/search?${searchParams(query, root)}`),

  // trash
  listTrash: () => get<TrashEntry[]>('/trash'),
  restoreTrash: (ids: string[]) => post<BatchResponse>('/trash/restore', trashIdsBody(ids)),
  // No body would empty the whole trash, so this always names the ids it deletes.
  deleteTrashForever: (ids: string[]) => del<void>('/trash', trashIdsBody(ids)),
  emptyTrash: () => del<void>('/trash'),

  // shares
  listShares: () => get<ShareLinkSummary[]>('/share'),
  mintShare: (path: string, expiresInSeconds?: number) => post<ShareLink>('/share/mint', mintBody(path, expiresInSeconds)),
  revokeShare: (tokenHash: string) => del<void>(`/share/${tokenHash}`),

  // agent lifecycle
  restartAgent: () => post<void>('/agent/restart'),
  wake: (mac: string) => post<{ status: 'sent' }>('/wol', { mac }),
};
