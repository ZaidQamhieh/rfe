import { isUserCancel } from './userCancel';

describe('isUserCancel', () => {
  it('recognises the AbortError a cancelled request rejects with', () => {
    expect(isUserCancel(Object.assign(new Error('The request was cancelled.'), { name: 'AbortError' }))).toBe(true);
  });
  it('recognises the native ERR_CANCELLED code', () => {
    expect(isUserCancel(Object.assign(new Error('cancelled'), { code: 'ERR_CANCELLED' }))).toBe(true);
  });
  it('recognises the native cancel wrapped in a rejection message', () => {
    expect(isUserCancel(new Error("Call to function 'RfeTransport.fetchToFile' has been rejected.\n→ Caused by: Canceled"))).toBe(true);
    expect(isUserCancel(new Error('cancelled'))).toBe(true);
  });
  it('does not hide real failures', () => {
    expect(isUserCancel(new Error('connection lost'))).toBe(false);
    expect(isUserCancel(Object.assign(new Error('x'), { code: 'ERR_CONNECTION' }))).toBe(false);
    expect(isUserCancel(Object.assign(new Error('HTTP 403'), { statusCode: 403 }))).toBe(false);
    expect(isUserCancel(null)).toBe(false);
    expect(isUserCancel('cancelled by the agent policy')).toBe(false);
  });
});
