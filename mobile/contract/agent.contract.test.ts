/**
 * Live contract test: the app's AgentClient, pairing service and pinning rule against a REAL rfe process.
 * Skipped unless RFE_AGENT_BIN points at a built agent (CI builds it; locally: `go build -o /tmp/rfe
 * ./cmd/agent` in agent/, then `RFE_AGENT_BIN=/tmp/rfe npx jest contract`).
 *
 * It also holds the app-side security cases (rfe-c1x.5): paths outside the jail, symlink escapes, read-only
 * mode, an unreachable host, name conflicts and stale writes.
 */
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { createHash, randomBytes, X509Certificate } from 'node:crypto';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

import { AgentApiError, AgentClient } from '../src/core/api/agentClient';
import { CertPinMismatch } from '../src/core/api/pin';
import { DeviceIdentity } from '../src/core/security/deviceIdentity';
import { MemorySecureStore } from '../src/core/security/secureStore';
import { HostStore, MemoryKeyValueStore } from '../src/core/storage/hostStore';
import { awaitPairing, loginWithAccount, PairRejected, requestPairing } from '../src/features/pairing/pairingService';
import { nodeTransport } from './nodeTransport';

const BIN = process.env.RFE_AGENT_BIN;
const live = BIN ? describe : describe.skip;
const PASSWORD = 'contract-pass-1';

type Running = { proc: ChildProcess; address: string; dataDir: string; root: string; pin: string };

async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address() as net.AddressInfo;
      s.close(() => resolve(port));
    });
    s.on('error', reject);
  });
}

async function startAgent(o: { readOnly?: boolean; seed?: (root: string) => void } = {}): Promise<Running> {
  const base = fs.mkdtempSync(path.join(os.tmpdir(), 'rfe-contract-'));
  const root = path.join(base, 'root');
  const dataDir = path.join(base, 'data');
  fs.mkdirSync(root, { recursive: true });
  fs.mkdirSync(dataDir, { recursive: true });
  o.seed?.(root);
  const added = spawnSync(BIN!, ['adduser', '-data', dataDir, '-password', PASSWORD, 'owner'], { encoding: 'utf8' });
  if (added.status !== 0) throw new Error(`adduser failed: ${added.stdout}${added.stderr}`);
  const port = await freePort();
  const args = ['serve', '-addr', `127.0.0.1:${port}`, '-data', dataDir, '-roots', root, '-name', 'contract-host'];
  if (o.readOnly) args.push('-read-only');
  const proc = spawn(BIN!, args, { stdio: 'ignore' });
  const address = `127.0.0.1:${port}`;
  const transport = nodeTransport();
  const probe = new AgentClient({ id: 'p', label: 'p', address }, { transport });
  for (let i = 0; i < 100; i++) {
    try {
      await probe.health();
      break;
    } catch {
      if (i === 99) throw new Error('agent did not start');
      await new Promise((r) => setTimeout(r, 100));
    }
  }
  const pem = fs.readFileSync(path.join(dataDir, 'agent-cert.pem'), 'utf8');
  const pin = createHash('sha256').update(new X509Certificate(pem).raw).digest('hex');
  return { proc, address, dataDir, root, pin };
}

async function pairedClient(a: Running): Promise<AgentClient> {
  const store = new HostStore(new MemoryKeyValueStore(), new MemorySecureStore());
  const identity = new DeviceIdentity(new MemorySecureStore(), (n) => new Uint8Array(randomBytes(n)));
  const host = await loginWithAccount({ transport: nodeTransport(), identity, store, deviceId: async () => null, probe: async () => a.pin, randomBytes: (n) => new Uint8Array(randomBytes(n)) }, { address: a.address, fingerprint: a.pin }, { username: 'owner', password: PASSWORD });
  return new AgentClient(host, { transport: nodeTransport(), deviceToken: (await store.getToken(host.id)) ?? undefined, pinnedFingerprint: await store.getPin(host.id) });
}

const stop = (a: Running) => {
  a.proc.kill();
  fs.rmSync(path.dirname(a.root), { recursive: true, force: true });
};

