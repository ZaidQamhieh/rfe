import { act, type ReactElement } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';

import { UpdateBanner } from './UpdateBanner';
import { downloadUpdate } from './updateService';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mockToast = { info: jest.fn(), error: jest.fn(), success: jest.fn() };

jest.mock('lucide-react-native', () => ({ Download: () => null, X: () => null }));
jest.mock('../../design/components', () => {
  const React = jest.requireActual('react');
  return {
    Pressable: ({ onPress, children }: { onPress: () => void; children: unknown }) => React.createElement('Pressable', { onPress }, children),
    Text: ({ children }: { children: unknown }) => React.createElement('Text', null, children),
    useToast: () => mockToast,
  };
});
jest.mock('../../design/theme', () => ({ useScheme: () => ({}) }));
jest.mock('./useUpdate', () => ({
  useSessionUpdate: () => ({ versionName: '9.9.9', versionCode: 7, size: 1000, url: 'https://example.invalid/app.apk', sha256: 'cd'.repeat(32) }),
}));
jest.mock('./updateService', () => ({
  dismissedCode: jest.fn().mockResolvedValue(0),
  dismissUpdate: jest.fn(),
  downloadUpdate: jest.fn(),
  installUpdate: jest.fn().mockResolvedValue('started'),
}));

async function render(el: ReactElement): Promise<ReactTestRenderer> {
  let t: ReactTestRenderer | undefined;
  await act(async () => {
    t = create(el);
  });
  return t as ReactTestRenderer;
}

describe('UpdateBanner', () => {
  it('shows the real download percentage in the toast while the update downloads', async () => {
    (downloadUpdate as jest.Mock).mockImplementation(async (_r: unknown, onProgress?: (pct: number) => void) => {
      onProgress?.(42);
    });
    const tree = await render(<UpdateBanner />);
    const tap = tree.root.findAllByType('Pressable' as never)[0];
    await act(async () => {
      tap.props.onPress();
    });
    const messages = mockToast.info.mock.calls.map((c) => String(c[0]));
    expect(messages.some((m) => m.includes('42'))).toBe(true);
    expect(mockToast.error).not.toHaveBeenCalled();
  });
});
