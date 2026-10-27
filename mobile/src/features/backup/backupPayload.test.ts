import { buildPayload, isBackedUpPrefKey, isBackedUpSecureKey, kvToPrefs, parsePayload, prefsToKv } from './backupPayload';

const rows = {
  rfe_hosts_v1: JSON.stringify([{ id: 'h1', label: 'PC' }]),
  'app.theme': '"dark"',
  rfe_photo_backup_enabled: 'true',
  rfe_count: '7',
  rfe_ratio: '0.5',
  rfe_photo_backup_albums: '["a","b"]',
  rn_legacy_import_v1: '1',
  'app.broken': '{not json',
};

test('kv rows become typed preferences; host objects become JSON strings; internal keys stay out', () => {
  const p = kvToPrefs(rows);
  expect(p['app.theme']).toEqual({ t: 'string', v: 'dark' });
  expect(p.rfe_photo_backup_enabled).toEqual({ t: 'bool', v: true });
  expect(p.rfe_count).toEqual({ t: 'int', v: 7 });
  expect(p.rfe_ratio).toEqual({ t: 'double', v: 0.5 });
  expect(p.rfe_photo_backup_albums).toEqual({ t: 'stringList', v: ['a', 'b'] });
  expect(p.rfe_hosts_v1).toEqual({ t: 'stringList', v: ['{"id":"h1","label":"PC"}'] });
  expect(p.rn_legacy_import_v1).toBeUndefined();
  expect(p['app.broken']).toEqual({ t: 'string', v: '{not json' });
});

test('preferences round-trip back to the stored form, and a crafted file cannot write internal keys', () => {
  const back = prefsToKv(kvToPrefs(rows));
  expect(back['app.theme']).toBe('"dark"');
  expect(back.rfe_photo_backup_enabled).toBe('true');
  expect(back.rfe_count).toBe('7');
  expect(JSON.parse(back.rfe_hosts_v1)).toEqual(['{"id":"h1","label":"PC"}']);
  expect(prefsToKv({ rn_legacy_import_v1: { t: 'string', v: 'x' }, 'app.ok': { t: 'bool', v: false } })).toEqual({ 'app.ok': 'false' });
  expect(isBackedUpPrefKey('rn_anything')).toBe(false);
});

test('only tokens and pins travel; the device identity and offline key never do', () => {
  const p = buildPayload({}, { rfe_token_h1: 't', rfe_fp_h1: 'f', rfe_device_identity_private_v1: 'k', rfe_device_identity_public_v1: 'p', 'rfe.offline_body_cache.aes_gcm_hkdf.v1': 'o' }, new Date('2026-09-30T12:00:00Z'));
  expect(p.secure).toEqual({ rfe_token_h1: 't', rfe_fp_h1: 'f' });
  expect(p.createdAt).toBe('2026-09-30T12:00:00.000Z');
  expect(isBackedUpSecureKey('rfe_device_identity_private_v1')).toBe(false);
});

test('a decrypted payload is validated', () => {
  const ok = parsePayload(JSON.stringify({ version: 1, createdAt: 'x', prefs: { 'app.a': { t: 'int', v: 3 } }, secure: { rfe_token_h1: 't' } }));
  expect(ok.prefs['app.a'].v).toBe(3);
  expect(() => parsePayload('nope')).toThrow('corrupted');
  expect(() => parsePayload('{"prefs":1,"secure":{}}')).toThrow('Malformed');
  expect(() => parsePayload(JSON.stringify({ prefs: { a: { t: 'blob', v: 1 } }, secure: {} }))).toThrow('Unknown preference type "blob"');
  expect(() => parsePayload(JSON.stringify({ prefs: { a: { t: 'bool', v: 'yes' } }, secure: {} }))).toThrow('corrupted');
  expect(() => parsePayload(JSON.stringify({ prefs: {}, secure: { k: 5 } }))).toThrow('corrupted');
});
