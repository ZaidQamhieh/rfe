// Run with `npm test`. Pins the display numbers the pages showed before the shared formatters, including the
// decimal rate (Overview) and binary rate (Settings) that differ on purpose.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { bytesToGB, formatBytes, rateParts } from '../src/lib/format.ts';

test('formatBytes keeps whole bytes bare and scales with one decimal', () => {
  assert.equal(formatBytes(0), '0 B');
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(1536), '1.5 KB');
  assert.equal(formatBytes(5 * 1024 * 1024), '5.0 MB');
});

test('formatBytes moves up a unit when one decimal rounds to 1024', () => {
  assert.equal(formatBytes(1048575), '1.0 MB');
  assert.equal(formatBytes(1024 * 1024 * 1024 - 1), '1.0 GB');
  assert.equal(formatBytes(1023.94 * 1024), '1023.9 KB');
  // The top unit has nowhere to go.
  assert.equal(formatBytes(1024 ** 4 * 1023.99), '1024.0 TB');
});

test('rateParts uses decimal units for base 1000 (Overview)', () => {
  assert.deepEqual(rateParts(1_500_000, 1000), { value: '1.5', unit: 'MB/s' });
  assert.deepEqual(rateParts(900_000, 1000), { value: '900', unit: 'KB/s' });
});

test('rateParts uses binary units for base 1024 (Settings)', () => {
  assert.deepEqual(rateParts(1.5 * 1024 * 1024, 1024), { value: '1.5', unit: 'MB/s' });
  assert.deepEqual(rateParts(512 * 1024, 1024), { value: '512', unit: 'KB/s' });
});

test('bytesToGB is decimal gigabytes', () => {
  assert.equal(bytesToGB(2e9), 2);
});
