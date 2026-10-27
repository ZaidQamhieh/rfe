import type { Host } from '../../core/models/host';
import { useActiveHost } from '../../state/activeHost';
import { parentPathOf, revealInExplorer } from './reveal';

jest.mock('../../state/activeHost', () => {
  const state = { active: null as unknown, setActive: (a: unknown) => (state.active = a) };
  return { useActiveHost: { getState: () => state } };
});

describe('parentPathOf', () => {
  it('returns the containing folder for POSIX and Windows paths', () => {
    expect(parentPathOf('/a/b/c.txt')).toBe('/a/b');
    expect(parentPathOf('/c.txt')).toBe('/');
    expect(parentPathOf('C:\\a\\b.txt')).toBe('C:\\a');
    expect(parentPathOf('/')).toBe('/');
  });
});

describe('revealInExplorer', () => {
  it('points the Files tab at the result folder and dismisses to it', () => {
    const host = { id: 'h', label: 'PC', address: 'a' } as Host;
    const dismissed: string[] = [];
    revealInExplorer({ dismissTo: (p: string) => void dismissed.push(p) }, host, '/r/docs/a.txt', '/r');
    expect((useActiveHost.getState() as unknown as { active: unknown }).active).toEqual({ host, health: null, rootPath: '/r', initialPath: '/r/docs' });
    expect(dismissed).toEqual(['/files']);
  });
});
