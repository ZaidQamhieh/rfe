/**
 * Why [name] cannot be the new name of an item, or null when it can. A rename takes a name, not a path: the agent
 * treats `a/b` as a move into a new folder, and `..` would climb out of the folder.
 */
export function invalidNameReason(name: string): string | null {
  if (name.trim() === '') return 'A name cannot be empty';
  if (name === '.' || name === '..') return `"${name}" is not a usable name`;
  if (/[/\\]/.test(name)) return "A name can't contain / or \\ (that would move the item)";
  if (/[\u0000-\u001f]/.test(name)) return "A name can't contain control characters";
  return null;
}
