/** Characters per group when a computer's certificate fingerprint is shown for comparison. */
const FINGERPRINT_GROUP_SIZE = 4;

/** "ab12…" → "AB12 34CD …" in groups of [FINGERPRINT_GROUP_SIZE], so the user can compare it with the one the computer shows. */
export const groupFingerprint = (fp: string) =>
  (fp.toUpperCase().match(new RegExp(`.{1,${FINGERPRINT_GROUP_SIZE}}`, 'g')) ?? []).join(' ');