const codeOf = async (p: Promise<unknown>) => {
  try {
    await p;
    return 'OK';
  } catch (e) {
    return e instanceof AgentApiError ? `${e.statusCode}:${e.code}` : `THROWN:${(e as Error).name}`;
  }
};

live('agent contract (read/write)', () => {
  let a: Running;
  let c: AgentClient;

  beforeAll(async () => {
    a = await startAgent({
      seed: (root) => {
        fs.mkdirSync(path.join(root, 'docs'));
        fs.writeFileSync(path.join(root, 'docs', 'a.txt'), 'hello\n');
        fs.writeFileSync(path.join(root, 'docs', 'copy.txt'), 'hello\n');
        fs.writeFileSync(path.join(root, 'findme-report.txt'), 'x');
        // A link inside the jail pointing outside it.
        const outside = fs.mkdtempSync(path.join(os.tmpdir(), 'rfe-outside-'));
        fs.writeFileSync(path.join(outside, 'secret.txt'), 'top secret');
        fs.symlinkSync(outside, path.join(root, 'escape'));
      },
    });
    c = await pairedClient(a);
  }, 60_000);
  afterAll(() => stop(a));

  it('reports health, status and the allowed root', async () => {
    const h = await c.health();
    expect(h.status).toBe('ok');
    expect(h.name).toBe('contract-host');
    const s = await c.status();
    expect(s.roots.map((r) => fs.realpathSync(r))).toEqual([fs.realpathSync(a.root)]);
    expect(s.accessDenied).toBe(false);
  });

  it('lists, reads metadata and checksums like the filesystem does', async () => {
    const l = await c.list(a.root);
    expect(l.entries.map((e) => e.name)).toEqual(expect.arrayContaining(['docs', 'findme-report.txt']));
    const m = await c.meta(path.join(a.root, 'docs', 'a.txt'));
    expect(m.size).toBe(6);
    expect(await c.checksum(m.path)).toBe(createHash('sha256').update('hello\n').digest('hex'));
    const batch = await c.batchChecksums([m.path, path.join(a.root, 'docs', 'copy.txt')]);
    expect(Object.values(batch)).toEqual([createHash('sha256').update('hello\n').digest('hex'), createHash('sha256').update('hello\n').digest('hex')]);
  });

  it('creates, writes with optimistic concurrency, renames, copies, moves and deletes', async () => {
    const dir = path.join(a.root, 'work');
    await c.createFolder(dir);
    const f = await c.createFile(path.join(dir, 'n.txt'));
    // The agent compares modification times at one-second resolution, so an edit in the same second as the
    // read is not detected; wait past it so the stale write below is deterministic.
    await new Promise((r) => setTimeout(r, 1100));
    const saved = await c.putContent(f.path, 'one', f.modified);
    expect(fs.readFileSync(f.path, 'utf8')).toBe('one');
    // A write based on an older version is refused.
    expect(await codeOf(c.putContent(f.path, 'two', f.modified))).toBe('409:STALE_WRITE');
    expect(fs.readFileSync(f.path, 'utf8')).toBe('one');
    await c.putContent(f.path, 'two', saved.modified);
    const r = await c.rename(f.path, path.join(dir, 'renamed.txt'));
    expect(r.name).toBe('renamed.txt');
    const cp = await c.copy([r.path], a.root);
    expect(cp.failed).toEqual([]);
    expect(fs.existsSync(path.join(a.root, 'renamed.txt'))).toBe(true);
    const mv = await c.move([path.join(a.root, 'renamed.txt')], path.join(a.root, 'docs'));
    expect(mv.failed).toEqual([]);
    expect(fs.existsSync(path.join(a.root, 'docs', 'renamed.txt'))).toBe(true);
    const del = await c.delete([path.join(a.root, 'docs', 'renamed.txt')]);
    expect(del.failed).toEqual([]);
    const trash = await c.listTrash();
    const item = trash.find((t) => t.name === 'renamed.txt');
    expect(item).toBeDefined();
    const back = await c.restoreTrash([item!.id]);
    expect(back.failed).toEqual([]);
    expect(fs.existsSync(path.join(a.root, 'docs', 'renamed.txt'))).toBe(true);
    await c.delete([path.join(a.root, 'docs', 'renamed.txt')], { permanent: true });
    expect(fs.existsSync(path.join(a.root, 'docs', 'renamed.txt'))).toBe(false);
  });

  it('reports name conflicts per item, and resolves them with keep-both and overwrite', async () => {
    const src = path.join(a.root, 'docs', 'a.txt');
    const dest = path.join(a.root, 'clash');
    await c.createFolder(dest);
    fs.writeFileSync(path.join(dest, 'a.txt'), 'existing');
    const clash = await c.copy([src], dest);
    expect(clash.failed).toHaveLength(1);
    expect(clash.failed[0].errorCode).toBe('CONFLICT');
    expect(fs.readFileSync(path.join(dest, 'a.txt'), 'utf8')).toBe('existing');
    const both = await c.copy([src], dest, { duplicate: true });
    expect(both.failed).toEqual([]);
    expect(fs.readdirSync(dest).sort()).toEqual(['a (1).txt', 'a.txt']);
    const over = await c.copy([src], dest, { overwrite: true });
    expect(over.failed).toEqual([]);
    expect(fs.readFileSync(path.join(dest, 'a.txt'), 'utf8')).toBe('hello\n');
  });

  it('compresses, lists and extracts an archive', async () => {
    const zip = await c.compress([path.join(a.root, 'docs', 'a.txt')], path.join(a.root, 'docs', 'pack.zip'));
    expect(zip.name).toBe('pack.zip');
    expect((await c.archiveList(zip.path)).map((e) => e.path)).toContain('a.txt');
    const out = path.join(a.root, 'unpacked');
    await c.createFolder(out);
    await c.extract(zip.path, out);
    expect(fs.readFileSync(path.join(out, 'a.txt'), 'utf8')).toBe('hello\n');
  });

  it('searches by name, with filters, and lists recent files', async () => {
    const hit = await c.search({ q: 'findme' });
    expect(hit.entries.map((e) => e.name)).toContain('findme-report.txt');
    const glob = await c.search({ q: 'findme*', types: ['document', 'other'] });
    expect(glob.entries.length).toBeGreaterThan(0);
    const none = await c.search({ q: 'findme', minSize: 1024 * 1024 });
    expect(none.entries).toEqual([]);
    const recent = await c.recent({ limit: 5 });
    expect(recent.entries.length).toBeGreaterThan(0);
    expect(recent.entries.filter((e) => e.isDir).map((e) => e.name)).toEqual([]);
  });

  it('answers share minting with a coded error while sharing is off, then mints, lists and revokes once the owner turns it on', async () => {
    const file = path.join(a.root, 'docs', 'a.txt');
    expect(await codeOf(c.mintShareLink(file))).toBe('403:FORBIDDEN');
    expect((await c.updateSettings({ allowSharing: true })).allowSharing).toBe(true);
    const link = await c.mintShareLink(file, 120);
    expect(link.url).toContain(link.token);
    expect(link.tokenHash).not.toBe('');
    expect((await c.listShareLinks()).map((l) => l.tokenHash)).toContain(link.tokenHash);
    await c.revokeShareLink(link.tokenHash);
    expect((await c.listShareLinks()).map((l) => l.tokenHash)).not.toContain(link.tokenHash);
  });

  it('lets the owner change host settings, and reads them back', async () => {
    const s = await c.status();
    expect(s.isAdmin).toBe(true);
    expect(s.isAdminKnown).toBe(true);
    expect((await c.updateSettings({ agentName: 'renamed-host' })).agentName).toBe('renamed-host');
    expect((await c.status()).agentName).toBe('renamed-host');
  });

  it('round-trips bandwidth limits', async () => {
    expect(await c.getBandwidth()).toEqual({ maxUploadBytesPerSec: 0, maxDownloadBytesPerSec: 0 });
    expect(await c.setBandwidth({ maxUploadBytesPerSec: 1024 * 1024 })).toEqual({ maxUploadBytesPerSec: 1024 * 1024, maxDownloadBytesPerSec: 0 });
    expect((await c.getBandwidth()).maxUploadBytesPerSec).toBe(1024 * 1024);
    await c.setBandwidth({ maxUploadBytesPerSec: 0 });
  });

  it('lists this device and records the login in the audit trail', async () => {
    const devices = await c.listDevices();
    const me = devices.find((d) => d.current);
    expect(me).toBeDefined();
    expect(me!.viaLogin).toBe(true);
    expect(me!.revoked).toBe(false);
    const audit = await c.audit({ limit: 20 });
    expect(audit.map((e) => e.action)).toEqual(expect.arrayContaining(['login']));
  });

  it('a device removes itself and its token stops working at once', async () => {
    const me = (await c.listDevices()).find((d) => d.current)!;
    await c.deleteDevice(me.id);
    expect(await codeOf(c.list(a.root))).toMatch(/^401:/);
  });
});

