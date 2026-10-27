import { effectiveMode } from './theme';

describe('effectiveMode', () => {
  it('applies true black only to a dark theme', () => {
    expect(effectiveMode('light', true, true)).toBe('light');
    expect(effectiveMode('system', true, false)).toBe('system');
    expect(effectiveMode('system', true, true)).toBe('amoled');
    expect(effectiveMode('dark', true, false)).toBe('amoled');
    expect(effectiveMode('dark', false, true)).toBe('dark');
    expect(effectiveMode('system', false, true)).toBe('system');
  });
});
