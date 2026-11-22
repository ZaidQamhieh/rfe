import { routeAddresses, routeForAddress, type Host, type HostRoute } from '../models/host';
import {
  parseDrive,
  parseHealth,
  parseArchiveEntry,
  parseBatchResult,
  parseEntry,
  parseListing,
  parseTrashEntry,
  type ArchiveEntry,
  type BatchResult,
  type Entry,
  type TrashEntry,
  parseStatus,
  type AgentStatus,
  parsePairResponse,
  parseAccountKey,
  parseAccountAssertion,
  type AccountKey,
  type AccountAssertion,
  parseAuditEntry,
  parseHostAppCatalog,
  parseBandwidth,
  parseDevice,
  parseSearchResult,
  parseUsage,
  parseShareLink,
  parsePairRequest,
  parseMediaInfo,
  type MediaInfo,
  type PairRequest,
  type AuditEntry,
  type BandwidthSettings,
  type Device,
  type DeviceGrants,
  type HostAppCatalog,
  type SearchResult,
  type UsageResult,
  type ShareLink,
  type Drive,
  type Health,
  type Listing,
  type PairResponse,
} from './models';
import { streamUrl, type StreamQuality } from './hlsStream';
import { CertPinMismatch, ERR_CERT_PIN_MISMATCH, ERR_PIN_POLICY, MissingCertPin, normalizeFingerprint } from './pin';
import { t } from '../../i18n';

export { ERR_CERT_PIN_MISMATCH, ERR_PIN_POLICY };

/** How long a share link lives when the caller names no length: one hour. */
const DEFAULT_SHARE_SECONDS = 60 * 60;
/** Longest side, in px, of a thumbnail when the caller gives no size. */
const DEFAULT_THUMB_SIZE = 256;
/** How long a pairing request waits for approval when the agent names no expiry, in seconds. */
const DEFAULT_PAIR_EXPIRY_SECONDS = 120;
/** Row limit for audit, search and recent-file queries when the caller names none. */
const DEFAULT_RESULT_LIMIT = 100;
/** Row limit for a folder listing page when the caller names none. */
const DEFAULT_LIST_LIMIT = 200;

/** An error named AbortError, what a cancelled request rejects with (the browser's own class is not in React Native). */
export function abortError(): Error {
  return Object.assign(new Error('The request was cancelled.'), { name: 'AbortError' });
}

/** True for the error a cancelled request rejects with. */
export const isAbortError = (e: unknown): boolean => e instanceof Error && e.name === 'AbortError';

/**
 * Settles with [work], or rejects with an AbortError the moment [signal] fires. The listener is removed once [work]
 * settles, so a long-lived signal does not collect them.
 */
function rejectOnAbort<T>(work: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(abortError());
    signal.addEventListener('abort', onAbort, { once: true });
    work.then(resolve, reject).finally(() => signal.removeEventListener('abort', onAbort));
  });
}

/** Native transport surface (implemented by modules/rfe-transport; faked in tests). */
export type TransportResponse = { status: number; headers: Record<string, string>; bodyText: string };

export interface Transport {
  request(args: {
    url: string;
    method: string;
    headers: Record<string, string>;
    bodyText: string | null;
    pin: string | null;
    timeoutMs?: number;
    /** Optional: the caller no longer wants the answer; a transport that can, stops the request. */
    signal?: AbortSignal;
  }): Promise<TransportResponse>;
}

/** Error codes the native transport rejects with. ERR_CERT_PIN_MISMATCH and ERR_PIN_POLICY live in ./pin. */
export const ERR_CONNECTION = 'ERR_CONNECTION';
/** Error code for a reply the app cannot read (not JSON, or the wrong shape). */
export const BAD_RESPONSE = 'BAD_RESPONSE';
/** Error code for an optional feature or route the agent is too old to offer. */
export const UNSUPPORTED_BY_AGENT = 'UNSUPPORTED_BY_AGENT';

/** Request header an apiVersion 3 agent reads the pair-poll nonce from (compat C3). */
export const PAIR_NONCE_HEADER = 'X-RFE-Pair-Nonce';
/** Query parameter agents before apiVersion 3 read the pair-poll nonce from. */
export const PAIR_NONCE_QUERY = 'nonce';
/** Per-item error code an apiVersion 3 agent gives a path that does not exist (compat C1). */
export const ERR_PATH_NOT_FOUND = 'PATH_NOT_FOUND';
const HTTP_NOT_FOUND = 404;
/** The status an agent answers a missing, revoked or unknown token with. */
export const HTTP_UNAUTHORIZED = 401;

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

