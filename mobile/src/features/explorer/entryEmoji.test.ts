import type { Entry } from '../../core/api/models';
import { emojiNameFor } from './entryEmoji';

const f = (name: string, mimeType?: string): Pick<Entry, 'isDir' | 'mimeType' | 'name'> => ({ isDir: false, name, mimeType });

describe('emojiNameFor', () => {
  it('maps folders, open while selected', () => {
    expect(emojiNameFor({ isDir: true, name: 'Taxes' })).toBe('file_folder');
    expect(emojiNameFor({ isDir: true, name: 'Taxes' }, { open: true })).toBe('open_file_folder');
  });

  it('maps media by MIME type', () => {
    expect(emojiNameFor(f('a', 'image/png'))).toBe('framed_picture');
    expect(emojiNameFor(f('a', 'video/mp4'))).toBe('film_frames');
    expect(emojiNameFor(f('a', 'audio/mpeg'))).toBe('musical_notes');
    expect(emojiNameFor(f('a', 'application/zip'))).toBe('package');
  });

  it('falls back to the extension when the type is generic or missing', () => {
    expect(emojiNameFor(f('IMG_1.HEIC', 'application/octet-stream'))).toBe('framed_picture');
    expect(emojiNameFor(f('clip.mkv'))).toBe('film_frames');
    expect(emojiNameFor(f('song.flac'))).toBe('musical_notes');
    expect(emojiNameFor(f('backup.7z'))).toBe('package');
  });

  it('draws text notes as a memo and every other file as a page', () => {
    expect(emojiNameFor(f('notes.txt'))).toBe('memo');
    expect(emojiNameFor(f('readme', 'text/plain'))).toBe('memo');
    expect(emojiNameFor(f('Budget.xlsx'))).toBe('page_facing_up');
    expect(emojiNameFor(f('contract.pdf', 'application/pdf'))).toBe('page_facing_up');
    expect(emojiNameFor(f('Makefile'))).toBe('page_facing_up');
    expect(emojiNameFor(f('.bashrc'))).toBe('page_facing_up');
  });
});
