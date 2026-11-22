import type { Host } from '../../core/models/host';
import type { TransferRecord } from '../../core/native';
import { pathCrumb, placesFor, stateLine } from './nameplateLogic';
import { activeTransferCount, stripModel } from './transferStrip';

const host = { id: 'h1', label: 'Studio PC', address: '192.168.1.20:8765', tailscaleAddress: '100.64.0.2:8765' } as Host;
const base = { online: true, checking: false, untrusted: false, activeAddress: '192.168.1.20:8765', lastSeen: null as Date | null };

describe('stateLine', () => {
  it('is a green lamp that says Connected', () => expect(stateLine(base, host)).toEqual({ lamp: 'go', phrase: 'Connected' }));
  it('waits while checking, whatever else is true', () => expect(stateLine({ ...base, checking: true, online: false }, host).lamp).toBe('wait'));
  it('stops on a changed identity', () => expect(stateLine({ ...base, untrusted: true, online: false }, host).lamp).toBe('stop'));
  it('stops, in words, when the computer no longer accepts this phone even though it answers', () => expect(stateLine({ ...base, refused: true, online: true }, host)).toEqual({ lamp: 'stop', phrase: 'No longer accepts this phone' }));
  it('is hollow and says Not reachable when offline', () => expect(stateLine({ ...base, online: false }, host)).toEqual({ lamp: 'off', phrase: 'Not reachable' }));
  it('says Not running with last seen when its network-card address is known', () => {
    const now = new Date('2026-01-01T12:00:00Z');
    const woken = { ...host, macAddress: 'aa:bb:cc:dd:ee:ff' } as Host;
    expect(stateLine({ ...base, online: false, lastSeen: new Date('2026-01-01T11:00:00Z') }, woken, now)).toMatchObject({ lamp: 'off', phrase: expect.stringContaining('Not running · seen') });
  });
});

describe('placesFor', () => {
  it('lists shared folders by name, sorted', () => {
    const p = placesFor({ kind: 'roots', roots: ['/srv/Zed', '/home/me/Docs'] }, null);
    expect(p.map((x) => x.title)).toEqual(['Docs', 'Zed']);
    expect(p[0].path).toBe('/home/me/Docs');
  });
  it('lists drives with free space', () => {
    const p = placesFor({ kind: 'drives' }, [{ path: 'D:\\', label: 'Media', totalBytes: 4000, freeBytes: 1000, isOS: false }]);
    expect(p).toHaveLength(1);
    expect(p[0]).toMatchObject({ kind: 'drive', title: 'Media', path: 'D:\\' });
    expect(p[0].subtitle).toContain('free of');
  });
  it('has no rows when access is denied or drives are not loaded', () => {
    expect(placesFor({ kind: 'denied' }, null)).toEqual([]);
    expect(placesFor({ kind: 'drives' }, null)).toEqual([]);
  });
  it('offers the whole computer when it opens straight into one root', () => {
    expect(placesFor({ kind: 'select', rootPath: '/' }, null)).toMatchObject([{ path: '/', title: 'All files' }]);
  });
});

describe('pathCrumb', () => {
  it('shows the two folders above the item', () => expect(pathCrumb('/home/zaid/Documents/Taxes')).toBe('zaid › Documents'));
  it('uses the folder itself at the top', () => expect(pathCrumb('/Taxes')).toBe('Taxes'));
  it('handles Windows paths', () => expect(pathCrumb('D:\\Audio\\Band')).toBe('D: › Audio'));
});

const rec = (id: string, over: Partial<TransferRecord> = {}): TransferRecord => ({
  id,
  hostId: 'h1',
  address: '10.0.0.2:8765',
  remotePath: `/docs/${id}.txt`,
  destPath: `/data/${id}.txt`,
  state: 'RUNNING',
  received: 0,
  total: 0,
  error: null,
  direction: 'UPLOAD',
  ...over,
});
const size = (n: number) => `${n}B`;
const names = { h1: 'Studio PC' };

describe('activeTransferCount', () => {
  it('counts running, waiting and paused but not finished or failed', () => {
    const all = [rec('a'), rec('b', { state: 'QUEUED' }), rec('c', { state: 'PAUSED' }), rec('d', { state: 'DONE' }), rec('e', { state: 'FAILED' }), rec('f', { state: 'CANCELLED' })];
    expect(activeTransferCount(all)).toBe(3);
  });
  it('is zero for an empty list', () => expect(activeTransferCount([])).toBe(0));
});

describe('stripModel', () => {
  it('is null when nothing is in progress', () => expect(stripModel([rec('a', { state: 'DONE' })], names, size)).toBeNull());
  it('names the file and the computer for one upload', () => {
    const m = stripModel([rec('a', { received: 50, total: 200 })], names, size);
    expect(m?.title).toBe('Sending a.txt to Studio PC');
    expect(m?.line).toBe('50B of 200B');
    expect(m?.progress).toBeCloseTo(0.25);
    expect(m?.paused).toBe(false);
  });
  it('counts files for several downloads and sums their bytes', () => {
    const m = stripModel([rec('a', { direction: 'DOWNLOAD', received: 100, total: 200 }), rec('b', { direction: 'DOWNLOAD', received: 100, total: 200 })], names, size);
    expect(m?.title).toBe('Receiving 2 files from Studio PC');
    expect(m?.progress).toBeCloseTo(0.5);
  });
  it('names neither direction nor computer when transfers disagree', () => {
    expect(stripModel([rec('a'), rec('b', { direction: 'DOWNLOAD' })], names, size)?.title).toBe('Moving 2 files');
  });
  it('leaves the computer out when its name is unknown or there are several', () => {
    expect(stripModel([rec('a')], {}, size)?.title).toBe('Sending a.txt');
    expect(stripModel([rec('a'), rec('b', { hostId: 'h2' })], names, size)?.title).toBe('Sending 2 files');
  });
  it('offers resume when everything is paused, else pauses only what runs', () => {
    expect(stripModel([rec('a', { state: 'PAUSED', total: 10, received: 5 })], names, size)).toMatchObject({ paused: true, targetIds: ['a'], line: 'Paused · 5B of 10B' });
    const mixed = stripModel([rec('a'), rec('b', { state: 'PAUSED' })], names, size);
    expect(mixed?.paused).toBe(false);
    expect(mixed?.targetIds).toEqual(['a']);
  });
  it('shows no detail line and an empty bar while sizes are unknown', () => {
    const m = stripModel([rec('a')], names, size);
    expect(m?.line).toBe('');
    expect(m?.progress).toBe(0);
  });
});
