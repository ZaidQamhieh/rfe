import { isTrustedApkUrl, isUpdateAvailable, parseRelease, percent, shouldSurfaceUpdate, manifestSignatureOk } from './updateLogic';

const good = { versionName: '2.1.0', versionCode: 82, size: 1000, url: 'https://github.com/ZaidQamhieh/rfe/releases/download/v2.1.0/app.apk', sha256: 'A'.repeat(64) };

test('reads the manifest the release workflow publishes', () => {
  expect(parseRelease(good)).toEqual({ ...good, sha256: 'a'.repeat(64) });
});

test('a manifest without a usable SHA-256 is refused, so a download is never installed unchecked', () => {
  expect(parseRelease({ ...good, sha256: undefined })).toBeNull();
  expect(parseRelease({ ...good, sha256: 'short' })).toBeNull();
  expect(parseRelease({ ...good, sha256: 'g'.repeat(64) })).toBeNull();
  expect(parseRelease({ ...good, sha256: 5 })).toBeNull();
});

test('a manifest without a version code or with an APK address off GitHub is refused', () => {
  expect(parseRelease(null)).toBeNull();
  expect(parseRelease({ ...good, versionCode: 0 })).toBeNull();
  expect(parseRelease({ ...good, versionCode: '82' })).toBeNull();
  expect(parseRelease({ ...good, url: 'https://evil.example/app.apk' })).toBeNull();
  expect(parseRelease({ ...good, url: 'http://github.com/x.apk' })).toBeNull();
  expect(parseRelease({ ...good, url: undefined })).toBeNull();
});

test('trusted hosts are GitHub and its release storage, exactly', () => {
  expect(isTrustedApkUrl('https://objects.githubusercontent.com/x')).toBe(true);
  expect(isTrustedApkUrl('https://github.com/a/b')).toBe(true);
  expect(isTrustedApkUrl('https://notgithub.com/a')).toBe(false);
  expect(isTrustedApkUrl('https://github.com.evil.example/a')).toBe(false);
  expect(isTrustedApkUrl('nonsense')).toBe(false);
});

test('newer than installed, and the banner respects a dismissal', () => {
  const r = parseRelease(good)!;
  expect(isUpdateAvailable(81, r)).toBe(true);
  expect(isUpdateAvailable(82, r)).toBe(false);
  expect(isUpdateAvailable(81, null)).toBe(false);
  expect(shouldSurfaceUpdate(r, 81)).toBe(true);
  expect(shouldSurfaceUpdate(r, 82)).toBe(false);
  expect(shouldSurfaceUpdate(null, 0)).toBe(false);
});

test('percent clamps and tolerates an unknown total', () => {
  expect(percent(50, 200)).toBe(25);
  expect(percent(300, 200)).toBe(100);
  expect(percent(5, 0)).toBe(0);
});

// Signed exactly as .github/workflows/release.yml does it (openssl pkeyutl -rawin | xxd) with the real release key.
const SIGNED_MANIFEST = '{"versionName":"x"}\n';
const SIGNATURE = '6607c554580a6b468616cfe531be2dd6f52f1de0ae3ab1a93dd01de136e8db3e7048f49d45c1b95401fd8079eac13967f41d9348f133b98055f3c3484c1b1300';

test('the pinned release key accepts only the manifest it signed', () => {
  expect(manifestSignatureOk(SIGNED_MANIFEST, SIGNATURE)).toBe(true);
  expect(manifestSignatureOk(SIGNED_MANIFEST, SIGNATURE + '\n')).toBe(true);
  expect(manifestSignatureOk(SIGNED_MANIFEST.replace('x', 'y'), SIGNATURE)).toBe(false);
  expect(manifestSignatureOk(SIGNED_MANIFEST, '')).toBe(false);
  expect(manifestSignatureOk(SIGNED_MANIFEST, 'zz'.repeat(64))).toBe(false);
  expect(manifestSignatureOk(SIGNED_MANIFEST, '00'.repeat(64))).toBe(false);
  expect(manifestSignatureOk(SIGNED_MANIFEST, SIGNATURE, '11'.repeat(32))).toBe(false);
});