export class AgentApiError extends Error {
  constructor(
    readonly statusCode: number,
    readonly code: string,
    message: string,
    /** The transport failure behind a CONNECTION error, so diagnostics can tell DNS from a timeout. */
    readonly cause?: unknown,
  ) {
    super(message);
    this.name = 'AgentApiError';
  }
}

/** The error for a streamed download or upload that came back non-2xx: the body is not read there, so only the status is known. */
export const statusError = (status: number) => new AgentApiError(status, status === 403 ? 'FORBIDDEN' : 'UNKNOWN', httpStatusMessage(status));

/** The message for an HTTP status the agent gave no text for, e.g. "HTTP 404". */
export const httpStatusMessage = (status: number): string => `HTTP ${status}`;
const BARE_HTTP_STATUS_MESSAGE = /^HTTP \d+$/;
/** True when a message is only the bare status, as a streamed call (which does not read the body) carries. */
export const isBareHttpStatusMessage = (message: string): boolean => BARE_HTTP_STATUS_MESSAGE.test(message);

/** The error for a success reply the app cannot read (not JSON, or not the expected shape). */
const badReplyError = (status: number) => new AgentApiError(status, BAD_RESPONSE, t('computerReplyUnreadable'));

const isSafeToRetryOnFallback = (m: string) => m === 'GET' || m === 'HEAD';

/** Only these two calls may run before a pin exists; neither carries a secret. */
function isSafeUnpinnedPreflight(method: string, path: string): boolean {
  return (method === 'GET' && path === '/health') || (method === 'POST' && path === '/auth/challenge');
}

export const errCode = (e: unknown): string | undefined =>
  typeof e === 'object' && e !== null && 'code' in e ? String((e as { code: unknown }).code) : undefined;

/** Request body for the pairing calls: the optional deviceId is sent only when it is set. */
function bodyWithDeviceId<T extends { deviceId?: string }>(a: T) {
  const { deviceId, ...rest } = a;
  return { ...rest, ...(deviceId ? { deviceId } : {}) };
}

export type AgentClientOptions = {
  transport: Transport;
  deviceToken?: string;
  /** Secure-store pin (never the metadata mirror). */
  pinnedFingerprint?: string | null;
  clientVersion?: string;
  /** Start from LAN (health probes) instead of the last good route. */
  probeLanFirst?: boolean;
  /** Per-request timeout for the native transport (connect/read/write). */
  timeoutMs?: number;
};

/**
 * Pinned agent client. Mirrors agent_client.dart: bearer token only after a
 * valid pin exists, no redirects, GET/HEAD-only route fallback, and pin
 * failures never trigger fallback.
 */
export class AgentClient {
  private static lastGoodRoute = new Map<string, number>();

  private readonly pin: string | null;
  private readonly addresses: string[];
  private addrIndex: number;

  constructor(
    readonly host: Host,
    private readonly opts: AgentClientOptions,
  ) {
    this.pin = normalizeFingerprint(opts.pinnedFingerprint ?? null);
    if (opts.pinnedFingerprint != null && this.pin === null) throw new MissingCertPin();
    if (opts.deviceToken && this.pin === null) throw new MissingCertPin();
    this.addresses = routeAddresses(host).map((r) => r.address);
    this.addrIndex = opts.probeLanFirst
      ? 0
      : Math.min(Math.max(AgentClient.lastGoodRoute.get(host.id) ?? 0, 0), this.addresses.length - 1);
  }

  get activeAddress(): string {
    return this.addresses[this.addrIndex];
  }

  get activeRoute(): HostRoute {
    return routeForAddress(this.host, this.activeAddress);
  }

