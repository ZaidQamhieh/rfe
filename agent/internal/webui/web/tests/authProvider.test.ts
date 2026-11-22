// Run with `npm test`. The auth context value must keep its identity between renders that change no auth state,
// otherwise every consumer of useAuth() re-renders whenever the provider's parent does.
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { test } from 'node:test';
import { createElement, memo, useState } from 'react';
import TestRenderer from 'react-test-renderer';

// Same test-only resolver as tests/sessionStatus.test.ts.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if ((specifier.startsWith('./') || specifier.startsWith('../')) && !/\.[cm]?[jt]s$/.test(specifier)) {
      return nextResolve(`${specifier}.ts`, context);
    }
    return nextResolve(specifier, context);
  },
});
const { api } = await import('../src/lib/api.ts');
const { AuthContext, useAuth } = await import('../src/lib/auth-context.ts');
const { useAuthProvider } = await import('../src/lib/useAuthProvider.ts');

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const { act } = TestRenderer;

// The agent is the network boundary: a signed-in session, and a logout that succeeds.
(api as { status: unknown }).status = async () => ({});
(api as { logoutSession: unknown }).logoutSession = async () => {};

/** Mounts the provider hook over one memoized consumer; returns what was seen and a way to re-render the parent. */
async function mount() {
  const values: ReturnType<typeof useAuthProvider>[] = [];
  const counts = { consumer: 0 };
  let bump = () => {};
  const Consumer = memo(function Consumer() {
    useAuth();
    counts.consumer += 1;
    return null;
  });
  function Parent() {
    const [, setN] = useState(0);
    bump = () => setN((n) => n + 1);
    const value = useAuthProvider();
    values.push(value);
    return createElement(AuthContext.Provider, { value }, createElement(Consumer));
  }
  let renderer!: TestRenderer.ReactTestRenderer;
  await act(async () => {
    renderer = TestRenderer.create(createElement(Parent));
  });
  return { values, counts, bump: () => act(async () => bump()), unmount: () => act(async () => renderer.unmount()) };
}

test('a parent re-render with no auth change keeps the context value and does not re-render consumers', async () => {
  const m = await mount();
  const rendersBefore = m.values.length;
  const consumerBefore = m.counts.consumer;
  const valueBefore = m.values.at(-1);
  for (let i = 0; i < 3; i++) await m.bump();
  assert.equal(m.values.length, rendersBefore + 3, 'the parent really re-rendered');
  assert.equal(m.values.at(-1), valueBefore, 'same context value object');
  assert.equal(m.counts.consumer, consumerBefore, 'consumer render count unchanged');
  await m.unmount();
});

test('an auth change publishes a new value and re-renders consumers once', async () => {
  const m = await mount();
  const consumerBefore = m.counts.consumer;
  const before = m.values.at(-1)!;
  assert.equal(before.authenticated, true);
  await act(async () => before.logout());
  const after = m.values.at(-1)!;
  assert.notEqual(after, before);
  assert.equal(after.authenticated, false);
  assert.equal(m.counts.consumer, consumerBefore + 1);
  await m.unmount();
});
