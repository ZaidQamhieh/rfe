import { normalizeDiscovered, parseDiscovered } from './discovery';

describe('discovery hints', () => {
  it('keeps valid IPv4 agents and de-duplicates by authority', () => {
    const a = { name: ' PC ', address: '192.168.1.5', port: 8765 };
    expect(normalizeDiscovered([a, a, { ...a, name: 'PC2' }])).toEqual([
      { name: 'PC2', address: '192.168.1.5', port: 8765, authority: '192.168.1.5:8765' },
    ]);
  });
  it.each([
    { name: '', address: '1.2.3.4', port: 1 },
    { name: 'x', address: '::1', port: 1 },
    { name: 'x', address: '256.1.1.1', port: 1 },
    { name: 'x', address: '1.2.3.4', port: 0 },
    { name: 'x', address: '1.2.3.4', port: 70000 },
    { name: 'x'.repeat(129), address: '1.2.3.4', port: 1 },
    { name: 'x', address: '1.2.3.4', port: '8765' },
    null,
    'str',
  ])('drops %j', (v) => expect(parseDiscovered(v)).toBeNull());
});