  private async send(
    addrIndex: number,
    method: string,
    path: string,
    query: Record<string, string | number | undefined> | undefined,
    json: unknown,
    text?: string,
    signal?: AbortSignal,
    extraHeaders?: Record<string, string>,
  ) {
    if (this.pin === null && !isSafeUnpinnedPreflight(method, path)) throw new MissingCertPin();
    const qs = query
      ? Object.entries(query)
          .filter(([, v]) => v !== undefined)
          .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
          .join('&')
      : '';
    const url = `https://${this.addresses[addrIndex]}/v1${path}${qs ? `?${qs}` : ''}`;
    const headers = { ...this.versionHeaders(), ...extraHeaders };
    if (json !== undefined) headers['Content-Type'] = 'application/json';
    else if (text !== undefined) headers['Content-Type'] = 'application/octet-stream';
    if (this.pin !== null) this.addBearer(headers);
    return this.opts.transport.request({
      url,
      method,
      headers,
      bodyText: json !== undefined ? JSON.stringify(json) : (text ?? null),
      pin: this.pin,
      timeoutMs: this.opts.timeoutMs,
      signal,
    });
  }

  private async call(
    method: string,
    path: string,
    o: { query?: Record<string, string | number | undefined>; json?: unknown; text?: string; signal?: AbortSignal; headers?: Record<string, string> } = {},
  ): Promise<unknown> {
    return (await this.callFull(method, path, o)).body;
  }

  /**
   * Like [call] but also returns the response headers (search reports truncation there). With a signal, the call rejects
   * with an AbortError as soon as the signal fires, even when the transport cannot stop the request itself.
   */
  private callFull(
    method: string,
    path: string,
    o: { query?: Record<string, string | number | undefined>; json?: unknown; text?: string; signal?: AbortSignal; headers?: Record<string, string> } = {},
  ): Promise<{ body: unknown; headers: Record<string, string> }> {
    if (!o.signal) return this.callFullUnbound(method, path, o);
    if (o.signal.aborted) return Promise.reject(abortError());
    return rejectOnAbort(this.callFullUnbound(method, path, o), o.signal);
  }

  private async callFullUnbound(
    method: string,
    path: string,
    o: { query?: Record<string, string | number | undefined>; json?: unknown; text?: string; signal?: AbortSignal; headers?: Record<string, string> },
  ): Promise<{ body: unknown; headers: Record<string, string> }> {
    let res;
    try {
      res = await this.send(this.addrIndex, method, path, o.query, o.json, o.text, o.signal, o.headers);
    } catch (e) {
      const code = errCode(e);
      if (code === ERR_CERT_PIN_MISMATCH) throw new CertPinMismatch();
      if (code === ERR_PIN_POLICY) throw new MissingCertPin();
      if (code !== ERR_CONNECTION || !isSafeToRetryOnFallback(method) || this.addresses.length < 2) {
        throw this.toApiError(e);
      }
      res = await this.fallback(method, path, o, e);
    }
    return { body: this.parse(res), headers: res.headers };
  }

  private async fallback(
    method: string,
    path: string,
    o: { query?: Record<string, string | number | undefined>; json?: unknown; signal?: AbortSignal; headers?: Record<string, string> },
    first: unknown,
  ) {
    const source = this.addrIndex;
    let last: unknown = first;
    for (let i = 0; i < this.addresses.length; i++) {
      if (i === source) continue;
      try {
        const res = await this.send(i, method, path, o.query, o.json, undefined, o.signal, o.headers);
        this.addrIndex = i;
        AgentClient.lastGoodRoute.set(this.host.id, i);
        return res;
      } catch (e) {
        // A pin mismatch is a security failure, not a reason to try another URL.
        if (errCode(e) === ERR_CERT_PIN_MISMATCH) throw new CertPinMismatch();
        if (errCode(e) !== ERR_CONNECTION) throw this.toApiError(e);
        last = e;
      }
    }
    throw this.toApiError(last);
  }

  private toApiError(e: unknown): Error {
    if (e instanceof AgentApiError || e instanceof CertPinMismatch || e instanceof MissingCertPin) return e;
    if (errCode(e) === ERR_CONNECTION) {
      return new AgentApiError(0, 'CONNECTION', t('connectionLostMessage'), e);
    }
    return new AgentApiError(0, 'UNKNOWN', e instanceof Error ? e.message : String(e));
  }

