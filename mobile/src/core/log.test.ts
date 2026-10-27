import { redact } from './log';

describe('redact', () => {
  it('hides bearer tokens and authorization headers', () => {
    expect(redact('Authorization: Bearer abc.def-123')).not.toContain('abc.def');
    expect(redact('sent Bearer abc.def-123 ok')).toBe('sent Bearer [redacted] ok');
  });
  it('hides key=value credentials, including JSON-shaped ones', () => {
    expect(redact('token=s3cr3t&x=1')).toBe('token=[redacted]&x=1');
    expect(redact('{"password":"hunter2","name":"n"}')).toBe('{"password":"[redacted]","name":"n"}');
    expect(redact('pairingCode: 123456')).toBe('pairingCode: [redacted]');
  });
  it('hides certificate fingerprints in both forms', () => {
    const hex = 'ab'.repeat(32);
    const colon = Array.from({ length: 32 }, () => 'ab').join(':');
    expect(redact(`pin ${hex}`)).toBe('pin [redacted]');
    expect(redact(`pin ${colon}`)).toBe('pin [redacted]');
  });
  it('hides long base64-looking blobs but keeps ordinary words and paths', () => {
    expect(redact(`key ${'A1b2'.repeat(12)}==`)).toBe('key [redacted]');
    expect(redact('GET /v1/files/list failed with status 502')).toBe('GET /v1/files/list failed with status 502');
  });
});

test('compound secret names are redacted too', () => {
  expect(redact('deviceToken=abc')).not.toContain('abc');
  expect(redact('rfe_token_h1: "xyz"')).not.toContain('xyz');
  expect(redact('statusCode=200')).toContain('200');
});
