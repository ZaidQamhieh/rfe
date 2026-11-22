// Run with `npm test`. Pins how the Files page flattens the loaded pages of a folder.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { entriesOfPages } from '../src/lib/listing.ts';

const e = (path: string) => ({ name: path.split('/').pop() ?? path, path, isDir: false, size: 1 });

test('pages are joined in order', () => {
  const got = entriesOfPages([{ entries: [e('/a'), e('/b')] }, { entries: [e('/c')] }]);
  assert.deepEqual(got.map((x) => x.path), ['/a', '/b', '/c']);
});

test('an entry repeated on a later page (the folder changed between requests) is listed once', () => {
  const got = entriesOfPages([{ entries: [e('/a'), e('/b')] }, { entries: [e('/b'), e('/c')] }]);
  assert.deepEqual(got.map((x) => x.path), ['/a', '/b', '/c']);
});

test('no pages is an empty list', () => {
  assert.deepEqual(entriesOfPages(undefined), []);
  assert.deepEqual(entriesOfPages([]), []);
});