  private parse(res: TransportResponse): unknown {
    let body: unknown;
    let unreadable = false;
    try {
      body = res.bodyText.length ? JSON.parse(res.bodyText) : null;
    } catch {
      body = null;
      unreadable = true;
    }
    if (res.status >= 200 && res.status < 300) {
      // A success whose body is not JSON (a proxy or captive-portal page) must not pass as "empty", or callers
      // would sign or index `undefined`.
      if (unreadable) throw badReplyError(res.status);
      return body;
    }
    const b = (typeof body === 'object' && body !== null ? body : {}) as Record<string, unknown>;
    throw new AgentApiError(
      res.status,
      typeof b.code === 'string' ? b.code : 'UNKNOWN',
      typeof b.message === 'string' ? b.message : httpStatusMessage(res.status),
    );
  }

  /**
   * Everything the native streaming downloader needs. Credentials are attached
   * only with a valid pin; the native side re-checks the pin during the TLS
   * handshake before any header is written.
   */
  downloadSpec(remotePath: string): { url: string; headers: Record<string, string>; pin: string } {
    if (this.pin === null) throw new MissingCertPin();
    return {
      url: `https://${this.activeAddress}/v1/content?path=${encodeURIComponent(remotePath)}`,
      headers: this.specHeaders(),
      pin: this.pin,
    };
  }

  /** URL and credentials of the change-hint stream (`GET /events`), shaped like [downloadSpec]. */
  eventsSpec(): { url: string; headers: Record<string, string>; pin: string } {
    if (this.pin === null) throw new MissingCertPin();
    return { url: `https://${this.activeAddress}/v1/events`, headers: this.specHeaders(), pin: this.pin };
  }

  /** The HLS playlist URL and credentials for a transcoded video (`GET /stream`), shaped like [downloadSpec]. */
  streamSpec(remotePath: string, quality: StreamQuality): { url: string; headers: Record<string, string>; pin: string } {
    if (this.pin === null) throw new MissingCertPin();
    return { url: streamUrl(this.activeAddress, remotePath, quality), headers: this.specHeaders(), pin: this.pin };
  }

  /** URL + credentials for the native thumbnail fetch (`GET /thumb`, JPEG, longest side ~[size] px). */
  thumbnailSpec(remotePath: string, size = DEFAULT_THUMB_SIZE): { url: string; headers: Record<string, string>; pin: string } {
    if (this.pin === null) throw new MissingCertPin();
    return { url: `https://${this.activeAddress}/v1/thumb?path=${encodeURIComponent(remotePath)}&size=${size}`, headers: this.specHeaders(), pin: this.pin };
  }

  private versionHeaders(): Record<string, string> {
    return { 'X-RFE-Client-Version': this.opts.clientVersion ?? 'rn' };
  }

  /** Headers for the native fetches: the version header plus the bearer token when one is stored. */
  private specHeaders(): Record<string, string> {
    const headers = this.versionHeaders();
    this.addBearer(headers);
    return headers;
  }

  /** The one place the bearer header is built; the caller decides whether sending it is allowed (pinned or not). */
  private addBearer(headers: Record<string, string>): void {
    if (this.opts.deviceToken) headers.Authorization = `Bearer ${this.opts.deviceToken}`;
  }

  async health(): Promise<Health> {
    return parseHealth((await this.call('GET', '/health')) as Record<string, unknown>);
  }

  /** Effective scope, roots and read-only policy for this device (`GET /settings`). */
  async status(): Promise<AgentStatus> {
    return parseStatus((await this.call('GET', '/settings')) as Record<string, unknown>);
  }

  /** Owner-only changes to the host (`PATCH /settings`); returns the settings as saved. */
  async updateSettings(patch: { readOnly?: boolean; agentName?: string; allowSharing?: boolean }): Promise<AgentStatus> {
    return parseStatus((await this.call('PATCH', '/settings', { json: patch })) as Record<string, unknown>);
  }

  // ---------------------------------------------------------------------------
  // Devices, bandwidth, audit
  // ---------------------------------------------------------------------------

  async listDevices(): Promise<Device[]> {
    const d = await this.call('GET', '/devices');
    return (Array.isArray(d) ? (d as Record<string, unknown>[]) : []).map(parseDevice);
  }

  /** Removes a device row; a device may only remove itself. `purge` deletes the row and kills its token at once. */
  async deleteDevice(id: string, purge = true): Promise<void> {
    try {
      await this.call('DELETE', `/devices/${encodeURIComponent(id)}`, { query: purge ? { purge: 'true' } : undefined });
    } catch (e) {
      // The wish of a removal is that the device is gone: an unknown device (404) already is.
      if (e instanceof AgentApiError && e.statusCode === HTTP_NOT_FOUND) return;
      throw e;
    }
  }

