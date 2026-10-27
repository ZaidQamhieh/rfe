import { SHARE_EXPIRY_PRESETS, sharedFiles } from './shareLogic';

const f = (path: string, fileName = 'a.txt', size: number | null = 5) => ({ path, fileName, size });

test('keeps file URIs and adds the scheme to bare paths', () => {
  expect(sharedFiles([f('file:///c/a.txt'), f('/c/b.txt', 'b.txt', null)])).toEqual([
    { name: 'a.txt', uri: 'file:///c/a.txt', size: 5 },
    { name: 'b.txt', uri: 'file:///c/b.txt', size: undefined },
  ]);
});

test('drops content URIs and empty paths, tolerates null', () => {
  expect(sharedFiles([f('content://x/1'), f('')])).toEqual([]);
  expect(sharedFiles(null)).toEqual([]);
});

test('falls back to the decoded last path segment for a missing name', () => {
  expect(sharedFiles([f('file:///c/my%20doc.pdf', '')])[0].name).toBe('my doc.pdf');
});

test('a shared name cannot climb out of the folder or the staging directory', () => {
  expect(sharedFiles([f('file:///c/x.txt', '../../etc/passwd')])[0].name).toBe('passwd');
  expect(sharedFiles([f('file:///c/x.txt', '..\\..\\evil.exe')])[0].name).toBe('evil.exe');
  expect(sharedFiles([f('file:///c/..%2F..%2Fsecret.txt', '')])[0].name).toBe('secret.txt');
  expect(sharedFiles([f('file:///c/x.txt', '..')])[0].name).toBe('file');
});

test('a malformed percent sequence in the path does not throw', () => {
  expect(sharedFiles([f('file:///c/100%.txt', '')])[0].name).toBe('100%.txt');
});

test('share expiry presets are 15 min, 1 h and 24 h, ascending, none above the agent cap', () => {
  expect(SHARE_EXPIRY_PRESETS.map((p) => p.seconds)).toEqual([900, 3600, 86400]);
  expect(Math.max(...SHARE_EXPIRY_PRESETS.map((p) => p.seconds))).toBeLessThanOrEqual(24 * 3600);
});
