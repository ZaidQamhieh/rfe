// Run with `npm test`. Pins the path helpers the Files page uses: `/` paths keep their old results, and a
// Windows drive path (C:\...) joins and breaks into crumbs with backslashes.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { crumbsOf, joinPath, splitPath } from '../src/lib/requests.ts';

test('joinPath puts a name under the root and under folders with /', () => {
  assert.equal(joinPath('/', 'a'), '/a');
  assert.equal(joinPath('', 'a'), '/a');
  assert.equal(joinPath('/x', 'a'), '/x/a');
  assert.equal(joinPath('/x/', 'a'), '/x/a');
});

test('joinPath uses backslashes only for a Windows drive or UNC path', () => {
  assert.equal(joinPath('C:\\', 'a'), 'C:\\a');
  assert.equal(joinPath('C:\\x', 'a'), 'C:\\x\\a');
  assert.equal(joinPath('\\\\server\\share\\', 'a'), '\\\\server\\share\\a');
  // A Linux folder whose name contains a backslash still joins with /.
  assert.equal(joinPath('/x/a\\b', 'c'), '/x/a\\b/c');
});

test('splitPath drops empty segments', () => {
  assert.deepEqual(splitPath('/a//b/'), ['a', 'b']);
  assert.deepEqual(splitPath('/'), []);
});

test('splitPath splits a POSIX path only on /, so a backslash in a name stays in it', () => {
  assert.deepEqual(splitPath('/x/a\\b/c'), ['x', 'a\\b', 'c']);
  assert.deepEqual(splitPath('C:\\x/y\\z'), ['C:', 'x', 'y', 'z']);
});

test('crumbsOf keeps a backslash inside a POSIX name as one crumb', () => {
  assert.deepEqual(crumbsOf('/x/a\\b'), [
    { label: 'x', full: '/x' },
    { label: 'a\\b', full: '/x/a\\b' },
  ]);
});

test('crumbsOf roots a UNC path at its server and share', () => {
  assert.deepEqual(crumbsOf('\\\\srv\\share\\a\\b'), [
    { label: '\\\\srv\\share', full: '\\\\srv\\share\\' },
    { label: 'a', full: '\\\\srv\\share\\a' },
    { label: 'b', full: '\\\\srv\\share\\a\\b' },
  ]);
  assert.deepEqual(crumbsOf('\\\\srv\\share'), [{ label: '\\\\srv\\share', full: '\\\\srv\\share\\' }]);
});

test('crumbsOf builds the full path up to each segment', () => {
  assert.deepEqual(crumbsOf('/'), []);
  assert.deepEqual(crumbsOf('/a/b'), [
    { label: 'a', full: '/a' },
    { label: 'b', full: '/a/b' },
  ]);
  assert.deepEqual(crumbsOf('C:\\a\\b'), [
    { label: 'C:', full: 'C:\\' },
    { label: 'a', full: 'C:\\a' },
    { label: 'b', full: 'C:\\a\\b' },
  ]);
});
