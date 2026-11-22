import type { Entry } from '../../core/api/models';
import { copyRoute, paneOrder, paneUp, relayable } from './dualLogic';

const e = (name: string, isDir: boolean): Entry => ({ name, path: '/' + name, isDir, size: 1, modified: '' }) as unknown as Entry;

describe('dual pane logic', () => {
  it('uses the agent copy on one computer and the relay between two', () => {
    expect(copyRoute('a', 'a')).toBe('same');
    expect(copyRoute('a', 'b')).toBe('relay');
  });
  it('relays only files', () => {
    const list = [e('x', false), e('d', true)];
    expect(relayable(list, new Set(['/x']))).toBe(true);
    expect(relayable(list, new Set(['/x', '/d']))).toBe(false);
    expect(relayable(list, new Set())).toBe(false);
  });
  it('orders folders before files, by name', () => {
    expect(paneOrder([e('b', false), e('z', true), e('a', false), e('c', true)]).map((x) => x.name)).toEqual(['c', 'z', 'a', 'b']);
  });
  it('stops going up at the top', () => {
    expect(paneUp('/a/b', '/a')).toBe('/a');
    expect(paneUp('/a', '/a')).toBeNull();
  });
});