live('agent contract (security)', () => {
  let a: Running;
  let c: AgentClient;
  let outsideDir: string;

  beforeAll(async () => {
    a = await startAgent({
      seed: (root) => {
        fs.writeFileSync(path.join(root, 'inside.txt'), 'inside');
        outsideDir = fs.mkdtempSync(path.join(os.tmpdir(), 'rfe-outside-'));
        fs.writeFileSync(path.join(outsideDir, 'secret.txt'), 'top secret');
        fs.symlinkSync(outsideDir, path.join(root, 'escape'));
        fs.symlinkSync(path.join(outsideDir, 'secret.txt'), path.join(root, 'link-to-secret'));
      },
    });
    c = await pairedClient(a);
  }, 60_000);
  afterAll(() => stop(a));

  it('refuses paths outside the shared root, including dot-dot climbs', async () => {
    expect(await codeOf(c.list(outsideDir))).not.toBe('OK');
    expect(await codeOf(c.list(`${a.root}/../..`))).not.toBe('OK');
    expect(await codeOf(c.meta(path.join(a.root, '..', path.basename(outsideDir), 'secret.txt')))).not.toBe('OK');
    expect(await codeOf(c.checksum(path.join(outsideDir, 'secret.txt')))).not.toBe('OK');
  });

  it('does not follow symlinks out of the root for listing, metadata, checksums or writes', async () => {
    expect(await codeOf(c.list(path.join(a.root, 'escape')))).not.toBe('OK');
    expect(await codeOf(c.checksum(path.join(a.root, 'escape', 'secret.txt')))).not.toBe('OK');
    expect(await codeOf(c.checksum(path.join(a.root, 'link-to-secret')))).not.toBe('OK');
    expect(await codeOf(c.putContent(path.join(a.root, 'link-to-secret'), 'overwritten'))).not.toBe('OK');
    expect(await codeOf(c.createFile(path.join(a.root, 'escape', 'planted.txt')))).not.toBe('OK');
    expect(fs.readFileSync(path.join(outsideDir, 'secret.txt'), 'utf8')).toBe('top secret');
    expect(fs.existsSync(path.join(outsideDir, 'planted.txt'))).toBe(false);
  });

  it('rejects a certificate that does not match the pin before any request is made', async () => {
    const wrong = new AgentClient({ id: 'x', label: 'x', address: a.address }, { transport: nodeTransport(), deviceToken: 'tok', pinnedFingerprint: '0'.repeat(64) });
    await expect(wrong.list(a.root)).rejects.toBeInstanceOf(CertPinMismatch);
  });

  it('rejects requests with a bad or missing bearer token', async () => {
    const anon = new AgentClient({ id: 'x', label: 'x', address: a.address }, { transport: nodeTransport(), deviceToken: 'not-a-real-token', pinnedFingerprint: a.pin });
    expect(await codeOf(anon.list(a.root))).toMatch(/^401:/);
  });

  it('surfaces an unreachable host as a coded connection error', async () => {
    const dead = new AgentClient({ id: 'x', label: 'x', address: `127.0.0.1:${await freePort()}` }, { transport: nodeTransport() });
    expect(await codeOf(dead.health())).toBe('0:CONNECTION');
  });
});

