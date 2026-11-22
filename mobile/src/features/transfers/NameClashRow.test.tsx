import { act, createElement, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';

import type { Host } from '../../core/models/host';
import type { TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { NameClashRow } from './NameClashRow';
import { resolveClash } from './resolveClash';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

jest.mock('lucide-react-native', () => ({}));
jest.mock('expo-file-system', () => ({ File: class { exists = false; } }));
jest.mock('../../services', () => ({ clientForHost: jest.fn(async () => ({ meta: async () => ({ size: 1 }) })) }));
jest.mock('./resolveClash', () => ({ resolveClash: jest.fn(), fileUriOf: (p: string) => `file://${p}` }));
jest.mock('../../design/theme', () => ({ useScheme: () => ({ rule2: '#000', wait: '#000' }) }));
jest.mock('../../design/components', () => ({
  Text: ({ children }: { children?: ReactNode }) => children ?? null,
  Button: (p: object) => jest.requireActual<typeof import('react')>('react').createElement('Button', p),
  useToast: () => ({ info: jest.fn(), error: jest.fn(), success: jest.fn() }),
}));
jest.mock('./TransferRows', () => ({
  TransferFrame: ({ children }: { children?: ReactNode }) => children ?? null,
  MetaLine: ({ children }: { children?: ReactNode }) => children ?? null,
}));

const host: Host = { id: 'h1', label: 'PC', address: 'pc:8765' };
const record = { id: 'u1', hostId: 'h1', address: 'a', remotePath: '/docs/a.txt', destPath: '/data/a.txt', state: 'FAILED', received: 0, total: 1, error: null } as TransferRecord;
const resolve = resolveClash as jest.Mock;

let tree: ReactTestRenderer;
const button = (label: string) => tree.root.findAll((n) => (n.type as unknown) === 'Button' && n.props.label === label)[0];
const press = async (label: string) => {
  await act(async () => {
    button(label).props.onPress();
  });
};

beforeEach(async () => {
  jest.clearAllMocks();
  resolve.mockReset();
  await act(async () => {
    tree = create(createElement(NameClashRow, { r: record, host }));
  });
});

describe('NameClashRow busy state', () => {
  it('gives the buttons back when the choice had nothing to upload', async () => {
    resolve.mockResolvedValue(false);
    await press(t('keepBothButton'));
    expect(button(t('keepBothButton')).props.busy).toBe(false);
    expect(button(t('trReplace')).props.disabled).toBe(false);
    await press(t('keepBothButton'));
    expect(resolve).toHaveBeenCalledTimes(2);
  });

  it('gives the buttons back when the choice failed', async () => {
    resolve.mockRejectedValue(new Error('x'));
    await press(t('trReplace'));
    expect(button(t('keepBothButton')).props.busy).toBe(false);
  });

  it('stays busy after a choice was carried out, so a double tap cannot queue twice', async () => {
    resolve.mockResolvedValue(true);
    await press(t('trReplace'));
    expect(button(t('keepBothButton')).props.busy).toBe(true);
    await press(t('trSkip'));
    expect(resolve).toHaveBeenCalledTimes(1);
  });
});