  /** Both flags are always sent: turning catalog access off must turn launch access off in the same operation. */
  async updateDeviceAppCapabilities(id: string, o: { viewApps: boolean; launchApps: boolean }): Promise<void> {
    await this.call('PATCH', `/devices/${encodeURIComponent(id)}`, { json: { viewApps: o.viewApps, launchApps: o.launchApps } });
  }

  /** Confines a phone to the folder [jailRoot] (an absolute path on the computer); "" lets it out again. */
  async updateDeviceJailRoot(id: string, jailRoot: string): Promise<void> {
    await this.call('PATCH', `/devices/${encodeURIComponent(id)}`, { json: { jailRoot } });
  }

  /** The full set of file grants in one PATCH, so the switches never drift from the host. */
  async updateDeviceFileCapabilities(id: string, grants: DeviceGrants): Promise<void> {
    await this.call('PATCH', `/devices/${encodeURIComponent(id)}`, { json: grants });
  }

  async getBandwidth(): Promise<BandwidthSettings> {
    return parseBandwidth((await this.call('GET', '/settings/bandwidth')) as Record<string, unknown>);
  }

  async setBandwidth(o: { maxUploadBytesPerSec?: number; maxDownloadBytesPerSec?: number }): Promise<BandwidthSettings> {
    return parseBandwidth((await this.call('PUT', '/settings/bandwidth', { json: o })) as Record<string, unknown>);
  }

  /** Account and device events, newest first. Owner-only; `before` pages backwards from an entry id. */
  async audit(o: { limit?: number; before?: number } = {}): Promise<AuditEntry[]> {
    const d = (await this.call('GET', '/audit', { query: { limit: o.limit ?? DEFAULT_RESULT_LIMIT, before: o.before } })) as Record<string, unknown>;
    return (Array.isArray(d.entries) ? (d.entries as Record<string, unknown>[]) : []).map(parseAuditEntry);
  }

  /** The launchable apps the host exposes to this device. 403 when app access is off for the device. */
  async listApps(): Promise<HostAppCatalog> {
    return parseHostAppCatalog((await this.call('GET', '/apps')) as Record<string, unknown>);
  }

  /** Starts one catalog app by its opaque id; the agent re-checks the id against its current catalog. */
  async launchApp(id: string): Promise<void> {
    const d = await this.call('POST', `/apps/${encodeURIComponent(id)}/launch`);
    if (!isObject(d) || d.status !== 'started') throw new Error(t('unexpectedLaunchResponse'));
  }

  async challenge(): Promise<string> {
    const d = await this.call("POST", "/auth/challenge");
    if (!isObject(d) || typeof d.nonce !== "string" || d.nonce === "")
      throw badReplyError(200);
    // An agent older than the v2 proof can only check a bare-nonce signature, which a relay can replay.
    // Refuse with a clear message instead of failing every sign-in with INVALID_SIGNATURE.
    if (d.proof !== "v2")
      throw new AgentApiError(0, UNSUPPORTED_BY_AGENT, t('agentTooOldToSignIn'));
    return d.nonce;
  }

  /** Password login, or (with `assertion`, no username/password) a login vouched for by the account server this agent trusts. */
  async login(
    a: {
      deviceLabel: string;
      devicePublicKey: string;
      nonce: string;
      signature: string;
      deviceId?: string;
    } & ({ username: string; password: string; assertion?: undefined } | { assertion: string; username?: undefined; password?: undefined }),
  ): Promise<PairResponse> {
    const d = await this.call('POST', '/login', { json: bodyWithDeviceId(a) });
    return parsePairResponse(d as Record<string, unknown>);
  }

  /** The account server's public signing key (`GET /accounts/key`, unauthenticated; 403 while the role is off). */
  async accountKey(): Promise<AccountKey> {
    return parseAccountKey((await this.call('GET', '/accounts/key')) as Record<string, unknown>);
  }

