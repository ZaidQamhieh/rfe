import { sha256 } from '@noble/hashes/sha2.js';

import {
  AgentApiError,
  abortError,
  AgentClient,
  BAD_RESPONSE,
  isAbortError,
  type Transport,
} from "../../core/api/agentClient";
import { t } from "../../i18n";
import {
  hasFeature,
  isMissingRoute,
  unsupportedByAgent,
} from "../../core/api/compat";
import type { Health, PairResponse } from "../../core/api/models";
import {
  CertPinMismatch,
  MissingCertPin,
  normalizeFingerprint,
} from "../../core/api/pin";
import type { Host } from "../../core/models/host";
import type { DeviceIdentity } from "../../core/security/deviceIdentity";
import { sleep as defaultSleep } from "../../core/sleep";
import { ignoreError } from "../../core/log";
import type { HostStore } from "../../core/storage/hostStore";
import { hexToBytes, matchCode } from "./matchCode";

/** A few dropped or busy polls in a row are retried; this many in a row fail the pairing. */
const MAX_TRANSIENT_POLL_ERRORS = 5;
/** Delay between approval polls when the caller does not set one. */
const DEFAULT_POLL_MS = 2000;
/** Device label sent with a pairing request when the caller sets none. */
const DEFAULT_DEVICE_LABEL = "Mobile App";

export type PairingDeps = {
  transport: Transport;
  identity: DeviceIdentity;
  store: HostStore;
  /** Android ID; lets a re-pair of the same phone reuse its device row. */
  deviceId: () => Promise<string | null>;
  deviceLabel?: string;
  /** TLS handshake only: the leaf SHA-256 the host at `https://<address>` presents. Never sends a request. */
  probe: (url: string) => Promise<string>;
  randomBytes: (n: number) => Uint8Array;
  /** Called with the computer's id after a pairing is saved, to forget its transfer rows that failed because the phone was refused. */
  clearStaleTransfers?: (hostId: string) => Promise<void>;
};

export type PairingTarget = { address: string; fingerprint: string };

/** Fields of the pairing QR: `{"address","certFingerprint","pairingCode"}`. */
export type PairingQr = { address: string; certFingerprint: string; pairingCode: string };

export type QrParseError = 'invalidQrFormat' | 'qrMissingFields' | 'qrInvalidFingerprint';

export function parsePairingQr(raw: string): { ok: true; qr: PairingQr } | { ok: false; error: QrParseError } {
  let json: unknown;
  try {
    json = JSON.parse(raw);
  } catch {
    return { ok: false, error: 'invalidQrFormat' };
  }
  if (typeof json !== 'object' || json === null || Array.isArray(json)) return { ok: false, error: 'invalidQrFormat' };
  const j = json as Record<string, unknown>;
  if (typeof j.address !== 'string' || typeof j.certFingerprint !== 'string' || typeof j.pairingCode !== 'string') {
    return { ok: false, error: 'qrMissingFields' };
  }
  const fp = normalizeFingerprint(j.certFingerprint);
  if (fp === null) return { ok: false, error: 'qrInvalidFingerprint' };
  return { ok: true, qr: { address: j.address, certFingerprint: fp, pairingCode: j.pairingCode } };
}

/**
 * What the device key signs (agent: security.DeviceProofMessageV2): the certificate this phone pinned plus the
 * agent's nonce. A relay that passes another agent's nonce to this phone gets a signature that names the wrong
 * certificate, so it cannot replay it at the real agent.
 */
export const proofMessage = (fingerprintHex: string, nonce: string) => `rfe-device-proof-v2\n${fingerprintHex.toLowerCase()}\n${nonce}`;

async function proof(client: AgentClient, identity: DeviceIdentity, fingerprintHex: string) {
  // The challenge goes through the pinned client too: a wrong host is rejected
  // before anything (even a nonce request) is exchanged.
  const nonce = await client.challenge();
  return {
    devicePublicKey: await identity.publicKeyBase64(),
    nonce,
    signature: await identity.signBase64(proofMessage(fingerprintHex, nonce)),
  };
}

const sameAddress = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/**
 * Before a password is sent to [target]: refuses a host already saved at this address under a different
 * certificate (the pin is never silently replaced), and reports whether the certificate is one this phone already
 * trusts. A first contact is not known; the caller shows its fingerprint for confirmation.
 */
export async function checkKnownPin(deps: PairingDeps, target: PairingTarget): Promise<'known' | 'new'> {
  const same = (await deps.store.listHosts()).filter((h) => sameAddress(h.address, target.address));
  if (same.some((h) => normalizeFingerprint(h.certFingerprint) !== target.fingerprint)) throw new CertPinMismatch();
  return same.length > 0 ? 'known' : 'new';
}

const incompleteResponse = () => new AgentApiError(0, BAD_RESPONSE, t('pairingResponseIncomplete'));

const deviceIdFor = async (deps: PairingDeps): Promise<string | undefined> => (await deps.deviceId()) ?? undefined;

