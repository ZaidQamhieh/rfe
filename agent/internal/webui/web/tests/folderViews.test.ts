// Run with `npm test`. A change made in this browser must refresh the folder listing and the search results
// without waiting for the agent's files event.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { QueryClient } from '@tanstack/react-query';

import { invalidateFolderViews } from '../src/lib/folderViews.ts';

function seeded() {
  const qc = new QueryClient();
  for (const key of [['listing', '/a'], ['listing', '/b'], ['fsearch', '/a', 'q'], ['fsearch', '/b', 'q'], ['trash']]) {
    qc.setQueryData(key, []);
  }
  const stale = (key: unknown[]) => qc.getQueryState(key)!.isInvalidated;
  return { qc, stale };
}

test('after a folder is created, that folder\'s listing and every search are marked stale', () => {
  const { qc, stale } = seeded();
  invalidateFolderViews(qc, '/a');
  assert.equal(stale(['listing', '/a']), true);
  assert.equal(stale(['fsearch', '/a', 'q']), true);
  assert.equal(stale(['fsearch', '/b', 'q']), true);
  assert.equal(stale(['listing', '/b']), false, 'another folder\'s listing is left alone');
  assert.equal(stale(['trash']), false);
});

test('after an upload with no folder given, every listing and every search are marked stale', () => {
  const { qc, stale } = seeded();
  invalidateFolderViews(qc);
  assert.equal(stale(['listing', '/a']), true);
  assert.equal(stale(['listing', '/b']), true);
  assert.equal(stale(['fsearch', '/a', 'q']), true);
  assert.equal(stale(['fsearch', '/b', 'q']), true);
  assert.equal(stale(['trash']), false);
});
