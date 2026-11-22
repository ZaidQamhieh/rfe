/** What the last check of a computer learned about whether it still accepts this phone's login. */
export type AccessOutcome = 'accepted' | 'unauthorized' | 'unreachable' | 'error';

/**
 * Whether the computer has removed this phone's access, after one more check. A refused login turns it on and an
 * accepted one turns it off; a computer that could not be asked, or failed some other way, changes nothing, so the
 * message stays until the phone is paired again instead of flipping back to "offline".
 */
export function nextRefused(previous: boolean, outcome: AccessOutcome): boolean {
  if (outcome === 'unauthorized') return true;
  if (outcome === 'accepted') return false;
  return previous;
}
