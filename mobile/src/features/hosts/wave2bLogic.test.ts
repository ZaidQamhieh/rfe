import type { Device, PairRequest } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { computerMenuEntries, computerMenuTarget } from './computerMenuLogic';
import { canBlockPhone, canEditPhone, folderProblem, onlyInsideLabel, pendingPhoneSubtitle, phoneSubtitle, phoneToggles, withoutRequest } from './phonesLogic';
import { homeStateView, switcherRowState } from './switcherLogic';

const host = (over: Partial<Host> = {}): Host => ({ id: 'h1', label: 'Studio PC', address: '192.168.1.20:8765', ...over });
const status = (over: Partial<Parameters<typeof switcherRowState>[0]> = {}) => ({ online: false, checking: false, untrusted: false, activeAddress: null, lastSeen: null, ...over });
const NOW = new Date('2026-11-10T12:00:00Z');

describe('switcherRowState', () => {
  it('online has no action and a normal picture', () => {
    const s = switcherRowState(status({ online: true, activeAddress: '192.168.1.20:8765' }), host(), NOW);
    expect(s).toMatchObject({ kind: 'online', off: false, action: null, phrase: 'Connected' });
  });
  it('checking is neither off nor actionable', () => {
    expect(switcherRowState(status({ checking: true }), host(), NOW)).toMatchObject({ kind: 'checking', off: false, action: null });
  });
  it('offline with a network-card address is asleep and offers Wake, with when it was last seen', () => {
    const s = switcherRowState(status({ lastSeen: new Date(NOW.getTime() - 3 * 3600_000) }), host({ macAddress: 'aa:bb:cc:dd:ee:ff' }), NOW);
    expect(s).toMatchObject({ kind: 'asleep', off: true, action: 'wake' });
    expect(s.phrase).toMatch(/^Not running · seen /);
  });
  it('asleep without a last-seen time just says Asleep', () => {
    expect(switcherRowState(status(), host({ macAddress: 'aa:bb:cc:dd:ee:ff' }), NOW).phrase).toBe('Not running');
  });
  it('offline with no network-card address is not reachable and has no action', () => {
    expect(switcherRowState(status(), host(), NOW)).toMatchObject({ kind: 'unreachable', off: true, action: null, phrase: 'Not reachable' });
  });
  it('a changed identity wins over everything and offers Review', () => {
    expect(switcherRowState(status({ online: true, untrusted: true }), host({ macAddress: 'aa:bb:cc:dd:ee:ff' }), NOW)).toMatchObject({ kind: 'untrusted', action: 'review', phrase: 'Its identity changed' });
  });
});

describe('computer menu', () => {
  it('lists the five entries and one rule in the mockup order', () => {
    const e = computerMenuEntries('Studio PC', true);
    expect(e.map((x) => x.key)).toEqual(['trash', 'apps', 'phones', 'about', 'rule', 'settings']);
    expect(e.find((x) => x.key === 'apps')).toMatchObject({ label: 'Apps on Studio PC', disabled: false });
  });
  it('greys Apps out when the computer has no apps to show', () => {
    expect(computerMenuEntries('Studio PC', false).find((x) => x.key === 'apps')).toMatchObject({ disabled: true });
  });
  it('routes per-computer entries with the computer id and the rest without', () => {
    expect(computerMenuTarget('phones', 'h1')).toEqual({ method: 'push', pathname: '/host/[id]/phones', params: { id: 'h1' } });
    expect(computerMenuTarget('about', 'h1')).toEqual({ method: 'push', pathname: '/host/[id]/about', params: { id: 'h1' } });
    expect(computerMenuTarget('trash', 'h1')).toEqual({ method: 'push', pathname: '/trash' });
    expect(computerMenuTarget('apps', 'h1')).toEqual({ method: 'push', pathname: '/host/[id]/apps', params: { id: 'h1' } });
    expect(computerMenuTarget('settings', 'h1')).toEqual({ method: 'push', pathname: '/host/[id]/settings', params: { id: 'h1' } });
  });
});

const device = (over: Partial<Device> = {}): Device => ({ id: 'd1', label: 'Leila’s phone', created: 0, lastSeen: NOW.getTime() - 2 * 3600_000, revoked: false, current: false, viaLogin: false, lastAddress: '', lastVersion: '', jailRoot: '', browse: true, download: true, upload: false, modify: false, delete: false, share: false, ...over });

