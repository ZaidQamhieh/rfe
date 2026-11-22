import { deleteDoneMessage } from './deleteToast';

describe('deleteDoneMessage', () => {
  it('says the file was deleted for good when permanent', () => {
    expect(deleteDoneMessage(true, 'notes.txt')).toBe('Deleted notes.txt');
  });
  it('says the file was moved to Trash otherwise', () => {
    expect(deleteDoneMessage(false, 'notes.txt')).toBe('Moved notes.txt to Trash');
  });
});
