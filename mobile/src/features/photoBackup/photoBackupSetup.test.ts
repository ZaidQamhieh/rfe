import { nextSetupStep, soleHostId } from './photoBackupLogic';

describe('nextSetupStep', () => {
  const on = { enabled: true, hostId: 'a' };
  it('is done once backup is on and its computer still exists', () => {
    expect(nextSetupStep(on, ['a', 'b'])).toBe('done');
  });
  it('asks to add a computer when none is paired, whatever the saved choice', () => {
    expect(nextSetupStep({ enabled: false, hostId: null }, [])).toBe('pairComputer');
    expect(nextSetupStep(on, [])).toBe('pairComputer');
  });
  it('asks for a computer when none is chosen or the chosen one was forgotten', () => {
    expect(nextSetupStep({ enabled: false, hostId: null }, ['a', 'b'])).toBe('chooseComputer');
    expect(nextSetupStep({ enabled: true, hostId: 'gone' }, ['a'])).toBe('chooseComputer');
  });
  it('asks to switch backup on when a computer is chosen but backup is off', () => {
    expect(nextSetupStep({ enabled: false, hostId: 'a' }, ['a'])).toBe('enable');
  });
});

describe('soleHostId', () => {
  it('picks the only computer and leaves a real choice alone', () => {
    expect(soleHostId(['a'])).toBe('a');
    expect(soleHostId(['a', 'b'])).toBeNull();
    expect(soleHostId([])).toBeNull();
  });
});