describe('phones with access', () => {
  it('shows the six toggles in order with their values', () => {
    expect(phoneToggles(device()).map((x) => [x.label, x.on])).toEqual([
      ['See files', true],
      ['Save to phone', true],
      ['Send files', false],
      ['Rename and move', false],
      ['Delete', false],
      ['Share links', false],
    ]);
  });
  it('adds the Wake computer toggle only when the computer reports that grant', () => {
    expect(phoneToggles(device()).map((x) => x.cap)).not.toContain('wake');
    expect(phoneToggles(device({ wake: false })).at(-1)).toEqual({ cap: 'wake', label: 'Wake computer', on: false });
    expect(phoneToggles(device({ wake: true })).at(-1)).toMatchObject({ cap: 'wake', on: true });
    expect(canEditPhone(device(), true)).toBe(true);
  });
  it('describes a phone by role, block state or last use', () => {
    expect(phoneSubtitle(device({ viaLogin: true, current: true }), NOW)).toBe('Owner · everything');
    expect(phoneSubtitle(device({ revoked: true }), NOW)).toBe('Blocked · cannot get in');
    expect(phoneSubtitle(device({ lastSeen: 0 }), NOW)).toBe('Not used yet');
    expect(phoneSubtitle(device(), NOW)).toMatch(/^Last used /);
  });
  it('only an owner edits a phone that has grants of its own and is not blocked', () => {
    expect(canEditPhone(device(), true)).toBe(true);
    expect(canEditPhone(device(), false)).toBe(false);
    expect(canEditPhone(device({ viaLogin: true }), true)).toBe(false);
    expect(canEditPhone(device({ revoked: true }), true)).toBe(false);
  });
  it('Block applies to another phone that is not blocked yet', () => {
    expect(canBlockPhone(device())).toBe(true);
    expect(canBlockPhone(device({ current: true }))).toBe(false);
    expect(canBlockPhone(device({ revoked: true }))).toBe(false);
  });
});

describe('waiting phones and the folder a phone is kept inside', () => {
  const req = (over: Partial<PairRequest> = {}): PairRequest => ({ id: 'r1', label: 'Mum’s Galaxy', matchCode: '6150 2283', remoteIp: '10.0.0.5', replaces: '', expiresAt: 0, ...over });

  it('a waiting phone shows the number to compare, and whom it would take over', () => {
    expect(pendingPhoneSubtitle(req())).toBe('Asking to come in · number 6150 2283');
    expect(pendingPhoneSubtitle(req({ replaces: 'Old phone' }))).toBe('Asking to take over Old phone · number 6150 2283');
    expect(pendingPhoneSubtitle(req({ matchCode: '' }))).toBe('Asking to come in');
  });
  it('answering removes only that request', () => {
    expect(withoutRequest([req({ id: 'a' }), req({ id: 'b' })], 'a').map((r) => r.id)).toEqual(['b']);
    expect(withoutRequest([req({ id: 'a' })], 'zzz')).toHaveLength(1);
  });
  it('Only inside names the folder, or says anywhere when there is none', () => {
    expect(onlyInsideLabel(device({ jailRoot: '/srv/leila' }))).toBe('/srv/leila');
    expect(onlyInsideLabel(device({ jailRoot: '' }))).toBe('Anywhere on this computer');
  });
  it('the folder prompt accepts full paths and blank, and rejects anything else', () => {
    for (const ok of ['', '   ', '/srv/leila', 'C:\\Users\\Leila', 'c:/data', '\\\\nas\\share']) expect(folderProblem(ok)).toBeNull();
    for (const bad of ['leila', '../x', 'Documents/Leila', 'C:']) expect(folderProblem(bad)).toMatch(/full path/);
  });
});

describe('homeStateView', () => {
  it('offline with a network-card address is asleep, without one it is lost', () => {
    expect(homeStateView(status(), host({ macAddress: 'aa:bb:cc:dd:ee:ff' }))).toBe('asleep');
    expect(homeStateView(status(), host())).toBe('lost');
  });
  it('shows nothing while checking, online, or when the identity changed', () => {
    expect(homeStateView(status({ checking: true }), host())).toBeNull();
    expect(homeStateView(status({ online: true }), host())).toBeNull();
    expect(homeStateView(status({ untrusted: true }), host())).toBeNull();
  });
});
