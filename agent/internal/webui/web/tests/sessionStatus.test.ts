// Run with `npm test`. Pins which failures of the session check sign the user out.
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { test } from 'node:test';

// Same test-only resolver as tests/api.test.ts: Node's loader needs the .ts extension that Vite adds.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if ((specifier.startsWith('./') || specifier.startsWith('../')) && !/\.[cm]?[jt]s$/.test(specifier)) {
      return nextResolve(`${specifier}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});
const { ApiError } = await import('../src/lib/api.ts');
const { isSessionRefused } = await import('../src/lib/sessionStatus.ts');

test('only an agent refusal of the session signs the user out', () => {
  assert.equal(isSessionRefused(new ApiError(401, 'UNAUTHORIZED', 'x')), true);
  assert.equal(isSessionRefused(new ApiError(403, 'FORBIDDEN', 'x')), true);
});

test('server errors and network failures keep the session', () => {
  assert.equal(isSessionRefused(new ApiError(500, 'INTERNAL', 'x')), false);
  assert.equal(isSessionRefused(new ApiError(503, 'HTTP_503', 'x')), false);
  assert.equal(isSessionRefused(new TypeError('Failed to fetch')), false);
  assert.equal(isSessionRefused(undefined), false);
});
