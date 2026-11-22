import type { TransferRecord } from '../../core/native';
import { AUTO_RETRY_DELAYS_MS, resetAutoRetry, scheduleAutoRetries, type AutoRetryTracks } from './autoRetry';
import { isAutoRetryable } from './transferLogic';

const SEC = 1000;
const CONN = 'ERR_CONNECTION: timeout';

const rec = (id: string, over: Partial<TransferRecord> = {}): TransferRecord => ({
  id,
  hostId: 'h1',
  address: 'a',
  remotePath: '/Docs/a.txt',
  destPath: 'content://a',
  state: 'RUNNING',
  received: 0,
  total: 100,
  error: null,
  ...over,
});
const failed = (id: string, error: string | null = CONN) => rec(id, { state: 'FAILED', error });

/** One scheduling tick with the app in the foreground. */
const tick = (records: TransferRecord[], tracks: AutoRetryTracks, now: number) => scheduleAutoRetries(records, tracks, now, true);

describe('isAutoRetryable', () => {
  it('is true only for a failed connection error', () => {
    expect(isAutoRetryable(failed('a'))).toBe(true);
    expect(isAutoRetryable(failed('b', 'HASH_MISMATCH: x'))).toBe(false);
    expect(isAutoRetryable(failed('c', 'CONFLICT: exists'))).toBe(false);
    expect(isAutoRetryable(rec('d', { state: 'RUNNING', error: CONN }))).toBe(false);
    expect(isAutoRetryable(failed('e', null))).toBe(false);
  });
});

describe('scheduleAutoRetries', () => {
  it('waits 10 s after the failure, resumes once, and does not resume again on a stale failed record', () => {
    const t0 = 1_000_000;
    let s = tick([failed('a')], new Map(), t0);
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, t0 + 10 * SEC - 1);
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, t0 + 10 * SEC);
    expect(s.resume).toEqual(['a']);
    s = tick([failed('a')], s.tracks, t0 + 11 * SEC);
    expect(s.resume).toEqual([]);
  });

  it('waits 1 min, then 5 min, each counted from the failure, and then stops for good', () => {
    const at = (sec: number) => 1_000_000 + sec * SEC;
    let s = tick([failed('a')], new Map(), at(0));
    s = tick([failed('a')], s.tracks, at(10)); // attempt 1
    expect(s.resume).toEqual(['a']);
    s = tick([rec('a', { state: 'RUNNING' })], s.tracks, at(20));
    s = tick([failed('a')], s.tracks, at(20)); // fails again at 20 s
    s = tick([failed('a')], s.tracks, at(79));
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, at(80)); // attempt 2 after 60 s
    expect(s.resume).toEqual(['a']);
    s = tick([failed('a')], s.tracks, at(90));
    s = tick([failed('a')], s.tracks, at(389));
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, at(390)); // attempt 3 after 300 s
    expect(s.resume).toEqual(['a']);
    s = tick([failed('a')], s.tracks, at(400));
    s = tick([failed('a')], s.tracks, at(400 + 3600));
    expect(s.resume).toEqual([]);
  });

  it('uses the named delays', () => {
    expect(AUTO_RETRY_DELAYS_MS).toEqual([10_000, 60_000, 300_000]);
  });

  it('a running attempt keeps its count, so the next wait is the longer one', () => {
    let s = tick([failed('a')], new Map(), 0);
    s = tick([failed('a')], s.tracks, 10 * SEC);
    s = tick([rec('a', { state: 'RUNNING' })], s.tracks, 11 * SEC);
    s = tick([failed('a')], s.tracks, 12 * SEC);
    s = tick([failed('a')], s.tracks, 12 * SEC + 10 * SEC);
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, 12 * SEC + 60 * SEC);
    expect(s.resume).toEqual(['a']);
  });

  it('does nothing in the background and leaves the tracks as they were', () => {
    const s0 = tick([failed('a')], new Map(), 0);
    const s = scheduleAutoRetries([failed('a')], s0.tracks, 100 * SEC, false);
    expect(s.resume).toEqual([]);
    expect(s.tracks).toBe(s0.tracks);
  });

  it('never resumes other failures, paused or cancelled transfers', () => {
    const records = [failed('c', 'CONFLICT: exists'), failed('h', 'HASH_MISMATCH: x'), rec('p', { state: 'PAUSED', error: CONN }), rec('x', { state: 'CANCELLED', error: CONN })];
    let s = tick(records, new Map(), 0);
    s = tick(records, s.tracks, 3600 * SEC);
    expect(s.resume).toEqual([]);
  });

  it('a success drops the count, so a later connection failure gets the first wait again', () => {
    let s = tick([failed('a')], new Map(), 0);
    s = tick([failed('a')], s.tracks, 10 * SEC);
    s = tick([rec('a', { state: 'DONE', error: null })], s.tracks, 11 * SEC);
    expect(s.tracks.has('a')).toBe(false);
    s = tick([failed('a')], s.tracks, 20 * SEC);
    s = tick([failed('a')], s.tracks, 29 * SEC);
    expect(s.resume).toEqual([]);
    s = tick([failed('a')], s.tracks, 30 * SEC);
    expect(s.resume).toEqual(['a']);
  });

  it('the owner tapping Retry starts the attempts again', () => {
    let s = tick([failed('a')], new Map(), 0);
    s = tick([failed('a')], s.tracks, 10 * SEC); // attempt 1
    s = tick([failed('a')], s.tracks, 20 * SEC);
    s = tick([failed('a')], s.tracks, 80 * SEC); // attempt 2
    s = tick([failed('a')], s.tracks, 90 * SEC);
    s = tick([failed('a')], s.tracks, 390 * SEC); // attempt 3
    s = tick([failed('a')], s.tracks, 400 * SEC);
    s = tick([failed('a')], s.tracks, 3600 * SEC);
    expect(s.resume).toEqual([]);
    const reset = resetAutoRetry(s.tracks, ['a']);
    s = tick([failed('a')], reset, 4000 * SEC);
    s = tick([failed('a')], s.tracks, 4010 * SEC);
    expect(s.resume).toEqual(['a']);
  });

  it('drops tracks for transfers that are no longer listed', () => {
    const s0 = tick([failed('a'), failed('b')], new Map(), 0);
    const s = tick([failed('a')], s0.tracks, 1 * SEC);
    expect(s.tracks.has('b')).toBe(false);
    expect(s.tracks.has('a')).toBe(true);
  });
});
