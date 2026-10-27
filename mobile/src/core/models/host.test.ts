import { hostFromJson, hostToJson, isValidInternetAddress, routeAddresses } from './host';

describe('isValidInternetAddress', () => {
  it.each(['files.example.com', 'files.example.com:8765', '203.0.113.7:443', '[2001:db8::1]:8765', '[::1]'])(
    'accepts %s',
    (v) => expect(isValidInternetAddress(v)).toBe(true),
  );
  it.each([
    '',
    ' a.com',
    'https://a.com',
    'a.com/path',
    'user@a.com',
    'a.com:0',
    'a.com:70000',
    '256.1.1.1',
    '-bad.example.com',
    '[1:2:3:4:5:6:7:8:9]',
    'a..com',
    'a.com?x=1',
  ])('rejects %j', (v) => expect(isValidInternetAddress(v)).toBe(false));
});

describe('routeAddresses', () => {
  it('orders lan, tailscale, direct https and drops invalid or duplicate entries', () => {
    const h = { id: 'h', label: 'l', address: '10.0.0.2:8765', tailscaleAddress: '10.0.0.2:8765', internetAddress: 'a.example.com' };
    expect(routeAddresses(h)).toEqual([
      { route: 'lan', address: '10.0.0.2:8765' },
      { route: 'directHttps', address: 'a.example.com' },
    ]);
    expect(routeAddresses({ ...h, internetAddress: 'https://evil' }).map((r) => r.route)).toEqual(['lan']);
  });
});

describe('persisted host json', () => {
  it('round-trips the Flutter shape and skips corrupt records', () => {
    const j = { id: 'h1', label: 'PC', address: '1.2.3.4:8765', certFingerprint: 'a'.repeat(64), macAddress: 'aa:bb' };
    expect(hostToJson(hostFromJson(j)!)).toEqual(j);
    expect(hostFromJson({ id: 'x' })).toBeNull();
    expect(hostFromJson('nope')).toBeNull();
  });
});

describe('host note', () => {
  it('round-trips through the stored JSON and is optional', () => {
    const h = { id: 'h', label: 'l', address: '10.0.0.2:8765', note: 'Home office' };
    expect(hostFromJson(hostToJson(h))).toEqual(h);
    expect(hostFromJson({ id: 'h', label: 'l', address: 'a' })).toEqual({ id: 'h', label: 'l', address: 'a' });
    expect(hostToJson({ id: 'h', label: 'l', address: 'a' })).not.toHaveProperty('note');
  });
});
