// Run with `npm test`. Pins what a Settings save sends: only what the user edited, on top of the agent's current values.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { applyRootChange, bandwidthBody } from '../src/lib/settingsEdit.ts';

const latest = { maxUploadBytesPerSec: 500, maxDownloadBytesPerSec: 900 };

test('an untouched limit keeps the current value instead of being sent as 0', () => {
  assert.deepEqual(bandwidthBody(latest, { upload: 700 }), { maxUploadBytesPerSec: 700, maxDownloadBytesPerSec: 900 });
  assert.deepEqual(bandwidthBody(latest, { download: 0 }), { maxUploadBytesPerSec: 500, maxDownloadBytesPerSec: 0 });
});

test('an edit that equals the current values, or no edit, sends nothing', () => {
  assert.equal(bandwidthBody(latest, {}), null);
  assert.equal(bandwidthBody(latest, { upload: 500, download: 900 }), null);
});

test('a folder is added to, or removed from, the list as it is now', () => {
  assert.deepEqual(applyRootChange(['/a', '/b'], { add: '/c' }), ['/a', '/b', '/c']);
  assert.deepEqual(applyRootChange(['/a', '/b'], { remove: '/a' }), ['/b']);
  assert.deepEqual(applyRootChange(['/a'], { add: '/a' }), ['/a']);
  assert.deepEqual(applyRootChange(['/a'], { remove: '/zzz' }), ['/a']);
});

test('two changes in a row, each applied to the result of the one before, keep both', () => {
  const afterFirst = applyRootChange(['/a'], { add: '/b' });
  assert.deepEqual(applyRootChange(afterFirst, { add: '/c' }), ['/a', '/b', '/c']);
});
