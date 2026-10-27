// Run with `npm test` (node's built-in runner; Node strips the types). These pin the request bodies the web client
// sends to what protocol/openapi.yaml and the agent's handlers actually read.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mintBody, renameBody, restoreFailure, searchParams, trashIdsBody, wolBody } from '../src/lib/requests.ts';

test('rename sends src and the full destination path, not path and newName', () => {
  assert.deepEqual(renameBody('/home/x/a.txt', 'b.txt'), { src: '/home/x/a.txt', dst: '/home/x/b.txt' });
  assert.deepEqual(renameBody('/a.txt', 'b.txt'), { src: '/a.txt', dst: '/b.txt' });
  assert.deepEqual(renameBody('C:\\docs\\a.txt', 'b.txt'), { src: 'C:\\docs\\a.txt', dst: 'C:\\docs\\b.txt' });
  assert.deepEqual(renameBody('C:\\a.txt', 'b.txt'), { src: 'C:\\a.txt', dst: 'C:\\b.txt' });
});

test('trash calls name ids as an array, never a bare id', () => {
  assert.deepEqual(trashIdsBody(['7']), { ids: ['7'] });
  assert.deepEqual(trashIdsBody(['1', '2']), { ids: ['1', '2'] });
});

test('search narrows to a folder with root, the parameter the agent reads', () => {
  const p = searchParams('cat', '/home/x');
  assert.equal(p.get('q'), 'cat');
  assert.equal(p.get('root'), '/home/x');
  assert.equal(p.has('path'), false);
  assert.equal(searchParams('cat').has('root'), false);
});

test('share mint uses expiresInSeconds and omits it when unset', () => {
  assert.deepEqual(mintBody('/a.txt', 3600), { path: '/a.txt', expiresInSeconds: 3600 });
  assert.deepEqual(mintBody('/a.txt'), { path: '/a.txt' });
});

test('wake-on-LAN sends mac', () => {
  assert.deepEqual(wolBody('aa:bb:cc:dd:ee:ff'), { mac: 'aa:bb:cc:dd:ee:ff' });
});

test('restoreFailure reports the first item the agent could not restore, and null when all worked', () => {
  assert.equal(restoreFailure({ results: [{ path: '/a', ok: true }] }), null);
  assert.equal(restoreFailure({ results: [{ path: '/a', ok: true }, { path: '/b', ok: false, error: { code: 'NOT_FOUND', message: 'gone' } }] }), 'gone');
  assert.equal(restoreFailure({ results: [{ path: '/b', ok: false }] }), 'Could not restore it');
  // No result at all must not read as success.
  assert.equal(restoreFailure({ results: [] }), 'Could not restore it');
  assert.equal(restoreFailure(undefined), 'Could not restore it');
});
