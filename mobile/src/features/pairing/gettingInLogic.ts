import { isUnsupportedByAgent } from '../../core/api/compat';
import { PairExpired, PairRejected } from './pairingService';

/** Why a pairing request ended without a new computer, in the words the screen needs (not the technical error). */
export type PairProblem = 'declined' | 'expired' | 'tooOld' | 'failed';

/** Sorts the error from a pairing request into one of four plain outcomes; anything unknown is `failed`. */
export function pairProblem(e: unknown): PairProblem {
  if (e instanceof PairRejected) return 'declined';
  if (e instanceof PairExpired) return 'expired';
  if (isUnsupportedByAgent(e)) return 'tooOld';
  return 'failed';
}

/** How many whole minutes a request stays open, at least one (the screen says "waits for 2 minutes"). */
export function waitMinutes(expiresInSeconds: number): number {
  if (!Number.isFinite(expiresInSeconds) || expiresInSeconds <= 0) return 1;
  return Math.max(1, Math.ceil(expiresInSeconds / 60));
}

/** The match number read out digit by digit for a screen reader: "4829 1307" becomes "4 8 2 9 1 3 0 7". */
export function spokenCode(matchCode: string): string {
  return matchCode.replace(/\D/g, '').split('').join(' ');
}

/** True when the typed address has something in it once spaces are trimmed (the one thing checked before asking). */
export function hasAddress(address: string): boolean {
  return address.trim().length > 0;
}

/** What is wrong with the code the computer shows, or null when it is complete: the boxes need all `length` characters. */
export function codeProblem(code: string, length: number): 'empty' | 'short' | null {
  if (code.length === 0) return 'empty';
  return code.length < length ? 'short' : null;
}