  /**
   * Asks this (account server) agent to vouch for the caller at one member agent (`POST /accounts/assertion`).
   * Needs a token from a password login on this agent. The result is a short-lived bearer credential: never log it.
   */
  async accountAssertion(a: { audience: string; devicePublicKey: string; memberNonce: string }): Promise<AccountAssertion> {
    return parseAccountAssertion((await this.call('POST', '/accounts/assertion', { json: a })) as Record<string, unknown>);
  }

  async register(a: {
    pairingCode: string;
    username: string;
    password: string;
    deviceLabel: string;
    devicePublicKey: string;
    nonce: string;
    signature: string;
    deviceId?: string;
  }): Promise<PairResponse> {
    const d = await this.call('POST', '/register', { json: bodyWithDeviceId(a) });
    return parsePairResponse(d as Record<string, unknown>);
  }

  async pair(a: {
    pairingCode: string;
    deviceLabel: string;
    devicePublicKey: string;
    nonce: string;
    signature: string;
    deviceId?: string;
  }): Promise<PairResponse> {
    const d = await this.call('POST', '/pair', { json: bodyWithDeviceId(a) });
    return parsePairResponse(d as Record<string, unknown>);
  }

  /** Asks the computer's owner to approve this device (no code); poll with [pairRequestStatus]. */
  async requestPair(a: {
    deviceLabel: string;
    devicePublicKey: string;
    nonce: string;
    signature: string;
    /** Hex SHA-256 of the 16-byte client nonce, revealed later with [revealPair]. */
    clientNonceCommit: string;
    deviceId?: string;
  }): Promise<{ requestId: string; expiresInSeconds: number }> {
    const d = (await this.call('POST', '/pair/request', { json: bodyWithDeviceId(a) })) as Record<string, unknown>;
    return { requestId: String(d.requestId ?? ''), expiresInSeconds: Number(d.expiresInSeconds ?? DEFAULT_PAIR_EXPIRY_SECONDS) };
  }

  /** Phones waiting to be let in (owner only: another device gets 403). */
  async listPairRequests(): Promise<PairRequest[]> {
    const d = (await this.call('GET', '/pair/requests')) as Record<string, unknown> | null;
    return (Array.isArray(d?.requests) ? (d.requests as Record<string, unknown>[]) : []).map(parsePairRequest);
  }

  /** Lets a waiting phone in (owner only); 404 when it was already answered or ran out of time. */
  async approvePairRequest(id: string): Promise<void> {
    await this.call('POST', `/pair/requests/${encodeURIComponent(id)}/approve`);
  }

  /** Turns a waiting phone away (owner only); 404 when it was already answered or ran out of time. */
  async rejectPairRequest(id: string): Promise<void> {
    await this.call('POST', `/pair/requests/${encodeURIComponent(id)}/reject`);
  }

  /** Sends the nonce committed in [requestPair]; only now is the owner asked to approve. */
  async revealPair(id: string, clientNonce: string): Promise<void> {
    await this.call('POST', `/pair/request/${encodeURIComponent(id)}/reveal`, { json: { clientNonce } });
  }

  /**
   * Polls a pairing request. The nonce goes in BOTH the `nonce` query parameter (agents before apiVersion 3 read only
   * that) and the `PAIR_NONCE_HEADER` header (an apiVersion 3 agent reads only that and ignores the query). The route is
   * unauthenticated, so the agent's version cannot be read first; each agent ignores the form it does not know.
   */
  async pairRequestStatus(id: string, clientNonce: string): Promise<{ status: 'pending' | 'rejected' } | { status: 'approved'; response: PairResponse }> {
    const d = (await this.call('GET', `/pair/request/${encodeURIComponent(id)}`, { query: { [PAIR_NONCE_QUERY]: clientNonce }, headers: { [PAIR_NONCE_HEADER]: clientNonce } })) as Record<string, unknown>;
    if (d.status === 'approved') return { status: 'approved', response: parsePairResponse(d) };
    return { status: d.status === 'rejected' ? 'rejected' : 'pending' };
  }

  async drives(): Promise<Drive[]> {
    const d = await this.call('GET', '/system/drives');
    if (!Array.isArray(d)) throw badReplyError(200);
    return (d as Record<string, unknown>[]).map(parseDrive);
  }

  async list(path: string, o: { cursor?: string; limit?: number } = {}): Promise<Listing> {
    const d = await this.call('GET', '/fs', { query: { path, cursor: o.cursor, limit: o.limit ?? DEFAULT_LIST_LIMIT } });
    return parseListing(d as Record<string, unknown>);
  }

