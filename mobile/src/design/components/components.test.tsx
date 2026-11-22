import type { LucideIcon } from 'lucide-react-native';
import { readdirSync } from 'node:fs';
import { join } from 'node:path';
import type { ReactElement } from 'react';
import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';

import { EMOJI_SVG } from '../emojiSvg';
import { Checkbox } from './Checkbox';
import { Chip } from './Chip';
import { DuoIcon, DUO_FILL_OPACITY } from './DuoIcon';
import { EMOJI_ALIAS, Emoji, emojiSource, greyscale } from './Emoji';
import { Row } from './Row';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// lucide-react-native ships untransformed ES modules that this jest config does not compile; only Checkbox needs a real glyph.
jest.mock('lucide-react-native', () => ({ Check: () => null }));

/** Stands in for a lucide icon: records the props DuoIcon passes (colour, stroke, fill) without rendering svg. */
const StubIcon = (() => null) as unknown as LucideIcon;

/** React 19's test renderer is async: mount inside act so effects and updates settle before the tree is inspected. */
async function render(el: ReactElement): Promise<ReactTestRenderer> {
  let t: ReactTestRenderer | undefined;
  await act(async () => {
    t = create(el);
  });
  return t as ReactTestRenderer;
}

/** The mockup's emoji folder, when this checkout has it (it is not shipped with the app). */
const MOCKUP_EMOJI_DIR = join(__dirname, '../../../../design/mobile-redesign/emoji');

describe('emoji', () => {
  it('has a non-empty svg for every name and resolves every alias to one', () => {
    for (const [name, svg] of Object.entries(EMOJI_SVG)) expect({ name, ok: svg.startsWith('<svg') }).toEqual({ name, ok: true });
    for (const [alias, file] of Object.entries(EMOJI_ALIAS)) expect({ alias, ok: file in EMOJI_SVG }).toEqual({ alias, ok: true });
    expect(emojiSource('folder')).toBe(EMOJI_SVG.file_folder);
  });

  it('covers every svg file of the mockup folder', () => {
    let files: string[];
    try {
      files = readdirSync(MOCKUP_EMOJI_DIR).filter((f) => f.endsWith('.svg'));
    } catch {
      return; // the design folder is not part of this checkout
    }
    expect(files.length).toBeGreaterThan(0);
    expect(Object.keys(EMOJI_SVG).sort()).toEqual(files.map((f) => f.slice(0, -4)).sort());
  });

  it('greys hex fills and leaves none and white alone', () => {
    expect(greyscale('<path fill="#FFB02E"/><path fill="none"/><path fill="white"/>')).toBe('<path fill="#b7b7b7"/><path fill="none"/><path fill="white"/>');
  });

  it('renders as an svg of the requested size', async () => {
    const t = await render(<Emoji name="folder" size={40} off />);
    const root = t.root.findAll((n) => n.props.width === 40 && n.props.height === 40);
    expect(root.length).toBeGreaterThan(0);
  });
});

describe('DuoIcon', () => {
  it('draws the line icon at 1.5 stroke with a 14% fill', async () => {
    const t = await render(<DuoIcon icon={StubIcon} size={24} color="#123456" />);
    const svg = t.root.findAll((n) => n.props.fill === '#123456' && n.props.fillOpacity === DUO_FILL_OPACITY && n.props.strokeWidth === 1.5);
    expect(svg.length).toBeGreaterThan(0);
  });
});

describe('Row, Chip and Checkbox', () => {
  it('cuts a long row title in the middle and marks the selection', async () => {
    const t = await render(<Row title="a-very-long-file-name.tar.gz" subtitle="12 MB" selected />);
    const title = t.root.findAll((n) => n.props.ellipsizeMode === 'middle');
    expect(title.length).toBeGreaterThan(0);
    expect(t.root.findAll((n) => n.props.accessibilityState?.selected === true).length).toBeGreaterThan(0);
  });

  it('reports chip selection and checkbox state to accessibility', async () => {
    const chip = await render(<Chip label="Photos" selected />);
    expect(chip.root.findAll((n) => n.props.accessibilityState?.selected === true).length).toBeGreaterThan(0);
    const box = await render(<Checkbox checked />);
    expect(box.root.findAll((n) => n.props.accessibilityRole === 'checkbox' && n.props.accessibilityState?.checked === true).length).toBeGreaterThan(0);
  });
});
