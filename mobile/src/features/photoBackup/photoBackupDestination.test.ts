import { needsDestination } from './photoBackupLogic';

it('needs a destination until a computer is chosen', () => {
  expect(needsDestination(null)).toBe(true);
  expect(needsDestination(undefined)).toBe(true);
  expect(needsDestination('')).toBe(true);
  expect(needsDestination('host-1')).toBe(false);
});
