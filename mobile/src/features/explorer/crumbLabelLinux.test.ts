import { crumbLabel } from './paths';

describe('crumbLabel on a Linux path', () => {
  it('keeps a backslash inside a folder name', () => {
    expect(crumbLabel(['/home/u', '/home/u/a\\b'], 1)).toBe('a\\b');
  });

  it('still splits a Windows path on backslashes', () => {
    expect(crumbLabel(['C:\\Users', 'C:\\Users\\x\\y'], 1)).toBe('y');
  });
});
