import type { TransferRecord } from '../../core/native';
import { newestFirst } from './transferLogic';

const rec = (id: string, enqueuedAt?: number): TransferRecord => ({ id, hostId: 'h', address: 'a', remotePath: '/f', destPath: '/d', state: 'DONE', received: 0, total: 0, error: null, enqueuedAt });

const permutations = <T,>(xs: T[]): T[][] => (xs.length <= 1 ? [xs] : xs.flatMap((x, i) => permutations([...xs.slice(0, i), ...xs.slice(i + 1)]).map((p) => [x, ...p])));

describe('newestFirst with rows that have no queue time', () => {
  it('is a total order: every input order sorts to the same list', () => {
    // Known times 5 and 1 with ids that disagree with them, and a row with no time whose id falls between theirs.
    const rows = [rec('a-newer', 5), rec('z-older', 1), rec('m-none')];
    const orders = new Set(permutations(rows).map((p) => [...p].sort(newestFirst).map((r) => r.id).join(',')));
    expect(orders.size).toBe(1);
  });

  it('is antisymmetric for a mixed pair', () => {
    const known = rec('d000000aa', 5);
    const unknown = rec('pb-1');
    expect(Math.sign(newestFirst(known, unknown))).toBe(-Math.sign(newestFirst(unknown, known)));
    expect(newestFirst(known, unknown)).not.toBe(0);
  });
});
