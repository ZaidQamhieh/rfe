import { sanitizeCode } from './codeInput';

test('pasted or fast-typed input is normalised to an 8-char code', () => {
  expect(sanitizeCode('ab-cd 1234extra')).toBe('ABCD1234');
  expect(sanitizeCode('  x ')).toBe('X');
  expect(sanitizeCode('')).toBe('');
});