async function commit(deps: PairingDeps, target: PairingTarget, resp: PairResponse): Promise<Host> {
  const pin = normalizeFingerprint(target.fingerprint);
  if (pin === null) throw new MissingCertPin();
  if (!resp.deviceToken || !resp.deviceId) throw incompleteResponse();
  // The same computer (same pinned certificate) paired again keeps its saved entry, so its name, favorites, kept
  // folders and backups stay; only the address, pin and token are refreshed. The computer may hand out a new device id
  // (it was removed there, or the phone's id changed), and that must not add a second copy of the same machine.
  // Found by the pinned certificate (the record's mirror, else the pin in the secure store, so a record saved before the
  // mirror existed still matches), or failing that by the device id the computer hands out: that is the same entry.
  const saved = await deps.store.listHosts();
  let known = saved.find((h) => normalizeFingerprint(h.certFingerprint) === pin);
  for (const h of known ? [] : saved) {
    if ((await deps.store.getPin(h.id)) === pin) {
      known = h;
      break;
    }
  }
  known ??= saved.find((h) => h.id === resp.deviceId);
  const host: Host = {
    ...known,
    id: known?.id ?? resp.deviceId,
    label: known?.label ?? (resp.agentName || target.address),
    address: target.address.trim(),
    certFingerprint: pin,
    tailscaleAddress: resp.tailscaleAddress ?? known?.tailscaleAddress,
  };
  await deps.store.commitPairing(host, { token: resp.deviceToken, fingerprint: pin });
  // The committed entry is listed last, so it is the copy a merge keeps. A failed merge is retried at the next app
  // start and must never fail a pairing that already succeeded.
  await deps.store.mergeDuplicates().catch(ignoreError('merge duplicate computers after pairing'));
  await deps.clearStaleTransfers?.(host.id).catch(ignoreError('clear stale transfers after pairing'));
  return host;
}

function pinnedClient(deps: PairingDeps, target: PairingTarget): AgentClient {
  const pin = normalizeFingerprint(target.fingerprint);
  if (pin === null) throw new MissingCertPin();
  return new AgentClient(
    { id: 'pairing_probe', label: 'probe', address: target.address.trim(), certFingerprint: pin },
    { transport: deps.transport, pinnedFingerprint: pin },
  );
}

export async function pairWithCode(deps: PairingDeps, target: PairingTarget, pairingCode: string): Promise<Host> {
  const client = pinnedClient(deps, target);
  const p = await proof(client, deps.identity, target.fingerprint);
  const resp = await client.pair({
    pairingCode: pairingCode.trim(),
    deviceLabel: deps.deviceLabel ?? DEFAULT_DEVICE_LABEL,
    ...p,
    deviceId: await deviceIdFor(deps),
  });
  return commit(deps, target, resp);
}

export async function loginWithAccount(
  deps: PairingDeps,
  target: PairingTarget,
  creds: { username: string; password: string },
): Promise<Host> {
  await checkKnownPin(deps, target);
  const client = pinnedClient(deps, target);
  const p = await proof(client, deps.identity, target.fingerprint);
  const resp = await client.login({
    username: creds.username.trim(),
    password: creds.password,
    deviceLabel: deps.deviceLabel ?? DEFAULT_DEVICE_LABEL,
    ...p,
    deviceId: await deviceIdFor(deps),
  });
  return commit(deps, target, resp);
}

/**
 * "Sign in with account server": [accountClient] is a client for a saved computer (the account server) this phone
 * already logged in to with a password. The member computer (the pinned [target]) issues a challenge, the account
 * server signs an assertion that names this phone's key, that challenge and the member's certificate, and the member
 * accepts it at /login. The password never reaches the member, and the assertion is used once, never stored or logged.
 * The member decides whether it trusts that account server; this phone only relays.
 */
export async function loginWithAccountServer(
  deps: PairingDeps,
  target: PairingTarget,
  accountClient: AgentClient,
): Promise<Host> {
  await checkKnownPin(deps, target);
  const pin = normalizeFingerprint(target.fingerprint);
  if (pin === null) throw new MissingCertPin();
  const client = pinnedClient(deps, target);
  const p = await proof(client, deps.identity, pin);
  const { assertion } = await accountClient.accountAssertion({
    audience: pin,
    devicePublicKey: p.devicePublicKey,
    memberNonce: p.nonce,
  });
  const resp = await client.login({
    assertion,
    deviceLabel: deps.deviceLabel ?? DEFAULT_DEVICE_LABEL,
    ...p,
    deviceId: await deviceIdFor(deps),
  });
  return commit(deps, target, resp);
}

export async function registerAccount(
  deps: PairingDeps,
  target: PairingTarget,
  a: { pairingCode: string; username: string; password: string },
): Promise<Host> {
  await checkKnownPin(deps, target);
  const client = pinnedClient(deps, target);
  const p = await proof(client, deps.identity, target.fingerprint);
  const resp = await client.register({
    pairingCode: a.pairingCode.trim(),
    username: a.username.trim(),
    password: a.password,
    deviceLabel: deps.deviceLabel ?? DEFAULT_DEVICE_LABEL,
    ...p,
    deviceId: await deviceIdFor(deps),
  });
  return commit(deps, target, resp);
}

