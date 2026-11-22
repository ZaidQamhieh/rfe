import { mergeDiscovered } from './discovery';

const pc = (n: string, address = '192.168.1.5', port = 8765) => ({ name: n, address, port });

describe('mergeDiscovered', () => {
  it('adds a computer the moment it arrives, keeping earlier ones in order', () => {
    const one = mergeDiscovered([], pc('A'));
    const two = mergeDiscovered(one, pc('B', '192.168.1.6'));
    expect(two.map((a) => a.name)).toEqual(['A', 'B']);
    expect(two[0].authority).toBe('192.168.1.5:8765');
  });
  it('updates a repeat of the same address and port instead of listing it twice', () => {
    const list = mergeDiscovered(mergeDiscovered([], pc('A')), pc('A renamed'));
    expect(list).toHaveLength(1);
    expect(list[0].name).toBe('A renamed');
  });
  it('ignores an untrusted hint that fails validation and returns the same list', () => {
    const start = mergeDiscovered([], pc('A'));
    expect(mergeDiscovered(start, { name: '', address: '192.168.1.9', port: 1 })).toBe(start);
    expect(mergeDiscovered(start, pc('x', 'not-an-ip'))).toBe(start);
    expect(mergeDiscovered(start, null)).toBe(start);
  });
  it('does not change the list it was given', () => {
    const start = mergeDiscovered([], pc('A'));
    mergeDiscovered(start, pc('B', '192.168.1.6'));
    expect(start).toHaveLength(1);
  });
});
