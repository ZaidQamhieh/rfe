// The destructive-action gate must run the action only when the owner says yes, and show the message it was given.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { confirmDestructive } from '../src/lib/confirm.ts';
import { parseBandwidthLimit } from '../src/lib/bandwidthInput.ts';

test('confirmDestructive passes the message through and returns the answer', () => {
  const seen: string[] = [];
  assert.equal(confirmDestructive('Delete x?', (m) => (seen.push(m), true)), true);
  assert.equal(confirmDestructive('Delete y?', () => false), false);
  assert.deepEqual(seen, ['Delete x?']);
});

test('an empty bandwidth field means unlimited', () => {
  assert.equal(parseBandwidthLimit(''), 0);
  assert.equal(parseBandwidthLimit('  '), 0);
});

test('a whole number is accepted as typed', () => {
  assert.equal(parseBandwidthLimit('1048576'), 1048576);
  assert.equal(parseBandwidthLimit(' 0 '), 0);
});

test('anything else is rejected rather than turned into unlimited', () => {
  for (const bad of ['abc', '-5', '1.5', '1e3', '12 34', '99999999999999999999']) {
    assert.equal(parseBandwidthLimit(bad), null, bad);
  }
});