/** The owner rejected the request on the computer. */
export class PairRejected extends Error {
  constructor() {
    super('The computer declined this pairing request.');
    this.name = 'PairRejected';
  }
}

/** Nobody answered on the computer in time. */
export class PairExpired extends Error {
  constructor() {
    super('The pairing request timed out. Ask again and approve it on the computer.');
    this.name = 'PairExpired';
  }
}

export type PairRequestHandle = { target: PairingTarget; requestId: string; clientNonce: string; matchCode: string; expiresInSeconds: number };

/**
 * Learns the certificate the host at [address] presents (first contact, trust on first use) so the user never has to
 * see or type a fingerprint; every later request is pinned to it.
 */
export async function probeTarget(deps: PairingDeps, address: string): Promise<PairingTarget> {
  const authority = address.trim().replace(/^https?:\/\//, '').replace(/\/+$/, '');
  const fp = normalizeFingerprint(await deps.probe(`https://${authority}`));
  if (fp === null) throw new MissingCertPin();
  return { address: authority, fingerprint: fp };
}

const hex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

const mapMissingRoute = (e: unknown): never => {
  throw isMissingRoute(e) ? unsupportedByAgent("pairRequest") : e;
};

/** Older agents have /pair/request but take the nonce up front: they answer the commit-reveal request with 400. */
const mapOlderPairProtocol = (e: unknown): never => {
  if (
    e instanceof AgentApiError &&
    e.statusCode === 400 &&
    e.code === "BAD_REQUEST"
  )
    throw unsupportedByAgent("pairRequest");
  return mapMissingRoute(e);
};

/**
 * Asks the computer at [address] to approve this phone; nothing is typed. The certificate the host presents is
 * pinned on first contact (the user never sees or enters it); the match code the computer shows is derived from that
 * pin, so a machine relaying the connection produces a code that differs from the one on the computer's screen.
 */
export async function requestPairing(
  deps: PairingDeps,
  address: string,
  knownHealth?: Health | null,
): Promise<PairRequestHandle> {
  // Health needs a token, so a first pairing usually has none: then the route itself answers, and 404/405 maps below.
  if (!hasFeature(knownHealth, "pairRequest"))
    throw unsupportedByAgent("pairRequest");
  const target = await probeTarget(deps, address);
  const fp = target.fingerprint;
  const client = pinnedClient(deps, target);
  const p = await proof(client, deps.identity, fp);
  // Commit to the nonce first and reveal it only once the agent has chosen the request id. A relay that
  // could see the nonce before the id exists could grind an id that makes both match codes agree.
  const clientNonce = hex(deps.randomBytes(16));
  const deviceId = await deviceIdFor(deps);
  const r = await client
    .requestPair({
      deviceLabel: deps.deviceLabel ?? DEFAULT_DEVICE_LABEL,
      clientNonceCommit: hex(sha256(hexToBytes(clientNonce))),
      ...p,
      deviceId,
    })
    .catch(mapOlderPairProtocol);
  if (!r.requestId) throw incompleteResponse();
  await client.revealPair(r.requestId, clientNonce).catch(mapMissingRoute);
  return {
    target,
    requestId: r.requestId,
    clientNonce,
    matchCode: matchCode(fp, clientNonce, r.requestId),
    expiresInSeconds: r.expiresInSeconds,
  };
}

/** Polls until the owner answers; resolves with the paired host. Aborting stops polling (the request lapses on the computer). */
export async function awaitPairing(
  deps: PairingDeps,
  h: PairRequestHandle,
  o: { signal?: AbortSignal; intervalMs?: number; sleep?: (ms: number) => Promise<void>; now?: () => number } = {},
): Promise<Host> {
  const client = pinnedClient(deps, h.target);
  const sleep = o.sleep ?? defaultSleep;
  const now = o.now ?? Date.now;
  const deadline = now() + (h.expiresInSeconds + 5) * 1000;
  let transient = 0;
  while (!o.signal?.aborted) {
    if (now() > deadline) throw new PairExpired();
    let s: Awaited<ReturnType<AgentClient['pairRequestStatus']>> | undefined;
    // Only the status request is retried; saving the approved host below is outside this try, so a failure to save
    // is a real failure and is never mistaken for a dropped poll.
    try {
      s = await client.pairRequestStatus(h.requestId, h.clientNonce);
      transient = 0;
    } catch (e) {
      if (isAbortError(e)) throw e;
      if (e instanceof CertPinMismatch || e instanceof MissingCertPin) throw e;
      if (e instanceof AgentApiError && e.code === 'NOT_FOUND') throw new PairExpired();
      // A dropped connection or a busy agent is retried; a few in a row is a real failure.
      if (++transient >= MAX_TRANSIENT_POLL_ERRORS) throw e;
    }
    if (s) {
      // Cancelled while this request was in flight: do not save the host and token the person just backed out of.
      if (o.signal?.aborted) throw abortError();
      if (s.status === 'approved') return commit(deps, h.target, s.response);
      if (s.status === 'rejected') throw new PairRejected();
    }
    await sleep(o.intervalMs ?? DEFAULT_POLL_MS);
  }
  throw abortError();
}