live('agent contract (read-only host)', () => {
  let a: Running;
  let c: AgentClient;
  beforeAll(async () => {
    a = await startAgent({ readOnly: true, seed: (root) => fs.writeFileSync(path.join(root, 'r.txt'), 'x') });
    c = await pairedClient(a);
  }, 60_000);
  afterAll(() => stop(a));

  it('lets reads through and refuses every kind of write', async () => {
    expect((await c.status()).readOnly).toBe(true);
    expect((await c.list(a.root)).entries.map((e) => e.name)).toContain('r.txt');
    const file = path.join(a.root, 'r.txt');
    const attempts: [string, () => Promise<unknown>][] = [
      ['createFolder', () => c.createFolder(path.join(a.root, 'nope'))],
      ['createFile', () => c.createFile(path.join(a.root, 'nope.txt'))],
      ['putContent', () => c.putContent(file, 'changed')],
      ['rename', () => c.rename(file, path.join(a.root, 'moved.txt'))],
    ];
    for (const [name, attempt] of attempts) expect([name, await codeOf(attempt())]).toEqual([name, '403:READ_ONLY']);
    // Batch endpoints answer 200 and report the refusal per item.
    const del = await c.delete([file]);
    expect(del.failed.map((f) => f.errorCode)).toEqual(['READ_ONLY']);
    const mv = await c.move([file], path.join(a.root, 'elsewhere'));
    expect(mv.failed.length).toBe(1);
    expect(fs.readFileSync(file, 'utf8')).toBe('x');
    expect(fs.readdirSync(a.root)).toEqual(['r.txt']);
  });
});

