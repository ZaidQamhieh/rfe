import en from './en.json';
import { format } from './format';
import { t } from './index';

describe('ICU subset', () => {
  it('substitutes placeholders and leaves unknown ones visible', () => {
    expect(format('Hello {name}!', { name: 'PC' })).toBe('Hello PC!');
    expect(format('Hello {name}!')).toBe('Hello {name}!');
  });
  it('handles plurals with =1 and other, including nested placeholders', () => {
    const m = '{count, plural, =1{1 result} other{{count} results}} in {host}';
    expect(format(m, { count: 1, host: 'A' })).toBe('1 result in A');
    expect(format(m, { count: 5, host: 'A' })).toBe('5 results in A');
    expect(format(m, { count: 0, host: 'A' })).toBe('0 results in A');
  });
  it('formats a real Flutter string', () => {
    expect(t('freeOfTotalDrives', { free: '1 GB', total: '2 GB', count: 2 })).toBe('1 GB free of 2 GB · 2 drives');
  });
  it('every shipped string parses without throwing', () => {
    const params = new Proxy({}, { get: () => 3 }) as Record<string, number>;
    for (const v of Object.values(en)) expect(() => format(v as string, params)).not.toThrow();
  });
});
