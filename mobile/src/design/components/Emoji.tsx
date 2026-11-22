import { SvgXml } from 'react-native-svg';

import { EMOJI_SVG, type EmojiFile } from '../emojiSvg';

/** Short names the mockup uses for things (screens.js `EM`), each pointing at a Fluent Emoji Flat file. */
export const EMOJI_ALIAS = {
  folder: 'file_folder',
  openfolder: 'open_file_folder',
  home: 'house',
  drive: 'computer_disk',
  monitor: 'desktop_computer',
  phone: 'mobile_phone',
  image: 'framed_picture',
  trash: 'wastebasket',
  apps: 'toolbox',
  inbox: 'inbox_tray',
  pin: 'pushpin',
  lock: 'locked_with_key',
  info: 'information',
} as const satisfies Record<string, EmojiFile>;

/** Every emoji file name (for example `file_folder`) plus the short aliases (for example `folder`). */
export type EmojiName = EmojiFile | keyof typeof EMOJI_ALIAS;

/** The inlined svg for [name], resolving a short alias to its file. */
export function emojiSource(name: EmojiName): string {
  const file: EmojiFile = name in EMOJI_ALIAS ? EMOJI_ALIAS[name as keyof typeof EMOJI_ALIAS] : (name as EmojiFile);
  return EMOJI_SVG[file];
}

const HEX_FILL = /fill="#([0-9a-fA-F]{6})"/g;

/** Replaces every `#RRGGBB` fill with its luminance grey (`white` and `none` fills are already neutral). */
export function greyscale(svg: string): string {
  return svg.replace(HEX_FILL, (_m, hex: string) => {
    const n = parseInt(hex, 16);
    const y = Math.round(0.2126 * ((n >> 16) & 255) + 0.7152 * ((n >> 8) & 255) + 0.0722 * (n & 255));
    return `fill="#${y.toString(16).padStart(2, '0').repeat(3)}"`;
  });
}

/** Nameplate `.e`: a Fluent flat emoji for a thing (folder, computer, phone). `off` greys it out at half opacity (`.e.off`). */
export function Emoji({ name, size = 40, off = false }: { name: EmojiName; size?: number; off?: boolean }) {
  const xml = emojiSource(name);
  return <SvgXml xml={off ? greyscale(xml) : xml} width={size} height={size} opacity={off ? 0.5 : 1} />;
}