live('agent contract (approve-on-computer pairing)', () => {
  let a: Running;
  beforeAll(async () => {
    a = await startAgent();
  }, 60_000);
  afterAll(() => stop(a));

  const deps = () => {
    const secure = new MemorySecureStore();
    const store = new HostStore(new MemoryKeyValueStore(), secure);
    return { store, deps: { transport: nodeTransport(), identity: new DeviceIdentity(secure, (n: number) => new Uint8Array(randomBytes(n))), store, deviceId: async () => null, probe: async () => a.pin, randomBytes: (n: number) => new Uint8Array(randomBytes(n)) } };
  };
  const cli = (verb: string, ...rest: string[]) => spawnSync(BIN!, ['pair', verb, '-data', a.dataDir, ...rest], { encoding: 'utf8' });

  it('approving on the computer completes pairing, and the match code equals the one the computer lists', async () => {
    const { store, deps: d } = deps();
    const h = await requestPairing(d, a.address);
    const listed = cli('requests').stdout;
    expect(listed).toContain(h.matchCode);
    const paired = awaitPairing(d, h, { intervalMs: 100 });
    expect(cli('accept', h.requestId.slice(0, 8)).status).toBe(0);
    const host = await paired;
    const client = new AgentClient(host, { transport: nodeTransport(), deviceToken: (await store.getToken(host.id)) ?? undefined, pinnedFingerprint: await store.getPin(host.id) });
    expect((await client.health()).status).toBeDefined();
    expect(await client.drives()).toBeDefined();
  });

  it('rejecting on the computer ends the request without a device', async () => {
    const { store, deps: d } = deps();
    const h = await requestPairing(d, a.address);
    const paired = awaitPairing(d, h, { intervalMs: 100 });
    expect(cli('reject', h.requestId.slice(0, 8)).status).toBe(0);
    await expect(paired).rejects.toBeInstanceOf(PairRejected);
    expect(await store.listHosts()).toEqual([]);
  });

  // Needs a notification service that presses Approve: run the suite under `dbus-run-session` with
  // contract/fake_notification_server.py started on that private bus, and RFE_NOTIFY_STUB=1.
  const stubbed = process.env.RFE_NOTIFY_STUB === '1' ? it : it.skip;
  stubbed('pressing Approve in the desktop notification completes pairing', async () => {
    const { store, deps: d } = deps();
    const h = await requestPairing(d, a.address);
    const host = await awaitPairing(d, h, { intervalMs: 100 });
    expect(await store.getToken(host.id)).toBeTruthy();
    expect(cli('requests').stdout).not.toContain(h.matchCode);
  }, 20_000);
});
