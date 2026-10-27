// Audit F-136: a thrown native error must never open the app lock.
/* eslint-disable @typescript-eslint/no-require-imports -- jest.mock factory + lazy require is the supported pattern */
import { authenticate } from './deviceAuth';

jest.mock('expo-local-authentication', () => ({
  hasHardwareAsync: jest.fn(),
  getEnrolledLevelAsync: jest.fn(),
  authenticateAsync: jest.fn(),
}));

const mock = require('expo-local-authentication') as Record<'hasHardwareAsync' | 'getEnrolledLevelAsync' | 'authenticateAsync', jest.Mock>;

beforeEach(() => {
  mock.hasHardwareAsync.mockResolvedValue(true);
  mock.getEnrolledLevelAsync.mockResolvedValue(2);
  mock.authenticateAsync.mockResolvedValue({ success: true });
});

describe('authenticate', () => {
  it('reports success, failure and the affirmative no-lock case', async () => {
    expect(await authenticate('r')).toBe('success');
    mock.authenticateAsync.mockResolvedValue({ success: false });
    expect(await authenticate('r')).toBe('failed');
    mock.getEnrolledLevelAsync.mockResolvedValue(0);
    expect(await authenticate('r')).toBe('unavailable');
  });

  it('treats a native error as failed, not unavailable', async () => {
    mock.authenticateAsync.mockRejectedValue(new Error('native'));
    expect(await authenticate('r')).toBe('failed');
    mock.hasHardwareAsync.mockRejectedValue(new Error('native'));
    expect(await authenticate('r')).toBe('failed');
  });
});
