import { log } from '../../core/log';
import { checkForUpdate } from './updateService';
import { sessionUpdateCheck } from './useUpdate';

jest.mock('./updateService', () => ({
  checkForUpdate: jest.fn(),
  downloadUpdate: jest.fn(),
  isApkReady: jest.fn(),
}));
jest.mock('../../core/native', () => ({ networkTransports: jest.fn() }));

const SIGNATURE_ERROR = 'The update manifest is not signed by the project';

describe('sessionUpdateCheck', () => {
  it('reports no update and logs the cause when the check fails', async () => {
    const warn = jest.spyOn(log, 'warn').mockImplementation(() => undefined);
    (checkForUpdate as jest.Mock).mockRejectedValue(new Error(SIGNATURE_ERROR));
    await expect(sessionUpdateCheck()).resolves.toBeNull();
    expect(warn).toHaveBeenCalledWith('update check', expect.objectContaining({ message: SIGNATURE_ERROR }));
  });
});
