import { cancelTransfer } from './cancelTransfer';

describe('cancelTransfer', () => {
  it('resolves and reports nothing when the engine confirms the cancel', async () => {
    const onError = jest.fn();
    await expect(cancelTransfer('d1', jest.fn().mockResolvedValue(undefined), onError)).resolves.toBeUndefined();
    expect(onError).not.toHaveBeenCalled();
  });

  it.each([
    ['an AbortError', Object.assign(new Error('The request was cancelled.'), { name: 'AbortError' })],
    ['the native ERR_CANCELLED code', Object.assign(new Error('cancelled'), { code: 'ERR_CANCELLED' })],
    ['OkHttp "Canceled" wrapped by the native module', new Error("Call to function 'RfeTransport.transferCancel' has been rejected.\n→ Caused by: Canceled")],
  ])('swallows %s without reporting it, so no failure message follows a cancel', async (_name, err) => {
    const onError = jest.fn();
    await expect(cancelTransfer('d1', jest.fn().mockRejectedValue(err), onError)).resolves.toBeUndefined();
    expect(onError).not.toHaveBeenCalled();
  });

  it('still reports a real failure, once, and never rejects', async () => {
    const onError = jest.fn();
    const boom = new Error('ERR_NO_CONTEXT');
    await expect(cancelTransfer('d1', jest.fn().mockRejectedValue(boom), onError)).resolves.toBeUndefined();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledWith(boom);
  });

  it('asks the engine to cancel the transfer it was given', async () => {
    const cancel = jest.fn().mockResolvedValue(undefined);
    await cancelTransfer('d9', cancel, jest.fn());
    expect(cancel).toHaveBeenCalledWith('d9');
  });
});
