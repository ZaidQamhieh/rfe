import { AgentApiError } from '../../core/api/agentClient';
import { codeProblem, hasAddress, pairProblem, spokenCode, waitMinutes } from './gettingInLogic';
import { PairExpired, PairRejected } from './pairingService';

describe('pairProblem', () => {
  test('a refusal and a timeout are told apart', () => {
    expect(pairProblem(new PairRejected())).toBe('declined');
    expect(pairProblem(new PairExpired())).toBe('expired');
  });
  test('anything else is a plain failure', () => {
    expect(pairProblem(new Error('boom'))).toBe('failed');
    expect(pairProblem('text')).toBe('failed');
    expect(pairProblem(new AgentApiError(500, 'INTERNAL', 'x'))).toBe('failed');
  });
});

describe('waitMinutes', () => {
  test('rounds up to whole minutes, never below one', () => {
    expect(waitMinutes(120)).toBe(2);
    expect(waitMinutes(121)).toBe(3);
    expect(waitMinutes(30)).toBe(1);
    expect(waitMinutes(0)).toBe(1);
    expect(waitMinutes(Number.NaN)).toBe(1);
  });
});

describe('spokenCode', () => {
  test('spaces the digits and drops the group gap', () => {
    expect(spokenCode('4829 1307')).toBe('4 8 2 9 1 3 0 7');
    expect(spokenCode('')).toBe('');
  });
});

describe('hasAddress and codeProblem', () => {
  test('blank addresses do not count', () => {
    expect(hasAddress('  ')).toBe(false);
    expect(hasAddress(' 192.168.1.4 ')).toBe(true);
  });
  test('a code is empty, short or fine', () => {
    expect(codeProblem('', 8)).toBe('empty');
    expect(codeProblem('AB12', 8)).toBe('short');
    expect(codeProblem('AB12CD34', 8)).toBeNull();
  });
});