  // ---------------------------------------------------------------------------
  // Filesystem — read
  // ---------------------------------------------------------------------------

  async meta(path: string): Promise<Entry> {
    return parseEntry((await this.call('GET', '/fs/meta', { query: { path } })) as Record<string, unknown>);
  }

  /** Size, capture date and length of a photo or video, read from its header; `{}` for a folder or an unsupported file. */
  async mediaInfo(path: string, signal?: AbortSignal): Promise<MediaInfo> {
    return parseMediaInfo(await this.call('GET', '/fs/media-info', { query: { path }, signal }) as Record<string, unknown>);
  }

  async checksum(path: string, algo = 'sha256'): Promise<string> {
    const d = (await this.call('GET', '/fs/checksum', { query: { path, algo } })) as Record<string, unknown>;
    return d.checksum as string;
  }

  async archiveList(path: string, limit?: number): Promise<ArchiveEntry[]> {
    const d = (await this.call('GET', '/fs/archive', { query: { path, limit } })) as Record<string, unknown>;
    return (Array.isArray(d.entries) ? (d.entries as Record<string, unknown>[]) : []).map(parseArchiveEntry);
  }

  /** Path -> hex hash; files the agent could not hash are omitted. */
  async batchChecksums(paths: string[], algo = 'sha256'): Promise<Record<string, string>> {
    const d = (await this.call('POST', '/fs/checksums', { json: { paths, algo } })) as Record<string, unknown>;
    const out: Record<string, string> = {};
    for (const it of Array.isArray(d.checksums) ? (d.checksums as Record<string, unknown>[]) : []) {
      if (typeof it.hash === 'string' && it.hash.length > 0 && typeof it.path === 'string') out[it.path] = it.hash;
    }
    return out;
  }

  // ---------------------------------------------------------------------------
  // Filesystem — write
  // ---------------------------------------------------------------------------

  /**
   * Overwrites a file with UTF-8 [text] (agent cap 5 MiB). [baseModified] (the Entry.modified last read)
   * makes the write optimistic: the agent answers 409 STALE_WRITE if the file changed since. Other
   * typed failures arrive as AgentApiError codes READ_ONLY and PAYLOAD_TOO_LARGE.
   */
  async putContent(path: string, text: string, baseModified?: string): Promise<Entry> {
    return parseEntry((await this.call('PUT', '/content', { query: { path, baseModified }, text })) as Record<string, unknown>);
  }

  private async entry(method: string, path: string, json: unknown): Promise<Entry> {
    return parseEntry((await this.call(method, path, { json })) as Record<string, unknown>);
  }

  createFolder(path: string) {
    return this.entry('POST', '/fs/folder', { path });
  }

  createFile(path: string) {
    return this.entry('POST', '/fs/file', { path });
  }

  rename(src: string, dst: string) {
    return this.entry('PATCH', '/fs/rename', { src, dst });
  }

  chmod(path: string, mode: string) {
    return this.entry('POST', '/fs/chmod', { path, mode });
  }

  /** Collision precedence is server-side: `duplicate` (keep both) wins over `overwrite`; otherwise a CONFLICT per item. */
  copy(sources: string[], destDir: string, o: { duplicate?: boolean; overwrite?: boolean } = {}): Promise<BatchResult> {
    return this.batchFs('/fs/copy', sources, destDir, o);
  }

  move(sources: string[], destDir: string, o: { duplicate?: boolean; overwrite?: boolean } = {}): Promise<BatchResult> {
    return this.batchFs('/fs/move', sources, destDir, o);
  }

  private async batchFs(path: string, sources: string[], destDir: string, o: { duplicate?: boolean; overwrite?: boolean }): Promise<BatchResult> {
    const d = await this.call('POST', path, { json: { sources, destDir, duplicate: o.duplicate ?? false, overwrite: o.overwrite ?? false } });
    return parseBatchResult(d as Record<string, unknown>);
  }

