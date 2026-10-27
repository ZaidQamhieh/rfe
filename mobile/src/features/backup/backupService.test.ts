import { MemorySecureStore } from '../../core/security/secureStore';
import { exportEnvelope, importEnvelope, type BackupStorage } from './backupService';

jest.mock('expo-file-system', () => ({ Directory: class {}, File: class {}, Paths: {} }));
// The native cipher is exercised by the JVM tests; here it is a reversible stand-in that checks the passphrase.
jest.mock('../../core/native', () => ({
  backupEncrypt: async (json: string, pass: string) => JSON.stringify({ pass, json }),
  backupDecrypt: async (env: string, pass: string) => {
    const e = JSON.parse(env) as { pass: string; json: string };
    if (e.pass !== pass) throw new Error('Incorrect passphrase, or this backup file has been corrupted.');
    return e.json;
  },
  shareFileExternal: async () => true,
}));

function fakeStorage(rows: Record<string, string>, secure: Record<string, string>, failWrites = false) {
  const store = new MemorySecureStore();
  for (const [k, v] of Object.entries(secure)) store.data.set(k, v);
  let current = { ...rows };
  const s: BackupStorage & { rows: () => Record<string, string> } = {
    allRows: () => ({ ...current }),
    replaceOwned: (next) => {
      current = { ...Object.fromEntries(Object.entries(current).filter(([k]) => k.startsWith('rn_'))), ...next };
    },
    secureAll: async () => Object.fromEntries(store.data),
    secure: failWrites ? { read: store.read.bind(store), delete: store.delete.bind(store), write: async () => { throw new Error('disk full'); } } : store,
    rows: () => current,
  };
  return { s, store };
}

test('export then import on another phone restores hosts, settings and tokens, and keeps that phone\'s identity', async () => {
  const src = fakeStorage({ 'app.theme': '"dark"', rfe_hosts_v1: '[{"id":"h1"}]', rn_legacy_import_v1: '1' }, { rfe_token_h1: 'tok', rfe_device_identity_private_v1: 'SRC' });
  const env = await exportEnvelope(src.s, 'twelve chars!!');
  expect(env).not.toContain('SRC');

  const dst = fakeStorage({ 'app.theme': '"light"', 'host.old.x': '1', rn_legacy_import_v1: '1' }, { rfe_token_old: 'old', rfe_device_identity_private_v1: 'DST' });
  await importEnvelope(dst.s, env, 'twelve chars!!');
  expect(dst.s.rows()['app.theme']).toBe('"dark"');
  expect(dst.s.rows()['host.old.x']).toBeUndefined();
  expect(dst.s.rows().rn_legacy_import_v1).toBe('1');
  expect(JSON.parse(dst.s.rows().rfe_hosts_v1)).toEqual(['{"id":"h1"}']);
  expect(await dst.store.read('rfe_token_h1')).toBe('tok');
  expect(await dst.store.read('rfe_token_old')).toBeNull();
  expect(await dst.store.read('rfe_device_identity_private_v1')).toBe('DST');
});

test('a wrong passphrase touches nothing', async () => {
  const src = fakeStorage({ 'app.a': '1' }, { rfe_token_h1: 't' });
  const env = await exportEnvelope(src.s, 'twelve chars!!');
  const dst = fakeStorage({ 'app.a': '2' }, { rfe_token_x: 'x' });
  await expect(importEnvelope(dst.s, env, 'not the passphrase')).rejects.toThrow('Incorrect passphrase');
  expect(dst.s.rows()['app.a']).toBe('2');
  expect(await dst.store.read('rfe_token_x')).toBe('x');
});

test('a write failure puts the previous state back', async () => {
  const src = fakeStorage({ 'app.a': '1' }, { rfe_token_h1: 't' });
  const env = await exportEnvelope(src.s, 'twelve chars!!');
  const dst = fakeStorage({ 'app.a': '2' }, { rfe_token_x: 'x' }, true);
  await expect(importEnvelope(dst.s, env, 'twelve chars!!')).rejects.toThrow('disk full');
  expect(dst.s.rows()['app.a']).toBe('2');
});
