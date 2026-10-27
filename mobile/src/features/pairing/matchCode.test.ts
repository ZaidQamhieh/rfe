import { matchCode } from './matchCode';

describe('matchCode', () => {
  it('equals the agent implementation (pairing.SAS test vector)', () => {
    expect(matchCode('a'.repeat(64), '00112233445566778899aabbccddeeff', 'req1')).toBe('7298 4153');
  });
  it('depends on the certificate seen', () => {
    expect(matchCode('b'.repeat(64), '00112233445566778899aabbccddeeff', 'req1')).not.toBe('7298 4153');
  });
});
