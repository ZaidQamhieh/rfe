import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createSseParser, queryKeysFor } from '../src/lib/events.ts';

test('the parser yields one kind per event, across chunk boundaries, and skips pings', () => {
  const got: string[] = [];
  const feed = createSseParser((k) => got.push(k));
  feed(': ping\n\nid: 1\nevent: fi');
  feed('les\ndata: {"kind":"files","seq":1}\n\nid: 2\r\nevent: transfers\r\n\r\n');
  assert.deepEqual(got, ['files', 'transfers']);
});

test('file events also refetch the folder search, which would otherwise keep showing the old names', () => {
  assert.ok(queryKeysFor('files').some((k) => k[0] === 'fsearch'));
  assert.ok(queryKeysFor('hello').some((k) => k[0] === 'fsearch'));
});

test('kinds map to query keys, unknown kinds to none, hello to everything', () => {
  assert.deepEqual(queryKeysFor('transfers'), [['transfers']]);
  assert.deepEqual(queryKeysFor('nope'), []);
  assert.ok(queryKeysFor('hello').length > queryKeysFor('files').length);
});
