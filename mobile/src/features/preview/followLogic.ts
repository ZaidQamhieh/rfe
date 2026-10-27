import { decodeAsText } from './textDecode';

/** The most one poll fetches; a bigger jump is read over several polls. */
export const FOLLOW_CHUNK_BYTES = 1024 * 1024;

export type FollowStep = { kind: 'idle' } | { kind: 'append'; range: string } | { kind: 'truncated' };

/** What to do once the host reports [current] bytes and [known] bytes are already shown. */
export function followStep(known: number, current: number): FollowStep {
  if (current < known) return { kind: 'truncated' };
  if (current === known) return { kind: 'idle' };
  const end = Math.min(current, known + FOLLOW_CHUNK_BYTES);
  return { kind: 'append', range: `bytes=${known}-${end - 1}` };
}

/** Bytes up to and including the last newline; a line still being written is not counted. */
export function completeLines(bytes: Uint8Array): number {
  for (let i = bytes.length - 1; i >= 0; i--) if (bytes[i] === 0x0a) return i + 1;
  return 0;
}

/** Length of [bytes] without an incomplete UTF-8 sequence at the end. */
export function utf8SafeLength(bytes: Uint8Array): number {
  let i = bytes.length;
  // Step back over continuation bytes (10xxxxxx) to the lead byte of the last character.
  let back = 0;
  while (i > 0 && back < 3 && (bytes[i - 1] & 0xc0) === 0x80) {
    i--;
    back++;
  }
  if (i === 0) return bytes.length;
  const lead = bytes[i - 1];
  const need = lead >= 0xf0 ? 4 : lead >= 0xe0 ? 3 : lead >= 0xc0 ? 2 : 1;
  return back + 1 < need ? i - 1 : bytes.length;
}

/**
 * The whole lines of a fetched chunk as text, and how many bytes they used. A newline never occurs inside a
 * multi-byte character, so cutting there keeps the text valid. A full-size chunk with no newline at all is a
 * very long line: it is taken up to a character boundary instead of stalling. Throws NotTextError for binary data.
 */
export function followChunk(bytes: Uint8Array): { text: string; consumed: number } {
  let consumed = completeLines(bytes);
  if (consumed === 0 && bytes.length >= FOLLOW_CHUNK_BYTES) consumed = utf8SafeLength(bytes);
  return consumed === 0 ? { text: '', consumed: 0 } : { text: decodeAsText(bytes.slice(0, consumed)), consumed };
}
