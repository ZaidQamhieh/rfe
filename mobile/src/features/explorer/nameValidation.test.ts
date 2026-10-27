import type { AgentClient } from '../../core/api/agentClient';
import { ListingCache, MemoryListingBackend } from '../../core/storage/listingCache';
import { createExplorer } from './explorerStore';
import { invalidNameReason } from './nameValidation';

describe('invalidNameReason', () => {
  it.each(['report.txt', 'a b', '.hidden', 'v1.2', '..foo', 'foo..', 'résumé.pdf'])('accepts %p', (name) => {
    expect(invalidNameReason(name)).toBeNull();
  });

  it.each([
    ['a/b', 'separator'],
    ['a\\b', 'separator'],
    ['/abs', 'separator'],
    ['..', 'dots'],
    ['.', 'dots'],
    ['', 'empty'],
    ['   ', 'empty'],
    ['a\u0000b', 'control'],
  ])('rejects %p', (name) => {
    expect(invalidNameReason(name)).not.toBeNull();
  });
});

function setup(client: Record<string, jest.Mock>) {
  const cache = new ListingCache(new MemoryListingBackend(), 3);
  return createExplorer({ hostId: 'h', rootPath: '/r', getClient: async () => client as unknown as AgentClient, cache, humanize: (e) => (e as Error).message });
}

describe('rename never turns a name into a path', () => {
  it('single rename refuses "a/b" without calling the host', async () => {
    const client = { rename: jest.fn(), list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }) };
    const ex = setup(client);
    await expect(ex.rename('/r/x', 'a/b')).rejects.toThrow(/separator|can't contain/i);
    expect(client.rename).not.toHaveBeenCalled();
  });

  it('batch rename reports bad names per item and still renames the good ones', async () => {
    const client = { rename: jest.fn().mockResolvedValue({}), list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }) };
    const ex = setup(client);
    const res = await ex.batchRename([
      { path: '/r/a', newName: 'sub/a' },
      { path: '/r/b', newName: '..' },
      { path: '/r/c', newName: 'ok' },
    ]);
    expect(res.failed.map((f) => [f.path, f.errorCode])).toEqual([
      ['/r/a', 'INVALID_NAME'],
      ['/r/b', 'INVALID_NAME'],
    ]);
    expect(client.rename.mock.calls.map((c) => c.join('>'))).toEqual(['/r/c>/r/.rfe-rn-2-ok', '/r/.rfe-rn-2-ok>/r/ok']);
  });
});