  /**
   * Reversible (agent trash) unless `permanent`. An apiVersion 3 agent reports a path that no longer exists as a failed
   * item with code `ERR_PATH_NOT_FOUND`; the wish of a delete is that the path is gone, so such an item counts as done (an
   * apiVersion 2 agent never reports it, so the result is the same on both). Other failures stay failures.
   */
  async delete(paths: string[], o: { permanent?: boolean } = {}): Promise<BatchResult> {
    const d = await this.call('DELETE', '/fs', { query: o.permanent ? { permanent: 'true' } : undefined, json: { paths } });
    const raw = parseBatchResult(d as Record<string, unknown>);
    const results = raw.results.map((r) => (!r.ok && r.errorCode === ERR_PATH_NOT_FOUND ? { path: r.path, ok: true } : r));
    return { results, failed: results.filter((r) => !r.ok) };
  }

  async listTrash(): Promise<TrashEntry[]> {
    const d = (await this.call('GET', '/trash')) as Record<string, unknown>;
    return (Array.isArray(d.items) ? (d.items as Record<string, unknown>[]) : []).map(parseTrashEntry);
  }

  async restoreTrash(ids: string[]): Promise<BatchResult> {
    return parseBatchResult((await this.call('POST', '/trash/restore', { json: { ids } })) as Record<string, unknown>);
  }

  async emptyTrash(ids?: string[]): Promise<void> {
    await this.call('DELETE', '/trash', { json: ids ? { ids } : undefined });
  }

  /** The agent auto-renames if `dest` exists; the returned entry has the real path. */
  compress(sources: string[], dest: string) {
    return this.entry('POST', '/fs/compress', { sources, dest });
  }

  extract(archive: string, destDir: string) {
    return this.entry('POST', '/fs/extract', { archive, destDir });
  }

  // ---------------------------------------------------------------------------
  // Search and recents
  // ---------------------------------------------------------------------------

  /** Filters are ANDed server-side; `*` or `?` in [q] makes it a glob. Truncation state is on the result, not an error. */
  async search(o: {
    q: string;
    root?: string;
    limit?: number;
    /** Search inside file contents (an apiVersion 3 agent); an older agent ignores it and searches names. */
    content?: boolean;
    types?: string[];
    ext?: string[];
    minSize?: number;
    maxSize?: number;
    modifiedAfter?: Date;
    modifiedBefore?: Date;
  }): Promise<SearchResult> {
    const { body, headers } = await this.callFull('GET', '/search', {
      query: {
        q: o.q,
        root: o.root,
        limit: o.limit ?? DEFAULT_RESULT_LIMIT,
        content: o.content ? 'true' : undefined,
        types: o.types?.length ? o.types.join(',') : undefined,
        ext: o.ext?.length ? o.ext.join(',') : undefined,
        minSize: o.minSize,
        maxSize: o.maxSize,
        modifiedAfter: o.modifiedAfter?.toISOString(),
        modifiedBefore: o.modifiedBefore?.toISOString(),
      },
    });
    return parseSearchResult(body, headers);
  }

  /** Most recently modified files (never folders), newest first. */
  async recent(o: { root?: string; limit?: number } = {}): Promise<SearchResult> {
    const { body, headers } = await this.callFull('GET', '/fs/recent', { query: { root: o.root, limit: o.limit ?? DEFAULT_RESULT_LIMIT } });
    return parseSearchResult(body, headers);
  }

  /** What takes the space under a folder: its children with their total sizes, largest first. */
  async usage(path: string): Promise<UsageResult> {
    const { body, headers } = await this.callFull('GET', '/fs/usage', { query: { path } });
    return parseUsage(body, headers);
  }

  // ---------------------------------------------------------------------------
  // Share links
  // ---------------------------------------------------------------------------

  /** One-time unauthenticated download link. 403 when the agent has sharing disabled, 413 above 500 MiB. */
  async mintShareLink(path: string, expiresInSeconds = DEFAULT_SHARE_SECONDS): Promise<ShareLink> {
    return parseShareLink((await this.call('POST', '/share/mint', { json: { path, expiresInSeconds } })) as Record<string, unknown>);
  }

  /** [tokenHash] is the hash from the mint response, not the raw token. */
  async revokeShareLink(tokenHash: string): Promise<void> {
    await this.call('DELETE', `/share/${encodeURIComponent(tokenHash)}`);
  }

  async listShareLinks(): Promise<ShareLink[]> {
    const d = await this.call('GET', '/share');
    return (Array.isArray(d) ? (d as Record<string, unknown>[]) : []).map(parseShareLink);
  }
}
