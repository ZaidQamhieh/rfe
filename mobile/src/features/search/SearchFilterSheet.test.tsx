import { act, type ReactNode } from 'react';
import { create, type ReactTestInstance } from 'react-test-renderer';

import { t } from '../../i18n';
import { SearchFilterSheet } from './SearchFilterSheet';
import { defaultFilters, type SearchFilters } from './searchLogic';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The sheet's own logic is under test, not the design system: every design component renders as a plain element.
jest.mock('../../design/components', () => {
  const { createElement } = jest.requireActual<typeof import('react')>('react');
  const plain = (name: string) => {
    const Plain = (p: { children?: ReactNode }) => createElement(name, p, p.children);
    Plain.displayName = name;
    return Plain;
  };
  return { BottomSheet: plain('BottomSheet'), Button: plain('Button'), MockupSwitch: plain('MockupSwitch'), Pressable: plain('Pressable'), SheetHead: plain('SheetHead'), Text: plain('Text') };
});
jest.mock('../../design/theme', () => ({ useScheme: () => ({ primary: '#000000', surfaceContainer: '#ffffff', surfaceContainerHigh: '#eeeeee', onSurfaceVariant: '#444444' }) }));

const button = (root: ReactTestInstance, label: string) => root.find((n) => (n.type as unknown) === 'Button' && n.props.label === label);

describe('SearchFilterSheet reset', () => {
  it('goes back to the screen\'s own scope, not always to "from here"', async () => {
    const applied: SearchFilters[] = [];
    const current: SearchFilters = { ...defaultFilters(false), size: 'mb10', includeHidden: true };
    let tree!: ReturnType<typeof create>;
    await act(async () => {
      tree = create(<SearchFilterSheet visible filters={current} currentPath="/" defaultFromHere={false} onApply={(f) => applied.push(f)} onClose={() => undefined} />);
    });
    await act(async () => button(tree.root, t('resetButton')).props.onPress());
    await act(async () => button(tree.root, t('applyButton')).props.onPress());
    expect(applied).toHaveLength(1);
    expect(applied[0].fromHere).toBe(false);
    expect(applied[0]).toEqual({ ...defaultFilters(), fromHere: false });
  });
});
