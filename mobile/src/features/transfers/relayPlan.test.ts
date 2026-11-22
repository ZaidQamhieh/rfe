import type { TransferRecord } from '../../core/native';
import { parseJobs, planRelay, type RelayJob } from './relayPlan';

const rec = (id: string, state: TransferRecord['state']): TransferRecord => ({ id, hostId: 'a', address: 'x', remotePath: '/f', destPath: '/d/' + id + '/f', state, received: 0, total: 0, error: null });
const job = (id: string): RelayJob => ({ downloadId: id, destHostId: 'b', destDir: '/in', name: 'f' });

describe('planRelay', () => {
  it('starts the upload of a finished download and leaves running ones alone', () => {
    const s = planRelay([job('d1'), job('d2'), job('d3')], [rec('d1', 'DONE'), rec('d2', 'RUNNING'), rec('d3', 'QUEUED')]);
    expect(s.start.map((x) => x.job.downloadId)).toEqual(['d1']);
    expect(s.start[0].localPath).toBe('/d/d1/f');
    expect(s.drop).toEqual([]);
  });
  it('drops failed, cancelled and vanished downloads', () => {
    const s = planRelay([job('a'), job('b'), job('c')], [rec('a', 'FAILED'), rec('b', 'CANCELLED')]);
    expect(s.drop).toEqual(['a', 'b', 'c']);
    expect(s.start).toEqual([]);
  });
});

describe('parseJobs', () => {
  it('survives garbage and keeps only well-formed jobs', () => {
    expect(parseJobs(null)).toEqual([]);
    expect(parseJobs('{')).toEqual([]);
    expect(parseJobs('{"a":1}')).toEqual([]);
    expect(parseJobs(JSON.stringify([job('x'), { downloadId: 1 }, null]))).toEqual([job('x')]);
  });
});
