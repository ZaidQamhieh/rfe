import { act, createElement, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';

import type { Host } from '../../core/models/host';
import { t } from '../../i18n';
import { hostStore } from '../../services';
import { RelayCopySheet } from './RelayCopySheet';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

jest.mock('lucide-react-native', () => ({ Monitor: () => null }));
jest.mock('../../services', () => ({ hostStore: { listHosts: jest.fn() } }));
jest.mock('../transfers/relayCopy', () => ({ destinationStart: jest.fn(), startRelayCopy: jest.fn() }));
jest.mock('./DestinationPicker', () => ({ DestinationPicker: () => null }));
jest.mock('../../design/theme', () => ({ useScheme: () => ({ onSurfaceVariant: '#000' }) }));
jest.mock('../../design/components', () => ({
  Text: ({ children }: { children?: ReactNode }) => children ?? null,
  BottomSheet: ({ children }: { children?: ReactNode }) => children ?? null,
  SheetHead: () => null,
  Pressable: ({ children }: { children?: ReactNode }) => children ?? null,
  useToast: () => ({ info: jest.fn(), error: jest.fn(), success: jest.fn() }),
}));

const listHosts = hostStore.listHosts as jest.Mock;
const source: Host = { id: 'a', label: 'A', address: 'a:1' };

/** All text the sheet renders. */
async function shown(): Promise<string> {
  let tree!: ReactTestRenderer;
  await act(async () => {
    tree = create(createElement(RelayCopySheet, { source, paths: ['/x'], onDone: () => {}, onClose: () => {} }));
  });
  return JSON.stringify(tree.toJSON());
}

describe('RelayCopySheet host list', () => {
  it('says the computers could not be loaded when the lookup fails, not that there are none', async () => {
    listHosts.mockRejectedValue(new Error('storage'));
    const out = await shown();
    expect(out).toContain(t('relayCopyLoadFailed'));
    expect(out).not.toContain(t('relayCopyNone'));
  });

  it('still says to pair a second computer when the lookup works and finds none', async () => {
    listHosts.mockResolvedValue([source]);
    const out = await shown();
    expect(out).toContain(t('relayCopyNone'));
  });
});
