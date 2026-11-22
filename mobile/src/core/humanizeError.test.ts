import { AgentApiError } from './api/agentClient';
import { CertPinMismatch } from './api/pin';
import { humanizeError, phoneRemovedMessage } from './humanizeError';

describe('humanizeError: wording moved to the string table', () => {
  it('keeps the English text of each message', () => {
    expect(phoneRemovedMessage()).toBe('This computer no longer accepts this phone. If it was removed there, remove this computer here and pair it again.');
    expect(humanizeError(new AgentApiError(401, 'X', 'HTTP 401'))).toBe(phoneRemovedMessage());
    expect(humanizeError(new AgentApiError(403, 'CAPABILITY_DENIED', 'x'))).toBe('This computer has not allowed this phone to do that. Allow it on the computer, in the device’s access settings, or the item may be outside the folders it shares.');
    expect(humanizeError(new CertPinMismatch())).toBe('This computer’s identity has changed, so RFE refused to connect. If you reinstalled its agent, remove this computer and pair it again. If not, do not trust this network.');
  });
});

describe('humanizeError: a phone the computer no longer recognises', () => {
  it('gives plain words, not the agent text about device ids and keys', () => {
    const m = humanizeError(new AgentApiError(401, 'DEVICE_KEY_MISMATCH', 'this device id is already bound to a different key - re-pair via QR code to reset trust'));
    expect(m).toMatch(/no longer recognises this phone/);
    expect(m).not.toMatch(/device id|key|fingerprint|agent/i);
  });

  it('tells a phone whose pairing code was refused as a replacement to pair with approval instead', () => {
    const m = humanizeError(new AgentApiError(409, 'PAIR_REPLACES', 'this computer already has a device with this id and a different key, so a pairing code cannot replace it'));
    expect(m).toMatch(/Pair with computer/);
    expect(m).not.toMatch(/device id|rfe remove/i);
  });

  it('treats the agent’s coded 401 for a revoked token as the phone being removed', () => {
    const revoked = new AgentApiError(401, 'UNAUTHORIZED', 'invalid or revoked token');
    expect(humanizeError(revoked)).toBe(phoneRemovedMessage());
    expect(humanizeError(revoked)).not.toMatch(/revoked token/);
  });

  it('leaves other agent messages as they are', () => {
    expect(humanizeError(new AgentApiError(409, 'CONFLICT', 'a file with this name exists'))).toBe('a file with this name exists');
  });
});
