import type { Entry } from '../../core/api/models';
import { isPreviewable, mimeFromExtension, previewKindOf, previewableSiblings, shouldPreloadOnCellular } from './previewKind';

const e = (name: string, mimeType?: string, isDir = false): Entry => ({ name, path: `/${name}`, isDir, mimeType, isSymlink: false });

describe('previewKindOf', () => {
  it('prefers MIME, then falls back to extension', () => {
    expect(previewKindOf(e('x', 'image/png'))).toBe('image');
    expect(previewKindOf(e('x', 'application/pdf'))).toBe('pdf');
    expect(previewKindOf(e('x', 'text/markdown'))).toBe('markdown');
    expect(previewKindOf(e('x', 'text/csv'))).toBe('csv');
    expect(previewKindOf(e('x', 'text/x-python'))).toBe('text');
    expect(previewKindOf(e('x', 'text/markdown; charset=utf-8'))).toBe('markdown');
    expect(previewKindOf(e('x', 'Text/CSV; charset=utf-8'))).toBe('csv');
    expect(previewKindOf(e('x', 'application/vnd.api+json'))).toBe('text');
    expect(previewKindOf(e('a.MP4'))).toBe('video');
    expect(previewKindOf(e('a.flac'))).toBe('audio');
    expect(previewKindOf(e('a.tgz'))).toBe('archive');
    expect(previewKindOf(e('main.rs'))).toBe('text');
    expect(previewKindOf(e('a.bin', 'application/octet-stream'))).toBe('none');
    expect(previewKindOf(e('noext'))).toBe('none');
    expect(previewKindOf(e('trail.'))).toBe('none');
  });
  it('folders are never previewable', () => {
    expect(isPreviewable(e('a.png', 'image/png', true))).toBe(false);
  });
});

describe('previewableSiblings', () => {
  it('filters to previewable entries in order and locates the tapped one by path', () => {
    const list = [e('a.png'), e('b.bin'), e('c.pdf'), e('d', undefined, true)];
    const r = previewableSiblings(list, { path: '/c.pdf' });
    expect(r.entries.map((x) => x.name)).toEqual(['a.png', 'c.pdf']);
    expect(r.index).toBe(1);
    expect(previewableSiblings(list, { path: '/b.bin' }).index).toBe(-1);
  });
});

test('cellular preload gate and MIME fallback', () => {
  expect(shouldPreloadOnCellular(false, false)).toBe(true);
  expect(shouldPreloadOnCellular(true, false)).toBe(false);
  expect(shouldPreloadOnCellular(true, true)).toBe(true);
  expect(mimeFromExtension('a.jpeg')).toBe('image/jpeg');
  expect(mimeFromExtension('a.unknown')).toBe('application/octet-stream');
  expect(mimeFromExtension('README')).toBe('application/octet-stream');
});
