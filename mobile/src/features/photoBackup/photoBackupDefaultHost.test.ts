import { defaultBackupHostId, type BackupHostState } from './photoBackupLogic';

const host = (id: string, over: Partial<BackupHostState> = {}): BackupHostState => ({ id, online: false, untrusted: false, refused: false, ...over });

describe('defaultBackupHostId', () => {
  it('prefers the connected computer over a saved one whose identity changed', () => {
    const hosts = [host('uj', { untrusted: true }), host('zaid', { online: true })];
    expect(defaultBackupHostId('uj', hosts)).toBe('zaid');
  });
  it('skips a saved computer that removed this phone', () => {
    expect(defaultBackupHostId('old', [host('old', { refused: true, online: true }), host('new', { online: true })])).toBe('new');
  });
  it('keeps the saved computer while it is answering, even if another one answers too', () => {
    expect(defaultBackupHostId('b', [host('a', { online: true }), host('b', { online: true })])).toBe('b');
  });
  it('moves to an answering computer when the saved one is silent', () => {
    expect(defaultBackupHostId('b', [host('a', { online: true }), host('b')])).toBe('a');
  });
  it('keeps a silent but valid saved computer when nothing answers', () => {
    expect(defaultBackupHostId('b', [host('a'), host('b')])).toBe('b');
  });
  it('returns null when the saved computer is forgotten or unusable and none answers', () => {
    expect(defaultBackupHostId('gone', [host('a')])).toBeNull();
    expect(defaultBackupHostId('u', [host('u', { untrusted: true })])).toBeNull();
    expect(defaultBackupHostId(null, [])).toBeNull();
  });
  it('never picks an untrusted or refused computer as the connected one', () => {
    expect(defaultBackupHostId(null, [host('x', { online: true, untrusted: true }), host('y', { online: true, refused: true })])).toBeNull();
  });
});
