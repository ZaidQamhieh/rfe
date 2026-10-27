import type { Entry } from '../../core/api/models';
import { fileUriFromPath, isSafeName, needsSync, pathFromFileUri } from './syncLogic';

const remote = (o: Partial<Entry>): Entry => ({ name: 'a.txt', path: '/a.txt', isDir: false, isSymlink: false, size: 10, modified: '2026-09-30T10:00:00Z', ...o });
const t0 = Date.parse('2026-09-30T10:00:00Z');

test('a file is downloaded when missing, resized, or changed after the local copy', () => {
  expect(needsSync(remote({}), undefined)).toBe(true);
  expect(needsSync(remote({}), { size: 9, modified: t0 + 1000 })).toBe(true);
  expect(needsSync(remote({}), { size: 10, modified: t0 - 1000 })).toBe(true);
});

test('an up-to-date file is left alone, including when a time is missing', () => {
  expect(needsSync(remote({}), { size: 10, modified: t0 + 1000 })).toBe(false);
  expect(needsSync(remote({ modified: undefined }), { size: 10, modified: t0 })).toBe(false);
  expect(needsSync(remote({}), { size: 10, modified: null })).toBe(false);
  expect(needsSync(remote({ size: undefined }), { size: 0, modified: t0 + 1 })).toBe(false);
});

test('only plain names are accepted', () => {
  expect(isSafeName('a b.txt')).toBe(true);
  for (const bad of ['', '.', '..', 'a/b', 'a\\b', '../x']) expect(isSafeName(bad)).toBe(false);
});

test('native paths round-trip through file URIs, including # % ? and spaces (F-111)', () => {
  const p = '/data/user/0/app/cache/sync/r1/report#1 100%?.pdf';
  const uri = fileUriFromPath(p);
  expect(uri).toBe('file:///data/user/0/app/cache/sync/r1/report%231%20100%25%3F.pdf');
  expect(uri.includes('#')).toBe(false);
  expect(pathFromFileUri(uri)).toBe(p);
  expect(pathFromFileUri('file:///c/100%.txt')).toBe('/c/100%.txt');
});
