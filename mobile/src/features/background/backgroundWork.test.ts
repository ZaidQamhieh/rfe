import { runBackgroundWork, type BackgroundDeps } from './backgroundWork';

function deps(o: Partial<BackgroundDeps> = {}) {
  const calls: string[] = [];
  const d: BackgroundDeps = {
    photoBackupScheduled: async () => true,
    runPhotoBackup: async () => {
      calls.push('backup');
      return { kind: 'enqueued', count: 2 };
    },
    waitForPhotoUploads: async (ms) => void calls.push(`wait:${ms}`),
    updateCheck: async () => void calls.push('update'),
    ...o,
  };
  return { d, calls };
}

test('runs the photo backup, waits for its uploads, then checks for an update', async () => {
  const { d, calls } = deps();
  const r = await runBackgroundWork(d, 1000);
  expect(calls).toEqual(['backup', 'wait:1000', 'update']);
  expect(r).toEqual({ photos: { kind: 'enqueued', count: 2 }, update: 'done' });
});

test('does not touch photos when automatic backup is off, and does not wait when nothing was queued', async () => {
  const off = deps({ photoBackupScheduled: async () => false });
  expect((await runBackgroundWork(off.d)).photos).toBe('off');
  expect(off.calls).toEqual(['update']);

  const idle = deps({ runPhotoBackup: async () => ({ kind: 'upToDate' }) });
  await runBackgroundWork(idle.d);
  expect(idle.calls).toEqual(['update']);
});

test('a failure in one part never skips the other', async () => {
  const a = deps({ runPhotoBackup: async () => { throw new Error('boom'); } });
  expect(await runBackgroundWork(a.d)).toEqual({ photos: 'error', update: 'done' });
  expect(a.calls).toEqual(['update']);
  const b = deps({ updateCheck: async () => { throw new Error('offline'); } });
  const r = await runBackgroundWork(b.d, 5);
  expect(r.update).toBe('error');
  expect(b.calls).toContain('backup');
});
